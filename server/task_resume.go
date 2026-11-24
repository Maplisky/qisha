package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/Maplisky/qisha/db"
)

// optionalInt 区分请求里「没给这个字段」与「显式给了 null」：前者表示沿用原值，
// 后者表示清空（对规划轮数上限即改回沿用系统设置）。
type optionalInt struct {
	Provided bool
	Value    *int
}

func (o *optionalInt) UnmarshalJSON(data []byte) error {
	o.Provided = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Value = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("必须是整数或 null: %w", err)
	}
	o.Value = &v
	return nil
}

// describeTaskTimeout 把时间上限渲染给活动流：0=不限时。
func describeTaskTimeout(seconds int) string {
	if seconds <= 0 {
		return "不限时"
	}
	return (time.Duration(seconds) * time.Second).String()
}

// describeTaskRounds 把任务级规划轮数上限渲染给活动流：nil=沿用系统设置，0=不限。
func describeTaskRounds(maxRounds *int) string {
	switch {
	case maxRounds == nil:
		return "沿用系统设置"
	case *maxRounds == 0:
		return "不限"
	default:
		return fmt.Sprintf("%d 轮", *maxRounds)
	}
}

// resumeTask 让已超时（终态 timeout）的任务由人工继续运行。
//
// 请求体可省略，也可只带要改的字段：
//   - timeout_seconds：时间上限（秒），0=不限时；
//   - max_rounds：规划轮数上限，null=改回沿用系统设置，0=不限。
//
// 上限先落库并同步到运行时句柄，再走与「重跑意图」相同的终态复活路径（admitTask）：
// 旧一轮的墙钟预算与轮数计数清零、挂起的收尾请求作废、因服务停止而中断的意图重开。
// 已写回的事实/发现/凭据全部保留，规划者针对仍未达成的目标继续派发。
func (s *Server) resumeTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var req struct {
		TimeoutSeconds *int        `json:"timeout_seconds"`
		MaxRounds      optionalInt `json:"max_rounds"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体无效："+err.Error())
			return
		}
	}
	// timeout_seconds 落在 INTEGER 列，超出 32 位会把整条 UPDATE 打挂。
	if req.TimeoutSeconds != nil && (*req.TimeoutSeconds < 0 || int64(*req.TimeoutSeconds) > math.MaxInt32) {
		writeErr(w, http.StatusBadRequest, "时间上限必须是不小于 0 的秒数（0=不限时）")
		return
	}
	if v := req.MaxRounds.Value; req.MaxRounds.Provided && v != nil && (*v < 0 || *v > db.MaxTaskRoundsLimit) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("规划轮数上限必须在 0–%d 之间（0=不限）", db.MaxTaskRoundsLimit))
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, http.StatusConflict, "任务正在删除，无法继续")
		return
	}
	defer s.engine.decInflight(t.ID)
	switch status := t.lifecycleSnapshot().Status; status {
	case "timeout":
		// 唯一允许的入口状态。
	case "done", "failed":
		writeErr(w, http.StatusConflict, "仅已超时的任务可以继续运行；已结束的任务请新增目标或重跑意图")
		return
	default:
		writeErr(w, http.StatusConflict, "任务尚未超时，无需继续")
		return
	}
	if req.TimeoutSeconds != nil || req.MaxRounds.Provided {
		if _, err := s.m.SetTaskLimits(t.ID, req.TimeoutSeconds, req.MaxRounds.Value, req.MaxRounds.Provided); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	timeoutText, roundsText := describeTaskTimeout(t.TimeoutSeconds), describeTaskRounds(t.MaxRounds)
	s.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text", Summary: fmt.Sprintf(
		"人工继续运行：时间上限 %s，规划轮数上限 %s。上一轮的时间预算与轮数计数已清零，已写回的成果保留，从当前进度继续",
		timeoutText, roundsText)})
	queued, err := s.admitTask(t, "resume")
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	log.Printf("[task] #%s 人工继续运行（时间上限=%s 轮数上限=%s queued=%v）", t.ID, timeoutText, roundsText, queued)
	writeJSON(w, http.StatusOK, taskControlResult{
		ID:     t.ID,
		Queued: queued,
		Status: map[bool]string{true: "queued", false: "running"}[queued],
	})
}
