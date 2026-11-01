package tracer

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore 在测试临时目录中创建一个独立的 Store，并在测试结束时关闭。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := openStore(dir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	t.Cleanup(func() {
		s.Shutdown(2 * time.Second)
	})
	return s
}

// makeSpan 构造一个最小可入库的 span。
func makeSpan(spanId, traceId, taskId string, runSeq int, stage string, success bool) *Span {
	now := time.Now().UnixMilli()
	return &Span{
		SpanID:      spanId,
		TraceID:     traceId,
		TaskID:      taskId,
		RunSeq:      runSeq,
		TraceType:   TraceTypeUpload,
		Stage:       stage,
		StartMs:     now,
		EndMs:       now + 100,
		DurationMs:  100,
		Success:     success,
		CreatedAtMs: now,
	}
}

// flushAndWait 同步等待异步写入完成。
//
// 由于 batch flush 由 timer 或 batch size 触发，简单 sleep 加上一次空转可以
// 把队列彻底冲刷掉。500 ms 是 storeBatchMaxLatency 的等量值，留 200 ms 余量。
func flushAndWait() {
	time.Sleep(storeBatchMaxLatency + 200*time.Millisecond)
}

// countRows 直连 DB 查询 task_traces 行数（绕过批量队列）。
func countRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM task_traces`).Scan(&n); err != nil {
		t.Fatalf("count rows failed: %v", err)
	}
	return n
}

func TestStore_OpenAndSchema(t *testing.T) {
	s := newTestStore(t)

	// 直接尝试一条 INSERT，能跑就说明 schema OK
	sp := makeSpan("a1", "t1", "task-a", 1, StageUploadFile, true)
	if err := s.writeInsertBatch([]*Span{sp}); err != nil {
		t.Fatalf("schema test insert failed: %v", err)
	}

	// 索引存在性：从 sqlite_master 查
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='task_traces'`)
	if err != nil {
		t.Fatalf("query indexes failed: %v", err)
	}
	defer rows.Close()
	want := map[string]bool{
		"ix_traces_trace_id":       false,
		"ix_traces_task_id":        false,
		"ix_traces_parent_span_id": false,
		"ix_traces_span_id":        false,
		"ix_traces_type_start":     false,
		"ix_traces_created_at":     false,
	}
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("missing expected index: %s", k)
		}
	}
}

