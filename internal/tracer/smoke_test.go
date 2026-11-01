package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"fmt"
	"testing"
	"time"
)

// 端到端"内核可用"烟雾测试：在不依赖任何业务包的情况下，
// 把任务 7 即将插入的埋点流程完整模拟一遍：
//
//	 file_scan (顶层独立 trace)
//	 upload_task (顶层)
//	  ├─ upload_file (foo.txt)
//	  │   ├─ md5_calc
//	  │   ├─ check_chunks
//	  │   └─ chunk_upload × N
//	  ├─ upload_file (bar.bin) [失败]
//	  │   └─ md5_calc [失败]
//	  └─ notify_complete
//
// 通过后说明 tracer 的内核 + 持久化 + 查询路径都已就绪，可以放心进入任务 7。

func TestSmoke_EndToEndUploadLikeFlow(t *testing.T) {
	withInitTracer(t)

	taskId := "smoke-task-1"

	// ──── 1. file_scan：独立的扫描 trace ────
	scanTraceId, _ := NewTraceId("")
	scanSpan := Start(scanTraceId, "", 1, TraceTypeScan, StageFileScan, "/some/dir")
	if scanSpan == "" {
		t.Fatal("scan span not created")
	}
	EndWithExtra(scanSpan, `{"filesFound":2,"dirsFound":1}`)

	// ──── 2. upload_task：本次任务的根 trace ────
	traceId, runSeq := NewTraceId(taskId)
	if runSeq != 1 {
		t.Fatalf("first run should be seq=1, got %d", runSeq)
	}
	taskSpan := Start(traceId, taskId, runSeq, TraceTypeUpload, StageUploadTask, "smoke")
	if taskSpan == "" {
		t.Fatal("upload_task span not created")
	}

	// ──── 3. upload_file (foo.txt) 完整链路 ────
	fooSpan := StartChild(taskSpan, StageUploadFile, "foo.txt")

	md5Span := StartChild(fooSpan, StageMD5Calc, "foo.txt")
	End(md5Span)

	chkSpan := StartChild(fooSpan, StageCheckChunks, "foo.txt")
	EndWithExtra(chkSpan, `{"completedChunks":2,"initialBytes":2048}`)

	for i := int64(1); i <= 5; i++ {
		c := StartChunkUpload(fooSpan, i, 5, "foo.txt")
		if c == "" {
			t.Fatalf("chunk %d span empty", i)
		}
		EndWithExtra(c, fmt.Sprintf(`{"chunkNumber":%d,"totalChunks":5}`, i))
	}
	End(fooSpan)

	// ──── 4. upload_file (bar.bin)：md5 失败导致整个文件失败 ────
	barSpan := StartChild(taskSpan, StageUploadFile, "bar.bin")
	barMD5 := StartChild(barSpan, StageMD5Calc, "bar.bin")
	Fail(barMD5, "io: file truncated")
	Fail(barSpan, "md5 calculation failed")

	// ──── 5. notify_complete ────
	notifySpan := StartChild(taskSpan, StageNotifyComplete, "")
	End(notifySpan)

	// ──── 6. upload_task 收尾 ────
	EndWithExtra(taskSpan, `{"totalFiles":2,"completed":1,"failed":1,"totalBytes":12345}`)
	CleanupTrace(traceId)

	waitForFlush()

	// ──── 验证持久化结果 ────

	// 6.1 全 span 数量 = 1 (scan) + 1 (task) + 8 (foo) + 2 (bar) + 1 (notify) = 13
	//   foo: upload_file + md5 + check_chunks + 5 × chunk_upload = 8
	//   bar: upload_file + md5_calc(failed) = 2
	totalRows := countRows(t, globalTracer.store)
	const expected = 13
	if totalRows != expected {
		t.Errorf("expected %d total rows, got %d", expected, totalRows)
	}

	// 6.2 trace list 应有两条 trace
	summaries, total, err := globalTracer.store.ListTraces(ListTracesQuery{})
	if err != nil {
		t.Fatalf("ListTraces failed: %v", err)
	}
	if total != 2 || len(summaries) != 2 {
		t.Fatalf("expected 2 traces, got total=%d len=%d", total, len(summaries))
	}

	// 6.3 upload trace 必须 hasFailure=true、scan trace 必须 hasFailure=false
	var uploadSummary, scanSummary *TraceSummary
	for i := range summaries {
		switch summaries[i].TraceType {
		case TraceTypeUpload:
			uploadSummary = &summaries[i]
		case TraceTypeScan:
			scanSummary = &summaries[i]
		}
	}
	if uploadSummary == nil || scanSummary == nil {
		t.Fatal("missing upload/scan summary")
	}
	if !uploadSummary.HasFailure {
		t.Error("upload trace should have hasFailure=true")
	}
	if scanSummary.HasFailure {
		t.Error("scan trace should not have hasFailure")
	}
	if uploadSummary.TaskID != taskId {
		t.Errorf("upload trace taskId mismatch: %s vs %s", uploadSummary.TaskID, taskId)
	}
	if uploadSummary.RunSeq != 1 {
		t.Errorf("upload trace runSeq=%d, expected 1", uploadSummary.RunSeq)
	}
	if uploadSummary.SpanCount != 12 { // 13 - 1 (scan span)
		t.Errorf("upload trace spanCount=%d, expected 12", uploadSummary.SpanCount)
	}

	// 6.4 顶层 span 检查：scan trace 有 1 个、upload trace 有 1 个
	scanTops, _ := globalTracer.store.GetTopSpans(scanTraceId)
	if len(scanTops) != 1 || scanTops[0].Stage != StageFileScan {
		t.Errorf("scan top spans wrong: %+v", scanTops)
	}
	uploadTops, _ := globalTracer.store.GetTopSpans(traceId)
	if len(uploadTops) != 1 || uploadTops[0].Stage != StageUploadTask {
		t.Errorf("upload top spans wrong: %+v", uploadTops)
	}

	// 6.5 upload_task 的子 span 应该有 3 个（foo, bar, notify）
	taskKids, _ := globalTracer.store.GetChildren(uploadTops[0].SpanID)
	if len(taskKids) != 3 {
		t.Errorf("upload_task should have 3 direct children, got %d", len(taskKids))
	}

	// 6.6 foo.txt 的子 span 应该有 5 个 chunk + 1 md5 + 1 check_chunks = 7
	var fooSpanRow *Span
	for i := range taskKids {
		if taskKids[i].Filename == "foo.txt" {
			fooSpanRow = &taskKids[i]
		}
	}
	if fooSpanRow == nil {
		t.Fatal("foo span not found in children")
	}
	fooKids, _ := globalTracer.store.GetChildren(fooSpanRow.SpanID)
	if len(fooKids) != 7 {
		t.Errorf("foo.txt should have 7 child spans, got %d", len(fooKids))
	}

	// 6.7 失败 span 的 ErrorMessage 完整保留
	allFooKids, _, _ := globalTracer.store.GetAllSpans(traceId, 5000)
	foundFailure := false
	for _, sp := range allFooKids {
		if sp.Stage == StageMD5Calc && !sp.Success {
			if sp.ErrorMessage != "io: file truncated" {
				t.Errorf("error message corrupted: %q", sp.ErrorMessage)
			}
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Error("failed md5_calc span not found")
	}

	// 6.8 active span 表应被清空
	if c := activeCountForTest(); c != 0 {
		t.Errorf("activeCount should be 0 after smoke flow, got %d", c)
	}
}

// TestSmoke_DisabledModule_NoSpansWritten 验证：关掉 upload 模块后，整个上传链路
// 一行 span 都不入库，但调用方使用 defer End 不会报错。
func TestSmoke_DisabledModule_NoSpansWritten(t *testing.T) {
	withInitTracer(t)
	if !SetModuleEnabled(TraceTypeUpload, false) {
		t.Fatal("SetModuleEnabled false failed")
	}

	traceId, runSeq := NewTraceId("disabled-task")
	taskSpan := Start(traceId, "disabled-task", runSeq, TraceTypeUpload, StageUploadTask, "")
	if taskSpan != "" {
		t.Errorf("upload module disabled but Start returned %q", taskSpan)
	}
	// 业务侧惯用法：拿到空 spanId 也照样 defer End / Child，全部应安全 no-op
	defer End(taskSpan)
	defer EndWithExtra(taskSpan, `{}`)

	for i := int64(1); i <= 100; i++ {
		c := StartChunkUpload(taskSpan, i, 100, "f")
		if c != "" {
			t.Errorf("chunk span unexpectedly created when module disabled: %q", c)
		}
		End(c)
	}

	waitForFlush()
	if rows := countRows(t, globalTracer.store); rows != 0 {
		t.Errorf("expected 0 rows when module disabled, got %d", rows)
	}
}

// TestSmoke_GlobalDisabled_RoundTripSafe 验证全局开关切换的"立即生效"特性，
// 以及在采集打开时已经创建的 span，关闭后 End 仍然能正确入库。
func TestSmoke_GlobalDisabled_RoundTripSafe(t *testing.T) {
	withInitTracer(t)

	// 全局开 → 创建 span
	traceId, _ := NewTraceId("rt-task")
	taskSpan := Start(traceId, "rt-task", 1, TraceTypeUpload, StageUploadTask, "")
	if taskSpan == "" {
		t.Fatal("Start unexpectedly empty under enabled state")
	}

	// 中途关全局
	config.AppConfig.TraceEnabled = false

	// 1) 已经在飞行中的 span End 必须能正常入库
	End(taskSpan)
	waitForFlush()
	if rows := countRows(t, globalTracer.store); rows != 1 {
		t.Errorf("in-flight span should be persisted on End, rows=%d", rows)
	}

	// 2) 关闭后新 Start 必须返回空
	if id := Start(traceId, "rt-task", 1, TraceTypeUpload, StageUploadFile, ""); id != "" {
		t.Errorf("Start should return empty when global off, got %q", id)
	}
}

// TestSmoke_RestartPreservesData 验证 Init/Shutdown 的持久化语义：
//
//	写入若干 span → Shutdown → 重新 Init → 仍能查到数据
func TestSmoke_RestartPreservesData(t *testing.T) {
	// 不使用 withInitTracer，因为我们要手动控制 Init/Shutdown 的生命周期
	old := globalTracer
	oldGlobal := config.AppConfig.TraceEnabled
	oldMap := config.AppConfig.TraceModuleEnabled
	oldThresh := config.AppConfig.TraceSamplingThreshold
	oldPersist := persistFn

	zero := 0
	config.AppConfig.TraceEnabled = true
	config.AppConfig.TraceModuleEnabled = map[string]bool{}
	config.AppConfig.TraceSamplingThreshold = &zero
	persistFn = func() error { return nil }

	dir := t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		globalTracer = old
		config.AppConfig.TraceEnabled = oldGlobal
		config.AppConfig.TraceModuleEnabled = oldMap
		config.AppConfig.TraceSamplingThreshold = oldThresh
		persistFn = oldPersist
	})

	// 第一次启动：写入 5 条 span
	globalTracer = nil
	if err := Init(dir); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	traceId, _ := NewTraceId("restart-task")
	root := Start(traceId, "restart-task", 1, TraceTypeUpload, StageUploadTask, "")
	for i := 0; i < 4; i++ {
		c := StartChild(root, StageUploadFile, fmt.Sprintf("f%d", i))
		End(c)
	}
	End(root)
	// 立即关闭，依赖 drainAndExit 把所有 span 落盘
	Shutdown()

	// 第二次启动：验证持久化数据
	globalTracer = nil
	if err := Init(dir); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	rows := countRows(t, globalTracer.store)
	if rows != 5 {
		t.Errorf("expected 5 rows after restart, got %d", rows)
	}

	// runSeq 应该递增到 2
	_, seq := NewTraceId("restart-task")
	if seq != 2 {
		t.Errorf("runSeq should be 2 after first task ended, got %d", seq)
	}
	_ = time.Now() // 防止 import 未使用
}
