"use client";

import { useEffect, useRef } from "react";
import { useAction } from "@/kn/cycle/common";

export function usePointAction() {
  const action = useAction();
  const alive = useRef(true);
  const lock = useRef(false);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const run = async <T,>(fn: () => Promise<T>): Promise<T | null> => {
    if (lock.current || !alive.current) return null;
    lock.current = true;
    try {
      const r = await action.run(fn);
      return alive.current ? r : null;
    } finally {
      lock.current = false;
    }
  };
  return { ...action, run };
}