func TestStore_NextRunSeq_Increments(t *testing.T) {
	s := newTestStore(t)

	if got := s.NextRunSeq("never-existed"); got != 1 {
		t.Errorf("first call should return 1, got %d", got)
	}

	// 直接写入两条，用 writeBatch 同步入库，避开异步等待
	a := makeSpan("a", "ta", "task-x", 3, StageUploadTask, true)
	b := makeSpan("b", "tb", "task-x", 7, StageUploadTask, true)
	if err := s.writeInsertBatch([]*Span{a, b}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	if got := s.NextRunSeq("task-x"); got != 8 {
		t.Errorf("expected NextRunSeq=8, got %d", got)
	}
}

func TestStore_Enqueue_TimerFlush(t *testing.T) {
	s := newTestStore(t)

	// 投递 5 条不到 batch size 的 span，等 timer 触发
	for i := 0; i < 5; i++ {
		sp := makeSpan(fmt.Sprintf("s%d", i), "trace-tf", "task-tf", 1, StageChunkUpload, true)
		s.Enqueue(sp)
	}
	flushAndWait()

	if got := countRows(t, s); got != 5 {
		t.Errorf("expected 5 rows after timer flush, got %d", got)
	}
}

func TestStore_Enqueue_BatchSizeFlush(t *testing.T) {
	s := newTestStore(t)

	// 投递正好 batch size，触发立即 flush（不依赖 timer）
	for i := 0; i < storeBatchMaxSize; i++ {
		sp := makeSpan(fmt.Sprintf("b%d", i), "trace-bf", "task-bf", 1, StageChunkUpload, true)
		s.Enqueue(sp)
	}
	// 等一小段以确保 goroutine 调度完成（不需要等满 timer）
	time.Sleep(300 * time.Millisecond)

	if got := countRows(t, s); got != storeBatchMaxSize {
		t.Errorf("expected %d rows after batch-size flush, got %d", storeBatchMaxSize, got)
	}
}

func TestStore_Enqueue_ChannelFull_DropsAndCounts(t *testing.T) {
	// 构造一个不启动 writer goroutine 的 Store，避免 channel 被实时排空。
	// 这里只验证 Enqueue 在 channel 满时的丢弃语义。
	s := &Store{
		writeCh: make(chan spanWriteOp, 4),
		closeCh: make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	// 投递 10 条 → 4 条入队，6 条被丢弃
	for i := 0; i < 10; i++ {
		sp := makeSpan(fmt.Sprintf("c%d", i), "trace-of", "task-of", 1, StageChunkUpload, true)
		s.Enqueue(sp)
	}
	dropped := s.Dropped()
	if dropped != 6 {
		t.Errorf("expected 6 dropped, got %d", dropped)
	}

	// nil span 与 nil store 应安全
	s.Enqueue(nil)
	var nilStore *Store
	nilStore.Enqueue(makeSpan("x", "y", "z", 1, StageOther, true))
	if nilStore.Dropped() != 0 {
		t.Error("nil store Dropped() should be 0")
	}
}

func TestStore_Shutdown_FlushesPending(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}

	const count = 50
	for i := 0; i < count; i++ {
		sp := makeSpan(fmt.Sprintf("p%d", i), "trace-sd", "task-sd", 1, StageChunkUpload, true)
		s.Enqueue(sp)
	}

	// 立即 Shutdown，验证 drainAndExit 把队列里的 50 条全部冲入磁盘
	s.Shutdown(3 * time.Second)

	// Shutdown 后 db 已经关闭，重新打开一个 store 验证持久化结果
	s2, err := openStore(dir)
	if err != nil {
		t.Fatalf("re-open failed: %v", err)
	}
	defer s2.Shutdown(2 * time.Second)

	got := countRows(t, s2)
	if got != count {
		t.Errorf("expected %d rows after shutdown flush, got %d", count, got)
	}
}

func TestStore_Cleanup_ByAge(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().UnixMilli()
	old := now - 8*24*60*60*1000 // 8 天前

	// 直接 writeBatch 控制 created_at_ms
	oldSpan := makeSpan("old", "trace-old", "task-z", 1, StageChunkUpload, true)
	oldSpan.CreatedAtMs = old
	freshSpan := makeSpan("fresh", "trace-fresh", "task-z", 1, StageChunkUpload, true)
	freshSpan.CreatedAtMs = now

	if err := s.writeInsertBatch([]*Span{oldSpan, freshSpan}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	deleted, err := s.Cleanup(7, 0) // 保留 7 天
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 row deleted by age, got %d", deleted)
	}
	if got := countRows(t, s); got != 1 {
		t.Errorf("expected 1 row remaining, got %d", got)
	}
}

func TestStore_Cleanup_ByRowCount_KeepsNewest(t *testing.T) {
	s := newTestStore(t)

	// 写 10 条 created_at_ms 递增的 span
	base := time.Now().UnixMilli()
	spans := make([]*Span, 0, 10)
	for i := 0; i < 10; i++ {
		sp := makeSpan(fmt.Sprintf("r%d", i), "trace-rc", "task-rc", 1, StageChunkUpload, true)
		sp.CreatedAtMs = base + int64(i*1000) // i=0 最旧, i=9 最新
		spans = append(spans, sp)
	}
	if err := s.writeInsertBatch(spans); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	deleted, err := s.Cleanup(0, 6) // 保留最新 6 条
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 4 {
		t.Errorf("expected 4 deleted, got %d", deleted)
	}
	if got := countRows(t, s); got != 6 {
		t.Errorf("expected 6 rows remaining, got %d", got)
	}

	// 验证保留的是最新的 6 条（spanId r4..r9）
	rows, err := s.db.Query(`SELECT span_id FROM task_traces ORDER BY created_at_ms ASC`)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()
	want := []string{"r4", "r5", "r6", "r7", "r8", "r9"}
	got := make([]string, 0, 6)
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		got = append(got, id)
	}
	if !equalStringSlices(want, got) {
		t.Errorf("kept wrong spans: want %v, got %v", want, got)
	}
}

func TestStore_Cleanup_NoOpWhenBothZero(t *testing.T) {
	s := newTestStore(t)
	sp := makeSpan("k", "trace", "task", 1, StageUploadFile, true)
	_ = s.writeInsertBatch([]*Span{sp})

	deleted, err := s.Cleanup(0, 0)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted with both args zero, got %d", deleted)
	}
}

