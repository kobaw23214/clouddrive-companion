package server

import (
	"clouddrive-companion-withWails2/internal/tracer"
	"net/http"
	"sync/atomic"
	"time"
)

// trace SSE 连接生命周期日志的 server 侧落库入口。
//
// 依赖方向：server → tracer（server.go 已 import tracer），tracer 通过
// SetSSEConnLogger 回调进来，与 api.SetOutboundLogger → LogOutboundHTTP 同一模式。

// LogSSEConnEvent 由 tracer 包通过 SetSSEConnLogger 回调进来，
// 转成统一的 HTTPRequestLog（direction="in"）走现有的异步批量写入通道。
func LogSSEConnEvent(e tracer.SSEConnLogEntry) {
	AppendHTTPLogToDisk(sseConnEventToHTTPLog(e))
}

// sseConnEventToHTTPLog 把 SSE 连接事件转成 HTTPRequestLog（纯函数，便于单测）。
//
// 说明文字放在 ResponseBody，与 LogOutboundHTTP 把网络错误串放 responseBody 的做法一致，
// 前端详情页无需特殊处理即可展示。
func sseConnEventToHTTPLog(e tracer.SSEConnLogEntry) HTTPRequestLog {
	return HTTPRequestLog{
		ID:           atomic.AddUint64(&httpLogIDSeq, 1),
		Timestamp:    time.UnixMilli(e.TimestampMs).Format("2006-01-02 15:04:05.000"),
		TimestampMs:  e.TimestampMs,
		Method:       http.MethodGet,
		Path:         e.Path,
		RemoteAddr:   e.Peer,
		Peer:         e.Peer,
		Direction:    "in",
		TaskID:       e.TaskID,
		StatusCode:   http.StatusOK,
		ResponseBody: e.Note,
		DurationMs:   e.DurationMs,
	}
}
