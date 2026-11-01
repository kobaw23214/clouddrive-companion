package tracer

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// TaskTracer 全局单例：管理活跃 span、模块开关、采样决策、持久化。
//
// 设计要点：
//   - 活跃 span 用 sync.Map 存（read-mostly 场景，sync.Map 比 sync.RWMutex+map 快）
//   - activeCount 用原子计数器，独立于 sync.Map 以避免遍历计数
//   - softCap 防止业务异常（Start 后忘了 End）导致内存爆炸
//   - 任意 API 接受空 spanId 都安全，让业务侧可以无脑 defer End
type TaskTracer struct {
	store       *Store
	moduleMgr   *ModuleManager
	sampler     *Sampler
	hub         *traceHub    // SSE 推送中心；store.writeBatch 落库后通过它广播
	activeSpans sync.Map     // map[spanId] *Span
	activeCount atomic.Int64 // 与 activeSpans 同步维护，避免 Range 计数
}

const (
	// activeSpanSoftCap 活跃 span 哈希表软上限。超出后 Start 直接返回空字符串。
	// 经验值：每条 Span 约 200 字节，20 万条约 40 MB。
	activeSpanSoftCap = 200_000

	// errMessageMaxBytes ErrorMessage 入库前的最大字节数。
	errMessageMaxBytes = 4 * 1024
	truncatedSuffix    = "... (truncated)"
)

// globalTracer 进程内单例。
var globalTracer *TaskTracer

// Init 初始化全局 tracer。失败时返回错误，调用方决定是否阻塞启动。
//
// 内部行为：
//   - 在 <dir>/traces.db 打开 SQLite，建表索引、启动后台 writer
//   - 注册内置模块（upload / scan）
//   - 启动每小时执行的 cleanup loop
//
// Init 是幂等的：重复调用直接返回 nil。
func Init(dir string) error {
	if globalTracer != nil {
		return nil
	}
	store, err := openStore(dir)
	if err != nil {
		return err
	}
	t := &TaskTracer{
		store:     store,
		moduleMgr: newModuleManager(),
		sampler:   newSampler(),
		hub:       newTraceHub(),
	}
	t.moduleMgr.register(TraceTypeUpload, "文件上传链路",
		"追踪扫描、MD5、分片状态查询、分片上传、合并通知全过程")
	t.moduleMgr.register(TraceTypeScan, "目录扫描链路",
		"追踪用户选择目录后的文件遍历过程")

	// 把 hub 接入 store：每次 batch 落库成功后，store 会调用 hub.BroadcastInsert/Update
	// 把变更推给前端订阅者，实现实时刷新。
	store.SetBroadcaster(t.hub)

	globalTracer = t

	// 启动时回填上次进程异常退出留下的"end_ms=0"幽灵 span（孤儿清理）。
	// 阈值 60 分钟：单文件上传基本不会超过这个时长，既不误伤当前的长任务，
	// 又能让用户进入 UI 时不会看到永远在转圈的虚假"进行中"。
	if n, err := store.SweepOrphans(60); err != nil {
		log.Printf("[tracer] 启动孤儿清理失败: %v", err)
	} else if n > 0 {
		log.Printf("[tracer] 启动孤儿清理回填 %d 条 end_ms=0 的残留 span", n)
	}

	// 启动时检查数据库尺寸：先 checkpoint(TRUNCATE) 把 wal 合并到主库（避免被
	// 反复删除后膨胀的 wal 误导 size 判断），再看主库是否超过用户配置阈值
	// （默认 500 MB）。超过则同步 VACUUM 真正释放磁盘给 OS。
	//
	// 这一段在每次启动时只跑一次，比"运行中按小时检查"更直观：用户重启即"压缩"。
	if before, after, vacuumed, err := store.CompactIfLarge(currentVacuumThresholdMB()); err != nil {
		log.Printf("[tracer] 启动压缩失败: %v", err)
	} else if vacuumed {
		log.Printf("[tracer] 启动压缩：数据库从 %d MB → %d MB（VACUUM 已执行）",
			before/1024/1024, after/1024/1024)
	} else {
		log.Printf("[tracer] 启动检查：数据库 %d MB，未触发 VACUUM（阈值 %d MB）",
			before/1024/1024, currentVacuumThresholdMB())
	}

	// 启动清理协程；getter 每次调用都从最新 config 读取，便于运行时修改阈值
	store.startCleanupLoop(currentMaxAgeDays, currentMaxRows, currentVacuumThresholdMB)

	return nil
}

