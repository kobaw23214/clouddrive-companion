package server

import (
	"bytes"
	"clouddrive-companion-withWails2/internal/config"
	"clouddrive-companion-withWails2/internal/preselect"
	"clouddrive-companion-withWails2/internal/tracer"
	"clouddrive-companion-withWails2/internal/uploader"
	"clouddrive-companion-withWails2/internal/utils"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxReselectBodyBytes /reselect 带预选列表时的 body 上限。
// 按一条路径平均 ~100 字节估算，32MB 约可容纳 30 万条，足够覆盖实际场景。
const maxReselectBodyBytes = 32 << 20

// reselectRequest /reselect 的可选 JSON body。
// 契约最小化：浏览器只需给一组 absolutePath 字符串（文件或目录均可），
// fileName/fileSize/relativePath 等字段客户端恢复时自行从磁盘获取。
type reselectRequest struct {
	Preselect []string `json:"preselect"`
}

// reselectResponse /reselect 带 body 时的 JSON 响应。
// files 明细浏览器自己就有，只回数量；missing 用于提示用户哪些文件已失效。
type reselectResponse struct {
	OK        bool                 `json:"ok"`
	Received  int                  `json:"received"`  // 收到的路径条数（去重前）
	Dirs      []preselect.DirEntry `json:"dirs"`      // 折叠恢复出的目录项
	FileCount int                  `json:"fileCount"` // 未能折叠的散文件数量
	Missing   []string             `json:"missing"`   // 已不存在或不可读的路径
}

var (
	SessionMutex    sync.RWMutex
	CurrentFileList []utils.FullFileInfo

	GlobalUploadManager *uploader.Manager
	ManagerMutex        sync.Mutex
)

// ScanStatus 扫描会话的状态枚举
const (
	ScanStatusIdle      = "idle"      // 没有正在进行的扫描
	ScanStatusScanning  = "scanning"  // 扫描进行中
	ScanStatusCompleted = "completed" // 扫描完成，文件列表可读
	ScanStatusCancelled = "cancelled" // 用户取消
	ScanStatusError     = "error"     // 扫描出错
	ScanStatusConsumed  = "consumed"  // 文件列表已被浏览器拉走
)

// ScanSession 扫描会话状态（无连接、可重复查询）
type ScanSession struct {
	TaskId       string `json:"taskId"`
	Status       string `json:"status"` // idle / scanning / completed / cancelled / error / consumed
	FilesFound   int    `json:"filesFound"`
	DirsFound    int    `json:"dirsFound"`
	CurrentPath  string `json:"currentPath,omitempty"`
	StartedAt    int64  `json:"startedAt,omitempty"`  // unix milliseconds
	FinishedAt   int64  `json:"finishedAt,omitempty"` // unix milliseconds
	ErrorMessage string `json:"errorMessage,omitempty"`

	// ─── 扫描链路并入上传 trace 用 ───
	// TraceId / ScanSpanId 由 utils.GenerateFileListWithMetadata 返回，
	// 上传启动时（HandleConfirmUpload）通过 ConsumeScanTrace 取走，
	// 然后 Manager.Start 据此把 scan trace 整体并入 upload trace。
	TraceId    string `json:"traceId,omitempty"`
	ScanSpanId string `json:"scanSpanId,omitempty"`
	// LinkedTaskId 防止同一次扫描被两个上传任务重复并入：第二次会拿到空字符串。
	LinkedTaskId string `json:"linkedTaskId,omitempty"`
}

var (
	scanSessionMu sync.RWMutex
	scanSession   = ScanSession{Status: ScanStatusIdle}
)

// BeginScanSession 标记一次新的扫描开始，覆盖旧会话
func BeginScanSession(taskId string) {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	scanSession = ScanSession{
		TaskId:    taskId,
		Status:    ScanStatusScanning,
		StartedAt: time.Now().UnixMilli(),
	}
}

// UpdateScanProgress 在扫描过程中更新进度（不改变状态）
func UpdateScanProgress(filesFound, dirsFound int, currentPath string) {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	if scanSession.Status != ScanStatusScanning {
		return
	}
	scanSession.FilesFound = filesFound
	scanSession.DirsFound = dirsFound
	scanSession.CurrentPath = currentPath
}

// CompleteScanSession 标记扫描成功完成，文件列表可读
func CompleteScanSession(filesFound, dirsFound int) {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	scanSession.FilesFound = filesFound
	scanSession.DirsFound = dirsFound
	scanSession.CurrentPath = ""
	scanSession.Status = ScanStatusCompleted
	scanSession.FinishedAt = time.Now().UnixMilli()
}

// CancelScanSession 标记扫描被取消
func CancelScanSession() {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	if scanSession.Status == ScanStatusScanning || scanSession.Status == ScanStatusIdle {
		scanSession.Status = ScanStatusCancelled
		scanSession.FinishedAt = time.Now().UnixMilli()
	}
}

// ResetScanSessionForReselect 用户点了"重新选择"时调用：把会话恢复为初始
// scanning 状态，并清空所有上次会话残留字段（traceId / linked / 进度等）。
//
// 与 CancelScanSession 不同：这里是"我要重新开始"，不是"取消"，所以浏览器
// 这一刻轮询 /scan-status 应该看到 scanning（正在准备/选择中），而不是
// 上一次扫描的 completed / consumed / cancelled 残留。
//
// 真正的扫描 traceId 仍在用户从原生对话框确认目录后由 BeginScanSession 写入；
// 这里只是覆盖一个空白的"在路上"状态，避免 reselect 后到选择对话框出现前的
// 那个时间窗里浏览器读到旧值。
func ResetScanSessionForReselect() {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	scanSession = ScanSession{
		Status:    ScanStatusScanning,
		StartedAt: time.Now().UnixMilli(),
	}
}

// FailScanSession 标记扫描出错
func FailScanSession(message string) {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	scanSession.Status = ScanStatusError
	scanSession.ErrorMessage = message
	scanSession.FinishedAt = time.Now().UnixMilli()
}

// MarkScanConsumed 标记 completed 状态的会话已被浏览器拉走
func MarkScanConsumed() {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	if scanSession.Status == ScanStatusCompleted {
		scanSession.Status = ScanStatusConsumed
	}
}

// GetScanSession 返回扫描会话快照
func GetScanSession() ScanSession {
	scanSessionMu.RLock()
	defer scanSessionMu.RUnlock()
	return scanSession
}

// SetScanTrace 在扫描完成后，把本次扫描的 traceId / scanSpanId 记到会话里，
// 等用户对该批文件发起上传时，HandleConfirmUpload 会通过 ConsumeScanTrace
// 取走这两个值并透传给 Manager。
func SetScanTrace(traceId, scanSpanId string, finishedAtMs int64) {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	scanSession.TraceId = traceId
	scanSession.ScanSpanId = scanSpanId
	if finishedAtMs > 0 {
		scanSession.FinishedAt = finishedAtMs
	}
}

// ConsumeScanTrace 原子地"认领"当前会话的扫描 trace：
//   - 若 LinkedTaskId 已被设置（上次扫描已被某个 upload 任务认领） → 返回空，调用方走原行为
//   - 否则把 LinkedTaskId 设为传入的 taskId 并返回 (traceId, scanSpanId, scanFinishedAt)
//
// 设计目的：让"扫描后立刻上传"的常见路径自动并入 trace，但避免：
//   - 一次扫描被两个 upload 任务都认领
//   - 续传 / 浏览器后续单独发起的 upload 误认领"很久以前"的扫描
//
// 这里不做时间窗校验，由调用方判断 ScanSession 是否最近、是否对应当前任务。
func ConsumeScanTrace(taskId string) (traceId, scanSpanId string, scanFinishedAtMs int64, ok bool) {
	scanSessionMu.Lock()
	defer scanSessionMu.Unlock()
	if scanSession.TraceId == "" || scanSession.LinkedTaskId != "" {
		return "", "", 0, false
	}
	scanSession.LinkedTaskId = taskId
	return scanSession.TraceId, scanSession.ScanSpanId, scanSession.FinishedAt, true
}

// HTTPRequestLog 单条 HTTP 请求日志
type HTTPRequestLog struct {
	ID                uint64              `json:"id"`
	Timestamp         string              `json:"timestamp"`   // 本地可读时间，例: 2026-05-15 09:27:21.123
	TimestampMs       int64               `json:"timestampMs"` // unix milliseconds
	Method            string              `json:"method"`
	Path              string              `json:"path"`
	Query             string              `json:"query,omitempty"`
	RemoteAddr        string              `json:"remoteAddr"`
	RequestHeaders    map[string][]string `json:"requestHeaders"`
	RequestBody       string              `json:"requestBody,omitempty"`
	RequestBodyTrunc  bool                `json:"requestBodyTruncated,omitempty"`
	StatusCode        int                 `json:"statusCode"`
	ResponseHeaders   map[string][]string `json:"responseHeaders"`
	ResponseBody      string              `json:"responseBody,omitempty"`
	ResponseBodyTrunc bool                `json:"responseBodyTruncated,omitempty"`
	ResponseBytes     int64               `json:"responseBytes"`
	DurationMs        int64               `json:"durationMs"`
	Direction         string              `json:"direction"`        // in=入站（本地服务收到）/ out=出站（发往远端后端）
	Peer              string              `json:"peer"`             // 对端地址：入站=RemoteAddr，出站=URL.Host
	TaskID            string              `json:"taskId,omitempty"` // 尽力解析的任务 ID，解析不到留空
}

// EndpointSessionInfo /get-metadata 端点（本地 HTTP 服务）的运行状态
type EndpointSessionInfo struct {
	Port    int  `json:"port"`
	Running bool `json:"running"`
}

const (
	// httpLogBodyMaxBytes 单个请求/响应体最多记录的字节数，超过则截断
	httpLogBodyMaxBytes = 1024 * 1024 // 1 MB
)

var (
	endpointInfoMu     sync.RWMutex
	GlobalEndpointInfo = EndpointSessionInfo{Running: false}

	httpLogIDSeq uint64
)

// SetEndpointRunning 设置端点运行状态
func SetEndpointRunning(port int) {
	endpointInfoMu.Lock()
	defer endpointInfoMu.Unlock()
	GlobalEndpointInfo.Port = port
	GlobalEndpointInfo.Running = true
}

// GetEndpointInfo 获取端点状态快照
func GetEndpointInfo() EndpointSessionInfo {
	endpointInfoMu.RLock()
	defer endpointInfoMu.RUnlock()
	return GlobalEndpointInfo
}

// truncateBody 把 body 字节切片转字符串，超过上限时截断并返回 truncated 标志
func truncateBody(body []byte) (string, bool) {
	if len(body) <= httpLogBodyMaxBytes {
		return string(body), false
	}
	return string(body[:httpLogBodyMaxBytes]), true
}

// recordingResponseWriter 包装 http.ResponseWriter，捕获状态码和响应体
type recordingResponseWriter struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	bodyCap  int  // body 缓存上限（字节）
	overflow bool // 是否已经超过缓存上限
	written  int64
}

