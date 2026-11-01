package tracer

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 实时链路追踪 SSE 通道。
//
// 设计目标：
//   - 与现有上传进度 SSE（internal/server/sse_*.go）的通道严格分离，互不干扰
//   - 单订阅者粒度到 traceId（一个前端页面只关心当前打开的那条 trace）
//   - 写入路径（store.writeBatch）成功提交事务后再广播，避免前端看到"假"事件
//   - 客户端断连时立刻清理订阅，防止 goroutine 泄漏
//
// 协议（SSE event payload，JSON 字符串）：
//
//	event: span
//	data : {"op":"insert"|"update", "span": {...完整 Span 字段...}}
//
// 浏览器端 EventSource 监听 'span' 事件即可。同时提供命名事件 'ping'
// 用于 30s 一次的保活心跳，防止反向代理切断。

// traceHub 维护 traceId → 订阅者集合。
type traceHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[*traceSubscriber]struct{}
}

func newTraceHub() *traceHub {
	return &traceHub{
		subscribers: make(map[string]map[*traceSubscriber]struct{}),
	}
}

// traceSubscriber 一个 SSE 连接。events 通道有 buffer，慢消费时丢弃旧事件而不
// 阻塞 broadcast 路径。
type traceSubscriber struct {
	traceId string
	events  chan []byte
	done    chan struct{}
}

func (h *traceHub) subscribe(traceId string) *traceSubscriber {
	sub := &traceSubscriber{
		traceId: traceId,
		events:  make(chan []byte, 256),
		done:    make(chan struct{}),
	}
	h.mu.Lock()
	if h.subscribers[traceId] == nil {
		h.subscribers[traceId] = make(map[*traceSubscriber]struct{})
	}
	h.subscribers[traceId][sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *traceHub) unsubscribe(sub *traceSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subscribers[sub.traceId]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(h.subscribers, sub.traceId)
		}
	}
	close(sub.done)
}

// broadcast 把 payload 推给当前 traceId 的所有订阅者。
//
// 慢订阅者的 events 通道满时丢弃本条事件（非阻塞），优先保证写入路径的吞吐。
func (h *traceHub) broadcast(traceId string, payload []byte) {
	h.mu.RLock()
	subs := h.subscribers[traceId]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	// 复制一份引用切片，再释放锁
	dst := make([]*traceSubscriber, 0, len(subs))
	for s := range subs {
		dst = append(dst, s)
	}
	h.mu.RUnlock()

	for _, s := range dst {
		select {
		case s.events <- payload:
		default:
			// 慢消费：直接丢；前端可以靠手动刷新拉全量补齐
		}
	}
}

// ─── SpanBroadcaster 实现 ───

type spanEvent struct {
	Op   string `json:"op"`
	Span *Span  `json:"span"`
}

func (h *traceHub) BroadcastInsert(s *Span) { h.broadcastSpan("insert", s) }
func (h *traceHub) BroadcastUpdate(s *Span) { h.broadcastSpan("update", s) }

func (h *traceHub) broadcastSpan(op string, s *Span) {
	if h == nil || s == nil || s.TraceID == "" {
		return
	}
	payload, err := json.Marshal(spanEvent{Op: op, Span: s})
	if err != nil {
		log.Printf("[tracer-hub] marshal span event failed: %v", err)
		return
	}
	h.broadcast(s.TraceID, payload)
}

// ─── HTTP handler ───

// HandleStream GET /task-traces/{traceId}/stream
//
// 长连接 SSE。客户端订阅后会收到：
//   - 每条新 span 的 'span' event（insert/update）
//   - 30s 一次的 'ping' event（保活）
//
// 客户端断连或服务端 shutdown 时自动结束。
func HandleStream(w http.ResponseWriter, r *http.Request) {
	if globalTracer == nil {
		notReady(w)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	// 解析 traceId：路径形如 /task-traces/{traceId}/stream
	rest := strings.TrimPrefix(r.URL.Path, "/task-traces/")
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 || parts[1] != "stream" {
		writeError(w, http.StatusBadRequest, "expected /task-traces/{traceId}/stream")
		return
	}
	traceId := parts[0]
	if traceId == "" {
		writeError(w, http.StatusBadRequest, "missing traceId")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 关闭代理缓冲（如有）
	w.WriteHeader(http.StatusOK)

	// 立刻发一行注释让 EventSource 进入 'open' 状态
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	// 显式打点：/task-traces/* 被 httpLogMiddleware 排除，SSE 连接生命周期由这里
	// 手动写入 http_logs（每连接 2 条：建立 + 断开）。断开用 defer 记录，
	// 保证 panic 路径也能留下断开日志。
	connectMs := time.Now().UnixMilli()
	taskId := lookupTaskIDForTrace(traceId)
	logSSEConn(SSEConnLogEntry{
		TraceID: traceId, TaskID: taskId, Path: r.URL.Path, Peer: r.RemoteAddr,
		TimestampMs: connectMs, DurationMs: 0, Note: "SSE 连接建立",
	})
	defer func() {
		disconnectMs := time.Now().UnixMilli()
		aliveMs := disconnectMs - connectMs
		logSSEConn(SSEConnLogEntry{
			TraceID: traceId, TaskID: taskId, Path: r.URL.Path, Peer: r.RemoteAddr,
			TimestampMs: disconnectMs, DurationMs: aliveMs, Note: sseDisconnectNote(aliveMs),
		})
	}()

	sub := globalTracer.hub.subscribe(traceId)
	defer globalTracer.hub.unsubscribe(sub)

	// 心跳 30s
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.done:
			return
		case payload := <-sub.events:
			// SSE event 格式：event: span\ndata: <json>\n\n
			_, _ = w.Write([]byte("event: span\ndata: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-ping.C:
			_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
			flusher.Flush()
		}
	}
}
