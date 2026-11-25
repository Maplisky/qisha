package agent

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Maplisky/qisha/db"
	"github.com/Maplisky/qisha/intercept"
	"github.com/Maplisky/qisha/sidequestion"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

// captureRun drives one agent turn-to-completion over Session.Prompt and emits a
// coalesced ActivityRecord per execution step (tool_use / tool_result / text /
// thinking / result). It is shared by every LLM agent in the system (worker,
// planner, …) so their execution is visible instead of a black box — the old
// agentcore.Run discarded every event. The emitted records carry only
// Kind/Tool/ToolUseID/IsError/Summary/Detail; the caller's emit fills in
// IntentID/Worker. Returns the final assistant text + terminal error.
//
// KindText/KindThinking arrive as streaming deltas (one event per fragment); a
// contiguous run is coalesced into a single record so the trace shows whole
// messages, not dozens of fragments.
func captureRun(ctx context.Context, opts agentcore.Options, input string, emit func(db.Activity)) (string, harness.TerminalReason, error) {
	ctx, opts.Hooks = WithCompletionLedger(ctx, opts.Hooks)
	s := agentcore.NewSession(opts)
	defer s.Close() // release the session's background-task manager (temp dir + processes)
	return captureRunSession(ctx, s, input, emit)
}

// captureRunSession is captureRun over an existing session, so a caller can run
// multiple prompts on the SAME conversation (e.g. a settlement round that reuses
// the worker's accumulated context after the main run hit max_turns).
func captureRunSession(ctx context.Context, s *agentcore.Session, input string, emit func(db.Activity)) (string, harness.TerminalReason, error) {
	ctx, auditTrace := intercept.WithTrace(ctx, input, approvalHistory(s.Messages()))
	defer auditTrace.Finish()
	var reason harness.TerminalReason
	rec := func(r db.Activity) {
		if r.Kind == "text" || r.Kind == "tool_result" {
			auditTrace.Append(db.InterceptContextEntry{Kind: r.Kind, Tool: r.Tool, ToolUseID: r.ToolUseID, Text: r.Detail, IsError: r.IsError})
		}
		if emit != nil {
			emit(r)
		}
	}
	toolNames := map[string]string{} // tool_use id -> name, to label results

	var tbuf strings.Builder
	var tkind string
	flush := func() {
		if tbuf.Len() == 0 {
			return
		}
		s := strings.TrimSpace(tbuf.String())
		k := tkind
		tbuf.Reset()
		tkind = ""
		if s != "" {
			rec(db.Activity{Kind: k, Summary: firstLine(s, 200), Detail: s})
		}
	}
	addDelta := func(kind, text string) {
		if text == "" {
			return
		}
		if tkind != "" && tkind != kind {
			flush()
		}
		tkind = kind
		tbuf.WriteString(text)
	}
	lastTool := &runTrace{startedAt: time.Now()}
	var lastUsage *llm.Usage

	// 台账里「这次调用的完成」要晚于它被观察到的那一刻，同名同参的重复调用才不会串。
	ledger := completionLedgerFrom(ctx)
	toolSince := map[string]int64{}
	toolKeys := map[string]string{}
	// pending 是引擎合成的「被中断」结果（tool_use id）：先压着，等钩子链报上这次调用的
	// 真实结果再落账。引擎可能在上限到点时给一次其实已经成功的调用合成这条结果，
	// 见 capture_ledger.go。
	var pending string
	recordToolResult := func(id, out string, isErr bool) {
		lastTool.done(id)
		auditTrace.Complete(id, out, isErr)
		rec(db.Activity{Kind: "tool_result", Tool: toolNames[id], ToolUseID: id,
			IsError: isErr, Summary: firstLine(out, 200), Detail: out})
		delete(toolSince, id)
		delete(toolKeys, id)
	}
	// resolveInterrupted 落账压着的那条结果：钩子链报上了真实结果就用它，否则照记引擎
	// 合成的「被中断」。
	resolveInterrupted := func() {
		if pending == "" {
			return
		}
		id := pending
		pending = ""
		out, isErr := harnessInterruptedResult, true
		if text, errFlag, ok := ledger.take(toolKeys[id], toolSince[id]); ok {
			out, isErr = text, errFlag
		}
		recordToolResult(id, out, isErr)
	}

	var finalText string
	var rerr error
	for ev, err := range s.Prompt(ctx, input) {
		if err != nil {
			flush()
			resolveInterrupted()
			if ctx.Err() != nil { // engine/user cancellation, not a provider failure
				sum, detail := terminalText(ctx, &harness.Terminal{Reason: reason, Err: ctx.Err()}, lastTool)
				rec(activityWithUsage(db.Activity{Kind: "result", Summary: firstLine(sum, 400), Detail: detail}, lastUsage))
				return finalText, reason, ctx.Err()
			}
			rec(activityWithUsage(db.Activity{Kind: "result", IsError: true, Summary: "执行出错: " + err.Error(), Detail: err.Error()}, lastUsage))
			return finalText, reason, err
		}
		// 用量事件紧跟在工具结果之后发出，那时工具可能还没收尾，不能拿它当「下一条实质
		// 事件」来落账压着的中断结果。
		if ev.Kind != harness.KindUsage {
			resolveInterrupted()
		}
		switch ev.Kind {
		case harness.KindToolUse:
			if ev.ToolUse == nil {
				continue
			}
			flush()
			toolNames[ev.ToolUse.ID] = ev.ToolUse.Name
			in := string(ev.ToolUse.Input)
			toolSince[ev.ToolUse.ID] = ledger.mark()
			toolKeys[ev.ToolUse.ID] = completionKey(ev.ToolUse.Name, ev.ToolUse.Input)
			lastTool.start(ev.ToolUse.ID, ev.ToolUse.Name, in)
			auditTrace.Start(ev.ToolUse.ID, ev.ToolUse.Name, ev.ToolUse.Input)
			rec(db.Activity{Kind: "tool_use", Tool: ev.ToolUse.Name, ToolUseID: ev.ToolUse.ID,
				Summary: ev.ToolUse.Name + " " + firstLine(in, 200), Detail: in})
		case harness.KindToolResult:
			if ev.ToolResult == nil {
				continue
			}
			flush()
			id := ev.ToolResult.ToolUseID
			out, isErr := blocksText(ev.ToolResult.Content), ev.ToolResult.IsError
			if isErr && out == harnessInterruptedResult {
				// 引擎合成的中断结果：钩子链若已看到这次调用的真实结果，现在就纠正；
				// 否则先压着，等下一条实质事件或流结束时再定（见 resolveInterrupted）。
				if text, errFlag, ok := ledger.take(toolKeys[id], toolSince[id]); ok {
					recordToolResult(id, text, errFlag)
				} else {
					pending = id
				}
				continue
			}
			recordToolResult(id, out, isErr)
		case harness.KindText:
			addDelta("text", ev.Text)
		case harness.KindThinking:
			addDelta("thinking", ev.Text)
		case harness.KindUsage:
			// live cumulative token usage (per model turn). Emitted as a non-rendered
			// "usage" activity carrying only the token fields; the UI uses the latest
			// one for a running session's live token count. Don't flush() here — the
			// buffered final-answer text must stay for the KindResult de-dup.
			if ev.Usage != nil {
				u := *ev.Usage
				lastUsage = &u
				rec(db.Activity{Kind: "usage",
					InputTokens: &u.InputTokens, OutputTokens: &u.OutputTokens,
					CacheReadTokens: &u.CacheReadTokens, CacheWriteTokens: &u.CacheWriteTokens})
			}
		case harness.KindResult:
			if ev.Terminal != nil {
				if ev.Terminal.Reason != harness.ReasonAbortedStreaming {
					sidequestion.Finish(ctx, ev.Terminal.Messages)
				}
				finalText = ev.Terminal.Text
				reason = ev.Terminal.Reason
				// the buffered tail text usually equals Terminal.Text (final answer);
				// drop it to avoid a duplicate record, the result row carries it.
				if tkind == "text" && strings.TrimSpace(tbuf.String()) == strings.TrimSpace(ev.Terminal.Text) {
					tbuf.Reset()
					tkind = ""
				}
				flush() // flush any trailing thinking / non-final text
				sum, detail := ev.Terminal.Text, ev.Terminal.Text
				if sum == "" || ev.Terminal.Reason == harness.ReasonAbortedTools || ev.Terminal.Reason == harness.ReasonAbortedStreaming {
					sum, detail = terminalText(ctx, ev.Terminal, lastTool)
				}
				u := ev.Terminal.Usage // cumulative token usage for this session
				rec(db.Activity{Kind: "result", IsError: ev.Terminal.Err != nil,
					Summary: firstLine(sum, 400), Detail: detail,
					InputTokens: &u.InputTokens, OutputTokens: &u.OutputTokens,
					CacheReadTokens: &u.CacheReadTokens, CacheWriteTokens: &u.CacheWriteTokens})
				if ev.Terminal.Err != nil {
					rerr = ev.Terminal.Err
				}
			}
		}
	}
	flush() // safety: any unflushed text if the stream ended without KindResult
	resolveInterrupted()
	return finalText, reason, rerr
}

