package convo

import (
	"math"
	"strings"
	"testing"
)

func TestRecordAPICalibration_EMA(t *testing.T) {
	m := NewManagerWithSystem(strings.Repeat("a", 500))
	m.RecordAPICalibration(1000, 1200) // 1.2
	m.RecordAPICalibration(1000, 1200)
	ema, last, n, ok := m.CalibrationHint()
	if !ok || n != 2 {
		t.Fatalf("CalibrationHint: ok=%v n=%d", ok, n)
	}
	if last != 1.2 {
		t.Fatalf("last ratio: got %v", last)
	}
	if ema <= 1.0 || ema > 1.25 {
		t.Fatalf("unexpected EMA: %v", ema)
	}
}

func TestRecordAPICalibration_skipsTinyAndOutliers(t *testing.T) {
	m := NewManagerWithSystem("sys")
	m.RecordAPICalibration(10, 5000)
	if _, _, _, ok := m.CalibrationHint(); ok {
		t.Fatal("expected skip tiny estimate")
	}
	m.RecordAPICalibration(1000, 25000) // ratio 25 — skipped
	if _, _, _, ok := m.CalibrationHint(); ok {
		t.Fatal("expected skip outlier ratio")
	}
	m.RecordAPICalibration(1000, 1500) // 1.5
	_, _, n, ok := m.CalibrationHint()
	if !ok || n != 1 {
		t.Fatalf("expected one sample, n=%d ok=%v", n, ok)
	}
}

func TestReset_clearsCalibration(t *testing.T) {
	m := NewManagerWithSystem("x")
	m.RecordAPICalibration(500, 600)
	m.Reset()
	if _, _, _, ok := m.CalibrationHint(); ok {
		t.Fatal("calibration should clear on Reset")
	}
}

func TestResetCalibration(t *testing.T) {
	m := NewManagerWithSystem("x")
	m.RecordAPICalibration(500, 600)
	m.ResetCalibration()
	if _, _, _, ok := m.CalibrationHint(); ok {
		t.Fatal("ResetCalibration should clear")
	}
}

func TestCalibrationHint_lastRatio(t *testing.T) {
	m := NewManagerWithSystem("s")
	m.RecordAPICalibration(800, 880)
	ema, last, _, ok := m.CalibrationHint()
	if !ok {
		t.Fatal("ok")
	}
	if math.Abs(last-1.1) > 0.001 {
		t.Fatalf("last: %v", last)
	}
	if math.Abs(ema-1.1) > 0.001 {
		t.Fatalf("ema first sample: %v", ema)
	}
}
