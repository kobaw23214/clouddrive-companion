package server

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"clouddrive-companion-withWails2/internal/api"

	_ "modernc.org/sqlite"
)

// resetHTTPLogStore 每个用例前后都把全局 store 复位，避免用例间串扰
func resetHTTPLogStore(t *testing.T) {
	t.Helper()
	if globalHTTPLogStore != nil {
		_ = globalHTTPLogStore.db.Close()
		globalHTTPLogStore = nil
	}
	t.Cleanup(func() {
		if globalHTTPLogStore != nil {
			_ = globalHTTPLogStore.db.Close()
			globalHTTPLogStore = nil
		}
	})
}

// waitFlush 等待异步 writer 把 channel 里的日志批量落盘（200ms 一批，留足余量）
func waitFlush() {
	time.Sleep(500 * time.Millisecond)
}

func appendTestLog(e HTTPRequestLog) {
	if e.RequestHeaders == nil {
		e.RequestHeaders = map[string][]string{}
	}
	if e.ResponseHeaders == nil {
		e.ResponseHeaders = map[string][]string{}
	}
	AppendHTTPLogToDisk(e)
}

// 老库平滑迁移：用旧版 schema（无 direction/peer/task_id）建库，
// Init 后老数据可读（direction 落默认值 'in'），新数据能带新字段写入
func TestMigrateAddsDirectionPeerTaskID(t *testing.T) {
	dir := t.TempDir()
	resetHTTPLogStore(t)

	// 先手工建一个旧版 schema 的库（迁移前的列结构）
	dbPath := filepath.Join(dir, "http_logs.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)",
		filepath.ToSlash(dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE http_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		middleware_id INTEGER,
		timestamp TEXT NOT NULL,
		timestamp_ms INTEGER NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		query TEXT,
		remote_addr TEXT,
		request_headers TEXT NOT NULL,
		request_body TEXT,
		request_body_truncated INTEGER NOT NULL DEFAULT 0,
		status_code INTEGER NOT NULL,
		response_headers TEXT NOT NULL,
		response_body TEXT,
		response_body_truncated INTEGER NOT NULL DEFAULT 0,
		response_bytes INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL
	);`)
	if err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO http_logs (
		middleware_id, timestamp, timestamp_ms, method, path, query, remote_addr,
		request_headers, request_body, request_body_truncated,
		status_code, response_headers, response_body, response_body_truncated,
		response_bytes, duration_ms
	) VALUES (1, '2026-07-27 10:00:00.000', 1000, 'GET', '/old', NULL, '127.0.0.1:1',
		'{}', NULL, 0, 200, '{}', NULL, 0, 0, 5)`)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	_ = db.Close()

	// Init 应触发迁移补列
	if err := InitHTTPLogStore(dir); err != nil {
		t.Fatalf("InitHTTPLogStore: %v", err)
	}

	// 老行可读，direction 走默认值 'in'
	entries, total, err := QuerySequenceEvents(0, 0, "", "", 1, 10)
	if err != nil {
		t.Fatalf("QuerySequenceEvents: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("老库应有 1 行, total=%d len=%d", total, len(entries))
	}
	if entries[0].Direction != "in" {
		t.Errorf("老行 direction 应默认 'in', got %q", entries[0].Direction)
	}

	// 新行带新字段写入、按 taskId+direction 过滤能命中
	appendTestLog(HTTPRequestLog{
		Timestamp: "2026-07-27 10:00:01.000", TimestampMs: 2000,
		Method: "POST", Path: "/dataImport/upload/chunk",
		StatusCode: 200, Direction: "out", Peer: "localhost:8081", TaskID: "task-1",
	})
	waitFlush()

	entries, total, err = QuerySequenceEvents(0, 0, "task-1", "out", 1, 10)
	if err != nil {
		t.Fatalf("QuerySequenceEvents: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("应命中 1 条出站日志, total=%d len=%d", total, len(entries))
	}
	if entries[0].Peer != "localhost:8081" || entries[0].TaskID != "task-1" {
		t.Errorf("新字段读写异常: %+v", entries[0])
	}
}

// QuerySequenceEvents：时间范围 / taskId / direction 过滤、正序排序、分页 total
func TestQuerySequenceEvents(t *testing.T) {
	dir := t.TempDir()
	resetHTTPLogStore(t)
	if err := InitHTTPLogStore(dir); err != nil {
		t.Fatalf("InitHTTPLogStore: %v", err)
	}

	appendTestLog(HTTPRequestLog{Timestamp: "t", TimestampMs: 3000, Method: "GET", Path: "/c", StatusCode: 200, Direction: "in", TaskID: "task-1"})
	appendTestLog(HTTPRequestLog{Timestamp: "t", TimestampMs: 1000, Method: "GET", Path: "/a", StatusCode: 200, Direction: "in", TaskID: "task-1"})
	appendTestLog(HTTPRequestLog{Timestamp: "t", TimestampMs: 2000, Method: "POST", Path: "/b", StatusCode: 200, Direction: "out", TaskID: "task-1"})
	appendTestLog(HTTPRequestLog{Timestamp: "t", TimestampMs: 4000, Method: "GET", Path: "/d", StatusCode: 200, Direction: "out", TaskID: "task-2"})
	waitFlush()

	// 正序：1000 → 2000 → 3000
	entries, total, err := QuerySequenceEvents(0, 0, "task-1", "", 1, 10)
	if err != nil {
		t.Fatalf("QuerySequenceEvents: %v", err)
	}
	if total != 3 || len(entries) != 3 {
		t.Fatalf("task-1 应有 3 条, total=%d len=%d", total, len(entries))
	}
	for i, want := range []int64{1000, 2000, 3000} {
		if entries[i].TimestampMs != want {
			t.Errorf("第 %d 条应为 timestampMs=%d（正序）, got %d", i, want, entries[i].TimestampMs)
		}
	}

	// direction 过滤
	_, total, _ = QuerySequenceEvents(0, 0, "", "out", 1, 10)
	if total != 2 {
		t.Errorf("direction=out 应有 2 条, got %d", total)
	}

	// 时间范围过滤 [1500, 3500)
	entries, total, _ = QuerySequenceEvents(1500, 3500, "", "", 1, 10)
	if total != 2 || entries[0].TimestampMs != 2000 || entries[1].TimestampMs != 3000 {
		t.Errorf("时间范围过滤异常: total=%d entries=%+v", total, entries)
	}

	// 分页：size=2 第 2 页只剩 1 条，total 仍是全量
	entries, total, _ = QuerySequenceEvents(0, 0, "task-1", "", 2, 2)
	if total != 3 || len(entries) != 1 || entries[0].TimestampMs != 3000 {
		t.Errorf("分页异常: total=%d entries=%+v", total, entries)
	}
}

// LogOutboundHTTP：api 出站日志转换成 direction=out 的 HTTPRequestLog 落库，
// 网络错误的 Error 串进 responseBody
func TestLogOutboundHTTP(t *testing.T) {
	dir := t.TempDir()
	resetHTTPLogStore(t)
	if err := InitHTTPLogStore(dir); err != nil {
		t.Fatalf("InitHTTPLogStore: %v", err)
	}

	LogOutboundHTTP(api.OutboundLogEntry{
		TimestampMs: 5000, Method: "GET", Path: "/dataImport/upload/command/t1",
		Peer: "localhost:8081", StatusCode: 0, TaskID: "t1",
		Error: "dial tcp: connection refused",
	})
	waitFlush()

	entries, total, err := QuerySequenceEvents(0, 0, "t1", "out", 1, 10)
	if err != nil {
		t.Fatalf("QuerySequenceEvents: %v", err)
	}
	if total != 1 {
		t.Fatalf("应有 1 条出站日志, got %d", total)
	}
	e := entries[0]
	if e.Direction != "out" || e.Peer != "localhost:8081" || e.StatusCode != 0 {
		t.Errorf("出站日志字段异常: %+v", e)
	}
	if e.ResponseBody != "dial tcp: connection refused" {
		t.Errorf("网络错误串应写入 responseBody, got %q", e.ResponseBody)
	}
}
