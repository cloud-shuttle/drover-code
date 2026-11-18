package convo

import (
	"sync"
)

// Budget tracks assembled input occupancy separately from lifetime billed usage
// and root-wide spend. Repeated prefixes are not newly accumulated context.
type Budget struct {
	mu sync.Mutex

	// Occupancy is current assembled input estimate (system+tools+messages+overhead).
	Occupancy int
	// LifetimeBilled is cumulative billed tokens across the session (not occupancy).
	LifetimeBilled int64
	// RootSpend is root-wide spend across parent+children (operator ceiling).
	RootSpend int64

	ModelWindow   int
	OutputReserve int
	SafetyMargin  int
	SoftThreshold int // min(100000, model_window - output_reserve - safety_margin)
	WarnRatio     float64 // default 0.8
}

// DefaultBudget returns a budget with the design-doc soft threshold heuristic.
func DefaultBudget(modelWindow int) *Budget {
	if modelWindow <= 0 {
		modelWindow = DefaultContextLimit
	}
	b := &Budget{
		ModelWindow:   modelWindow,
		OutputReserve: 8192,
		SafetyMargin:  4096,
		WarnRatio:     0.8,
	}
	b.recomputeLocked()
	return b
}

func (b *Budget) recomputeLocked() {
	headroom := b.ModelWindow - b.OutputReserve - b.SafetyMargin
	if headroom < 1 {
		headroom = 1
	}
	soft := 100_000
	if headroom < soft {
		soft = headroom
	}
	b.SoftThreshold = soft
}

// RecomputeForModel updates limits when the model (window) changes.
func (b *Budget) RecomputeForModel(modelWindow, outputReserve, safetyMargin int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if modelWindow > 0 {
		b.ModelWindow = modelWindow
	}
	if outputReserve > 0 {
		b.OutputReserve = outputReserve
	}
	if safetyMargin > 0 {
		b.SafetyMargin = safetyMargin
	}
	b.recomputeLocked()
}

// SetOccupancy records current assembled input estimate (not cumulative).
func (b *Budget) SetOccupancy(tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if tokens < 0 {
		tokens = 0
	}
	b.Occupancy = tokens
}

// AddBilled accumulates lifetime billed usage (separate from occupancy).
func (b *Budget) AddBilled(tokens int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if tokens > 0 {
		b.LifetimeBilled += tokens
	}
}

// AddRootSpend accumulates root-wide spend including child calls.
func (b *Budget) AddRootSpend(tokens int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if tokens > 0 {
		b.RootSpend += tokens
	}
}

// Snapshot is a consistent read of budget counters.
type Snapshot struct {
	Occupancy     int
	LifetimeBilled int64
	RootSpend     int64
	SoftThreshold int
	Warn          bool
	OverSoft      bool
}

// Snapshot returns current counters. Cumulative usage alone must not trigger
// compaction — only Occupancy vs SoftThreshold does.
func (b *Budget) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Snapshot{
		Occupancy:     b.Occupancy,
		LifetimeBilled: b.LifetimeBilled,
		RootSpend:     b.RootSpend,
		SoftThreshold: b.SoftThreshold,
	}
	if b.SoftThreshold > 0 {
		s.OverSoft = b.Occupancy > b.SoftThreshold
		ratio := b.WarnRatio
		if ratio <= 0 {
			ratio = 0.8
		}
		s.Warn = b.Occupancy >= int(float64(b.SoftThreshold)*ratio)
	}
	return s
}

// ShouldCompact reports whether occupancy crossed the soft threshold.
// Lifetime billed usage never triggers this by itself.
func (b *Budget) ShouldCompact() bool {
	return b.Snapshot().OverSoft
}