func (rw *recordingResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recordingResponseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	rw.written += int64(len(b))
	if !rw.overflow {
		remaining := rw.bodyCap - rw.body.Len()
		if remaining > 0 {
			if len(b) <= remaining {
				rw.body.Write(b)
			} else {
				rw.body.Write(b[:remaining])
				rw.overflow = true
			}
		} else {
			rw.overflow = true
		}
	}
	return rw.ResponseWriter.Write(b)
}

// httpLogMiddleware 记录所有进入本地 HTTP 服务的请求/响应
func httpLogMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// 过滤掉查询日志相关的 API 请求，避免污染日志数据库。
		// /httplog/sequence 是时序图查看页（数据接口 /http-logs/* 已被前缀排除），
		// 页面本身也排除，避免打开/刷新查看器在时序图里制造自污染。
		if strings.HasPrefix(path, "/http-logs/") || strings.HasPrefix(path, "/app-logs/") ||
			strings.HasPrefix(path, "/task-traces/") || path == "/httplog/sequence" {
			next(w, r)
			return
		}

		start := time.Now()

		// 读取并保留请求体（仅 1MB 以内）
		var reqBodyStr string
		var reqTrunc bool
		var reqBodyBytes []byte
		if r.Body != nil && r.ContentLength != 0 {
			limited := io.LimitReader(r.Body, httpLogBodyMaxBytes+1)
			buf, _ := io.ReadAll(limited)
			if len(buf) > httpLogBodyMaxBytes {
				reqBodyStr = string(buf[:httpLogBodyMaxBytes])
				reqTrunc = true
				// 把剩下没读到的丢弃，保持 ContentLength 一致
				_, _ = io.Copy(io.Discard, r.Body)
			} else {
				reqBodyStr = string(buf)
				reqBodyBytes = buf
			}
			r.Body.Close()
			// 重新装回 body，让真正的 handler 继续可读
			r.Body = io.NopCloser(bytes.NewReader(buf))
		}

		rec := &recordingResponseWriter{
			ResponseWriter: w,
			bodyCap:        httpLogBodyMaxBytes,
		}
		next(rec, r)

		respBodyStr, respTrunc := truncateBody(rec.body.Bytes())
		// rec.overflow 优先于 truncateBody 的判断（因为我们在 Write 阶段就可能停止收集）
		if rec.overflow {
			respTrunc = true
		}

		// 复制 header 快照（map[string][]string，避免后续被修改）
		reqHeaders := make(map[string][]string, len(r.Header))
		for k, v := range r.Header {
			reqHeaders[k] = append([]string(nil), v...)
		}
		respHeaders := make(map[string][]string, len(rec.Header()))
		for k, v := range rec.Header() {
			respHeaders[k] = append([]string(nil), v...)
		}

		entry := HTTPRequestLog{
			ID:                atomic.AddUint64(&httpLogIDSeq, 1),
			Timestamp:         start.Format("2006-01-02 15:04:05.000"),
			TimestampMs:       start.UnixMilli(),
			Method:            r.Method,
			Path:              r.URL.Path,
			Query:             r.URL.RawQuery,
			RemoteAddr:        r.RemoteAddr,
			RequestHeaders:    reqHeaders,
			RequestBody:       reqBodyStr,
			RequestBodyTrunc:  reqTrunc,
			StatusCode:        rec.status,
			ResponseHeaders:   respHeaders,
			ResponseBody:      respBodyStr,
			ResponseBodyTrunc: respTrunc,
			ResponseBytes:     rec.written,
			DurationMs:        time.Since(start).Milliseconds(),
			Direction:         "in",
			Peer:              r.RemoteAddr,
			TaskID:            extractInboundTaskID(r, reqBodyBytes),
		}
		AppendHTTPLogToDisk(entry)
	}
}

