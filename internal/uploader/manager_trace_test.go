package uploader

import (
	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/utils"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// 本文件验证 manager + chunk 上传链路的 tracer 埋点是否正确产出
// upload_task / upload_file / md5_calc / check_chunks / chunk_upload / notify_complete
// 6 类 span。通过本地 httptest 模拟后端，避免依赖真实服务。

// fakeBackend 模拟后端 4 个端点：upload chunk / check_chunks / complete / command。
type fakeBackend struct {
	server         *httptest.Server
	notifyCalled   atomic.Int32
	checkCallCount atomic.Int32
	chunkCallCount atomic.Int32
}

func startFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	fb := &fakeBackend{}

	mux := http.NewServeMux()
	// /dataImport/upload/check
	mux.HandleFunc("/dataImport/upload/check", func(w http.ResponseWriter, r *http.Request) {
		fb.checkCallCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// 默认返回空数组（没有已上传分片）
		_ = json.NewEncoder(w).Encode([]int64{})
	})
	// /dataImport/upload/chunk —— 接受任意 multipart，返回 200
	mux.HandleFunc("/dataImport/upload/chunk", func(w http.ResponseWriter, r *http.Request) {
		fb.chunkCallCount.Add(1)
		_ = r.ParseMultipartForm(32 << 20)
		w.WriteHeader(http.StatusOK)
	})
	// /dataImport/upload/complete
	mux.HandleFunc("/dataImport/upload/complete", func(w http.ResponseWriter, r *http.Request) {
		fb.notifyCalled.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	// /dataImport/upload/command/{taskId} —— PollCommands 用，返回空命令
	mux.HandleFunc("/dataImport/upload/command/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"command": ""})
	})

	fb.server = httptest.NewServer(mux)
	t.Cleanup(fb.server.Close)
	return fb
}

// installTestEnv 配置 tracer + config + api client 指向 httptest，方便独立测试。
func installTestEnv(t *testing.T, fb *fakeBackend) (storeRef *tracer.Store, manifestDir string) {
	t.Helper()

	yes := true
	no := false
	zero := 0
	old := config.AppConfig
	config.AppConfig = config.Config{
		ChunkSize:              1024, // 1 KB / chunk，方便制造多分片
		BackendUploadURL:       fb.server.URL + "/dataImport/upload/chunk",
		BackendCompleteURL:     fb.server.URL + "/dataImport/upload/complete",
		BackendCommandURL:      fb.server.URL + "/dataImport/upload/command",
		BackendCheckURL:        fb.server.URL + "/dataImport/upload/check",
		MaxWorkers:             2,
		MaxRetries:             1,
		HTTPTimeout:            5,
		MergeTimeoutMultiplier: 2,
		MaxFilesInBatch:        100,
		SendRelativePath:       &yes,
		SendFileSize:           &no,
		EnableMD5Cache:         false,
		TraceEnabled:           true,
		TraceModuleEnabled:     map[string]bool{},
		TraceSamplingThreshold: &zero, // 测试时关闭采样
		TraceMaxAgeDays:        &zero,
		TraceMaxRows:           &zero,
		TraceVacuumThresholdMB: &zero,
	}
	t.Cleanup(func() { config.AppConfig = old })

	api.InitClient()

	tracer.Shutdown()
	traceDir := t.TempDir()
	if err := tracer.Init(traceDir); err != nil {
		t.Fatalf("tracer.Init failed: %v", err)
	}
	t.Cleanup(func() { tracer.Shutdown() })

	manifestDir = t.TempDir()
	return tracer.StoreRef(), manifestDir
}

// makeTempFiles 在临时目录创建 n 个 1KB+1B（即 2 个 chunk）的文件并返回 FullFileInfo 列表。
func makeTempFiles(t *testing.T, count int, sizeBytes int) []utils.FullFileInfo {
	t.Helper()
	dir := t.TempDir()
	out := make([]utils.FullFileInfo, 0, count)
	data := make([]byte, sizeBytes)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	for i := 0; i < count; i++ {
		name := filepath.Base(t.Name()) + "_" + filepath.Base(dir)
		_ = name
		path := filepath.Join(dir, "f"+intToStr(i)+".bin")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		out = append(out, utils.FullFileInfo{
			Metadata: utils.FileMetadata{
				Index:    i,
				FileName: filepath.Base(path),
			},
			AbsolutePath: filepath.ToSlash(path),
			RelativePath: filepath.Base(path),
			FileSize:     int64(sizeBytes),
		})
	}
	return out
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestManagerStart_HappyPath_EmitsFullTraceTree(t *testing.T) {
	fb := startFakeBackend(t)
	store, manifestDir := installTestEnv(t, fb)

	// 2 个文件 × 2049 字节 = 每文件 3 个 chunk（chunkSize=1024）
	files := makeTempFiles(t, 2, 2049)

	taskInfo := TaskInfo{
		TaskId:   "test-happy",
		TaskName: "happy-task",
		Dir:      "/some/dir",
	}
	progresses := make([]FileProgress, len(files))
	for i, f := range files {
		progresses[i] = FileProgress{
			FileName:     f.Metadata.FileName,
			RelativePath: f.RelativePath,
			TotalSize:    f.FileSize,
			Status:       "QUEUED",
		}
	}

	// Stats 必填，monitor 用
	Stats = &UploadStats{}
	Stats.SetTotalFiles(int64(len(files)))
	Stats.SetTotalBytes(int64(len(files)) * 2049)
	Stats.SetStartTime(time.Now())

	mgr := NewManager(taskInfo, files, int64(len(files))*2049, manifestDir, progresses)
	ok := mgr.Start()
	if !ok {
		t.Fatalf("Start returned false (failed=%d)", mgr.failed)
	}
	if fb.notifyCalled.Load() != 1 {
		t.Errorf("expected 1 NotifyBackendTaskComplete call, got %d", fb.notifyCalled.Load())
	}

	// 等待 tracer 后台 flush
	time.Sleep(700 * time.Millisecond)

	// 校验 trace 摘要
	summaries, total, err := store.ListTraces(tracer.ListTracesQuery{})
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if total != 1 || len(summaries) != 1 {
		t.Fatalf("expected exactly 1 upload trace, got total=%d", total)
	}
	sum := summaries[0]
	if sum.TraceType != tracer.TraceTypeUpload {
		t.Errorf("expected upload type, got %s", sum.TraceType)
	}
	if sum.HasFailure {
		t.Error("happy path should not have failure")
	}
	if sum.TaskID != "test-happy" {
		t.Errorf("taskId mismatch: %s", sum.TaskID)
	}
	if sum.RunSeq != 1 {
		t.Errorf("runSeq expected 1, got %d", sum.RunSeq)
	}

	// 校验 span 树：upload_task 下应有 2 × upload_file + 1 × notify_complete = 3 直接子
	traceId := sum.TraceID
	tops, _ := store.GetTopSpans(traceId)
	if len(tops) != 1 || tops[0].Stage != tracer.StageUploadTask {
		t.Fatalf("expected single upload_task top span, got %+v", tops)
	}
	taskSpan := tops[0]
	taskKids, _ := store.GetChildren(taskSpan.SpanID)
	if len(taskKids) != 3 {
		t.Errorf("upload_task should have 3 direct children (2 file + 1 notify), got %d", len(taskKids))
	}

	// 校验每个 upload_file 有 md5 + check_chunks + 3 chunk + server_merge = 6 子 span
	uploadFileCount := 0
	notifyCount := 0
	for _, k := range taskKids {
		switch k.Stage {
		case tracer.StageUploadFile:
			uploadFileCount++
			grandKids, _ := store.GetChildren(k.SpanID)
			if len(grandKids) != 6 {
				t.Errorf("upload_file (%s) should have 6 children (1 md5 + 1 check + 3 chunk + 1 server_merge), got %d",
					k.Filename, len(grandKids))
			}
			// 子 span 类别统计
			stages := map[string]int{}
			for _, gk := range grandKids {
				stages[gk.Stage]++
			}
			if stages[tracer.StageMD5Calc] != 1 {
				t.Errorf("expected 1 md5_calc span, got %d", stages[tracer.StageMD5Calc])
			}
			if stages[tracer.StageCheckChunks] != 1 {
				t.Errorf("expected 1 check_chunks span, got %d", stages[tracer.StageCheckChunks])
			}
			if stages[tracer.StageChunkUpload] != 3 {
				t.Errorf("expected 3 chunk_upload spans, got %d", stages[tracer.StageChunkUpload])
			}
			if stages[tracer.StageServerMerge] != 1 {
				t.Errorf("expected 1 server_merge span (sibling of chunks under upload_file), got %d", stages[tracer.StageServerMerge])
			}
		case tracer.StageNotifyComplete:
			notifyCount++
			if !k.Success {
				t.Error("notify_complete should be success")
			}
		}
	}
	if uploadFileCount != 2 {
		t.Errorf("expected 2 upload_file children, got %d", uploadFileCount)
	}
	if notifyCount != 1 {
		t.Errorf("expected 1 notify_complete child, got %d", notifyCount)
	}

	// 总 span 数 = upload_task + 2×(upload_file + 6 children) + notify_complete = 1 + 14 + 1 = 16
	all, truncated, _ := store.GetAllSpans(traceId, 100)
	if truncated {
		t.Error("unexpected truncation")
	}
	if len(all) != 16 {
		t.Errorf("expected 16 total spans, got %d", len(all))
	}
}

func TestManagerStart_UploadFailure_MarksTaskFail(t *testing.T) {
	fb := startFakeBackend(t)
	store, manifestDir := installTestEnv(t, fb)

	// 把 chunk 路由换成始终失败，让上传必败
	fb.server.Close()
	failingMux := http.NewServeMux()
	failingMux.HandleFunc("/dataImport/upload/check", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]int64{})
	})
	failingMux.HandleFunc("/dataImport/upload/chunk", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	failingMux.HandleFunc("/dataImport/upload/complete", func(w http.ResponseWriter, r *http.Request) {
		fb.notifyCalled.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	failingMux.HandleFunc("/dataImport/upload/command/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"command": ""})
	})
	failingServer := httptest.NewServer(failingMux)
	t.Cleanup(failingServer.Close)
	config.AppConfig.BackendUploadURL = failingServer.URL + "/dataImport/upload/chunk"
	config.AppConfig.BackendCheckURL = failingServer.URL + "/dataImport/upload/check"
	config.AppConfig.BackendCompleteURL = failingServer.URL + "/dataImport/upload/complete"
	config.AppConfig.BackendCommandURL = failingServer.URL + "/dataImport/upload/command"

	files := makeTempFiles(t, 1, 100) // 1 文件 1 chunk
	taskInfo := TaskInfo{TaskId: "test-fail", TaskName: "fail-task"}
	progresses := []FileProgress{{
		FileName: files[0].Metadata.FileName, RelativePath: files[0].RelativePath,
		TotalSize: files[0].FileSize, Status: "QUEUED",
	}}

	Stats = &UploadStats{}
	Stats.SetTotalFiles(1)
	Stats.SetStartTime(time.Now())

	mgr := NewManager(taskInfo, files, 100, manifestDir, progresses)
	ok := mgr.Start()
	if ok {
		t.Error("Start should return false on upload failure")
	}
	if fb.notifyCalled.Load() != 0 {
		t.Errorf("notify should NOT be called on failure, got %d", fb.notifyCalled.Load())
	}

	time.Sleep(700 * time.Millisecond)

	summaries, _, _ := store.ListTraces(tracer.ListTracesQuery{})
	if len(summaries) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(summaries))
	}
	if !summaries[0].HasFailure {
		t.Error("expected hasFailure=true")
	}

	tops, _ := store.GetTopSpans(summaries[0].TraceID)
	if len(tops) != 1 {
		t.Fatalf("expected 1 top span")
	}
	if tops[0].Success {
		t.Error("upload_task should be marked failed")
	}
	if tops[0].ErrorMessage == "" {
		t.Error("error message should be populated")
	}
}

func TestEstimateSpanCount_VariousScenarios(t *testing.T) {
	old := config.AppConfig.ChunkSize
	t.Cleanup(func() { config.AppConfig.ChunkSize = old })
	config.AppConfig.ChunkSize = 1024

	cases := []struct {
		name     string
		files    []utils.FullFileInfo
		expected int
	}{
		{"empty", []utils.FullFileInfo{}, 2},                                     // 仅 task + notify
		{"single 0-byte file", []utils.FullFileInfo{{FileSize: 0}}, 2 + 3 + 1},   // task + notify + (file + md5 + check + 1 chunk)
		{"single 1KB file", []utils.FullFileInfo{{FileSize: 1024}}, 2 + 3 + 1},
		{"single 2049B file", []utils.FullFileInfo{{FileSize: 2049}}, 2 + 3 + 3},
		{"two 1MB files", []utils.FullFileInfo{
			{FileSize: 1024 * 1024}, {FileSize: 1024 * 1024},
		}, 2 + 2*(3+1024)},
	}
	for _, c := range cases {
		got := estimateSpanCount(c.files)
		if got != c.expected {
			t.Errorf("%s: expected %d, got %d", c.name, c.expected, got)
		}
	}
}
