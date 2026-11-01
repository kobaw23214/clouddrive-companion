package server

import (
	"clouddrive-companion-withWails2/internal/tracer"
	"testing"
)

// TestSSEConnEventToHTTPLog 验证 SSE 连接事件 → HTTPRequestLog 的字段映射。
func TestSSEConnEventToHTTPLog(t *testing.T) {
	connect := sseConnEventToHTTPLog(tracer.SSEConnLogEntry{
		TraceID:     "trace-1",
		TaskID:      "task-1",
		Path:        "/task-traces/trace-1/stream",
		Peer:        "127.0.0.1:43210",
		TimestampMs: 1753000000000,
		DurationMs:  0,
		Note:        "SSE 连接建立",
	})

	if connect.Method != "GET" {
		t.Errorf("method = %q, want GET", connect.Method)
	}
	if connect.Direction != "in" {
		t.Errorf("direction = %q, want in", connect.Direction)
	}
	if connect.StatusCode != 200 {
		t.Errorf("statusCode = %d, want 200", connect.StatusCode)
	}
	if connect.Path != "/task-traces/trace-1/stream" {
		t.Errorf("path = %q", connect.Path)
	}
	if connect.Peer != "127.0.0.1:43210" || connect.RemoteAddr != "127.0.0.1:43210" {
		t.Errorf("peer/remoteAddr = %q/%q", connect.Peer, connect.RemoteAddr)
	}
	if connect.TaskID != "task-1" {
		t.Errorf("taskId = %q", connect.TaskID)
	}
	if connect.ResponseBody != "SSE 连接建立" {
		t.Errorf("responseBody = %q, want 说明文字", connect.ResponseBody)
	}
	if connect.DurationMs != 0 {
		t.Errorf("durationMs = %d, want 0", connect.DurationMs)
	}
	if connect.TimestampMs != 1753000000000 {
		t.Errorf("timestampMs = %d", connect.TimestampMs)
	}
	if connect.Timestamp == "" {
		t.Error("timestamp 可读时间不应为空")
	}

	// 断开条：durationMs 带存活时长，ID 自增
	disconnect := sseConnEventToHTTPLog(tracer.SSEConnLogEntry{
		TraceID:     "trace-1",
		Path:        "/task-traces/trace-1/stream",
		Peer:        "127.0.0.1:43210",
		TimestampMs: 1753000060000,
		DurationMs:  60000,
		Note:        "SSE 连接断开，持续 60000 毫秒",
	})
	if disconnect.DurationMs != 60000 {
		t.Errorf("disconnect durationMs = %d, want 60000", disconnect.DurationMs)
	}
	if disconnect.ID <= connect.ID {
		t.Errorf("ID 应自增: connect=%d disconnect=%d", connect.ID, disconnect.ID)
	}
}
