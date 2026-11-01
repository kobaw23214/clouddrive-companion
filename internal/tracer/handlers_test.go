package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withHandlerEnv 给 handler 测试准备一份独立的 globalTracer + config + persistFn stub。
func withHandlerEnv(t *testing.T) *Store {
	t.Helper()
	old := globalTracer
	oldGlobal := config.AppConfig.TraceEnabled
	oldMap := config.AppConfig.TraceModuleEnabled
	oldThresh := config.AppConfig.TraceSamplingThreshold
	oldAge := config.AppConfig.TraceMaxAgeDays
	oldRows := config.AppConfig.TraceMaxRows
	oldVac := config.AppConfig.TraceVacuumThresholdMB
	oldDir := config.AppSupportDir
	oldPersist := persistFn

	tmpDir := t.TempDir()
	config.AppSupportDir = tmpDir
	config.AppConfig.TraceEnabled = true
	config.AppConfig.TraceModuleEnabled = map[string]bool{}
	zero := 0
	config.AppConfig.TraceSamplingThreshold = &zero
	config.AppConfig.TraceMaxAgeDays = &zero
	config.AppConfig.TraceMaxRows = &zero
	config.AppConfig.TraceVacuumThresholdMB = &zero
	persistFn = func() error { return nil } // 测试不实际落盘 config.json

	globalTracer = nil
	if err := Init(t.TempDir()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	t.Cleanup(func() {
		Shutdown()
		globalTracer = old
		config.AppConfig.TraceEnabled = oldGlobal
		config.AppConfig.TraceModuleEnabled = oldMap
		config.AppConfig.TraceSamplingThreshold = oldThresh
		config.AppConfig.TraceMaxAgeDays = oldAge
		config.AppConfig.TraceMaxRows = oldRows
		config.AppConfig.TraceVacuumThresholdMB = oldVac
		config.AppSupportDir = oldDir
		persistFn = oldPersist
	})
	return globalTracer.store
}

func doReq(method, target string, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	return w_route(method, target, r, w)
}

// w_route 模拟 server.StartLocalServer 中的注册顺序，把请求路由到正确的 handler。
func w_route(method, target string, r *http.Request, w *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	path := r.URL.Path
	switch {
	case path == "/task-traces/list":
		HandleListTraces(w, r)
	case path == "/task-traces/modules":
		HandleListModules(w, r)
	case strings.HasPrefix(path, "/task-traces/modules/"):
		HandleToggleModule(w, r)
	case path == "/task-traces/global":
		HandleToggleGlobal(w, r)
	case path == "/task-traces/config":
		HandleConfig(w, r)
	case path == "/task-traces/cleanup":
		HandleCleanup(w, r)
	case strings.HasPrefix(path, "/task-traces/span/"):
		HandleSpanRoutes(w, r)
	case strings.HasPrefix(path, "/task-traces/"):
		HandleTraceRoutes(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	return w
}

// ─── List modules / toggle ─────────────────────────────────

func TestHandleListModules_OK(t *testing.T) {
	withHandlerEnv(t)

	w := doReq(http.MethodGet, "/task-traces/modules", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Global  bool          `json:"global"`
		Modules []TraceModule `json:"modules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Global {
		t.Error("expected global=true")
	}
	if len(resp.Modules) < 2 {
		t.Errorf("expected at least 2 modules, got %d", len(resp.Modules))
	}
}

func TestHandleListModules_MethodNotAllowed(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPost, "/task-traces/modules", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET" {
		t.Errorf("expected Allow=GET, got %q", got)
	}
}

func TestHandleToggleModule_OK(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/modules/upload?enabled=false", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if config.AppConfig.TraceModuleEnabled["upload"] {
		t.Error("upload module should be disabled in config")
	}
}

func TestHandleToggleModule_NotFound(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/modules/nonexistent?enabled=true", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleToggleModule_MissingCode(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/modules/?enabled=true", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 missing code, got %d", w.Code)
	}
}

func TestHandleToggleModule_MissingEnabledParam(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/modules/upload", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing 'enabled', got %d", w.Code)
	}
}

func TestHandleToggleModule_InvalidEnabledParam(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/modules/upload?enabled=maybe", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid 'enabled', got %d", w.Code)
	}
}

func TestHandleToggleGlobal_OK(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/global?enabled=false", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if config.AppConfig.TraceEnabled {
		t.Error("global flag should be flipped to false")
	}
}

func TestHandleToggleGlobal_PersistFailure_Returns500(t *testing.T) {
	withHandlerEnv(t)
	persistFn = func() error { return fmtError("disk full") }

	w := doReq(http.MethodPatch, "/task-traces/global?enabled=false", "")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on persist failure, got %d", w.Code)
	}
}

// fmtError 简单封装，避免在 handlers_test 引入额外 import
type fmtError string

func (e fmtError) Error() string { return string(e) }

// ─── Config GET / PATCH ────────────────────────────────────

func TestHandleConfig_GET(t *testing.T) {
	withHandlerEnv(t)
	// 设置一些非零值
	v := 42
	config.AppConfig.TraceSamplingThreshold = &v

	w := doReq(http.MethodGet, "/task-traces/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var view traceConfigView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.SamplingThreshold != 42 {
		t.Errorf("expected SamplingThreshold=42, got %d", view.SamplingThreshold)
	}
}

func TestHandleConfig_PATCH_PartialUpdate(t *testing.T) {
	withHandlerEnv(t)
	body := `{"traceSamplingThreshold": 100, "traceMaxAgeDays": 14}`
	w := doReq(http.MethodPatch, "/task-traces/config", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if v := currentSamplingThreshold(); v != 100 {
		t.Errorf("SamplingThreshold expected 100, got %d", v)
	}
	if v := currentMaxAgeDays(); v != 14 {
		t.Errorf("MaxAgeDays expected 14, got %d", v)
	}
	// 未传字段应保持默认（0 在测试环境）
	if v := currentMaxRows(); v != 0 {
		t.Errorf("MaxRows should remain 0, got %d", v)
	}
}

func TestHandleConfig_PATCH_NegativeRejected(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/config", `{"traceMaxAgeDays": -3}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative value, got %d", w.Code)
	}
	// 未发生修改
	if v := currentMaxAgeDays(); v != 0 {
		t.Errorf("MaxAgeDays should remain 0 after rejected patch, got %d", v)
	}
}

func TestHandleConfig_PATCH_InvalidJSON(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/config", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleConfig_PATCH_UnknownField(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodPatch, "/task-traces/config", `{"unknownKey": 7}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown field, got %d", w.Code)
	}
}

func TestHandleConfig_PATCH_PersistFailure(t *testing.T) {
	withHandlerEnv(t)
	persistFn = func() error { return fmtError("io") }

	w := doReq(http.MethodPatch, "/task-traces/config", `{"traceMaxAgeDays": 7}`)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on persist failure, got %d", w.Code)
	}
}

