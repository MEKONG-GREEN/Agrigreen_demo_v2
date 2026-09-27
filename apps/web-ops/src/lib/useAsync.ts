"use client";

import { useEffect, useRef, useState } from "react";
import { ApiProblem } from "@/api/problem";

export interface AsyncState<T> {
  data: T | undefined;
  error: ApiProblem | null;
  loading: boolean;
  reload: () => void;
}

export function toApiProblem(e: unknown): ApiProblem {
  return e instanceof ApiProblem ? e : new ApiProblem({ type: "", title: String(e), status: 0, code: "internal_error" });
}

function sameDeps(a: readonly unknown[], b: readonly unknown[]): boolean {
  return a.length === b.length && a.every((v, i) => Object.is(v, b[i]));
}

/**
 * Số thứ tự yêu cầu: tăng khi deps đổi (so nông) hoặc khi gọi bump().
 * Dùng mẫu điều chỉnh state trong lúc render của React thay cho setState trong effect.
 */
export function useRequestKey(deps: readonly unknown[]): { value: number; bump: () => void } {
  const [prev, setPrev] = useState(deps);
  const [value, setValue] = useState(0);
  if (!sameDeps(prev, deps)) {
    setPrev(deps);
    setValue((v) => v + 1);
  }
  return { value, bump: () => setValue((v) => v + 1) };
}

/**
 * Nạp dữ liệu theo `deps`. Mỗi lần deps đổi (hoặc reload) sinh một "khóa yêu cầu";
 * kết quả chỉ được nhận nếu khớp khóa hiện tại → không ghi đè sai thứ tự.
 */
export function useAsync<T>(fn: () => Promise<T>, deps: readonly unknown[]): AsyncState<T> {
  const key = useRequestKey(deps);
  const [res, setRes] = useState<{ key: number; data?: T; error: ApiProblem | null } | null>(null);
  const fnRef = useRef(fn);
  useEffect(() => {
    fnRef.current = fn;
  });

  useEffect(() => {
    let active = true;
    fnRef.current().then(
      (data) => {
        if (active) setRes({ key: key.value, data, error: null });
      },
      (e: unknown) => {
        if (active) setRes({ key: key.value, error: toApiProblem(e) });
      },
    );
    return () => {
      active = false;
    };
  }, [key.value]);

  const loading = res?.key !== key.value;
  return {
    data: res?.data,
    error: loading ? null : (res?.error ?? null),
    loading,
    reload: key.bump,
  };
}
