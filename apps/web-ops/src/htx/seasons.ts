"use client";

import { useMemo } from "react";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { CropSeason } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { useAsync } from "@/lib/useAsync";

/** Mọi vụ (trong phạm vi) của HTX đang xem — tra mã lô / nông hộ cho nhật ký, sản lượng. */
export function useSeasonIndex(coopRef: string, deps: readonly unknown[] = []): { seasons: CropSeason[]; byRef: Map<string, CropSeason>; loading: boolean; reload: () => void } {
  const { api } = useAuth();
  const res = useAsync(
    () => collectPages((cursor) => unwrap(api.GET("/v1/cropcycle/seasons", { params: { query: { cooperative_ref: coopRef || undefined, cursor, limit: 200 } } })), 4000),
    [api, coopRef, ...deps],
  );
  const seasons = useMemo(() => res.data ?? [], [res.data]);
  const byRef = useMemo(() => new Map(seasons.map((s) => [s.season_ref, s])), [seasons]);
  return { seasons, byRef, loading: res.loading, reload: res.reload };
}
