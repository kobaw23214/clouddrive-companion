package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// HTTP 接口层。所有路由都暴露在 /task-traces/* 下，命名风格与现有 /http-logs/、
// /app-logs/ 保持一致。
//
// 注意路由分发：精确路径（/task-traces/list 等）必须在 server.go 中早于通配前缀
// （/task-traces/）注册，否则会被通配吃掉。Go ServeMux 的 longest-prefix-match
// 机制保证了"先特定后通配"的顺序无关性，但仍然推荐显式按这个顺序注册以方便阅读。

// ─── HTTP 工具 ───

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// requireMethod 校验 HTTP method；不匹配返回 405 并写 Allow 头。
func requireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

// notReady 在 tracer 未初始化时返回 503，便于运维定位。
func notReady(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "tracer not initialized")
}

// ─── 模块开关 ───

// HandleListModules GET /task-traces/modules
//
// 响应：
//
//	{ "global": true, "modules": [...] }
func HandleListModules(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if globalTracer == nil {
		notReady(w)
		return
	}
	writeJSON(w, struct {
		Global  bool          `json:"global"`
		Modules []TraceModule `json:"modules"`
	}{
		Global:  config.AppConfig.TraceEnabled,
		Modules: globalTracer.moduleMgr.list(),
	})
}

// HandleToggleModule PATCH /task-traces/modules/{code}?enabled=true|false
func HandleToggleModule(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPatch) {
		return
	}
	if globalTracer == nil {
		notReady(w)
		return
	}
	code := strings.TrimPrefix(r.URL.Path, "/task-traces/modules/")
	code = strings.Trim(code, "/")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing module code")
		return
	}
	enabled, err := parseBoolStrict(r.URL.Query().Get("enabled"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid 'enabled' query parameter")
		return
	}
	if !globalTracer.moduleMgr.setEnabled(code, enabled) {
		writeError(w, http.StatusNotFound, "module not found: "+code)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleToggleGlobal PATCH /task-traces/global?enabled=true|false
func HandleToggleGlobal(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPatch) {
		return
	}
	enabled, err := parseBoolStrict(r.URL.Query().Get("enabled"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid 'enabled' query parameter")
		return
	}
	if !setGlobalEnabled(enabled) {
		writeError(w, http.StatusInternalServerError, "failed to persist global flag")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// parseBoolStrict 与 strconv.ParseBool 不同：空字符串视为非法（避免 ?enabled= 被吞掉）。
func parseBoolStrict(s string) (bool, error) {
	if s == "" {
		return false, strconv.ErrSyntax
	}
	return strconv.ParseBool(s)
}

// ─── 配置 ───

// traceConfigView /task-traces/config GET 返回结构。
type traceConfigView struct {
	SamplingThreshold int `json:"traceSamplingThreshold"`
	MaxAgeDays        int `json:"traceMaxAgeDays"`
	MaxRows           int `json:"traceMaxRows"`
	VacuumThresholdMB int `json:"traceVacuumThresholdMB"`
}

// HandleConfig GET / PATCH /task-traces/config
func HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, traceConfigView{
			SamplingThreshold: currentSamplingThreshold(),
			MaxAgeDays:        currentMaxAgeDays(),
			MaxRows:           currentMaxRows(),
			VacuumThresholdMB: currentVacuumThresholdMB(),
		})
	case http.MethodPatch:
		var p TraceConfigPatch
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		if err := applyConfigPatch(p); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := persistFn(); err != nil {
			writeError(w, http.StatusInternalServerError, "persist failed: "+err.Error())
			return
		}
		// 响应回当前完整配置，方便前端无需再次 GET
		writeJSON(w, traceConfigView{
			SamplingThreshold: currentSamplingThreshold(),
			MaxAgeDays:        currentMaxAgeDays(),
			MaxRows:           currentMaxRows(),
			VacuumThresholdMB: currentVacuumThresholdMB(),
		})
	default:
		w.Header().Set("Allow", "GET, PATCH")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// applyConfigPatch 把 patch 中非 nil 字段写入 AppConfig（保留 *int 指针语义）。
//
// 校验：所有字段都必须 >= 0；负数返回错误，不做任何修改。
func applyConfigPatch(p TraceConfigPatch) error {
	// 先校验
	if p.SamplingThreshold != nil && *p.SamplingThreshold < 0 {
		return invalidField("traceSamplingThreshold")
	}
	if p.MaxAgeDays != nil && *p.MaxAgeDays < 0 {
		return invalidField("traceMaxAgeDays")
	}
	if p.MaxRows != nil && *p.MaxRows < 0 {
		return invalidField("traceMaxRows")
	}
	if p.VacuumThresholdMB != nil && *p.VacuumThresholdMB < 0 {
		return invalidField("traceVacuumThresholdMB")
	}
	// 再写入
	if p.SamplingThreshold != nil {
		v := *p.SamplingThreshold
		config.AppConfig.TraceSamplingThreshold = &v
	}
	if p.MaxAgeDays != nil {
		v := *p.MaxAgeDays
		config.AppConfig.TraceMaxAgeDays = &v
	}
	if p.MaxRows != nil {
		v := *p.MaxRows
		config.AppConfig.TraceMaxRows = &v
	}
	if p.VacuumThresholdMB != nil {
		v := *p.VacuumThresholdMB
		config.AppConfig.TraceVacuumThresholdMB = &v
	}
	return nil
}

type configError string

func (e configError) Error() string { return string(e) }

func invalidField(name string) error {
	return configError("invalid value for " + name + " (must be >= 0)")
}

// ─── 列表 / 详情 ───

// HandleListTraces GET /task-traces/list
//
// 查询参数：
//
//	traceType=upload|scan
//	taskId=<string>
//	from=<msEpoch>
//	to=<msEpoch>
//	page=<int>  默认 1
//	size=<int>  默认 50，上限 200
func HandleListTraces(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if globalTracer == nil {
		notReady(w)
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	fromMs, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	toMs, _ := strconv.ParseInt(q.Get("to"), 10, 64)

	summaries, total, err := globalTracer.store.ListTraces(ListTracesQuery{
		TraceType: q.Get("traceType"),
		TaskID:    q.Get("taskId"),
		FromMs:    fromMs,
		ToMs:      toMs,
		Page:      page,
		Size:      size,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// page/size 在 store 内有规范化逻辑；这里按规范化后的值返回给前端
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	} else if size > 200 {
		size = 200
	}

	writeJSON(w, struct {
		Total int            `json:"total"`
		Page  int            `json:"page"`
		Size  int            `json:"size"`
		Items []TraceSummary `json:"items"`
	}{Total: total, Page: page, Size: size, Items: summaries})
}

// HandleTraceRoutes 路由分发：
//
//	GET    /task-traces/{traceId}/top-spans
//	GET    /task-traces/{traceId}/all
//	DELETE /task-traces/{traceId}
//
// 由于 server.go 注册的是前缀 /task-traces/，对于命中已经在前面被特定路由抢走的
// （/list、/modules、/global、/config、/cleanup、/span/...）这里不会被调用。
func HandleTraceRoutes(w http.ResponseWriter, r *http.Request) {
	if globalTracer == nil {
		notReady(w)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/task-traces/")
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		writeError(w, http.StatusBadRequest, "missing traceId")
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	traceId := parts[0]

	// /task-traces/{traceId}（无子路径）→ 仅支持 DELETE
	if len(parts) == 1 {
		if !requireMethod(w, r, http.MethodDelete) {
			return
		}
		n, err := globalTracer.store.DeleteTrace(traceId)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]int{"deleted": n})
		return
	}

	// /task-traces/{traceId}/{sub}
	switch parts[1] {
	case "top-spans":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		spans, err := globalTracer.store.GetTopSpans(traceId)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, spans)
	case "all":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		spans, truncated, err := globalTracer.store.GetAllSpans(traceId, 5000)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, struct {
			Spans     []Span `json:"spans"`
			Truncated bool   `json:"truncated"`
		}{Spans: spans, Truncated: truncated})
	case "stream":
		// 实时链路追踪 SSE 通道。HandleStream 内部自己解析 traceId，
		// 这里直接转发即可。
		HandleStream(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown sub-path: "+parts[1])
	}
}

// HandleSpanRoutes 路由分发：GET /task-traces/span/{spanId}/children
func HandleSpanRoutes(w http.ResponseWriter, r *http.Request) {
	if globalTracer == nil {
		notReady(w)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/task-traces/span/")
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 || parts[1] != "children" {
		writeError(w, http.StatusBadRequest, "expected /task-traces/span/{spanId}/children")
		return
	}
	spans, err := globalTracer.store.GetChildren(parts[0])
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, spans)
}

// HandleCleanup POST /task-traces/cleanup?maxAgeDays=N&maxRows=M
//
// 参数缺省时使用 config 中当前值。返回 {deleted: <int>}。
func HandleCleanup(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if globalTracer == nil {
		notReady(w)
		return
	}
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("maxAgeDays"))
	rows, _ := strconv.Atoi(q.Get("maxRows"))
	if days <= 0 {
		days = currentMaxAgeDays()
	}
	if rows <= 0 {
		rows = currentMaxRows()
	}
	n, err := globalTracer.store.Cleanup(days, rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]int{"deleted": n})
}