// extractInboundTaskID 入站请求尽力解析 taskId：
// 先看 URL query，再从已缓存的小 body 里解析（urlencoded 表单或小 JSON）。
// body 已被中间件缓存过，这里不做任何额外 IO；body 被截断时放弃解析。
func extractInboundTaskID(r *http.Request, bodyBytes []byte) string {
	if taskID := r.URL.Query().Get("taskId"); taskID != "" {
		return taskID
	}
	if len(bodyBytes) == 0 {
		return ""
	}
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		if vals, err := url.ParseQuery(string(bodyBytes)); err == nil {
			return vals.Get("taskId")
		}
	case strings.HasPrefix(ct, "application/json"):
		var payload struct {
			TaskID string `json:"taskId"`
		}
		if err := json.Unmarshal(bodyBytes, &payload); err == nil {
			return payload.TaskID
		}
	}
	return ""
}

// AppInterface 定义 App 需要实现的接口
type AppInterface interface {
	ShowFileSelector()
	ShowFileSelectorWithPreselect(result *preselect.Result)
	TriggerStatsRefresh()
	ShowDashboard()
	ShowConfigEditor()
	Quit()
}

// isPortAvailable 检查端口是否可用
func isPortAvailable(port int) bool {
	address := fmt.Sprintf("localhost:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// killProcessOnPort 终止占用端口的进程
func killProcessOnPort(port int) error {
	log.Printf("正在检查端口 %d 的占用情况...", port)

	cmd := exec.Command("netstat", "-ano")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("执行netstat失败: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	portStr := fmt.Sprintf(":%d", port)
	var pid string

	for _, line := range lines {
		if strings.Contains(line, portStr) && strings.Contains(line, "LISTENING") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				pid = fields[len(fields)-1]
				break
			}
		}
	}

	if pid == "" {
		log.Printf("端口 %d 未被占用", port)
		return nil
	}

	log.Printf("发现进程 PID=%s 占用端口 %d，正在终止...", pid, port)

	killCmd := exec.Command("taskkill", "/F", "/PID", pid)
	if err := killCmd.Run(); err != nil {
		return fmt.Errorf("终止进程 %s 失败: %w", pid, err)
	}

	log.Printf("已成功终止占用端口 %d 的进程 (PID=%s)", port, pid)
	time.Sleep(500 * time.Millisecond)
	return nil
}

// StartLocalServer 启动本地服务器
func StartLocalServer(app AppInterface) {
	port := config.AppConfig.LocalServerPort
	if !isPortAvailable(port) {
		log.Printf("Port %d is occupied, attempting to clear...", port)
		if err := killProcessOnPort(port); err != nil {
			log.Printf("Warning: Failed to clear port: %v", err)
			log.Fatalf("端口 %d 被占用且清理失败: %v", port, err)
			os.Exit(1)
		}
	}

	serverAddr := fmt.Sprintf(":%d", config.AppConfig.LocalServerPort)
	server := &http.Server{Addr: serverAddr}

	corsHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			// PNA / LNA：浏览器在预检里带 Access-Control-Request-Private-Network: true 时，
			// 服务端必须回 Access-Control-Allow-Private-Network: true，否则预检失败、
			// 真实请求被「公网/局域网 -> loopback」的 PNA 策略拦截。
			// 仅在浏览器明确请求时回该头，避免给所有响应无谓加头。
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				w.Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return corsHandler(httpLogMiddleware(h))
	}

	http.HandleFunc("/ping", wrap(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	http.HandleFunc("/shutdown", wrap(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received shutdown request from frontend.")
		w.WriteHeader(http.StatusOK)
		go func() {
			time.Sleep(500 * time.Millisecond)
			// 走 App.Quit 统一退出：先收掉托盘图标（避免残留"点不动"的幽灵图标）、
			// flush 追踪队列和历史库，再退进程。不要在这里裸 os.Exit。
			app.Quit()
		}()
	}))

	http.HandleFunc("/reselect", wrap(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received re-select request from frontend.")

		// 可选 body：{"preselect": ["E:/a/b.pdf", ...]}。
		// "追加"场景下浏览器把上次已确认的文件列表（absolutePath）带回来，
		// 客户端折叠成"目录 + 散文件"后在文件选择器里恢复勾选；
		// 空 body / 不带 preselect 时保持原有"重新选择"行为，完全兼容旧调用方。
		var preselectPaths []string
		if r.Body != nil {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReselectBodyBytes))
			if err != nil {
				http.Error(w, "failed to read request body (too large?)", http.StatusRequestEntityTooLarge)
				return
			}
			if len(strings.TrimSpace(string(body))) > 0 {
				var req reselectRequest
				if err := json.Unmarshal(body, &req); err != nil {
					http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
					return
				}
				preselectPaths = req.Preselect
			}
		}

		// 把扫描会话重置为 scanning（"准备中/选择中"），覆盖上一次扫描的
		// completed/consumed/cancelled 残留。等用户在原生对话框点确认后，
		// BeginScanSession 会再次覆写带有正式 taskId 的 scanning 状态。
		ResetScanSessionForReselect()

		if len(preselectPaths) == 0 {
			app.ShowFileSelector()
			w.WriteHeader(http.StatusOK)
			return
		}

		result := preselect.Collapse(preselectPaths)
		app.ShowFileSelectorWithPreselect(result)

		w.Header().Set("Content-Type", "application/json")
		resp := reselectResponse{
			OK:        true,
			Received:  result.Received,
			Dirs:      result.Dirs,
			FileCount: len(result.Files),
			Missing:   result.Missing,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Warning: 写入 /reselect 响应失败: %v", err)
		}
	}))

	http.HandleFunc("/openClient", wrap(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received openClient request, showing dashboard.")
		app.ShowDashboard()
		w.WriteHeader(http.StatusOK)
	}))

	http.HandleFunc("/openConfigEditor", wrap(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received openConfigEditor request, showing config editor.")
		app.ShowConfigEditor()
		w.WriteHeader(http.StatusOK)
	}))

	http.HandleFunc("/get-metadata", wrap(HandleGetMetadata))
	http.HandleFunc("/scan-status", wrap(HandleGetScanStatus))
	http.HandleFunc("/pending-tasks", wrap(HandleGetPendingTasks))

	// PNA/LNA 诊断探测端点（配合 testing/pna-test.html）。
	// 注意：HandlePNAProbe 自带完整 CORS + PNA 预检处理，不走通用 wrap，
	// 仅套 httpLogMiddleware 以便在 HTTP 日志里也能看到探测记录。
	http.HandleFunc("/pna-probe", httpLogMiddleware(HandlePNAProbe))

	// 应用日志查看（浏览器版）
	http.HandleFunc("/applog", wrap(HandleAppLogViewerPage))
	http.HandleFunc("/app-logs/list", wrap(HandleAppLogList))
	http.HandleFunc("/app-logs/read", wrap(HandleAppLogRead))
	http.HandleFunc("/app-logs/delete", wrap(HandleAppLogDelete))

	// HTTP 请求日志查看（浏览器版）
	http.HandleFunc("/httplog", wrap(HandleHTTPLogViewerPage))
	http.HandleFunc("/httplog/sequence", wrap(HandleHTTPLogSequencePage))
	http.HandleFunc("/http-logs/query", wrap(HandleHTTPLogQuery))
	http.HandleFunc("/http-logs/detail", wrap(HandleHTTPLogDetail))
	http.HandleFunc("/http-logs/stats", wrap(HandleHTTPLogStatsAPI))
	http.HandleFunc("/http-logs/delete", wrap(HandleHTTPLogDelete))
	http.HandleFunc("/http-logs/sequence", wrap(HandleHTTPLogSequence))

	// 任务级链路追踪 ——
	// Go ServeMux 的 longest-prefix-match 决定了"精确路径"会先于"前缀通配"被匹配，
	// 这里仍按可读性顺序：先精确（list/modules/global/config/cleanup/span/viewer 前缀）后通配（traceId 前缀）。
	http.HandleFunc("/task-traces/viewer", wrap(tracer.HandleViewerPage))
	http.HandleFunc("/task-traces/list", wrap(tracer.HandleListTraces))
	http.HandleFunc("/task-traces/modules", wrap(tracer.HandleListModules))
	http.HandleFunc("/task-traces/modules/", wrap(tracer.HandleToggleModule))
	http.HandleFunc("/task-traces/global", wrap(tracer.HandleToggleGlobal))
	http.HandleFunc("/task-traces/config", wrap(tracer.HandleConfig))
	http.HandleFunc("/task-traces/cleanup", wrap(tracer.HandleCleanup))
	http.HandleFunc("/task-traces/span/", wrap(tracer.HandleSpanRoutes))
	http.HandleFunc("/task-traces/", wrap(tracer.HandleTraceRoutes))

	http.HandleFunc("/start-upload", wrap(func(w http.ResponseWriter, r *http.Request) {
		HandleConfirmUpload(w, r, app)
	}))

	http.HandleFunc("/resume-upload", wrap(func(w http.ResponseWriter, r *http.Request) {
		HandleResumeUpload(w, r, app)
	}))

	log.Printf("Local server starting on http://localhost%s", serverAddr)
	SetEndpointRunning(port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Local server failed: %v", err)
	}
}