// Shutdown 关闭全局 tracer。等待最多 3 秒让后台 writer flush 完毕。
func Shutdown() {
	if globalTracer == nil {
		return
	}
	globalTracer.store.Shutdown(3 * time.Second)
	globalTracer = nil
}

// ─── span 生命周期 ───

// NewTraceId 为某 taskId 生成新的 traceId 与 runSeq。
//
//	taskId == ""：仅生成 traceId，runSeq 固定为 1（如独立的 file_scan 链路）
//	taskId != ""：runSeq = (历史最大 run_seq 中该 taskId 的) + 1
//
// 该方法不向数据库写入；后续 Start/End 才会触发落盘。
func NewTraceId(taskId string) (traceId string, runSeq int) {
	traceId = randomHex16()
	runSeq = 1
	if globalTracer != nil && taskId != "" {
		runSeq = globalTracer.store.NextRunSeq(taskId)
	}
	return
}

// Start 创建一个顶层 span。
//
// 返回空字符串的几种情况，调用方都需要把后续 End/Fail 视为 no-op：
//   - tracer 未初始化（Init 失败或测试环境）
//   - 全局开关关 / 该 traceType 模块开关关
//   - 活跃 span 数超过 softCap（防 OOM）
//
// Start 只能用于顶层；子 span 用 StartChild。
func Start(traceId, taskId string, runSeq int, traceType, stage, filename string) string {
	if globalTracer == nil {
		return ""
	}
	return startSpan(globalTracer, "", traceId, taskId, runSeq, traceType, stage, filename)
}

// StartChild 创建一个子 span，自动从活跃 parent 继承 traceId / taskId / runSeq / traceType。
//
// 返回空字符串的情况：
//   - tracer 未初始化
//   - parentSpanId 为空 / 不在活跃表（父已 End / 父被采样跳过 / 父被开关丢弃）
//   - 模块或全局开关关
//   - 活跃 span 数超过 softCap
//
// StartChild 不会回退到顶层 span：父丢失就是丢失，避免出现 trace 断链。
func StartChild(parentSpanId, stage, filename string) string {
	if globalTracer == nil || parentSpanId == "" {
		return ""
	}
	v, ok := globalTracer.activeSpans.Load(parentSpanId)
	if !ok {
		return ""
	}
	p := v.(*Span)
	return startSpan(globalTracer, parentSpanId, p.TraceID, p.TaskID, p.RunSeq, p.TraceType, stage, filename)
}

// StartChunkUpload 是 chunk_upload 阶段的便捷入口：在 startSpan 之前先咨询采样器。
//
// 业务侧只需提供 chunkSeq（>= 1）即可；不需要关心采样间隔的计算逻辑。
//
// totalChunks > 0 且 chunkSeq == totalChunks 时强制采样：最后一个分片承载着
// "服务端合并" 的关键耗时（见 RecordSpan + StageServerMerge），不能因采样丢失。
func StartChunkUpload(parentSpanId string, chunkSeq, totalChunks int64, filename string) string {
	if globalTracer == nil || parentSpanId == "" {
		return ""
	}
	v, ok := globalTracer.activeSpans.Load(parentSpanId)
	if !ok {
		return ""
	}
	p := v.(*Span)
	isLast := totalChunks > 0 && chunkSeq == totalChunks
	if !isLast && !globalTracer.sampler.shouldRecord(p.TraceID, chunkSeq) {
		return ""
	}
	return startSpan(globalTracer, parentSpanId, p.TraceID, p.TaskID, p.RunSeq, p.TraceType, StageChunkUpload, filename)
}

