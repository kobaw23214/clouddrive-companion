package tracer

import (
	"clouddrive-companion-withWails2/internal/config"
	"sync"
	"testing"
)

// withThreshold 在测试期间固定采样阈值，并在结束时还原全局状态。
func withThreshold(t *testing.T, threshold int) {
	t.Helper()
	old := config.AppConfig.TraceSamplingThreshold
	v := threshold
	config.AppConfig.TraceSamplingThreshold = &v
	t.Cleanup(func() {
		config.AppConfig.TraceSamplingThreshold = old
	})
}

func TestSampler_NoSampling_WhenThresholdIsZero(t *testing.T) {
	withThreshold(t, 0)
	s := newSampler()
	s.PrepareTrace("trace-a", 1_000_000)

	if s.activeDecisionsLen() != 0 {
		t.Errorf("threshold=0 should never write decisions, got %d", s.activeDecisionsLen())
	}
	if !s.shouldRecord("trace-a", 1) {
		t.Error("threshold=0 should always record")
	}
	if s.SampleRate("trace-a") != 1 {
		t.Errorf("SampleRate should be 1, got %d", s.SampleRate("trace-a"))
	}
}

func TestSampler_BelowThreshold_NoSampling(t *testing.T) {
	withThreshold(t, 1000)
	s := newSampler()
	s.PrepareTrace("trace-a", 500)

	if s.activeDecisionsLen() != 0 {
		t.Errorf("estimated < threshold should not sample, decisions=%d", s.activeDecisionsLen())
	}
	if !s.shouldRecord("trace-a", 17) {
		t.Error("non-sampled trace should always record")
	}
}

func TestSampler_AboveThreshold_EvenDistribution(t *testing.T) {
	withThreshold(t, 100)
	s := newSampler()
	s.PrepareTrace("trace-a", 1000) // N = 10

	if got := s.SampleRate("trace-a"); got != 10 {
		t.Fatalf("expected SampleRate=10, got %d", got)
	}

	recorded := 0
	for i := int64(1); i <= 1000; i++ {
		if s.shouldRecord("trace-a", i) {
			recorded++
		}
	}
	// 1000 / 10 = 100，满足"每 N 个分片记录 1 个"
	if recorded != 100 {
		t.Errorf("expected 100 recorded out of 1000 with N=10, got %d", recorded)
	}
}

func TestSampler_RoundUpForUnevenDivision(t *testing.T) {
	// 阈值 100，预估 250 → ceil(250/100) = 3
	withThreshold(t, 100)
	s := newSampler()
	s.PrepareTrace("trace-a", 250)

	if got := s.SampleRate("trace-a"); got != 3 {
		t.Errorf("expected SampleRate=3 (ceil(250/100)), got %d", got)
	}
}

func TestSampler_PrepareTraceWithEmptyId_NoOp(t *testing.T) {
	withThreshold(t, 100)
	s := newSampler()
	s.PrepareTrace("", 100_000)

	if s.activeDecisionsLen() != 0 {
		t.Errorf("empty traceId should not be recorded, got %d entries", s.activeDecisionsLen())
	}
}

func TestSampler_ShouldRecord_GuardsAgainstNonPositiveSeq(t *testing.T) {
	withThreshold(t, 100)
	s := newSampler()
	s.PrepareTrace("trace-a", 1000) // N=10

	// 业务侧不该传 0 或负数，但 sampler 不能因此 panic 或返回 false
	if !s.shouldRecord("trace-a", 0) {
		t.Error("chunkSeq=0 should defensively return true, not false")
	}
	if !s.shouldRecord("trace-a", -1) {
		t.Error("chunkSeq=-1 should defensively return true")
	}
}

func TestSampler_Cleanup_RemovesDecision(t *testing.T) {
	withThreshold(t, 100)
	s := newSampler()
	s.PrepareTrace("trace-a", 1000)

	if s.activeDecisionsLen() != 1 {
		t.Fatalf("setup failed: expected 1 decision, got %d", s.activeDecisionsLen())
	}
	s.Cleanup("trace-a")
	if s.activeDecisionsLen() != 0 {
		t.Errorf("Cleanup should remove decision, got %d", s.activeDecisionsLen())
	}
	// Cleanup 后再 shouldRecord，应该退化为全量
	if !s.shouldRecord("trace-a", 7) {
		t.Error("after cleanup, shouldRecord must return true")
	}
}

// TestSampler_ConcurrentReadWrite 验证 shouldRecord 与 PrepareTrace/Cleanup
// 并发时不会出现数据竞态。go test -race 会捕获问题。
func TestSampler_ConcurrentReadWrite(t *testing.T) {
	withThreshold(t, 100)
	s := newSampler()
	s.PrepareTrace("trace-a", 1000)

	var wg sync.WaitGroup
	const writers = 4
	const readers = 8

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				traceId := "trace-x"
				s.PrepareTrace(traceId, 1000)
				s.Cleanup(traceId)
			}
		}(i)
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := int64(1); j <= 200; j++ {
				_ = s.shouldRecord("trace-a", j)
				_ = s.SampleRate("trace-a")
			}
		}()
	}
	wg.Wait()

	// trace-a 始终存在，最终决策应保留
	if got := s.SampleRate("trace-a"); got != 10 {
		t.Errorf("trace-a SampleRate corrupted: expected 10, got %d", got)
	}
}

// TestSampler_PropertyEvenSampling 是属性测试：对任意 N>=2 与任意 K>=N，
// 在 chunkSeq=1..K 中精确产出 floor(K/N) 次 record。
func TestSampler_PropertyEvenSampling(t *testing.T) {
	cases := []struct {
		threshold     int
		estimated     int
		totalChunks   int64
		expectMin     int
		expectMax     int
	}{
		{100, 1000, 1000, 100, 100},   // N=10
		{100, 250, 250, 83, 84},        // N=3, 250/3 = 83 余 1
		{100, 500, 500, 100, 100},      // N=5
		{1, 10, 10, 1, 1},              // N=10，10 个分片中只有 seq=10 命中
		{50, 200, 200, 50, 50},         // N=4
	}
	for _, c := range cases {
		withThreshold(t, c.threshold)
		s := newSampler()
		s.PrepareTrace("t", c.estimated)
		recorded := 0
		for i := int64(1); i <= c.totalChunks; i++ {
			if s.shouldRecord("t", i) {
				recorded++
			}
		}
		if recorded < c.expectMin || recorded > c.expectMax {
			t.Errorf("threshold=%d est=%d chunks=%d → recorded=%d, expected in [%d,%d]",
				c.threshold, c.estimated, c.totalChunks, recorded, c.expectMin, c.expectMax)
		}
	}
}
