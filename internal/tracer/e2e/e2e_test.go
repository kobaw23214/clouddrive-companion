package e2e_test

import (
	"clouddrive-companion-withWails2/internal/api"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/uploader"
	"clouddrive-companion-withWails2/internal/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 检查点 ② —— 后端整体可用
//
// 这个文件起一个真实的 HTTP server（挂 tracer 的 handlers），同时起一个 fake 后端
// 模拟分片上传/通知后端。然后通过真实的 uploader.Manager 跑完整任务，验证：
//
//   1. /task-traces/list 能看到 scan + upload 两条 trace
//   2. /task-traces/{traceId}/all 能看到完整的 span 树
//   3. PATCH 全局开关 → 新任务零 span；恢复后重新采集
//   4. PATCH 采样阈值 → chunk_upload span 数量符合采样预期
//   5. 修改配置 → 重启后配置保留（持久化到 config.json）
//
// 本包独立于 tracer 包，避免 uploader → tracer 的 import cycle。

// e2eEnv 装载一次完整的 tracer + fake 后端 + handler server。
type e2eEnv struct {
	t              *testing.T
	traceServer    *httptest.Server
	backend        *httptest.Server
	manifestDir    string
	configFilePath string

	notifyCalled atomic.Int32
	chunkCalled  atomic.Int32
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()

	env := &e2eEnv{t: t}

	// fake 后端
	mux := http.NewServeMux()
	mux.HandleFunc("/dataImport/upload/check", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]int64{})
	})
	mux.HandleFunc("/dataImport/upload/chunk", func(w http.ResponseWriter, r *http.Request) {
		env.chunkCalled.Add(1)
		_ = r.ParseMultipartForm(32 << 20)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/dataImport/upload/complete", func(w http.ResponseWriter, r *http.Request) {
		env.notifyCalled.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/dataImport/upload/command/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"command": ""})
	})
	env.backend = httptest.NewServer(mux)
	t.Cleanup(env.backend.Close)

	// config + AppSupportDir
	yes := true
	no := false
	zero := 0
	oldCfg := config.AppConfig
	oldDir := config.AppSupportDir
	tmpDir := t.TempDir()
	config.AppSupportDir = tmpDir
	env.configFilePath = filepath.Join(tmpDir, "config.json")
	config.AppConfig = config.Config{
		ChunkSize:              1024,
		BackendUploadURL:       env.backend.URL + "/dataImport/upload/chunk",
		BackendCompleteURL:     env.backend.URL + "/dataImport/upload/complete",
		BackendCommandURL:      env.backend.URL + "/dataImport/upload/command",
		BackendCheckURL:        env.backend.URL + "/dataImport/upload/check",
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
		TraceSamplingThreshold: &zero,
		TraceMaxAgeDays:        &zero,
		TraceMaxRows:           &zero,
		TraceVacuumThresholdMB: &zero,
	}
	t.Cleanup(func() {
		config.AppConfig = oldCfg
		config.AppSupportDir = oldDir
	})

	api.InitClient()

	// tracer
	tracer.Shutdown()
	if err := tracer.Init(filepath.Join(tmpDir, "task_trace")); err != nil {
		t.Fatalf("tracer.Init: %v", err)
	}
	t.Cleanup(tracer.Shutdown)
	env.manifestDir = t.TempDir()

	// /task-traces/* handler server
	traceMux := http.NewServeMux()
	traceMux.HandleFunc("/task-traces/viewer", tracer.HandleViewerPage)
	traceMux.HandleFunc("/task-traces/list", tracer.HandleListTraces)
	traceMux.HandleFunc("/task-traces/modules", tracer.HandleListModules)
	traceMux.HandleFunc("/task-traces/modules/", tracer.HandleToggleModule)
	traceMux.HandleFunc("/task-traces/global", tracer.HandleToggleGlobal)
	traceMux.HandleFunc("/task-traces/config", tracer.HandleConfig)
	traceMux.HandleFunc("/task-traces/cleanup", tracer.HandleCleanup)
	traceMux.HandleFunc("/task-traces/span/", tracer.HandleSpanRoutes)
	traceMux.HandleFunc("/task-traces/", tracer.HandleTraceRoutes)
	env.traceServer = httptest.NewServer(traceMux)
	t.Cleanup(env.traceServer.Close)

	return env
}