// startSpan 内部统一入口，所有 Start/StartChild/StartChunkUpload 都汇总到这里。
//
// 创建 span 时立刻把"进行中"占位行入队（end_ms=0），让前端在任务还没结束时
// 也能看到链路。finish 时再走 UPDATE 回填。
func startSpan(t *TaskTracer, parentId, traceId, taskId string, runSeq int,
	traceType, stage, filename string) string {

	if !t.moduleMgr.isEnabled(traceType) {
		return ""
	}
	if t.activeCount.Load() >= activeSpanSoftCap {
		// 软上限保护：丢弃静默，但记一行 warn 防止运维盲区
		// 这里用单次日志避免刷屏；soft cap 命中本身就是异常信号
		log.Printf("[tracer] 活跃 span 超过 softCap=%d，拒绝新 span (trace=%s stage=%s)",
			activeSpanSoftCap, traceId, stage)
		return ""
	}
	spanId := randomHex16()
	now := time.Now().UnixMilli()
	s := &Span{
		SpanID:       spanId,
		ParentSpanID: parentId,
		TraceID:      traceId,
		TaskID:       taskId,
		RunSeq:       runSeq,
		TraceType:    traceType,
		Stage:        stage,
		Filename:     filename,
		StartMs:      now,
		EndMs:        0, // 0 表示进行中；finish 时 UPDATE 回填
		DurationMs:   0,
		Success:      true,
		CreatedAtMs:  now,
	}
	t.activeSpans.Store(spanId, s)
	t.activeCount.Add(1)
	t.store.Enqueue(s)
	return spanId
}

// End 标记 span 成功结束。spanId 为空或不在活跃表时是 no-op。
func End(spanId string) { finish(spanId, true, "", "", 0) }

// EndWithExtra 同 End，但额外附加 extra（JSON 字符串）。
func EndWithExtra(spanId, extra string) { finish(spanId, true, "", extra, 0) }

// EndAtWithExtra 同 EndWithExtra，但用业务侧提供的显式 endMs 作为结束时间。
//
// 适用场景：业务侧已经独立测量到了"真正结束"的时刻（比如最后一个分片的
// chunk_upload 应当在 httptrace.WroteRequest 触发时结束，而不是包含整个 HTTP
// 等服务端处理的时间），希望让 span 时长更准确。
//
// endMs <= span.StartMs 时退化为 time.Now()，避免出现负 duration。
func EndAtWithExtra(spanId, extra string, endMs int64) { finish(spanId, true, "", extra, endMs) }

// Fail 标记 span 失败结束，errMsg 会被截断到 4 KB。
func Fail(spanId, errMsg string) { finish(spanId, false, errMsg, "", 0) }

// FailWithExtra 同 Fail，但额外附加 extra（JSON 字符串）。
func FailWithExtra(spanId, errMsg, extra string) { finish(spanId, false, errMsg, extra, 0) }

// finish 内部统一收尾：从活跃表原子取出 span、填写结束信息、入队回填。
//
// 用 LoadAndDelete 保证幂等：重复调用 End/Fail 同 spanId 只会有一次入队。
//
// endMsOverride > 0 且 > span.StartMs 时用之；否则用 time.Now().
//
// 写入路径走 UPDATE（startSpan 时已经 INSERT 过占位行），仅回填 end_ms /
// duration_ms / success / error_message / extra 五个字段。
func finish(spanId string, success bool, errMsg, extra string, endMsOverride int64) {
	if globalTracer == nil || spanId == "" {
		return
	}
	v, loaded := globalTracer.activeSpans.LoadAndDelete(spanId)
	if !loaded {
		return
	}
	globalTracer.activeCount.Add(-1)

	s := v.(*Span)
	end := time.Now().UnixMilli()
	if endMsOverride > s.StartMs {
		end = endMsOverride
	}
	s.EndMs = end
	s.DurationMs = end - s.StartMs
	s.Success = success
	if errMsg != "" {
		s.ErrorMessage = truncate(errMsg, errMessageMaxBytes)
	}
	if extra != "" {
		s.Extra = extra
	}
	globalTracer.store.EnqueueUpdate(s)
}

// ─── 采样桥接函数 ───

// PrepareSampling 通知采样器为某 traceId 决定采样间隔。
//
// 应在任务开始时调用一次（如 Manager.Start 顶部），传入预估的 span 总数。
func PrepareSampling(traceId string, estimatedSpans int) {
	if globalTracer == nil {
		return
	}
	globalTracer.sampler.PrepareTrace(traceId, estimatedSpans)
}

// SampleRate 返回某 traceId 的采样间隔（每 N 个 chunk 记录 1 个）。未采样返回 1。
//
// 主要用于业务侧把采样率写入 span.Extra，让前端按 duration_ms × N 推算 CPU 耗时。
func SampleRate(traceId string) int {
	if globalTracer == nil {
		return 1
	}
	return globalTracer.sampler.SampleRate(traceId)
}