// ─── List traces ────────────────────────────────────────────

// seedHandlerData 通过真实 tracer API 写入若干 trace，避免直接 writeBatch 绕过 channel。
func seedHandlerData(t *testing.T, count int) []string {
	t.Helper()
	traceIds := make([]string, 0, count)
	for i := 0; i < count; i++ {
		taskId := fmt.Sprintf("task-%d", i)
		tid, _ := NewTraceId(taskId)
		traceIds = append(traceIds, tid)
		root := Start(tid, taskId, 1, TraceTypeUpload, StageUploadTask, "")
		End(root)
	}
	// 等待 batch flush
	time.Sleep(700 * time.Millisecond)
	return traceIds
}

func TestHandleListTraces_BasicPagination(t *testing.T) {
	withHandlerEnv(t)
	seedHandlerData(t, 5)

	w := doReq(http.MethodGet, "/task-traces/list?page=1&size=2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Total int            `json:"total"`
		Page  int            `json:"page"`
		Size  int            `json:"size"`
		Items []TraceSummary `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 5 {
		t.Errorf("expected total=5, got %d", resp.Total)
	}
	if resp.Page != 1 || resp.Size != 2 {
		t.Errorf("unexpected page/size: page=%d size=%d", resp.Page, resp.Size)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Items))
	}
}

func TestHandleListTraces_PageZeroRegularizedTo1(t *testing.T) {
	withHandlerEnv(t)
	seedHandlerData(t, 3)

	w := doReq(http.MethodGet, "/task-traces/list?page=0&size=10", "")
	var resp struct {
		Page int `json:"page"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Page != 1 {
		t.Errorf("page=0 should regularize to 1, got %d", resp.Page)
	}
}

func TestHandleListTraces_SizeClampedTo200(t *testing.T) {
	withHandlerEnv(t)
	seedHandlerData(t, 2)

	w := doReq(http.MethodGet, "/task-traces/list?size=999", "")
	var resp struct {
		Size int `json:"size"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Size != 200 {
		t.Errorf("size=999 should clamp to 200, got %d", resp.Size)
	}
}

func TestHandleListTraces_FilterByTaskId(t *testing.T) {
	withHandlerEnv(t)
	seedHandlerData(t, 5)

	w := doReq(http.MethodGet, "/task-traces/list?taskId=task-2", "")
	var resp struct {
		Items []TraceSummary `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 trace for task-2, got %d", len(resp.Items))
	}
	if resp.Items[0].TaskID != "task-2" {
		t.Errorf("filter mismatch: %s", resp.Items[0].TaskID)
	}
}

// ─── Trace routes (top-spans / all / DELETE) ────────────────

func TestHandleTraceRoutes_TopSpans(t *testing.T) {
	withHandlerEnv(t)
	traceIds := seedHandlerData(t, 1)

	w := doReq(http.MethodGet, "/task-traces/"+traceIds[0]+"/top-spans", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var spans []Span
	if err := json.Unmarshal(w.Body.Bytes(), &spans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(spans) != 1 {
		t.Errorf("expected 1 top span, got %d", len(spans))
	}
}

func TestHandleTraceRoutes_All(t *testing.T) {
	withHandlerEnv(t)
	tid, _ := NewTraceId("task-all")
	root := Start(tid, "task-all", 1, TraceTypeUpload, StageUploadTask, "")
	for i := 0; i < 3; i++ {
		c := StartChild(root, StageUploadFile, fmt.Sprintf("f%d", i))
		End(c)
	}
	End(root)
	time.Sleep(700 * time.Millisecond)

	w := doReq(http.MethodGet, "/task-traces/"+tid+"/all", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Spans     []Span `json:"spans"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Spans) != 4 {
		t.Errorf("expected 4 spans, got %d", len(resp.Spans))
	}
	if resp.Truncated {
		t.Error("expected truncated=false")
	}
}

func TestHandleTraceRoutes_DeleteReturnsCount(t *testing.T) {
	withHandlerEnv(t)
	traceIds := seedHandlerData(t, 1)

	w := doReq(http.MethodDelete, "/task-traces/"+traceIds[0], "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Deleted != 1 {
		t.Errorf("expected deleted=1, got %d", resp.Deleted)
	}
}

func TestHandleTraceRoutes_UnknownSubpath(t *testing.T) {
	withHandlerEnv(t)
	traceIds := seedHandlerData(t, 1)

	w := doReq(http.MethodGet, "/task-traces/"+traceIds[0]+"/whatever", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ─── Span children ─────────────────────────────────────────

func TestHandleSpanRoutes_Children(t *testing.T) {
	withHandlerEnv(t)
	tid, _ := NewTraceId("task-children")
	root := Start(tid, "task-children", 1, TraceTypeUpload, StageUploadTask, "")
	for i := 0; i < 4; i++ {
		c := StartChild(root, StageUploadFile, "")
		End(c)
	}
	End(root)
	time.Sleep(700 * time.Millisecond)

	w := doReq(http.MethodGet, "/task-traces/span/"+root+"/children", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var spans []Span
	_ = json.Unmarshal(w.Body.Bytes(), &spans)
	if len(spans) != 4 {
		t.Errorf("expected 4 children, got %d", len(spans))
	}
}

func TestHandleSpanRoutes_BadFormat(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodGet, "/task-traces/span/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── Cleanup ───────────────────────────────────────────────

func TestHandleCleanup_OK(t *testing.T) {
	withHandlerEnv(t)
	// 写入 5 条 trace
	seedHandlerData(t, 5)
	// 把所有 created_at_ms 改为 30 天前
	old := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	if _, err := globalTracer.store.db.Exec(`UPDATE task_traces SET created_at_ms=?`, old); err != nil {
		t.Fatalf("seed update: %v", err)
	}

	w := doReq(http.MethodPost, "/task-traces/cleanup?maxAgeDays=7&maxRows=0", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Deleted != 5 {
		t.Errorf("expected 5 deleted, got %d", resp.Deleted)
	}
}

func TestHandleCleanup_MethodNotAllowed(t *testing.T) {
	withHandlerEnv(t)
	w := doReq(http.MethodGet, "/task-traces/cleanup", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// ─── 服务未就绪 ────────────────────────────────────────────

func TestHandlers_ReturnServiceUnavailableWhenTracerNil(t *testing.T) {
	old := globalTracer
	globalTracer = nil
	t.Cleanup(func() { globalTracer = old })

	endpoints := []struct {
		method, path string
	}{
		{http.MethodGet, "/task-traces/list"},
		{http.MethodGet, "/task-traces/modules"},
		{http.MethodPatch, "/task-traces/modules/upload?enabled=true"},
		{http.MethodPost, "/task-traces/cleanup"},
		{http.MethodGet, "/task-traces/abc/top-spans"},
		{http.MethodGet, "/task-traces/span/x/children"},
	}
	for _, ep := range endpoints {
		w := doReq(ep.method, ep.path, "")
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: expected 503, got %d", ep.method, ep.path, w.Code)
		}
	}
}
