package agent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/Autumn-27/norma/harness"
)

// 工具结果台账：纠正入库引擎在墙钟到点那一刻合成的「被中断」结果。
//
// 背景（v1.0.1 靶场验收，任务 #6 的终局判定轮）：引擎的工具执行流在「工具已经执行完、
// 状态还没从执行中翻成已完成」的窗口里，如果运行的墙钟上限恰好到点，就会给这次调用合成
// 一条 "tool execution interrupted" 推给事件流（third_party/norma/harness/streaming.go
// 的 finish → drainSynthetic）。随后状态照样翻成已完成，模型拿到的是真实结果——于是
// 三处对不上：日志和模型都是成功，活动流里却显示「被中断」。
//
// 引擎目录按约定不改（third_party/README.md），所以在这边包一层：钩子链亲眼看到工具的
// 真实结果时记一笔，采集层遇到引擎合成的「被中断」时拿这笔记录纠正。
//
// 合成的结果送达时，工具可能还没收尾（上限先到、工具随后才返回），所以采集层纠正不了
// 的先压着，等下一条实质事件或流结束时再定——见 captureRunSession 的 resolveInterrupted。

// harnessInterruptedResult 是引擎合成的中断结果原文。引擎升级改了它，这里会失配，
// 届时只是退回纠正前的行为。
const harnessInterruptedResult = "tool execution interrupted"

// completionLedgerCap 是台账保留的最近完成数：够配对同一轮里并发的一批工具调用。
const completionLedgerCap = 64

type toolCompletion struct {
	key     string
	text    string
	isError bool
	at      int64
}

// completionLedger 按发生顺序记下钩子链看到的工具结果。stamp 单调递增：采集层看到
// tool_use 时取一个，完成记录只有晚于它才算「这次调用的完成」——同名同参的重复调用
// 才不会串。nil 台账的所有操作都是空操作。
type completionLedger struct {
	mu    sync.Mutex
	stamp int64
	done  []toolCompletion
}

func completionKey(name string, input []byte) string {
	return name + "\x00" + string(input)
}

// mark 取当前序号，稍后用它判断一次完成是否发生在这次 tool_use 之后。
func (l *completionLedger) mark() int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stamp++
	return l.stamp
}

func (l *completionLedger) put(key, text string, isError bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stamp++
	l.done = append(l.done, toolCompletion{key: key, text: text, isError: isError, at: l.stamp})
	if len(l.done) > completionLedgerCap {
		l.done = l.done[len(l.done)-completionLedgerCap:]
	}
}

// take 取走 key 对应、晚于 since 的那次完成；没有则 ok=false。
func (l *completionLedger) take(key string, since int64) (text string, isError, ok bool) {
	if l == nil {
		return "", false, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.done) - 1; i >= 0; i-- {
		if c := l.done[i]; c.key == key && c.at > since {
			text, isError, ok = c.text, c.isError, true
			l.done = append(l.done[:i], l.done[i+1:]...)
			return
		}
	}
	return "", false, false
}

type completionLedgerCtxKey struct{}

// WithCompletionLedger 给钩子链加一层「记下工具的真实结果」，并把台账挂到 ctx 上供
// captureRunSession 取用。hooks 为 nil 时原样返回——没有钩子链就没有台账，采集层照旧。
func WithCompletionLedger(ctx context.Context, hooks harness.HookRunner) (context.Context, harness.HookRunner) {
	if hooks == nil {
		return ctx, nil
	}
	l := &completionLedger{}
	return context.WithValue(ctx, completionLedgerCtxKey{}, l), completionHooks{HookRunner: hooks, ledger: l}
}

func completionLedgerFrom(ctx context.Context) *completionLedger {
	l, _ := ctx.Value(completionLedgerCtxKey{}).(*completionLedger)
	return l
}

type completionHooks struct {
	harness.HookRunner
	ledger *completionLedger
}

// PostToolUse 先记账再转发：墙钟到点不挑时候，记下得越早，到点时纠正得越及时。
func (h completionHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	var text string
	if json.Unmarshal(result, &text) != nil {
		text = string(result)
	}
	h.ledger.put(completionKey(name, input), text, isErr)
	h.HookRunner.PostToolUse(ctx, name, input, result, isErr)
}
