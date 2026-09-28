"use client";

import { useEffect, useRef } from "react";

/**
 * Gọi `fn` ngay khi gắn và sau mỗi `ms` (null = dừng). Tab ẩn thì bỏ qua lượt gọi để không tốn lời gọi API;
 * tab hiện lại thì gọi ngay một lần.
 */
export function useInterval(fn: () => void, ms: number | null): void {
  const ref = useRef(fn);
  useEffect(() => {
    ref.current = fn;
  });
  useEffect(() => {
    if (ms === null) return;
    const tick = () => {
      if (typeof document === "undefined" || document.visibilityState !== "hidden") ref.current();
    };
    tick();
    const t = setInterval(tick, ms);
    const onVisible = () => {
      if (document.visibilityState === "visible") ref.current();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(t);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [ms]);
}
