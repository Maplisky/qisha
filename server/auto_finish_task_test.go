package server

import "testing"

// 任务级规划轮数上限覆盖全局值；空转/覆盖率判据仍只由全局配置决定；全局自动收尾关闭时
// 只保留任务显式给出的轮数上限，其余判据不得被顺带打开。
func TestEvaluateAutoFinishForTask(t *testing.T) {
	iptr := func(n int) *int { return &n }
	enabled := autoFinishConfig{Enabled: true, IdleRounds: 3, MinRounds: 5, CoveragePct: 0, MaxRounds: 80}
	disabled := autoFinishConfig{Enabled: false, IdleRounds: 3, MinRounds: 5, CoveragePct: 0, MaxRounds: 80}
	cases := []struct {
		name       string
		round      int64
		idle       int
		active     bool
		cfg        autoFinishConfig
		override   *int
		wantReason string
		wantDetail string
	}{
		{"nil-inherits-global-limit", 80, 0, true, enabled, nil, "auto_rounds", "达到最大规划轮数 80"},
		{"nil-below-global-limit", 79, 0, true, enabled, nil, "", ""},
		{"lower-override-triggers-early", 30, 0, true, enabled, iptr(30), "auto_rounds", "达到任务设置的规划轮数上限 30"},
		{"higher-override-not-yet", 80, 0, true, enabled, iptr(200), "", ""},
		{"higher-override-reached", 200, 0, true, enabled, iptr(200), "auto_rounds", "达到任务设置的规划轮数上限 200"},
		{"zero-override-lifts-round-limit", 500, 0, true, enabled, iptr(0), "", ""},
		{"zero-override-keeps-idle-rule", 5, 3, false, enabled, iptr(0), "auto_idle", ""},
		{"disabled-without-override", 500, 9, false, disabled, nil, "", ""},
		{"disabled-zero-override", 500, 9, false, disabled, iptr(0), "", ""},
		{"disabled-override-below", 19, 9, false, disabled, iptr(20), "", ""},
		{"disabled-explicit-override-reached", 20, 9, false, disabled, iptr(20), "auto_rounds", "达到任务设置的规划轮数上限 20"},
	}
	for _, tc := range cases {
		reason, detail, ok := evaluateAutoFinishForTask(tc.round, tc.idle, tc.active, 0, tc.cfg, tc.override)
		if tc.wantReason == "" {
			if ok {
				t.Fatalf("%s: unexpected settle (%s)", tc.name, reason)
			}
			continue
		}
		if !ok || reason != tc.wantReason {
			t.Fatalf("%s: got (%q,%v) want (%q,true)", tc.name, reason, ok, tc.wantReason)
		}
		if tc.wantDetail != "" && detail != tc.wantDetail {
			t.Fatalf("%s: detail=%q want %q", tc.name, detail, tc.wantDetail)
		}
	}
}
