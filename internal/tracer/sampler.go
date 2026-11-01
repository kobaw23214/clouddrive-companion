package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"sync"
)

// Sampler 控制 chunk_upload 阶段的采样率，避免单任务百万分片把磁盘打爆。
//
// 工作机制：
//   - 业务侧在任务开始（Manager.Start）时调用 PrepareTrace，传入预估 span 数
//   - 若预估值超过 config.TraceSamplingThreshold，按"向上取整"算出采样间隔 N
//     N = ceil(estimated / threshold)
//   - 真实分片上传时，业务侧每个分片调一次 shouldRecord(traceId, chunkSeq)
//     若该 trace 被采样，则只在 chunkSeq % N == 0 时返回 true
//
// 设计取舍：
//   - 用 chunkSeq 而不是全局原子计数器，是为了在 trace 内做"严格均匀"的
//     1/N 采样，否则并发分片会导致采样分布不均
//   - 决策 map 仅在 trace 被采样时写入；未采样的 trace 不占内存
type Sampler struct {
	mu        sync.RWMutex
	decisions map[string]int // traceId → 采样间隔 N（>=2）
}

// thresholdFn 读取当前采样阈值；提取为变量以便测试覆盖。
var thresholdFn = currentSamplingThreshold

func newSampler() *Sampler {
	return &Sampler{decisions: make(map[string]int)}
}

// currentSamplingThreshold 从 config 读取阈值。返回值含义：
//
//	> 0 → 阈值，超过后启用采样
//	== 0 → 强制全量（用户主动禁用采样）
//	未配置（指针 nil） → 也视为 0（极端情况；正常初始化时不应为 nil）
func currentSamplingThreshold() int {
	if config.AppConfig.TraceSamplingThreshold == nil {
		return 0
	}
	return *config.AppConfig.TraceSamplingThreshold
}

// PrepareTrace 根据预估 span 数决定该 traceId 是否需要采样。
//
//   - threshold == 0：禁用采样（写入空决策）
//   - estimatedSpans <= threshold：不采样（同样不写决策）
//   - 否则：N = ceil(estimatedSpans / threshold)，写入 decisions
func (s *Sampler) PrepareTrace(traceId string, estimatedSpans int) {
	if traceId == "" {
		return
	}
	threshold := thresholdFn()
	if threshold <= 0 || estimatedSpans <= threshold {
		return
	}
	n := (estimatedSpans + threshold - 1) / threshold // 向上取整
	if n < 2 {
		return
	}
	s.mu.Lock()
	s.decisions[traceId] = n
	s.mu.Unlock()
}

// shouldRecord 决定某个分片 span 是否应被记录。
//
// 仅用于 chunk_upload 阶段；其他阶段不应调用。
func (s *Sampler) shouldRecord(traceId string, chunkSeq int64) bool {
	s.mu.RLock()
	n, sampled := s.decisions[traceId]
	s.mu.RUnlock()
	if !sampled || n <= 1 {
		return true
	}
	if chunkSeq <= 0 {
		// 防御：业务侧应保证 chunkSeq >= 1
		return true
	}
	return chunkSeq%int64(n) == 0
}

// SampleRate 返回该 traceId 当前的采样间隔 N（即"每 N 个分片记录 1 个"）。
// 未采样返回 1。
//
// 用于业务侧把采样率写入 span.Extra，便于前端按 duration_ms × N 推算 CPU 耗时。
func (s *Sampler) SampleRate(traceId string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n, ok := s.decisions[traceId]; ok && n > 1 {
		return n
	}
	return 1
}

// Cleanup 在 trace 结束时释放决策表条目，避免长期运行时内存泄漏。
func (s *Sampler) Cleanup(traceId string) {
	if traceId == "" {
		return
	}
	s.mu.Lock()
	delete(s.decisions, traceId)
	s.mu.Unlock()
}

// activeDecisionsLen 仅用于测试：返回当前决策表的大小。
func (s *Sampler) activeDecisionsLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.decisions)
}