func activityWithUsage(activity db.Activity, usage *llm.Usage) db.Activity {
	if usage == nil {
		return activity
	}
	u := *usage
	activity.InputTokens = &u.InputTokens
	activity.OutputTokens = &u.OutputTokens
	activity.CacheReadTokens = &u.CacheReadTokens
	activity.CacheWriteTokens = &u.CacheWriteTokens
	return activity
}

// blocksText concatenates the text of a tool-result's content blocks.
func blocksText(blocks []llm.ContentBlock) string {
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Type == llm.BlockText && bl.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(bl.Text)
		}
	}
	return b.String()
}

// firstLine returns a single-line, rune-capped preview for the summary column.
func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if before, _, found := strings.Cut(s, "\n"); found {
		s = before
	}
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

// Preserve the recorded session's visible messages, excluding thinking blocks.
// This is audit context; the judge receives only bounded, paired execution
// evidence selected from it, never assistant prose or thinking blocks.
func approvalHistory(messages []llm.Message) []db.InterceptContextEntry {
	var entries []db.InterceptContextEntry
	for _, message := range messages {
		for _, block := range message.Content {
			entry := db.InterceptContextEntry{Kind: string(message.Role)}
			switch block.Type {
			case llm.BlockText:
				entry.Text = block.Text
			case llm.BlockToolUse:
				entry.Kind, entry.Tool, entry.ToolUseID, entry.Text = "tool_use", block.Name, block.ID, string(block.Input)
			case llm.BlockToolResult:
				entry.Kind, entry.ToolUseID, entry.Text, entry.IsError = "tool_result", block.ToolUseID, blocksText(block.Content), block.IsError
			default:
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries
}
