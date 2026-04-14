package evals

import "testing"

func TestResult_passed(t *testing.T) {
	r := &Result{Scores: map[string]float64{"a": 1.0, "b": 1.0}}
	if !r.passed() {
		t.Fatal("expected pass")
	}
	r.Scores["b"] = 0.5
	if r.passed() {
		t.Fatal("expected fail on low score")
	}
	r.Scores["b"] = 1.0
	r.Err = errFake{}
	if r.passed() {
		t.Fatal("expected fail on err")
	}
}

type errFake struct{}

func (errFake) Error() string { return "x" }

func TestRunner_score_toolAccuracy(t *testing.T) {
	r := NewRunner(nil, "k", "m")
	res := &Result{
		ToolsCalled: []string{"read_file", "glob"},
		Scores:      map[string]float64{},
	}
	r.score(res, Expectations{ToolsCalled: []string{"read_file", "glob"}})
	if res.Scores["tool-accuracy"] != 1.0 {
		t.Fatalf("got %v", res.Scores)
	}
}

func TestRunner_score_outputContains(t *testing.T) {
	r := NewRunner(nil, "k", "m")
	res := &Result{Output: "Hello WORLD", Scores: map[string]float64{}}
	r.score(res, Expectations{OutputContains: []string{"hello", "world"}})
	if v := res.Scores["output-contains"]; v != 1.0 {
		t.Fatalf("got %f", v)
	}
}

func TestRunner_score_toolsNotCalled(t *testing.T) {
	r := NewRunner(nil, "k", "m")
	res := &Result{ToolsCalled: []string{"read_file"}, Scores: map[string]float64{}}
	r.score(res, Expectations{ToolsNotCalled: []string{"bash"}})
	if res.Scores["tool-restraint"] != 1.0 {
		t.Fatal(res.Scores)
	}
	res2 := &Result{ToolsCalled: []string{"bash"}, Scores: map[string]float64{}}
	r.score(res2, Expectations{ToolsNotCalled: []string{"bash"}})
	if res2.Scores["tool-restraint"] != 0.0 {
		t.Fatal(res2.Scores)
	}
}

func TestRunner_score_efficiencyPenalty(t *testing.T) {
	r := NewRunner(nil, "k", "m")
	res := &Result{Turns: 10, Scores: map[string]float64{}}
	r.score(res, Expectations{MaxTurns: 5})
	if got := res.Scores["efficiency"]; got >= 1.0 {
		t.Fatalf("expected < 1, got %v", got)
	}
}
