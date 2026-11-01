package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

// withInitTracer 在临时目录里启动一个独立的 globalTracer，并在测试结束时还原。
//
// 由于 globalTracer 是包级变量，串行测试可以共享一份；这里仍然每个测试独立
// 初始化，确保 active span / sampler 状态干净。
func withInitTracer(t *testing.T) {
	t.Helper()

	old := globalTracer
	oldGlobal := config.AppConfig.TraceEnabled
	oldMap := config.AppConfig.TraceModuleEnabled
	oldThresh := config.AppConfig.TraceSamplingThreshold
	oldPersist := persistFn

	zero := 0
	config.AppConfig.TraceEnabled = true
	config.AppConfig.TraceModuleEnabled = map[string]bool{}
	config.AppConfig.TraceSamplingThreshold = &zero // 禁用采样，避免影响 chunk 测试
	persistFn = func() error { return nil }

	dir := t.TempDir()
	globalTracer = nil // 强制重新初始化
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	t.Cleanup(func() {
		Shutdown()
		globalTracer = old
		config.AppConfig.TraceEnabled = oldGlobal
		config.AppConfig.TraceModuleEnabled = oldMap
		config.AppConfig.TraceSamplingThreshold = oldThresh
		persistFn = oldPersist
	})
}

// waitForFlush 等待异步 writer 把队列冲完。
func waitForFlush() {
	time.Sleep(storeBatchMaxLatency + 200*time.Millisecond)
}

func TestInit_RegistersBuiltinModules(t *testing.T) {
	withInitTracer(t)

	mods := Modules()
	if len(mods) < 2 {
		t.Fatalf("expected at least 2 builtin modules, got %d", len(mods))
	}
	codes := map[string]bool{}
	for _, m := range mods {
		codes[m.Code] = true
	}
	if !codes[TraceTypeUpload] || !codes[TraceTypeScan] {
		t.Errorf("expected upload+scan modules, got %v", codes)
	}
}

func TestInit_Idempotent(t *testing.T) {
	withInitTracer(t)
	// 二次 Init 不应报错且不替换 globalTracer
	first := globalTracer
	if err := Init(t.TempDir()); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}
	if globalTracer != first {
		t.Error("Init should be idempotent (globalTracer must not change)")
	}
}

func TestStartEnd_BasicLifecycle(t *testing.T) {
	withInitTracer(t)

	traceId, runSeq := NewTraceId("task-basic")
	if len(traceId) != 32 {
		t.Errorf("traceId should be 32 hex chars, got %d", len(traceId))
	}
	if runSeq != 1 {
		t.Errorf("first runSeq should be 1, got %d", runSeq)
	}

	spanId := Start(traceId, "task-basic", runSeq, TraceTypeUpload, StageUploadTask, "")
	if spanId == "" {
		t.Fatal("Start returned empty spanId on enabled tracer")
	}
	if activeCountForTest() != 1 {
		t.Errorf("activeCount expected 1, got %d", activeCountForTest())
	}

	End(spanId)
	if activeCountForTest() != 0 {
		t.Errorf("activeCount expected 0 after End, got %d", activeCountForTest())
	}
	waitForFlush()
	if got := countRows(t, globalTracer.store); got != 1 {
		t.Errorf("expected 1 row in db, got %d", got)
	}
}

func TestStart_DisabledGlobal_ReturnsEmpty(t *testing.T) {
	withInitTracer(t)
	config.AppConfig.TraceEnabled = false

	spanId := Start("trace", "task", 1, TraceTypeUpload, StageUploadTask, "")
	if spanId != "" {
		t.Errorf("expected empty spanId when global off, got %q", spanId)
	}
	if activeCountForTest() != 0 {
		t.Error("disabled global should not push to active map")
	}
}

func TestStart_DisabledModule_ReturnsEmpty(t *testing.T) {
	withInitTracer(t)
	if !SetModuleEnabled(TraceTypeUpload, false) {
		t.Fatal("SetModuleEnabled returned false")
	}

	spanId := Start("trace", "task", 1, TraceTypeUpload, StageUploadTask, "")
	if spanId != "" {
		t.Errorf("disabled upload module should reject Start, got %q", spanId)
	}

	// scan 模块未关，应该还能创建
	scanId := Start("scan-trace", "", 1, TraceTypeScan, StageFileScan, "")
	if scanId == "" {
		t.Error("scan module should still be enabled")
	}
	End(scanId)
}

func TestEnd_EmptySpanId_NoCrash(t *testing.T) {
	withInitTracer(t)
	// 不应 panic
	End("")
	EndWithExtra("", "{}")
	Fail("", "boom")
	FailWithExtra("", "boom", "{}")
}