// CleanupTrace 在任务结束时释放采样器中该 traceId 的决策条目。
func CleanupTrace(traceId string) {
	if globalTracer == nil {
		return
	}
	globalTracer.sampler.Cleanup(traceId)
}

// RecordSpan 用显式的 startMs/endMs 记录一条已经结束的子 span（不进活跃表）。
//
// 适用场景：业务侧已经独立测量好了某段耗时（比如通过 httptrace 切出
// "客户端写完请求 → 收到首字节" 的服务器处理时间），希望以子 span 形式入链。
//
// parentSpanId 为空 / parent 已不在活跃表 / 模块开关关 时，本调用是 no-op，并打 warn 日志，
// 便于排查"为什么我记的 span 没出现在链路里"。
// startMs/endMs 应为毫秒级 Unix 时间戳；endMs <= startMs 时会用 startMs 兜底。
func RecordSpan(parentSpanId, stage, filename string, startMs, endMs int64, success bool, errMsg, extra string) {
	if globalTracer == nil {
		log.Printf("[tracer] RecordSpan 丢弃 stage=%s file=%s reason=tracer未初始化", stage, filename)
		return
	}
	if parentSpanId == "" {
		log.Printf("[tracer] RecordSpan 丢弃 stage=%s file=%s reason=parentSpanId为空", stage, filename)
		return
	}
	v, ok := globalTracer.activeSpans.Load(parentSpanId)
	if !ok {
		log.Printf("[tracer] RecordSpan 丢弃 stage=%s file=%s parent=%s reason=parent不在活跃表(可能已 End 或开关关)",
			stage, filename, parentSpanId)
		return
	}
	p := v.(*Span)
	if !globalTracer.moduleMgr.isEnabled(p.TraceType) {
		log.Printf("[tracer] RecordSpan 丢弃 stage=%s file=%s reason=模块 %s 未启用",
			stage, filename, p.TraceType)
		return
	}
	if endMs < startMs {
		endMs = startMs
	}
	now := time.Now().UnixMilli()
	s := &Span{
		SpanID:       randomHex16(),
		ParentSpanID: parentSpanId,
		TraceID:      p.TraceID,
		TaskID:       p.TaskID,
		RunSeq:       p.RunSeq,
		TraceType:    p.TraceType,
		Stage:        stage,
		Filename:     filename,
		StartMs:      startMs,
		EndMs:        endMs,
		DurationMs:   endMs - startMs,
		Success:      success,
		CreatedAtMs:  now,
	}
	if !success && errMsg != "" {
		s.ErrorMessage = truncate(errMsg, errMessageMaxBytes)
	}
	if extra != "" {
		s.Extra = extra
	}
	globalTracer.store.Enqueue(s)
}

// RetagTrace 同步把某 traceId 下所有 span 的 trace_type / task_id / run_seq
// 改写到新值。用于"扫描完成后用户点了上传"的场景：把扫描时建立的 scan trace
// 整体并入新建的 upload trace，这样前端列表里只显示一条"文件上传"，
// 点进去能看到 file_scan + metadata_send + upload_task 的完整时间轴。
//
// 调用前应保证：
//   - 该 traceId 下不再有活跃 span（finish 路径已全部走完，否则 span End 时
//     仍会带旧 traceType 入队，造成同一 trace 内 trace_type 不一致）
//
// 该函数同步执行 SQL，不走 writeCh 队列，避免与并发写入产生 ordering 问题。
func RetagTrace(traceId, newTraceType, newTaskId string, newRunSeq int) error {
	if globalTracer == nil {
		return nil
	}
	return globalTracer.store.RetagTrace(traceId, newTraceType, newTaskId, newRunSeq)
}

// ReparentSpan 同步改写单条 span 的 parent_span_id。常配合 RetagTrace 使用：
// 把扫描顶层 span 重新挂到新创建的 upload_task 下（如果你想做嵌套）。
//
// 此 API 当前在并入设计里**未使用**（三段保持平级，更符合"时间轴并列"的 UX），
// 但保留以备将来需要做层级合并时使用。
func ReparentSpan(spanId, newParentSpanId string) error {
	if globalTracer == nil {
		return nil
	}
	return globalTracer.store.ReparentSpan(spanId, newParentSpanId)
}

