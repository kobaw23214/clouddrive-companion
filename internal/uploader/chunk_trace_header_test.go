package uploader

import (
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestAttachTraceHeaders 验证出站请求的链路透传头注入逻辑。
func TestAttachTraceHeaders(t *testing.T) {
	// 未初始化 tracer / 空 spanId → 不加头
	tracer.Shutdown()
	req, _ := http.NewRequest("GET", "http://example.com/x", nil)
	attachTraceHeaders(req, "")
	if req.Header.Get("X-Trace-Id") != "" || req.Header.Get("X-Parent-Span-Id") != "" {
		t.Error("空 spanId 不应注入任何链路透传头")
	}

	// 初始化 tracer 并创建一个活跃 span
	old := config.AppConfig.TraceEnabled
	oldMap := config.AppConfig.TraceModuleEnabled
	config.AppConfig.TraceEnabled = true
	config.AppConfig.TraceModuleEnabled = map[string]bool{}
	if err := tracer.Init(t.TempDir()); err != nil {
		t.Fatalf("tracer.Init: %v", err)
	}
	t.Cleanup(func() {
		tracer.Shutdown()
		config.AppConfig.TraceEnabled = old
		config.AppConfig.TraceModuleEnabled = oldMap
	})

	traceId, runSeq := tracer.NewTraceId("task-hdr-1")
	spanId := tracer.Start(traceId, "task-hdr-1", runSeq, tracer.TraceTypeUpload, tracer.StageUploadTask, "")
	if spanId == "" {
		t.Fatal("tracer.Start returned empty spanId")
	}

	req2, _ := http.NewRequest("POST", "http://example.com/upload", nil)
	attachTraceHeaders(req2, spanId)
	if got := req2.Header.Get("X-Trace-Id"); got != traceId {
		t.Errorf("X-Trace-Id = %q, want %q", got, traceId)
	}
	if got := req2.Header.Get("X-Parent-Span-Id"); got != spanId {
		t.Errorf("X-Parent-Span-Id = %q, want %q", got, spanId)
	}

	// span 结束后不再注入
	tracer.End(spanId)
	req3, _ := http.NewRequest("POST", "http://example.com/upload", nil)
	attachTraceHeaders(req3, spanId)
	if req3.Header.Get("X-Trace-Id") != "" {
		t.Error("span 已结束，不应再注入 X-Trace-Id")
	}
}

// headerCapture 记录 check / chunk 请求携带的链路透传头。
type headerCapture struct {
	mu           sync.Mutex
	checkHeaders []http.Header
	chunkHeaders []http.Header
}

// TestChunkUpload_SendsTraceHeaders 端到端验证：真实跑一遍上传流程，
// check 与 chunk 请求都应带上 X-Trace-Id / X-Parent-Span-Id，且与链路数据一致。
func TestChunkUpload_SendsTraceHeaders(t *testing.T) {
	fb := startFakeBackend(t)
	store, manifestDir := installTestEnv(t, fb)

	// 换成带头捕获的后端（复用 failure 测试的替换手法）
	fb.server.Close()
	hc := &headerCapture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/dataImport/upload/check", func(w http.ResponseWriter, r *http.Request) {
		hc.mu.Lock()
		hc.checkHeaders = append(hc.checkHeaders, r.Header.Clone())
		hc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]int64{})
	})
	mux.HandleFunc("/dataImport/upload/chunk", func(w http.ResponseWriter, r *http.Request) {
		hc.mu.Lock()
		hc.chunkHeaders = append(hc.chunkHeaders, r.Header.Clone())
		hc.mu.Unlock()
		_ = r.ParseMultipartForm(32 << 20)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/dataImport/upload/complete", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/dataImport/upload/command/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"command": ""})
	})
	capturingServer := httptest.NewServer(mux)
	t.Cleanup(capturingServer.Close)
	config.AppConfig.BackendUploadURL = capturingServer.URL + "/dataImport/upload/chunk"
	config.AppConfig.BackendCheckURL = capturingServer.URL + "/dataImport/upload/check"
	config.AppConfig.BackendCompleteURL = capturingServer.URL + "/dataImport/upload/complete"
	config.AppConfig.BackendCommandURL = capturingServer.URL + "/dataImport/upload/command"

	files := makeTempFiles(t, 1, 100) // 1 文件 1 chunk
	taskInfo := TaskInfo{TaskId: "test-trace-hdr", TaskName: "hdr-task"}
	progresses := []FileProgress{{
		FileName: files[0].Metadata.FileName, RelativePath: files[0].RelativePath,
		TotalSize: files[0].FileSize, Status: "QUEUED",
	}}
	Stats = &UploadStats{}
	Stats.SetTotalFiles(1)
	Stats.SetStartTime(time.Now())

	mgr := NewManager(taskInfo, files, 100, manifestDir, progresses)
	if !mgr.Start() {
		t.Fatal("Start returned false")
	}
	time.Sleep(700 * time.Millisecond) // 等 tracer 后台 flush

	// 取出本次任务的 traceId 与全部 spanId
	summaries, total, err := store.ListTraces(tracer.ListTracesQuery{})
	if err != nil || total != 1 {
		t.Fatalf("expected 1 trace, total=%d err=%v", total, err)
	}
	traceId := summaries[0].TraceID
	allSpans, _, err := store.GetAllSpans(traceId, 100)
	if err != nil {
		t.Fatalf("GetAllSpans: %v", err)
	}
	spanIds := map[string]bool{}
	for _, sp := range allSpans {
		spanIds[sp.SpanID] = true
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()
	if len(hc.checkHeaders) == 0 {
		t.Fatal("check 端点未收到请求")
	}
	if len(hc.chunkHeaders) == 0 {
		t.Fatal("chunk 端点未收到请求")
	}

	assertHeaders := func(name string, h http.Header) {
		t.Helper()
		if got := h.Get("X-Trace-Id"); got != traceId {
			t.Errorf("%s: X-Trace-Id = %q, want %q", name, got, traceId)
		}
		parentSpanId := h.Get("X-Parent-Span-Id")
		if parentSpanId == "" {
			t.Errorf("%s: 缺少 X-Parent-Span-Id", name)
		} else if !spanIds[parentSpanId] {
			t.Errorf("%s: X-Parent-Span-Id %q 不在本 trace 的 span 集合里", name, parentSpanId)
		}
	}
	for i, h := range hc.checkHeaders {
		assertHeaders("check#"+intToStr(i), h)
	}
	for i, h := range hc.chunkHeaders {
		assertHeaders("chunk#"+intToStr(i), h)
	}
}