func TestEnd_DoubleCall_OnlyOneEnqueue(t *testing.T) {
	withInitTracer(t)

	traceId, _ := NewTraceId("task-x")
	spanId := Start(traceId, "task-x", 1, TraceTypeUpload, StageUploadFile, "a.txt")
	if spanId == "" {
		t.Fatal("Start returned empty")
	}

	End(spanId)
	End(spanId) // 重复调用应 no-op
	Fail(spanId, "boom")

	waitForFlush()
	if got := countRows(t, globalTracer.store); got != 1 {
		t.Errorf("expected exactly 1 enqueued span despite double End, got %d", got)
	}
}

func TestStartChild_InheritsContext(t *testing.T) {
	withInitTracer(t)

	traceId, runSeq := NewTraceId("task-c")
	parent := Start(traceId, "task-c", runSeq, TraceTypeUpload, StageUploadTask, "")
	child := StartChild(parent, StageUploadFile, "f.txt")
	if child == "" {
		t.Fatal("StartChild returned empty")
	}

	v, _ := globalTracer.activeSpans.Load(child)
	cs := v.(*Span)
	if cs.TraceID != traceId {
		t.Errorf("child traceId mismatch: %s vs %s", cs.TraceID, traceId)
	}
	if cs.TaskID != "task-c" {
		t.Errorf("child taskId not inherited, got %s", cs.TaskID)
	}
	if cs.RunSeq != runSeq {
		t.Errorf("child runSeq not inherited, got %d", cs.RunSeq)
	}
	if cs.TraceType != TraceTypeUpload {
		t.Errorf("child traceType not inherited, got %s", cs.TraceType)
	}
	if cs.ParentSpanID != parent {
		t.Errorf("child parent not set: %s vs %s", cs.ParentSpanID, parent)
	}

	End(child)
	End(parent)
}

func TestStartChild_UnknownParent_ReturnsEmpty(t *testing.T) {
	withInitTracer(t)
	if got := StartChild("ghost", StageMD5Calc, ""); got != "" {
		t.Errorf("unknown parent should return empty, got %q", got)
	}
	if got := StartChild("", StageMD5Calc, ""); got != "" {
		t.Errorf("empty parent should return empty, got %q", got)
	}
}

func TestStartChild_AfterParentEnded_ReturnsEmpty(t *testing.T) {
	withInitTracer(t)

	traceId, _ := NewTraceId("task-aftermath")
	parent := Start(traceId, "task-aftermath", 1, TraceTypeUpload, StageUploadTask, "")
	End(parent) // 父已结束

	if got := StartChild(parent, StageUploadFile, ""); got != "" {
		t.Errorf("StartChild after parent End should return empty, got %q", got)
	}
}

