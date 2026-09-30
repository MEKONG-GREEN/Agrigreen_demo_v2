"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { ApiProblem } from "@/api/problem";
import { toApiProblem, useRequestKey } from "./useAsync";

/** Cỡ trang mặc định của danh sách G4 (nút "Tải thêm" nối trang kế tiếp). */
export const LIST_PAGE_SIZE = 5;

/** Một trang của route liệt kê có con trỏ (`{items, next_cursor}`). */
export interface CursorPage<T> {
  items?: T[] | null;
  next_cursor?: string | null;
}

export interface PagedState<T> {
  /** Các mục đã nạp (mọi trang), undefined khi trang đầu chưa về. */
  items: T[] | undefined;
  loading: boolean;
  loadingMore: boolean;
  error: ApiProblem | null;
  hasMore: boolean;
  loadMore: () => void;
  reload: () => void;
}

interface Snap<T> {
  key: number;
  items: T[];
  next: string | null;
  busy: boolean;
  error: ApiProblem | null;
}

/**
 * Nạp danh sách theo con trỏ: trang đầu theo `deps` (đổi deps / reload → nạp lại từ đầu), "Tải thêm" nối trang
 * kế tiếp bằng `next_cursor`. Kết quả của yêu cầu cũ (khóa khác) bị bỏ — không ghi đè sai thứ tự.
 */
export function usePaged<T>(fetchPage: (cursor: string | null) => Promise<CursorPage<T>>, deps: readonly unknown[]): PagedState<T> {
  const key = useRequestKey(deps);
  const [snap, setSnap] = useState<Snap<T> | null>(null);
  const fnRef = useRef(fetchPage);
  useEffect(() => {
    fnRef.current = fetchPage;
  });

  useEffect(() => {
    let active = true;
    const k = key.value;
    fnRef.current(null).then(
      (p) => {
        if (active) setSnap({ key: k, items: p.items ?? [], next: p.next_cursor ?? null, busy: false, error: null });
      },
      (e: unknown) => {
        if (active) setSnap({ key: k, items: [], next: null, busy: false, error: toApiProblem(e) });
      },
    );
    return () => {
      active = false;
    };
  }, [key.value]);

  const cur = snap?.key === key.value ? snap : null;
  const loadMore = useCallback(() => {
    if (!cur?.next || cur.busy) return;
    const k = cur.key;
    const cursor = cur.next;
    setSnap((s) => (s && s.key === k ? { ...s, busy: true, error: null } : s));
    fnRef.current(cursor).then(
      (p) => setSnap((s) => (s && s.key === k ? { ...s, items: [...s.items, ...(p.items ?? [])], next: p.next_cursor ?? null, busy: false } : s)),
      (e: unknown) => setSnap((s) => (s && s.key === k ? { ...s, busy: false, error: toApiProblem(e) } : s)),
    );
  }, [cur]);

  return {
    items: cur ? cur.items : undefined,
    loading: !cur,
    loadingMore: cur?.busy ?? false,
    error: cur?.error ?? null,
    hasMore: !!cur?.next,
    loadMore,
    reload: key.bump,
  };
}
