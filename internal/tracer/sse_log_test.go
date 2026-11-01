package tracer

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureSSELogger 替换全局 sseConnLogger 为捕获器，测试结束时还原。
func captureSSELogger(t *testing.T) *[]SSEConnLogEntry {
	t.Helper()
	old := sseConnLogger
	captured := &[]SSEConnLogEntry{}
	sseConnLogger = func(e SSEConnLogEntry) { *captured = append(*captured, e) }
	t.Cleanup(func() { sseConnLogger = old })
	return captured
}

func TestSSEDisconnectNote(t *testing.T) {
	got := sseDisconnectNote(1500)
	want := "SSE 连接断开，持续 1500 毫秒"
	if got != want {
		t.Errorf("sseDisconnectNote(1500) = %q, want %q", got, want)
	}
	if got := sseDisconnectNote(0); got != "SSE 连接断开，持续 0 毫秒" {
		t.Errorf("sseDisconnectNote(0) = %q", got)
	}
}

func TestLogSSEConn_NilLoggerIsNoop(t *testing.T) {
	old := sseConnLogger
	sseConnLogger = nil
	t.Cleanup(func() { sseConnLogger = old })
	// 不注册回调时打点应静默丢弃（不 panic）
	logSSEConn(SSEConnLogEntry{Note: "x"})
}

func TestLookupTaskIDForTrace(t *testing.T) {
	withInitTracer(t)

	// 空 traceId / 未知 traceId → 空串
	if got := lookupTaskIDForTrace(""); got != "" {
		t.Errorf("empty traceId should return '', got %q", got)
	}
	if got := lookupTaskIDForTrace("no-such-trace"); got != "" {
		t.Errorf("unknown traceId should return '', got %q", got)
	}

	// 造一条带 taskId 的顶层 span 并等落库
	traceId, runSeq := NewTraceId("task-sse-1")
	spanId := Start(traceId, "task-sse-1", runSeq, TraceTypeUpload, StageUploadTask, "")
	if spanId == "" {
		t.Fatal("Start returned empty spanId")
	}
	End(spanId)
	waitForFlush()

	if got := lookupTaskIDForTrace(traceId); got != "task-sse-1" {
		t.Errorf("lookupTaskIDForTrace = %q, want %q", got, "task-sse-1")
	}
}

// TestHandleStream_LogsConnectAndDisconnect 驱动完整 SSE handler，
// 验证连接建立/断开各写一条日志，且断开条带有存活时长。
func TestHandleStream_LogsConnectAndDisconnect(t *testing.T) {
	withHandlerEnv(t)
	captured := captureSSELogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/task-traces/trace-xyz/stream", nil).WithContext(ctx)
	r.RemoteAddr = "127.0.0.1:56789"
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		HandleStream(w, r)
		close(done)
	}()

	// 等建立条出现（说明 handler 已进入流式循环）
	waitFor(t, func() bool { return len(*captured) >= 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleStream did not return after context cancel")
	}
	// 断开条由 defer 写出，handler 返回后必然已记录
	if len(*captured) != 2 {
		t.Fatalf("expected 2 SSE conn logs, got %d: %+v", len(*captured), *captured)
	}

	conn := (*captured)[0]
	if conn.TraceID != "trace-xyz" {
		t.Errorf("connect entry traceId = %q", conn.TraceID)
	}
	if conn.Path != "/task-traces/trace-xyz/stream" {
		t.Errorf("connect entry path = %q", conn.Path)
	}
	if conn.Peer != "127.0.0.1:56789" {
		t.Errorf("connect entry peer = %q", conn.Peer)
	}
	if conn.Note != "SSE 连接建立" {
		t.Errorf("connect entry note = %q", conn.Note)
	}
	if conn.DurationMs != 0 {
		t.Errorf("connect entry durationMs = %d, want 0", conn.DurationMs)
	}
	if conn.TimestampMs <= 0 {
		t.Errorf("connect entry timestampMs = %d", conn.TimestampMs)
	}

	disc := (*captured)[1]
	if !strings.HasPrefix(disc.Note, "SSE 连接断开，持续 ") {
		t.Errorf("disconnect entry note = %q", disc.Note)
	}
	if disc.DurationMs < 0 {
		t.Errorf("disconnect entry durationMs = %d, want >= 0", disc.DurationMs)
	}
	if disc.TimestampMs < conn.TimestampMs {
		t.Errorf("disconnect timestampMs %d < connect timestampMs %d", disc.TimestampMs, conn.TimestampMs)
	}
	// 该 traceId 在库里没有 span，taskId 尽力解析应为空串
	if disc.TaskID != "" {
		t.Errorf("disconnect entry taskId = %q, want ''", disc.TaskID)
	}
}

// waitFor 轮询条件直到满足或超时（2 秒）。
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}

func TestSpanTraceContext(t *testing.T) {
	withInitTracer(t)

	// 空 spanId / 未知 spanId → not ok
	if _, ok := SpanTraceContext(""); ok {
		t.Error("empty spanId should not be ok")
	}
	if _, ok := SpanTraceContext("no-such-span"); ok {
		t.Error("unknown spanId should not be ok")
	}

	traceId, runSeq := NewTraceId("task-ctx-1")
	spanId := Start(traceId, "task-ctx-1", runSeq, TraceTypeUpload, StageUploadTask, "")
	if spanId == "" {
		t.Fatal("Start returned empty spanId")
	}

	gotTrace, ok := SpanTraceContext(spanId)
	if !ok {
		t.Fatal("active span should be ok")
	}
	if gotTrace != traceId {
		t.Errorf("SpanTraceContext traceId = %q, want %q", gotTrace, traceId)
	}

	// span 结束后不在活跃表 → not ok
	End(spanId)
	if _, ok := SpanTraceContext(spanId); ok {
		t.Error("ended span should not be ok")
	}
}