func TestFail_ErrorMessageTruncated(t *testing.T) {
	withInitTracer(t)

	traceId, _ := NewTraceId("task-err")
	spanId := Start(traceId, "task-err", 1, TraceTypeUpload, StageUploadFile, "")
	huge := strings.Repeat("X", 5*1024) // 5 KB
	Fail(spanId, huge)

	waitForFlush()

	sp, err := globalTracer.store.GetSpan(spanId)
	if err != nil {
		t.Fatalf("GetSpan failed: %v", err)
	}
	if len(sp.ErrorMessage) > errMessageMaxBytes {
		t.Errorf("error message not truncated, len=%d", len(sp.ErrorMessage))
	}
	if !strings.HasSuffix(sp.ErrorMessage, truncatedSuffix) {
		t.Errorf("expected truncated suffix, got tail %q", sp.ErrorMessage[max(0, len(sp.ErrorMessage)-30):])
	}
	if sp.Success {
		t.Error("Fail should mark Success=false")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestFail_ShortError_NoTruncation(t *testing.T) {
	withInitTracer(t)
	traceId, _ := NewTraceId("task-short-err")
	spanId := Start(traceId, "task-short-err", 1, TraceTypeUpload, StageUploadFile, "")
	Fail(spanId, "boom")

	waitForFlush()
	sp, _ := globalTracer.store.GetSpan(spanId)
	if sp.ErrorMessage != "boom" {
		t.Errorf("short error mangled: %q", sp.ErrorMessage)
	}
}

func TestNewTraceId_TaskIdReuse_RunSeqIncrements(t *testing.T) {
	withInitTracer(t)

	traceId1, runSeq1 := NewTraceId("task-reuse")
	id1 := Start(traceId1, "task-reuse", runSeq1, TraceTypeUpload, StageUploadTask, "")
	End(id1)
	waitForFlush()

	_, runSeq2 := NewTraceId("task-reuse")
	if runSeq2 != runSeq1+1 {
		t.Errorf("expected runSeq 2 for second run, got %d", runSeq2)
	}
}

func TestNewTraceId_EmptyTaskId_AlwaysOne(t *testing.T) {
	withInitTracer(t)

	for i := 0; i < 3; i++ {
		_, runSeq := NewTraceId("")
		if runSeq != 1 {
			t.Errorf("empty taskId runSeq must be 1, got %d on iteration %d", runSeq, i)
		}
	}
}

func TestActiveSpan_SoftCap_RejectsNew(t *testing.T) {
	withInitTracer(t)
	// 把 activeCount 强行抬到 soft cap 上限
	globalTracer.activeCount.Store(activeSpanSoftCap)

	spanId := Start("trace", "task", 1, TraceTypeUpload, StageUploadTask, "")
	if spanId != "" {
		t.Errorf("at soft cap should reject new span, got %q", spanId)
	}

	globalTracer.activeCount.Store(0) // 还原
}

func TestStart_EndWithExtra_PersistedExtra(t *testing.T) {
	withInitTracer(t)

	traceId, _ := NewTraceId("task-extra")
	spanId := Start(traceId, "task-extra", 1, TraceTypeUpload, StageUploadTask, "")
	EndWithExtra(spanId, `{"hello":"world"}`)

	waitForFlush()
	sp, err := globalTracer.store.GetSpan(spanId)
	if err != nil {
		t.Fatalf("GetSpan failed: %v", err)
	}
	if sp.Extra != `{"hello":"world"}` {
		t.Errorf("extra not persisted: %q", sp.Extra)
	}
	if !sp.Success {
		t.Error("End should mark success=true")
	}
}

// ─── ChunkUpload + 采样 ──────────────────────────────────

func TestStartChunkUpload_RespectsSampling(t *testing.T) {
	withInitTracer(t)

	// 设置阈值，让该 trace 必采样
	thresh := 100
	config.AppConfig.TraceSamplingThreshold = &thresh

	traceId, _ := NewTraceId("task-samp")
	parent := Start(traceId, "task-samp", 1, TraceTypeUpload, StageUploadTask, "")

	// 预估 1000 span → N=10
	PrepareSampling(traceId, 1000)
	if got := SampleRate(traceId); got != 10 {
		t.Fatalf("SampleRate expected 10, got %d", got)
	}

	recorded := 0
	for i := int64(1); i <= 1000; i++ {
		id := StartChunkUpload(parent, i, 1000, "f.txt")
		if id != "" {
			recorded++
			End(id)
		}
	}
	if recorded != 100 {
		t.Errorf("with N=10 expected 100 recorded, got %d", recorded)
	}

	End(parent)
	CleanupTrace(traceId)
}

func TestStartChunkUpload_NoSampling_FullRecord(t *testing.T) {
	withInitTracer(t)

	traceId, _ := NewTraceId("task-full")
	parent := Start(traceId, "task-full", 1, TraceTypeUpload, StageUploadTask, "")

	// 没调 PrepareSampling，应全量
	for i := int64(1); i <= 50; i++ {
		id := StartChunkUpload(parent, i, 50, "")
		if id == "" {
			t.Errorf("chunk %d unexpectedly skipped", i)
		}
		End(id)
	}
	End(parent)
}

// 即使采样剔除，"最后一个分片"也必须被记录 —— 它承载着 server_merge 子 span。
func TestStartChunkUpload_LastChunkAlwaysRecorded(t *testing.T) {
	withInitTracer(t)

	thresh := 100
	config.AppConfig.TraceSamplingThreshold = &thresh

	traceId, _ := NewTraceId("task-last")
	parent := Start(traceId, "task-last", 1, TraceTypeUpload, StageUploadTask, "")

	// 预估 1000 → N=10。total=999（不是 N 的倍数，正常会被采样跳过）
	PrepareSampling(traceId, 1000)
	if got := SampleRate(traceId); got != 10 {
		t.Fatalf("SampleRate expected 10, got %d", got)
	}

	// chunkSeq=999 不是 10 的倍数，但因为是 totalChunks（最后一个），必须被记录
	id := StartChunkUpload(parent, 999, 999, "f.txt")
	if id == "" {
		t.Error("last chunk must be recorded even when sampling rules would skip it")
	}
	End(id)

	// 中间分片仍走采样规则
	skipped := StartChunkUpload(parent, 7, 999, "f.txt")
	if skipped != "" {
		t.Error("non-last, non-multiple chunk should be sampled out")
		End(skipped)
	}

	End(parent)
	CleanupTrace(traceId)
}

// ─── 属性测试 ────────────────────────────────────────────

// TestProperty_FinishIdempotent 任意次数 End/Fail 同 spanId 只入队 1 次。
func TestProperty_FinishIdempotent(t *testing.T) {
	withInitTracer(t)

	r := rand.New(rand.NewSource(42))
	const trials = 50

	for i := 0; i < trials; i++ {
		traceId, _ := NewTraceId("task-prop")
		spanId := Start(traceId, "task-prop", 1, TraceTypeUpload, StageUploadFile,
			fmt.Sprintf("f%d", i))
		if spanId == "" {
			t.Fatal("Start returned empty unexpectedly")
		}

		// 随机调用 End / Fail / EndWithExtra 多次
		ops := r.Intn(5) + 1
		for j := 0; j < ops; j++ {
			switch r.Intn(4) {
			case 0:
				End(spanId)
			case 1:
				Fail(spanId, "x")
			case 2:
				EndWithExtra(spanId, "{}")
			case 3:
				FailWithExtra(spanId, "x", "{}")
			}
		}
	}

	waitForFlush()
	got := countRows(t, globalTracer.store)
	if got != trials {
		t.Errorf("expected exactly %d rows (idempotent), got %d", trials, got)
	}
}

// TestProperty_StartChildAlwaysInheritsParentTrace
// 任意 (parent.traceId, parent.runSeq) 组合的子 span，traceId/runSeq 必须严格相等。
func TestProperty_StartChildAlwaysInheritsParentTrace(t *testing.T) {
	withInitTracer(t)

	r := rand.New(rand.NewSource(1))
	for i := 0; i < 30; i++ {
		taskId := fmt.Sprintf("task-%d", i)
		traceId, runSeq := NewTraceId(taskId)
		// 故意制造非 1 的 runSeq：通过先写一个老 span 再 NewTraceId
		if r.Intn(2) == 0 {
			old := Start(traceId, taskId, 1, TraceTypeUpload, StageUploadTask, "")
			End(old)
			waitForFlush()
			traceId, runSeq = NewTraceId(taskId)
		}

		parent := Start(traceId, taskId, runSeq, TraceTypeUpload, StageUploadTask, "")
		child := StartChild(parent, StageUploadFile, "")
		if child == "" {
			t.Fatalf("StartChild returned empty for parent=%s", parent)
		}
		v, _ := globalTracer.activeSpans.Load(child)
		c := v.(*Span)
		if c.TraceID != traceId || c.TaskID != taskId || c.RunSeq != runSeq {
			t.Errorf("inheritance broken: child=(%s, %s, %d) parent=(%s, %s, %d)",
				c.TraceID, c.TaskID, c.RunSeq, traceId, taskId, runSeq)
		}
		End(child)
		End(parent)
	}
}

// TestProperty_ConcurrentStartEnd 大量并发 Start/End 不应丢条不应崩溃。
func TestProperty_ConcurrentStartEnd(t *testing.T) {
	withInitTracer(t)

	const goroutines = 16
	const perGoroutine = 100

	traceId, _ := NewTraceId("task-conc")
	parent := Start(traceId, "task-conc", 1, TraceTypeUpload, StageUploadTask, "")

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				id := StartChild(parent, StageUploadFile,
					fmt.Sprintf("g%d-f%d", gid, i))
				if id == "" {
					t.Errorf("StartChild unexpectedly empty (g=%d i=%d)", gid, i)
					return
				}
				End(id)
			}
		}(g)
	}
	wg.Wait()
	End(parent)

	waitForFlush()
	got := countRows(t, globalTracer.store)
	want := goroutines*perGoroutine + 1 // + parent
	if got != want {
		t.Errorf("expected %d spans persisted, got %d (lost %d)", want, got, want-got)
	}
	if activeCountForTest() != 0 {
		t.Errorf("active count should be 0 after all End, got %d", activeCountForTest())
	}
}

