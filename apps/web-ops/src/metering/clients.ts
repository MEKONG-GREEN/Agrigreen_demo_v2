"use client";

import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { ApiClientRecord, MeteringStatement } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { API_CLIENT_ROLES, hasRole } from "@/auth/principal";
import { shortRef } from "@/apiclient/rules";
import { useAsync } from "@/lib/useAsync";

export interface ClientOption {
  id: string;
  label: string;
}

/**
 * Khách hàng để chọn ở màn hình đo lường. Người có vai khách hàng API đọc tên tổ chức từ
 * GET /v1/api-clients; người chỉ có vai đo lường (metering_viewer, billing_*) không đọc được danh sách đó
 * (403) → lấy mã khách hàng từ các bảng kê (không tên).
 */
export function useClientOptions() {
  const { adminApi, principal } = useAuth();
  const canList = hasRole(principal, ...API_CLIENT_ROLES);
  return useAsync(async (): Promise<ClientOption[]> => {
    if (canList) {
      const items = await collectPages<ApiClientRecord>((cursor) => unwrap(adminApi.GET("/v1/api-clients", { params: { query: { cursor, limit: 100 } } })), 500);
      return items.filter((c) => c.status !== "proposed").map((c) => ({ id: c.id, label: c.display_name }));
    }
    const sts = await collectPages<MeteringStatement>((cursor) => unwrap(adminApi.GET("/v1/metering/statements", { params: { query: { cursor, limit: 200 } } })), 1000);
    const ids = Array.from(new Set(sts.map((s) => s.client_id)));
    return ids.map((id) => ({ id, label: `Khách hàng ${shortRef(id)}` }));
  }, [adminApi, canList]);
}

export function clientLabel(options: readonly ClientOption[] | undefined, id: string | null | undefined): string {
  if (!id) return "Mọi khách hàng";
  return options?.find((o) => o.id === id)?.label ?? `Khách hàng ${shortRef(id)}`;
}
