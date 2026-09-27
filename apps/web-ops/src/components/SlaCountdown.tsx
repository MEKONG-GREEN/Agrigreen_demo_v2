"use client";

import { useEffect, useState } from "react";
import { slaState } from "@/lib/sla";
import { formatDateTime } from "@/lib/format";
import { Icon } from "./Icon";

/** Đồng hồ dùng chung, cập nhật theo chu kỳ (mặc định 30 giây). */
export function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

const TONE_CLASS = { ok: "c-ok", warn: "c-warn", bad: "c-bad", done: "c-mute" } as const;

export function SlaCountdown({ dueAt, resolvedAt, now }: { dueAt: string; resolvedAt?: string | null; now: number }) {
  const s = slaState(dueAt, now, resolvedAt);
  return (
    <span className={`chip ${TONE_CLASS[s.tone]}`} title={`Hạn xử lý: ${formatDateTime(dueAt)}`}>
      <Icon name="clock" size={13} />
      {s.label}
    </span>
  );
}