// ─── helpers ─────────────────────────────────────────────

func TestTruncate_HelperBehavior(t *testing.T) {
	short := "hello"
	if got := truncate(short, 100); got != short {
		t.Errorf("short string altered: %q", got)
	}

	long := strings.Repeat("a", 100)
	got := truncate(long, 30)
	if len(got) > 30 {
		t.Errorf("truncate exceeded max: len=%d", len(got))
	}
	if !strings.HasSuffix(got, truncatedSuffix) {
		t.Error("missing truncated suffix")
	}

	// UTF-8 边界保护：每个汉字 3 字节
	chinese := strings.Repeat("汉", 50) // 150 字节
	got = truncate(chinese, 25)
	if len(got) > 25 {
		t.Errorf("utf8 truncate exceeded max: len=%d", len(got))
	}
	// 解码不应失败：尾部 truncatedSuffix 是 ASCII，前缀里我们要求每个汉字完整
	prefix := strings.TrimSuffix(got, truncatedSuffix)
	if len(prefix)%3 != 0 {
		t.Errorf("utf8 boundary not respected: prefix len=%d (should be multiple of 3)", len(prefix))
	}
}

func TestRandomHex16_Format(t *testing.T) {
	for i := 0; i < 100; i++ {
		h := randomHex16()
		if len(h) != 32 {
			t.Errorf("expected 32 hex chars, got %d (%q)", len(h), h)
		}
		for _, c := range h {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("non-hex char %q in %s", c, h)
				break
			}
		}
	}
}
