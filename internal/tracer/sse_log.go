package tracer

import "fmt"

// trace SSE 连接生命周期的日志打点。
//
// 背景：/task-traces/* 前缀被 server 的 httpLogMiddleware 排除（避免高频轮询污染
// http_logs），因此 SSE 长连接的建立/断开不会出现在 HTTP 时序图里。这里在
// HandleStream 内显式打点，每个连接只写 2 条（建立 + 断开），量级极小。
//
// 接线方式与 api.SetOutboundLogger 相同：tracer 定义回调类型，main 在启动时把
// server.LogSSEConnEvent 注册进来，避免 tracer → server 的 import 环。

// SSEConnLogEntry 一条 SSE 连接生命周期日志（建立/断开各一条）。
type SSEConnLogEntry struct {
	TraceID     string // 订阅的 traceId
	TaskID      string // 尽力从 traces 库解析，查不到留空
	Path        string // 请求路径，形如 /task-traces/{traceId}/stream
	Peer        string // 客户端地址（RemoteAddr）
	TimestampMs int64  // 事件发生时间（毫秒）；建立条=建立时间，断开条=断开时间
	DurationMs  int64  // 建立条为 0；断开条为连接存活时长
	Note        string // 说明文字，由 server 侧写入 responseBody 便于前端展示
}

// sseConnLogger 出站日志回调，由 main 在 HTTP 日志存储初始化成功后接线；nil 时打点为零开销
var sseConnLogger func(SSEConnLogEntry)

// SetSSEConnLogger 注册 SSE 连接日志回调（幂等，启动时调用一次）
func SetSSEConnLogger(fn func(SSEConnLogEntry)) {
	sseConnLogger = fn
}

// logSSEConn 发出一条 SSE 连接日志；未注册回调时直接丢弃
func logSSEConn(e SSEConnLogEntry) {
	if sseConnLogger != nil {
		sseConnLogger(e)
	}
}

// sseDisconnectNote 断开条的说明文字（独立函数便于单测）
func sseDisconnectNote(aliveMs int64) string {
	return fmt.Sprintf("SSE 连接断开，持续 %d 毫秒", aliveMs)
}

// lookupTaskIDForTrace 尽力从 traces 库解析 traceId 对应的 taskId：
// 取首个带 taskId 的顶层 span。复用现有的 GetTopSpans 轻查询（trace_id 索引），
// 不为此新建重查询；查不到返回空串，调用方接受空值。
func lookupTaskIDForTrace(traceId string) string {
	if globalTracer == nil || globalTracer.store == nil || traceId == "" {
		return ""
	}
	tops, err := globalTracer.store.GetTopSpans(traceId)
	if err != nil {
		return ""
	}
	for _, sp := range tops {
		if sp.TaskID != "" {
			return sp.TaskID
		}
	}
	return ""
}