// StartUploadProcess 启动上传进程
func StartUploadProcess(uploadManager *uploader.Manager, taskInfo uploader.TaskInfo, taskId string, app AppInterface) {
	defer func() {
		uploader.ActiveUploads.Delete(taskId)
		log.Printf("任务 %s 已从活跃列表移除。", taskId)

		ManagerMutex.Lock()
		if GlobalUploadManager == uploadManager {
			GlobalUploadManager = nil
		}
		ManagerMutex.Unlock()
		log.Printf("Upload task %s finished.", taskInfo.TaskId)
	}()

	ManagerMutex.Lock()
	GlobalUploadManager = uploadManager
	ManagerMutex.Unlock()

	log.Printf("为任务 %s 启动命令轮询器...", uploadManager.GetTaskId())
	go uploader.PollCommands(uploadManager, app)

	if success := uploadManager.Start(); success {
		// NotifyBackendTaskComplete 已迁回 Manager.Start 内部（与 notify_complete span 一起埋点），
		// 这里：
		//   1. 把任务的最终快照一次性写入 task_history.db（manifest 删除后历史仍可查）
		//   2. 删除本地 manifest / .completed.log / .failed.log
		snapshot := uploadManager.BuildHistorySnapshot("COMPLETED")
		if err := uploader.SaveHistoryRecord(snapshot); err != nil {
			log.Printf("Warning: 保存任务 %s 历史记录失败: %v", taskInfo.TaskId, err)
		}
		log.Println("All files uploaded successfully, cleaning up local task files.")
		if err := uploader.DeleteTaskFiles(taskInfo.TaskId); err != nil {
			log.Printf("Warning: Failed to delete task files for %s: %v", taskInfo.TaskId, err)
		}
	} else {
		// 部分失败 / 取消 / 未跑完：保留 manifest 让用户能续传，
		// 同时也写一条 history 记录（finalStatus 由当前快照决定）。
		// 这一条记录与 manifest 共存：用户如果点续传 → 任务再次成功后会
		// 走 INSERT OR REPLACE 把历史更新为 COMPLETED。
		failedCount := uploadManager.GetTaskProgress().FailedFiles
		var finalStatus string
		if failedCount > 0 {
			finalStatus = "PARTIAL_COMPLETED"
		} else {
			finalStatus = "INTERRUPTED"
		}
		snapshot := uploadManager.BuildHistorySnapshot(finalStatus)
		if err := uploader.SaveHistoryRecord(snapshot); err != nil {
			log.Printf("Warning: 保存任务 %s 历史记录失败: %v", taskInfo.TaskId, err)
		}
		log.Println("One or more files failed to upload. Task metadata will be kept for resume.")
	}
	uploadManager.LogFinalStats()
}
