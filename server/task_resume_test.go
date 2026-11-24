package server

import (
	"encoding/json"
	"testing"
)

// optionalInt 必须区分「没给字段」（沿用原值）与「显式 null」（清空）：前者不能被误当成
// 后者，否则「继续运行」时只想改时间上限的请求会把轮数上限悄悄抹掉。
func TestOptionalIntDistinguishesAbsentAndNull(t *testing.T) {
	var req struct {
		Timeout *int        `json:"timeout_seconds"`
		Rounds  optionalInt `json:"max_rounds"`
	}
	if err := json.Unmarshal([]byte(`{"timeout_seconds":7200,"max_rounds":40}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Timeout == nil || *req.Timeout != 7200 {
		t.Fatalf("timeout: got %v want 7200", req.Timeout)
	}
	if !req.Rounds.Provided || req.Rounds.Value == nil || *req.Rounds.Value != 40 {
		t.Fatalf("max_rounds=40: got %+v", req.Rounds)
	}

	req.Rounds = optionalInt{}
	if err := json.Unmarshal([]byte(`{"timeout_seconds":0,"max_rounds":null}`), &req); err != nil {
		t.Fatal(err)
	}
	if !req.Rounds.Provided || req.Rounds.Value != nil {
		t.Fatalf("max_rounds=null: got %+v want Provided=true Value=nil", req.Rounds)
	}

	req.Rounds = optionalInt{}
	if err := json.Unmarshal([]byte(`{"timeout_seconds":3600}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Rounds.Provided {
		t.Fatalf("max_rounds absent: got %+v want Provided=false", req.Rounds)
	}

	req.Rounds = optionalInt{}
	if err := json.Unmarshal([]byte(`{"max_rounds":12.5}`), &req); err == nil {
		t.Fatal("max_rounds=12.5: want error for non-integer")
	}
}

func TestDescribeTaskLimits(t *testing.T) {
	iptr := func(n int) *int { return &n }
	cases := []struct {
		seconds  int
		rounds   *int
		wantTime string
		wantRnds string
	}{
		{0, nil, "不限时", "沿用系统设置"},
		{7200, iptr(0), "2h0m0s", "不限"},
		{5400, iptr(120), "1h30m0s", "120 轮"},
	}
	for _, tc := range cases {
		if got := describeTaskTimeout(tc.seconds); got != tc.wantTime {
			t.Fatalf("timeout(%d)=%q want %q", tc.seconds, got, tc.wantTime)
		}
		if got := describeTaskRounds(tc.rounds); got != tc.wantRnds {
			t.Fatalf("rounds(%v)=%q want %q", tc.rounds, got, tc.wantRnds)
		}
	}
}
