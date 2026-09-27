"use client";

import { useMemo } from "react";
import type { ApiClient } from "@/api/client";
import { unitResolver } from "@/api/adapters/units";
import type { AdminUnit } from "@/api/types";
import { useAsync } from "./useAsync";

const EMPTY: ReadonlyMap<string, AdminUnit> = new Map();

/** Bảng tra tên đơn vị hành chính cho các mã đang hiển thị (kèm đơn vị cha). Lỗi tra cứu → hiện mã. */
export function useUnits(api: ApiClient, codes: readonly (string | null | undefined)[]): ReadonlyMap<string, AdminUnit> {
  const key = useMemo(() => Array.from(new Set(codes.filter((c): c is string => !!c))).sort().join(","), [codes]);
  const res = useAsync(() => (key ? unitResolver(api).many(key.split(",")) : Promise.resolve(new Map<string, AdminUnit>())), [api, key]);
  return res.data ?? EMPTY;
}
