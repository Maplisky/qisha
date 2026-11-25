package server

import (
	"context"
	"strings"
	"testing"

	"github.com/Maplisky/qisha/db"
)

// 预算提醒挂在纠偏钩子上，用真实链路（usage 到顶 → 入队 → PreToolUse）验证放行规则。
func budgetNudgeHooks(t *testing.T) (*Engine, steerHooks) {
	t.Helper()
	e := NewEngine(nil)
	e.work[7] = &workExecution{} // queueSteer 只给运行中的 work 入队
	h := steerHooks{drain: func(name string, input []byte) (string, bool) { return e.drainSteerFor(7, name, input) }}
	in := 1200
	intentBudgetWatcher(intentBudgetConfig{Enabled: true, Tokens: 1000}, func(msg string) error {
		return e.steerBudget(7, msg)
	}, "test")(db.Activity{Kind: "usage", InputTokens: &in})
	return e, h
}

func (e *Engine) queuedSteer() int {
	e.steerMu.Lock()
	defer e.steerMu.Unlock()
	return len(e.steerBox[7])
}

// 意图预算的收尾提醒：写回与清理类调用放行，提醒留在队列里等下一次别的调用再投递。
// v1.0.1 靶场验收（任务 #6）里，被它拦下的恰好是删除自建测试文件的命令。
func TestBudgetNudgeLetsWriteBackAndCleanupThrough(t *testing.T) {
	e, h := budgetNudgeHooks(t)
	if n := e.queuedSteer(); n != 1 {
		t.Fatalf("usage 到顶后应入队一条预算提醒，现有 %d 条", n)
	}

	cleanup := []byte(`{"command":"rm -f /var/www/html/pojun-test.txt"}`)
	if block, msg, _ := h.PreToolUse(context.Background(), "Bash", cleanup); block {
		t.Fatalf("清理命令被预算提醒拦下了: %q", msg)
	}
	for _, name := range []string{"record_fact", "insert_assets", "report_finding", "save_evidence"} {
		if block, msg, _ := h.PreToolUse(context.Background(), name, []byte(`{}`)); block {
			t.Fatalf("写回工具 %s 被预算提醒拦下了: %q", name, msg)
		}
	}
	if n := e.queuedSteer(); n != 1 {
		t.Fatalf("放行后提醒应留在队列里，现有 %d 条", n)
	}

	probe := []byte(`{"command":"nmap -sV 10.0.0.5"}`)
	block, msg, _ := h.PreToolUse(context.Background(), "Bash", probe)
	if !block || !strings.Contains(msg, "【预算提醒】") || !strings.Contains(msg, "先清理干净再收尾") {
		t.Fatalf("新的探测应被拦下并收到预算提醒: block=%v msg=%q", block, msg)
	}
	if n := e.queuedSteer(); n != 0 {
		t.Fatalf("提醒投递后应出队，现有 %d 条", n)
	}
	if block, _, _ := h.PreToolUse(context.Background(), "Bash", probe); block {
		t.Fatal("队列为空后不该再拦")
	}
}

// 规划者 / 人 / 注入告警的纠偏保持原语义：拦下紧接着的一次工具调用，不看是哪种调用；
// 也不因为前面压着一条预算提醒而延迟。
func TestOperatorSteerStillInterruptsNextCall(t *testing.T) {
	e, h := budgetNudgeHooks(t)
	if err := e.SteerWork(7, "停止对该入口的硬打"); err != nil {
		t.Fatal(err)
	}
	if n := e.queuedSteer(); n != 2 {
		t.Fatalf("应有两条排队消息，现有 %d 条", n)
	}

	block, msg, _ := h.PreToolUse(context.Background(), "record_fact", []byte(`{}`))
	if !block || !strings.Contains(msg, "停止对该入口的硬打") {
		t.Fatalf("规划者纠偏应拦下紧接着的调用（哪怕它是写回）: block=%v msg=%q", block, msg)
	}
	q := e.steerBox[7]
	if len(q) != 1 || q[0].kind != steerBudgetNudge {
		t.Fatalf("预算提醒应原样留在队列里: %+v", q)
	}
}

func TestSteerQueueKinds(t *testing.T) {
	e := NewEngine(nil)
	if err := e.steerBudget(7, budgetNudgeText(1000)); err == nil {
		t.Fatal("没有运行中的 work 时预算提醒不该入队")
	}
	if err := e.SteerWork(7, "规划者指令"); err == nil {
		t.Fatal("没有运行中的 work 时规划者纠偏不该入队")
	}
	e.work[7] = &workExecution{}
	if err := e.SteerWork(7, "规划者指令"); err != nil {
		t.Fatal(err)
	}
	if err := e.steerBudget(7, budgetNudgeText(1000)); err != nil {
		t.Fatal(err)
	}
	if err := e.steerBudget(7, "  "); err == nil {
		t.Fatal("空消息不该入队")
	}
	q := e.steerBox[7]
	if len(q) != 2 || q[0].kind != steerFromOperator || q[1].kind != steerBudgetNudge {
		t.Fatalf("入队顺序或种类不对: %+v", q)
	}
}