func (env *e2eEnv) runOneUpload(taskId string, fileCount, fileSizeBytes int) {
	env.t.Helper()
	dir := env.t.TempDir()
	for i := 0; i < fileCount; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file_%d.bin", i))
		data := make([]byte, fileSizeBytes)
		for j := range data {
			data[j] = byte('A' + (j % 26))
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			env.t.Fatalf("write file: %v", err)
		}
	}

	files, _, _, _, err := utils.GenerateFileListWithMetadata(dir, nil)
	if err != nil {
		env.t.Fatalf("scan failed: %v", err)
	}

	progresses := make([]uploader.FileProgress, len(files))
	for i, f := range files {
		progresses[i] = uploader.FileProgress{
			FileName:     f.Metadata.FileName,
			RelativePath: f.RelativePath,
			TotalSize:    f.FileSize,
			Status:       "QUEUED",
		}
	}
	uploader.Stats = &uploader.UploadStats{}
	uploader.Stats.SetTotalFiles(int64(len(files)))
	uploader.Stats.SetStartTime(time.Now())

	mgr := uploader.NewManager(
		uploader.TaskInfo{TaskId: taskId, TaskName: taskId, Dir: dir},
		files, int64(len(files)*fileSizeBytes), env.manifestDir, progresses,
	)
	mgr.Start()
}

func (env *e2eEnv) httpJSON(method, path, body string, v interface{}) int {
	env.t.Helper()
	url := env.traceServer.URL + path
	var req *http.Request
	var err error
	if body == "" {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		env.t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		env.t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if v != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, v); err != nil {
			env.t.Fatalf("decode %s body=%s: %v", path, raw, err)
		}
	}
	return resp.StatusCode
}

func (env *e2eEnv) waitForFlush() {
	time.Sleep(700 * time.Millisecond)
}

// 检查项 1：/list 看到 scan + upload 两条 trace

func TestE2E_ListShowsScanAndUploadTraces(t *testing.T) {
	env := setupE2E(t)
	env.runOneUpload("e2e-task-1", 2, 1024)
	env.waitForFlush()

	var resp struct {
		Total int                    `json:"total"`
		Items []tracer.TraceSummary  `json:"items"`
	}
	if got := env.httpJSON(http.MethodGet, "/task-traces/list", "", &resp); got != 200 {
		t.Fatalf("expected 200, got %d", got)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 traces (scan + upload), got %d", resp.Total)
	}
	traceTypes := map[string]bool{}
	for _, item := range resp.Items {
		traceTypes[item.TraceType] = true
	}
	if !traceTypes[tracer.TraceTypeScan] || !traceTypes[tracer.TraceTypeUpload] {
		t.Errorf("missing trace types: %v", traceTypes)
	}
}

// 检查项 2：/{traceId}/all 看到完整 span 树