// RecordSpanWithTrace 在不依赖活跃父 span 的情况下，按显式时间戳记录一条
// 已结束的 span 入链。
//
// 适用场景：业务侧只知道 traceId 和需要的字段（比如 metadata_send 的两个时间戳），
// 而对应的 parent span 可能已经 End 不在活跃表里（scan 顶层 span 在扫描结束时
// 就已 End）。RecordSpan 受"父必须活跃"约束所以不适用，这里直接绕过。
//
// 入参语义与 startSpan + finish 的组合等价：直接构造 Span 并 Enqueue。
// 不修改活跃表，不影响 softCap。
func RecordSpanWithTrace(traceId, taskId string, runSeq int, traceType,
	parentSpanId, stage, filename string,
	startMs, endMs int64, success bool, errMsg, extra string) {
	if globalTracer == nil || traceId == "" {
		return
	}
	if !globalTracer.moduleMgr.isEnabled(traceType) {
		return
	}
	if endMs < startMs {
		endMs = startMs
	}
	now := time.Now().UnixMilli()
	s := &Span{
		SpanID:       randomHex16(),
		ParentSpanID: parentSpanId,
		TraceID:      traceId,
		TaskID:       taskId,
		RunSeq:       runSeq,
		TraceType:    traceType,
		Stage:        stage,
		Filename:     filename,
		StartMs:      startMs,
		EndMs:        endMs,
		DurationMs:   endMs - startMs,
		Success:      success,
		CreatedAtMs:  now,
	}
	if !success && errMsg != "" {
		s.ErrorMessage = truncate(errMsg, errMessageMaxBytes)
	}
	if extra != "" {
		s.Extra = extra
	}
	globalTracer.store.Enqueue(s)
}

// ─── 暴露给 handler 的访问器 ───

// Modules 返回所有已注册模块的快照（含 enabled 状态）。
func Modules() []TraceModule {
	if globalTracer == nil {
		return nil
	}
	return globalTracer.moduleMgr.list()
}

// SetModuleEnabled 切换某模块开关。返回 false 表示模块不存在。
func SetModuleEnabled(code string, enabled bool) bool {
	if globalTracer == nil {
		return false
	}
	return globalTracer.moduleMgr.setEnabled(code, enabled)
}

// SetGlobalEnabled 切换全局总开关。返回 false 表示持久化失败。
func SetGlobalEnabled(enabled bool) bool {
	return setGlobalEnabled(enabled)
}

// StoreRef 返回底层 Store，handler 层用它做查询（ListTraces / GetTopSpans 等）。
//
// 测试以外的调用方应只用它做只读查询，不应直接 Enqueue。
func StoreRef() *Store {
	if globalTracer == nil {
		return nil
	}
	return globalTracer.store
}

// SpanTraceContext 返回活跃 span 的 traceId，用于出站 HTTP 请求的链路透传
// （业务侧据此设置 X-Trace-Id / X-Parent-Span-Id 头）。
//
// spanId 为空、span 已结束（不在活跃表）或 tracer 未初始化时 ok=false，
// 调用方静默跳过即可。该函数只读活跃表，不触发任何写入。
func SpanTraceContext(spanId string) (traceId string, ok bool) {
	if globalTracer == nil || spanId == "" {
		return "", false
	}
	v, found := globalTracer.activeSpans.Load(spanId)
	if !found {
		return "", false
	}
	return v.(*Span).TraceID, true
}

// ─── helpers ───

// randomHex16 返回 16 字节 crypto/rand 随机数的 32 位 hex。
//
// 极端 fallback：rand.Read 失败时返回纳秒时间戳的 hex（基本不可能触发，
// 仅用于让本函数永远有有效返回值）。
func randomHex16() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		nanoHex := hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
		if len(nanoHex) >= 32 {
			return nanoHex[:32]
		}
		// 极少数极端情况，重复填充
		for len(nanoHex) < 32 {
			nanoHex += "0"
		}
		return nanoHex[:32]
	}
	return hex.EncodeToString(b)
}

// truncate 把超过 maxBytes 的字符串截短并附 truncatedSuffix 标记。
//
// 注意：按字节截断，可能切到 UTF-8 多字节中间。修正方式见后续函数。
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes - len(truncatedSuffix)
	if cut < 0 {
		cut = 0
	}
	// 避免切断 UTF-8 多字节字符：向前寻找首字节
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + truncatedSuffix
}

// activeCountForTest 测试用，返回当前活跃 span 数。
func activeCountForTest() int64 {
	if globalTracer == nil {
		return 0
	}
	return globalTracer.activeCount.Load()
}
