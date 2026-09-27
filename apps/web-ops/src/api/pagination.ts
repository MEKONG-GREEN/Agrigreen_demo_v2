"use client";

import { useEffect, useRef, useState } from "react";
import { toApiProblem, useRequestKey } from "@/lib/useAsync";
import type { Page } from "./types";
import type { ApiProblem } from "./problem";

export type PageFetcher<T> = (cursor: string | undefined) => Promise<Page<T>>;

/** Gom nhiều trang (R-53) tới khi hết cursor hoặc đạt `max` bản ghi. */
export async function collectPages<T>(fetchPage: PageFetcher<T>, max = 1000): Promise<T[]> {
  const out: T[] = [];
  let cursor: string | undefined;
  const seen = new Set<string>();
  do {
    const page = await fetchPage(cursor);
    out.push(...page.items);
    const next = page.next_cursor ?? undefined;
    if (next && seen.has(next)) break; // chống vòng lặp nếu server trả lại cursor cũ
    if (next) seen.add(next);
    cursor = next;
  } while (cursor && out.length < max);
  return out.slice(0, max);
}

export interface PagedList<T> {
  items: T[];
  loading: boolean;
  error: ApiProblem | null;
  hasMore: boolean;
  loadMore: () => void;
  reload: () => void;
}

interface PagedState<T> {
  key: number | null;
  items: T[];
  cursor: string | null;
  error: ApiProblem | null;
  busy: boolean;
}

/**
 * Danh sách phân trang cursor cho UI ("Tải thêm"). `deps` đổi (vd từ khoá tìm kiếm)
 * thì nạp lại từ trang đầu; kết quả của yêu cầu cũ bị bỏ qua.
 */
export function usePagedList<T>(fetchPage: PageFetcher<T>, deps: readonly unknown[]): PagedList<T> {
  const req = useRequestKey(deps);
  const key = req.value;
  const [st, setSt] = useState<PagedState<T>>({ key: null, items: [], cursor: null, error: null, busy: false });
  const fetchRef = useRef(fetchPage);
  useEffect(() => {
    fetchRef.current = fetchPage;
  });

  useEffect(() => {
    let active = true;
    fetchRef.current(undefined).then(
      (page) => {
        if (active) setSt({ key, items: page.items, cursor: page.next_cursor, error: null, busy: false });
      },
      (e: unknown) => {
        if (active) setSt({ key, items: [], cursor: null, error: toApiProblem(e), busy: false });
      },
    );
    return () => {
      active = false;
    };
  }, [key]);

  const initial = st.key !== key;

  const loadMore = () => {
    if (initial || st.busy || !st.cursor) return;
    const cursor = st.cursor;
    setSt((s) => ({ ...s, busy: true, error: null }));
    fetchRef.current(cursor).then(
      (page) => setSt((s) => (s.key === key ? { ...s, items: [...s.items, ...page.items], cursor: page.next_cursor, busy: false } : s)),
      (e: unknown) => setSt((s) => (s.key === key ? { ...s, error: toApiProblem(e), busy: false } : s)),
    );
  };

  return {
    items: initial ? [] : st.items,
    loading: initial || st.busy,
    error: initial ? null : st.error,
    hasMore: !initial && st.cursor !== null,
    loadMore,
    reload: req.bump,
  };
}