func TestE2E_AllSpansReturnsCompleteTree(t *testing.T) {
	env := setupE2E(t)
	env.runOneUpload("e2e-task-2", 1, 100)
	env.waitForFlush()

	var listResp struct {
		Items []tracer.TraceSummary `json:"items"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/list?traceType=upload", "", &listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 upload trace, got %d", len(listResp.Items))
	}
	traceId := listResp.Items[0].TraceID

	var allResp struct {
		Spans     []tracer.Span `json:"spans"`
		Truncated bool          `json:"truncated"`
	}
	if got := env.httpJSON(http.MethodGet, "/task-traces/"+traceId+"/all", "", &allResp); got != 200 {
		t.Fatalf("expected 200, got %d", got)
	}
	if allResp.Truncated {
		t.Error("unexpected truncation")
	}

	stages := map[string]int{}
	for _, sp := range allResp.Spans {
		stages[sp.Stage]++
	}
	want := map[string]int{
		tracer.StageUploadTask:     1,
		tracer.StageUploadFile:     1,
		tracer.StageMD5Calc:        1,
		tracer.StageCheckChunks:    1,
		tracer.StageChunkUpload:    1,
		tracer.StageNotifyComplete: 1,
	}
	for stage, count := range want {
		if stages[stage] != count {
			t.Errorf("stage %s: expected %d, got %d (full: %v)", stage, count, stages[stage], stages)
		}
	}
}

// 检查项 3：全局开关切换立即生效

func TestE2E_GlobalDisableStopsCollection(t *testing.T) {
	env := setupE2E(t)

	if got := env.httpJSON(http.MethodPatch, "/task-traces/global?enabled=false", "", nil); got != 200 {
		t.Fatalf("expected 200 from PATCH /global, got %d", got)
	}

	env.runOneUpload("disabled-task", 1, 100)
	env.waitForFlush()

	var listResp struct {
		Total int `json:"total"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/list", "", &listResp)
	if listResp.Total != 0 {
		t.Errorf("expected 0 traces while global off, got %d", listResp.Total)
	}

	if got := env.httpJSON(http.MethodPatch, "/task-traces/global?enabled=true", "", nil); got != 200 {
		t.Fatalf("expected 200 from re-enable, got %d", got)
	}

	env.runOneUpload("re-enabled-task", 1, 100)
	env.waitForFlush()

	env.httpJSON(http.MethodGet, "/task-traces/list", "", &listResp)
	if listResp.Total != 2 {
		t.Errorf("expected 2 traces after re-enable, got %d", listResp.Total)
	}
}

// 检查项 4：采样阈值 PATCH 后立即生效

func TestE2E_SamplingThresholdAffectsChunkSpans(t *testing.T) {
	env := setupE2E(t)

	// 阈值降到 5：1 文件 8 chunks，estimated = 2 + 3 + 8 = 13；ceil(13/5)=3
	// 8 chunks 中按 i%3==0 命中：seq=3, 6；最后一个 chunk (seq=8) 强制采样 → 3 个 chunk_upload span
	body := `{"traceSamplingThreshold": 5}`
	if got := env.httpJSON(http.MethodPatch, "/task-traces/config", body, nil); got != 200 {
		t.Fatalf("expected 200 from PATCH /config, got %d", got)
	}

	env.runOneUpload("e2e-sampling", 1, 8*1024)
	env.waitForFlush()

	var listResp struct {
		Items []tracer.TraceSummary `json:"items"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/list?traceType=upload", "", &listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 upload trace, got %d", len(listResp.Items))
	}
	var allResp struct {
		Spans []tracer.Span `json:"spans"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/"+listResp.Items[0].TraceID+"/all", "", &allResp)

	chunkCount := 0
	for _, sp := range allResp.Spans {
		if sp.Stage == tracer.StageChunkUpload {
			chunkCount++
		}
	}
	if chunkCount != 3 {
		t.Errorf("expected 3 chunk_upload spans (sample 1/3 of 8 + forced last), got %d", chunkCount)
	}

	// 最后一个 chunk_upload 下挂 1 条 server_merge
	mergeCount := 0
	for _, sp := range allResp.Spans {
		if sp.Stage == tracer.StageServerMerge {
			mergeCount++
		}
	}
	if mergeCount != 1 {
		t.Errorf("expected 1 server_merge span, got %d", mergeCount)
	}

	stages := map[string]int{}
	for _, sp := range allResp.Spans {
		stages[sp.Stage]++
	}
	if stages[tracer.StageMD5Calc] != 1 || stages[tracer.StageCheckChunks] != 1 {
		t.Errorf("non-chunk stages should be full record: %v", stages)
	}
}

// 检查项 5：配置持久化跨"重启"保留

func TestE2E_ConfigPersistedAcrossRestart(t *testing.T) {
	env := setupE2E(t)

	body := `{"traceSamplingThreshold": 999, "traceMaxAgeDays": 21}`
	if got := env.httpJSON(http.MethodPatch, "/task-traces/config", body, nil); got != 200 {
		t.Fatalf("expected 200, got %d", got)
	}

	data, err := os.ReadFile(env.configFilePath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if saved.TraceSamplingThreshold == nil || *saved.TraceSamplingThreshold != 999 {
		t.Errorf("traceSamplingThreshold not persisted: %v", saved.TraceSamplingThreshold)
	}
	if saved.TraceMaxAgeDays == nil || *saved.TraceMaxAgeDays != 21 {
		t.Errorf("traceMaxAgeDays not persisted: %v", saved.TraceMaxAgeDays)
	}

	// 模拟重启：清空并 reload
	config.AppConfig = config.Config{}
	config.LoadConfig()

	if config.AppConfig.TraceSamplingThreshold == nil || *config.AppConfig.TraceSamplingThreshold != 999 {
		t.Errorf("after restart: TraceSamplingThreshold expected 999, got %v",
			config.AppConfig.TraceSamplingThreshold)
	}
	if config.AppConfig.TraceMaxAgeDays == nil || *config.AppConfig.TraceMaxAgeDays != 21 {
		t.Errorf("after restart: TraceMaxAgeDays expected 21, got %v",
			config.AppConfig.TraceMaxAgeDays)
	}
}

// 检查项 6：notify_complete 集成验证（成功路径触发）

func TestE2E_NotifyCompleteIntegrated(t *testing.T) {
	env := setupE2E(t)
	env.runOneUpload("e2e-notify", 1, 100)
	env.waitForFlush()

	if env.notifyCalled.Load() != 1 {
		t.Errorf("expected backend notify to be called once, got %d", env.notifyCalled.Load())
	}

	var listResp struct {
		Items []tracer.TraceSummary `json:"items"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/list?traceType=upload", "", &listResp)
	var allResp struct {
		Spans []tracer.Span `json:"spans"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/"+listResp.Items[0].TraceID+"/all", "", &allResp)

	hasNotify := false
	for _, sp := range allResp.Spans {
		if sp.Stage == tracer.StageNotifyComplete && sp.Success {
			hasNotify = true
		}
	}
	if !hasNotify {
		t.Error("notify_complete span missing or not success")
	}
}

// 检查项 7：DELETE /{traceId} 真实删除

func TestE2E_DeleteTraceRemovesAllSpans(t *testing.T) {
	env := setupE2E(t)
	env.runOneUpload("e2e-del", 1, 100)
	env.waitForFlush()

	var listResp struct {
		Items []tracer.TraceSummary `json:"items"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/list?traceType=upload", "", &listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 upload trace, got %d", len(listResp.Items))
	}
	traceId := listResp.Items[0].TraceID

	var delResp struct {
		Deleted int `json:"deleted"`
	}
	if got := env.httpJSON(http.MethodDelete, "/task-traces/"+traceId, "", &delResp); got != 200 {
		t.Fatalf("expected 200, got %d", got)
	}
	if delResp.Deleted < 1 {
		t.Errorf("expected at least 1 deleted, got %d", delResp.Deleted)
	}

	var allResp struct {
		Spans []tracer.Span `json:"spans"`
	}
	env.httpJSON(http.MethodGet, "/task-traces/"+traceId+"/all", "", &allResp)
	if len(allResp.Spans) != 0 {
		t.Errorf("after DELETE expected 0 spans, got %d", len(allResp.Spans))
	}
}


// 检查项 8：浏览器版查看器 HTML 能正常下发
//
// 该测试不渲染 HTML（不依赖 jsdom），只验证：
//   - GET /task-traces/viewer 返回 200
//   - Content-Type 是 text/html
//   - 响应体非空且包含核心标识（标题字符串）
func TestE2E_ViewerPage_Returns200HTML(t *testing.T) {
	env := setupE2E(t)

	resp, err := http.Get(env.traceServer.URL + "/task-traces/viewer")
	if err != nil {
		t.Fatalf("GET viewer failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 1024 {
		t.Errorf("viewer body too small (%d bytes), suspect embed failed", len(body))
	}
	if !strings.Contains(string(body), "任务级链路追踪") {
		t.Error("viewer body missing expected title")
	}
}

