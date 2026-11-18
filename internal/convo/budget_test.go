package convo

import "testing"

func TestSoftThresholdHeuristic(t *testing.T) {
	b := DefaultBudget(200_000)
	if b.SoftThreshold != 100_000 {
		t.Fatalf("soft=%d want 100000", b.SoftThreshold)
	}
	b.RecomputeForModel(50_000, 8192, 4096)
	want := 50_000 - 8192 - 4096
	if b.SoftThreshold != want {
		t.Fatalf("soft=%d want %d", b.SoftThreshold, want)
	}
}

func TestLifetimeDoesNotTriggerCompaction(t *testing.T) {
	b := DefaultBudget(200_000)
	b.AddBilled(5_000_000)
	b.SetOccupancy(10)
	if b.ShouldCompact() {
		t.Fatal("lifetime billed must not trigger compaction")
	}
	b.SetOccupancy(100_001)
	if !b.ShouldCompact() {
		t.Fatal("occupancy over soft should compact")
	}
}

func TestWarnAt80Percent(t *testing.T) {
	b := DefaultBudget(200_000)
	b.SetOccupancy(80_000)
	s := b.Snapshot()
	if !s.Warn {
		t.Fatal("expected warn at 80%")
	}
	if s.OverSoft {
		t.Fatal("80k should not be over soft 100k")
	}
}