// ─── 查询 API ─────────────────────────────────────────────

func seedSpans(t *testing.T, s *Store) {
	t.Helper()
	now := time.Now().UnixMilli()
	spans := []*Span{
		// trace-A：2 个顶层 + 3 个子（1 失败）
		{SpanID: "A-top1", TraceID: "trace-A", TaskID: "task-A", RunSeq: 1,
			TraceType: TraceTypeUpload, Stage: StageUploadTask,
			StartMs: now, EndMs: now + 1000, DurationMs: 1000, Success: true,
			CreatedAtMs: now},
		{SpanID: "A-top2", TraceID: "trace-A", TaskID: "task-A", RunSeq: 1,
			TraceType: TraceTypeUpload, Stage: StageNotifyComplete,
			StartMs: now + 1000, EndMs: now + 1200, DurationMs: 200, Success: true,
			CreatedAtMs: now},
		{SpanID: "A-c1", ParentSpanID: "A-top1", TraceID: "trace-A", TaskID: "task-A", RunSeq: 1,
			TraceType: TraceTypeUpload, Stage: StageUploadFile, Filename: "a.txt",
			StartMs: now, EndMs: now + 500, DurationMs: 500, Success: true,
			CreatedAtMs: now},
		{SpanID: "A-c2", ParentSpanID: "A-top1", TraceID: "trace-A", TaskID: "task-A", RunSeq: 1,
			TraceType: TraceTypeUpload, Stage: StageUploadFile, Filename: "b.txt",
			StartMs: now + 100, EndMs: now + 300, DurationMs: 200, Success: false,
			ErrorMessage: "boom", CreatedAtMs: now},
		{SpanID: "A-c3", ParentSpanID: "A-c1", TraceID: "trace-A", TaskID: "task-A", RunSeq: 1,
			TraceType: TraceTypeUpload, Stage: StageMD5Calc, Filename: "a.txt",
			StartMs: now, EndMs: now + 50, DurationMs: 50, Success: true,
			CreatedAtMs: now},

		// trace-B：1 个顶层成功
		{SpanID: "B-top1", TraceID: "trace-B", TaskID: "task-B", RunSeq: 1,
			TraceType: TraceTypeScan, Stage: StageFileScan,
			StartMs: now - 5000, EndMs: now - 4000, DurationMs: 1000, Success: true,
			CreatedAtMs: now - 5000},
	}
	if err := s.writeInsertBatch(spans); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
}

func TestStore_GetTopSpans(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	tops, err := s.GetTopSpans("trace-A")
	if err != nil {
		t.Fatalf("GetTopSpans failed: %v", err)
	}
	if len(tops) != 2 {
		t.Fatalf("expected 2 top spans, got %d", len(tops))
	}
	if tops[0].SpanID != "A-top1" || tops[1].SpanID != "A-top2" {
		t.Errorf("ordering wrong, got %v", []string{tops[0].SpanID, tops[1].SpanID})
	}
}

func TestStore_GetChildren(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	kids, err := s.GetChildren("A-top1")
	if err != nil {
		t.Fatalf("GetChildren failed: %v", err)
	}
	if len(kids) != 2 {
		t.Fatalf("expected 2 children, got %d", len(kids))
	}
	// 失败 span 的 ErrorMessage 应保留
	for _, k := range kids {
		if k.SpanID == "A-c2" && k.ErrorMessage != "boom" {
			t.Errorf("error message lost, got %q", k.ErrorMessage)
		}
	}
}

func TestStore_GetAllSpans_Truncation(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	// trace-A 有 5 条，limit=3 应该 truncated=true
	all, truncated, err := s.GetAllSpans("trace-A", 3)
	if err != nil {
		t.Fatalf("GetAllSpans failed: %v", err)
	}
	if !truncated {
		t.Error("expected truncated=true")
	}
	if len(all) != 3 {
		t.Errorf("expected len=3 when limit=3, got %d", len(all))
	}

	// limit 足够大时 truncated=false
	all2, truncated2, _ := s.GetAllSpans("trace-A", 100)
	if truncated2 {
		t.Error("expected truncated=false when limit > total")
	}
	if len(all2) != 5 {
		t.Errorf("expected 5 spans for trace-A, got %d", len(all2))
	}
}

