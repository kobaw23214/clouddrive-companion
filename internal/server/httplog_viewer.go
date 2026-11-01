package server

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
)

//go:embed httplog_viewer.html
var httplogViewerHTML []byte

//go:embed httplog_sequence.html
var httplogSequenceHTML []byte

// HandleHTTPLogViewerPage 浏览器端的 HTTP 请求日志查看器（嵌入静态 HTML）
func HandleHTTPLogViewerPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(httplogViewerHTML)
}

// HandleHTTPLogSequencePage 浏览器端的 HTTP 时序图页面（嵌入静态 HTML，原生 SVG 无外部依赖）
func HandleHTTPLogSequencePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(httplogSequenceHTML)
}

// HandleHTTPLogQuery 浏览器端查询 HTTP 日志（分页 + 过滤）
func HandleHTTPLogQuery(w http.ResponseWriter, r *http.Request) {
	q := HTTPLogQuery{}
	if v := r.URL.Query().Get("page"); v != "" {
		q.Page, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("size"); v != "" {
		q.Size, _ = strconv.Atoi(v)
	}
	q.Method = r.URL.Query().Get("method")
	q.PathLike = r.URL.Query().Get("pathLike")
	q.StatusGroup = r.URL.Query().Get("statusGroup")
	q.ExcludePing = r.URL.Query().Get("excludePing") == "true"
	if v := r.URL.Query().Get("startMs"); v != "" {
		q.StartMs, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("endMs"); v != "" {
		q.EndMs, _ = strconv.ParseInt(v, 10, 64)
	}

	page, err := QueryHTTPLogs(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// HandleHTTPLogDetail 浏览器端获取单条 HTTP 日志详情
func HandleHTTPLogDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	entry, err := GetHTTPLogByID(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// HandleHTTPLogStatsAPI 浏览器端获取 HTTP 日志统计
func HandleHTTPLogStatsAPI(w http.ResponseWriter, r *http.Request) {
	stats := GetHTTPLogStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// HandleHTTPLogSequence 浏览器端时序图数据：按时间正序返回指定范围内的日志
func HandleHTTPLogSequence(w http.ResponseWriter, r *http.Request) {
	var fromMs, toMs int64
	if v := r.URL.Query().Get("fromMs"); v != "" {
		fromMs, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("toMs"); v != "" {
		toMs, _ = strconv.ParseInt(v, 10, 64)
	}
	taskID := r.URL.Query().Get("taskId")
	direction := r.URL.Query().Get("direction")
	page, size := 0, 0
	if v := r.URL.Query().Get("page"); v != "" {
		page, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("size"); v != "" {
		size, _ = strconv.Atoi(v)
	}

	entries, total, err := QuerySequenceEvents(fromMs, toMs, taskID, direction, page, size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if page < 1 {
		page = 1
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HTTPLogPage{Total: total, Page: page, Size: size, Entries: entries})
}

// HandleHTTPLogDelete 浏览器端删除 HTTP 日志（支持单个或批量，空数组则删除全部）
func HandleHTTPLogDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var ids []uint64
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := DeleteHTTPLogs(ids); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
