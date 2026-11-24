"use client";

import * as React from "react";

import { api } from "@/lib/api";

// 单任务规划轮数上限的绝对上限，与后端 db.MaxTaskRoundsLimit 一致。
export const MAX_TASK_ROUNDS_LIMIT = 500;

// parseLimitInput 把上限输入框解析成非负整数：空串返回 null（=留空），非法输入返回 undefined。
export function parseLimitInput(raw: string): number | null | undefined {
  const text = raw.trim();
  if (text === "") return null;
  if (!/^\d+$/.test(text)) return undefined;
  return Number(text);
}

// useGlobalMaxRounds 读取系统设置里的全局规划轮数上限，用于「留空 = 沿用系统设置」的提示；
// 读不到时保持 undefined，提示里就不带具体数字。
export function useGlobalMaxRounds(): number | undefined {
  const [value, setValue] = React.useState<number | undefined>(undefined);
  React.useEffect(() => {
    let alive = true;
    api
      .settings()
      .then((s) => {
        if (alive) setValue(s.auto_finish_max_rounds ?? 80);
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);
  return value;
}

export function roundsInheritHint(globalMax: number | undefined): string {
  if (globalMax === undefined) return "留空 = 沿用系统设置";
  if (globalMax > 0) return `留空 = 沿用系统设置（当前 ${globalMax} 轮）`;
  return "留空 = 沿用系统设置（当前不限）";
}
