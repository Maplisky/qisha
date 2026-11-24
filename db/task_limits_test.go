package db

import "testing"

func TestNormalizeTaskMaxRounds(t *testing.T) {
	iptr := func(n int) *int { return &n }
	if got := NormalizeTaskMaxRounds(nil); got != nil {
		t.Fatalf("nil: got %v want nil (沿用全局)", *got)
	}
	if got := NormalizeTaskMaxRounds(iptr(-1)); got != nil {
		t.Fatalf("negative: got %v want nil (HTTP 层已拒绝负数，库内兜底视为沿用全局)", *got)
	}
	if got := NormalizeTaskMaxRounds(iptr(0)); got == nil || *got != 0 {
		t.Fatalf("zero: got %v want 0 (不限)", got)
	}
	if got := NormalizeTaskMaxRounds(iptr(120)); got == nil || *got != 120 {
		t.Fatalf("120: got %v want 120", got)
	}
	if got := NormalizeTaskMaxRounds(iptr(MaxTaskRoundsLimit + 7)); got == nil || *got != MaxTaskRoundsLimit {
		t.Fatalf("over-limit: got %v want clamp to %d", got, MaxTaskRoundsLimit)
	}
}