func TestStore_GetSpan_NotFound(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.GetSpan("ghost"); err == nil {
		t.Error("expected error for unknown spanId")
	}
}

func TestStore_DeleteTrace(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	n, err := s.DeleteTrace("trace-A")
	if err != nil {
		t.Fatalf("DeleteTrace failed: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 deleted, got %d", n)
	}
	if got := countRows(t, s); got != 1 {
		t.Errorf("expected 1 row remaining (trace-B), got %d", got)
	}
}

func TestStore_ListTraces_Aggregation(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	res, total, err := s.ListTraces(ListTracesQuery{Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("ListTraces failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 traces total, got %d", total)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(res))
	}

	// trace-A: 5 spans, 1 failure
	var a *TraceSummary
	for i := range res {
		if res[i].TraceID == "trace-A" {
			a = &res[i]
		}
	}
	if a == nil {
		t.Fatal("trace-A missing in summaries")
	}
	if a.SpanCount != 5 {
		t.Errorf("trace-A spanCount expected 5, got %d", a.SpanCount)
	}
	if !a.HasFailure {
		t.Error("trace-A should have hasFailure=true")
	}
	if a.TaskID != "task-A" {
		t.Errorf("trace-A taskId expected task-A, got %s", a.TaskID)
	}
}

func TestStore_ListTraces_FilterByTraceType(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	res, total, err := s.ListTraces(ListTracesQuery{TraceType: TraceTypeScan, Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("filter by traceType failed: %v", err)
	}
	if total != 1 || len(res) != 1 {
		t.Fatalf("expected exactly 1 scan trace, got total=%d len=%d", total, len(res))
	}
	if res[0].TraceID != "trace-B" {
		t.Errorf("expected trace-B, got %s", res[0].TraceID)
	}
}

func TestStore_ListTraces_FilterByTaskId(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	res, total, _ := s.ListTraces(ListTracesQuery{TaskID: "task-A"})
	if total != 1 || len(res) != 1 {
		t.Fatalf("expected 1 trace for task-A, got total=%d", total)
	}
}

func TestStore_ListTraces_PaginationBounds(t *testing.T) {
	s := newTestStore(t)
	// 写 5 条不同 traceId，每条 1 个 span
	now := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		sp := &Span{
			SpanID: fmt.Sprintf("p%d", i), TraceID: fmt.Sprintf("trace-%d", i),
			TaskID: "task-p", RunSeq: 1, TraceType: TraceTypeUpload, Stage: StageUploadTask,
			StartMs: now + int64(i*10), EndMs: now + int64(i*10+5),
			DurationMs: 5, Success: true, CreatedAtMs: now,
		}
		_ = s.writeInsertBatch([]*Span{sp})
	}

	// page=0 → 应被规范成 1；size=1000 → 截到 200（这里只有 5 条所以无影响）
	res, total, err := s.ListTraces(ListTracesQuery{Page: 0, Size: 1000})
	if err != nil {
		t.Fatalf("ListTraces failed: %v", err)
	}
	if total != 5 {
		t.Errorf("expected 5 traces total, got %d", total)
	}
	if len(res) != 5 {
		t.Errorf("expected 5 returned, got %d", len(res))
	}

	// 第 2 页，size=2，应该有 2 条
	res, _, _ = s.ListTraces(ListTracesQuery{Page: 2, Size: 2})
	if len(res) != 2 {
		t.Errorf("page=2 size=2 should return 2 rows, got %d", len(res))
	}
}

func TestStore_ListTraces_FilterByTimeRange(t *testing.T) {
	s := newTestStore(t)
	seedSpans(t, s)

	now := time.Now().UnixMilli()
	// 只查最近 1 秒内的 trace（trace-B 是 5 秒前，应被排除）
	res, total, _ := s.ListTraces(ListTracesQuery{FromMs: now - 1000})
	if total != 1 || len(res) != 1 {
		t.Fatalf("expected 1 trace, got total=%d", total)
	}
	if res[0].TraceID != "trace-A" {
		t.Errorf("expected trace-A, got %s", res[0].TraceID)
	}
}

// ─── helpers ─────────────────────────────────────────────

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 防 import "filepath" 未使用警告（部分平台路径处理需要）
var _ = filepath.ToSlash

