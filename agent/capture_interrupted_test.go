package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	"github.com/Autumn-27/norma/tool"
	"github.com/Maplisky/qisha/db"
)

// slowPostHook 在工具执行完之后拖住钩子链，把引擎里「执行完、状态还没翻成已完成」的
// 窗口撑开——墙钟上限要撞进这个窗口，平时它只有几十微秒。
type slowPostHook struct {
	delay time.Duration
}

func (slowPostHook) PreToolUse(context.Context, string, []byte) (bool, string, []byte) {
	return false, "", nil
}

func (h slowPostHook) PostToolUse(context.Context, string, []byte, []byte, bool) { time.Sleep(h.delay) }

func (slowPostHook) Stop(context.Context, []llm.Message) (bool, []string, string) {
	return false, nil, ""
}

// 引擎的墙钟上限撞上工具收尾时，事件流里会是一条合成的「被中断」，而工具其实成功了
// （v1.0.1 靶场验收里，终局判定轮的 goal_met 就这样在活动流里显示成被中断）。活动流
// 必须记下工具的真实结果。时序按真实事件顺序缩成两种：
//   - 上限在钩子链收尾途中到点：合成结果送达时，钩子已记下真实结果；
//   - 上限先到、工具随后才成功返回：合成结果先到，真实结果晚一步。
//
// 对照：工具始终没返回，记下的仍是引擎合成的「被中断」。
//
// 用例不走引擎的收尾分支：工具超时后进收尾时，引擎会就地重赋 toolCtx，与还没退出的
// 工具协程构成一次数据竞争（third_party/norma/harness/query.go 的收尾分支），-race 下
// 会把用例判失败。真实结果晚一步的那种时序改用「下一条活动落库要花时间」来留出窗口。
func TestCaptureRunRecordsRealResultWhenDeadlineHitsFinishingTool(t *testing.T) {
	const budget = 300 * time.Millisecond
	cases := []struct {
		name      string
		toolDelay time.Duration // 工具多久后返回成功（负数 = 一直不返回）；不理会墙钟上限
		hookDelay time.Duration // 工具返回后钩子链拖多久
		slowEmit  time.Duration // 合成结果之后那条用量记录的落库耗时
		wantText  string
		wantErr   bool
	}{
		{"上限在钩子链收尾途中到点", 0, 3 * budget, 0, "probe ok", false},
		{"上限先到，工具随后成功返回", 3 * budget / 2, 0, 3 * budget, "probe ok", false},
		{"工具始终没返回", -1, 0, 0, harnessInterruptedResult, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			probe := tool.Build(tool.Spec{Name: "Probe", Schema: map[string]any{"type": "object"},
				Run: func(context.Context, json.RawMessage, *tool.ToolContext) (tool.Result, error) {
					switch {
					case tc.toolDelay < 0:
						<-release
					case tc.toolDelay > 0:
						time.Sleep(tc.toolDelay)
					}
					return tool.Text("probe ok"), nil
				}})
			provider := captureUsageProvider{stream: func(_ context.Context, yield func(llm.StreamEvent, error) bool) {
				for _, ev := range []llm.StreamEvent{
					{Type: llm.SEMessageStart, Usage: llm.Usage{InputTokens: 11, CacheReadTokens: 3}},
					{Type: llm.SEToolUseStart, ToolID: "probe-call", ToolName: "Probe"},
					{Type: llm.SEToolInputJSON, Text: "{}"},
					{Type: llm.SEMessageDelta, StopReason: "tool_use", Usage: llm.Usage{OutputTokens: 7, CacheWriteTokens: 2}},
				} {
					if !yield(ev, nil) {
						return
					}
				}
			}}
			var hooks harness.HookRunner = toolLogSink(KindWorker)
			if tc.hookDelay > 0 {
				hooks = slowPostHook{delay: tc.hookDelay}
			}
			opts := agentcore.Options{Provider: provider, Tools: []tool.CoreTool{probe}, Hooks: hooks,
				PermissionMode: permission.ModeBypass, WorkingDir: t.TempDir(), MaxTurns: 2, MaxDuration: budget}

			var acts []db.Activity
			_, _, err := captureRun(t.Context(), opts, "run the probe", func(a db.Activity) {
				if a.Kind == "usage" {
					time.Sleep(tc.slowEmit)
				}
				acts = append(acts, a)
			})
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			useAt, resultAt, finalAt := -1, -1, -1
			var got db.Activity
			for i, a := range acts {
				switch a.Kind {
				case "tool_use":
					useAt = i
				case "tool_result":
					if resultAt >= 0 {
						t.Fatalf("同一次调用记了多条工具结果: %+v", acts)
					}
					resultAt, got = i, a
				case "result":
					finalAt = i
				}
			}
			if useAt < 0 || resultAt < 0 || useAt > resultAt || finalAt < resultAt {
				t.Fatalf("活动顺序不对: tool_use=%d tool_result=%d result=%d，全部活动=%+v", useAt, resultAt, finalAt, acts)
			}
			if got.Detail != tc.wantText || got.IsError != tc.wantErr {
				t.Fatalf("工具结果记成了 %q（isError=%v），应为 %q（isError=%v）", got.Detail, got.IsError, tc.wantText, tc.wantErr)
			}
		})
	}
}

