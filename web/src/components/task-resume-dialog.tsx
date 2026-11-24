"use client";

import * as React from "react";

import { PlayIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { api } from "@/lib/api";
import type { Task } from "@/lib/types";

import { MAX_TASK_ROUNDS_LIMIT, parseLimitInput, roundsInheritHint, useGlobalMaxRounds } from "./task-limits";

interface TaskResumeDialogProps {
  task: Task;
  onClose: () => void;
  onResumed?: (result: { queued: boolean }) => void;
}

// 继续运行一个已超时的任务，可同时重设时间上限与规划轮数上限。仅在打开时挂载（任务列表与
// 详情页共用）。初值取任务当前的上限；没被改动的字段不会发给后端——原值如果不是整分钟
// （例如 90 秒），也不能因为用户只动了轮数就被悄悄取整。
export function TaskResumeDialog({ task, onClose, onResumed }: TaskResumeDialogProps) {
  const timeoutId = React.useId();
  const roundsId = React.useId();
  const globalMax = useGlobalMaxRounds();
  const initialTimeout =
    task.timeout_seconds && task.timeout_seconds > 0 ? String(Math.ceil(task.timeout_seconds / 60)) : "";
  const initialRounds = task.max_rounds == null ? "" : String(task.max_rounds);
  const [timeoutMin, setTimeoutMin] = React.useState(initialTimeout);
  const [maxRounds, setMaxRounds] = React.useState(initialRounds);
  const [submitting, setSubmitting] = React.useState(false);
  const submitLock = React.useRef(false);

  async function resume() {
    if (submitLock.current) return;
    const input: { timeoutSeconds?: number; maxRounds?: number | null } = {};
    if (timeoutMin.trim() !== initialTimeout) {
      const minutes = parseLimitInput(timeoutMin);
      if (minutes === undefined) {
        toast.error("时间上限必须是不小于 0 的整数分钟");
        return;
      }
      input.timeoutSeconds = (minutes ?? 0) * 60;
    }
    if (maxRounds.trim() !== initialRounds) {
      const rounds = parseLimitInput(maxRounds);
      if (rounds === undefined) {
        toast.error("规划轮数上限必须是不小于 0 的整数");
        return;
      }
      if (rounds !== null && rounds > MAX_TASK_ROUNDS_LIMIT) {
        toast.error(`规划轮数上限最多 ${MAX_TASK_ROUNDS_LIMIT}`);
        return;
      }
      input.maxRounds = rounds;
    }
    submitLock.current = true;
    setSubmitting(true);
    try {
      const result = await api.resumeTask(task.id, input);
      onResumed?.(result);
      onClose();
      toast.success(result.queued ? `任务 #${task.id} 已继续，进入队列` : `任务 #${task.id} 已继续运行`);
    } catch (e) {
      toast.error(`继续运行失败：${(e as Error).message}`);
    } finally {
      submitLock.current = false;
      setSubmitting(false);
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && !submitLock.current && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>继续运行任务 #{task.id}</DialogTitle>
          <DialogDescription>
            任务已超时停止。继续后会按下面的上限重新开始计时与计轮；已完成的意图、事实与发现全部保留，规划者将针对仍未达成的目标继续派发。
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field data-disabled={submitting}>
            <FieldLabel htmlFor={timeoutId}>时间上限（分钟）</FieldLabel>
            <Input
              id={timeoutId}
              type="number"
              min={0}
              className="w-40"
              placeholder="留空 = 不限时"
              value={timeoutMin}
              disabled={submitting}
              onChange={(e) => setTimeoutMin(e.target.value)}
            />
            <FieldDescription>到点后触发优雅收尾，任务再次进入超时终态。</FieldDescription>
          </Field>
          <Field data-disabled={submitting}>
            <FieldLabel htmlFor={roundsId}>规划轮数上限</FieldLabel>
            <Input
              id={roundsId}
              type="number"
              min={0}
              max={MAX_TASK_ROUNDS_LIMIT}
              className="w-40"
              placeholder="留空 = 沿用系统设置"
              value={maxRounds}
              disabled={submitting}
              onChange={(e) => setMaxRounds(e.target.value)}
            />
            <FieldDescription>
              {roundsInheritHint(globalMax)}；填 0 = 不限。规划轮数是规划者被唤醒做决策的次数，达到上限同样会收尾。
            </FieldDescription>
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button variant="outline" disabled={submitting} onClick={onClose}>
            取消
          </Button>
          <Button disabled={submitting} onClick={() => void resume()}>
            {submitting ? <Spinner data-icon="inline-start" /> : <PlayIcon data-icon="inline-start" />}
            {submitting ? "正在继续…" : "继续运行"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