func TestCompletionLedgerPairsOnlyLaterCompletions(t *testing.T) {
	l := &completionLedger{}
	key := completionKey("Bash", []byte(`{"command":"id"}`))
	l.put(key, "旧的一次", false)

	// 这次调用开始之后才有完成记录，早于它的不算。
	since := l.mark()
	if _, _, ok := l.take(key, since); ok {
		t.Fatal("调用开始之前的完成记录不该被这次调用取走")
	}
	l.put(completionKey("Bash", []byte(`{"command":"whoami"}`)), "别的调用", false)
	l.put(key, "uid=0", false)
	text, _, ok := l.take(key, since)
	if !ok || text != "uid=0" {
		t.Fatalf("取回应为本次调用的完成: ok=%v text=%q", ok, text)
	}
	if _, _, ok := l.take(key, since); ok {
		t.Fatal("完成记录只该被取走一次")
	}

	// 只保留最近若干条，长跑不会把台账撑大。
	for i := 0; i < 3*completionLedgerCap; i++ {
		l.put("k", "x", false)
	}
	if len(l.done) != completionLedgerCap {
		t.Fatalf("台账保留了 %d 条，上限 %d", len(l.done), completionLedgerCap)
	}

	// 没有钩子链时没有台账：全部是空操作，不 panic。
	var none *completionLedger
	none.put("k", "x", false)
	if none.mark() != 0 {
		t.Fatal("空台账的 mark 应为 0")
	}
	if _, _, ok := none.take("k", 0); ok {
		t.Fatal("空台账取不到任何记录")
	}
}

// 记账层只包住 PostToolUse，其余钩子原样转发。
func TestCompletionHooksForwardEverything(t *testing.T) {
	inner := &countingHookRunner{}
	_, hooks := WithCompletionLedger(context.Background(), inner)
	if block, msg, _ := hooks.PreToolUse(context.Background(), "record_fact", []byte(`{}`)); !block || msg != "stopped" {
		t.Fatalf("PreToolUse 没有原样转发: block=%v msg=%q", block, msg)
	}
	if prevent, blocking, _ := hooks.Stop(context.Background(), nil); !prevent || len(blocking) != 1 {
		t.Fatalf("Stop 没有原样转发: prevent=%v blocking=%v", prevent, blocking)
	}
	hooks.PostToolUse(context.Background(), "record_fact", []byte(`{"a":1}`), []byte(`"written"`), false)
	if inner.posts != 1 {
		t.Fatalf("PostToolUse 没有转发给内层: %d 次", inner.posts)
	}
	if ctx, none := WithCompletionLedger(context.Background(), nil); none != nil || completionLedgerFrom(ctx) != nil {
		t.Fatal("没有钩子链时不该凭空造出台账")
	}
}

type countingHookRunner struct{ posts int }

func (c *countingHookRunner) PreToolUse(context.Context, string, []byte) (bool, string, []byte) {
	return true, "stopped", nil
}

func (c *countingHookRunner) PostToolUse(context.Context, string, []byte, []byte, bool) { c.posts++ }

func (c *countingHookRunner) Stop(context.Context, []llm.Message) (bool, []string, string) {
	return true, []string{"keep going"}, ""
}
