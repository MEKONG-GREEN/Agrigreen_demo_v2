import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";

export type PendingPage243 = components["schemas"]["KHTCPendingReviewPage243"];
export const PENDING_INBOX243 = "/kn/khtc/phieu-cho-ra-soat";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function validCursor243(value: unknown): value is string {
  return typeof value === "string" && uuid.test(value) && value !== "00000000-0000-0000-0000-000000000000";
}

export type PendingFilters245 = { report_type?: "regular" | "adhoc"; period_id?: string; department?: string };

export async function readPending243(api: ApiClient, cursor: string | null, signal: AbortSignal, filters: PendingFilters245 = {}): Promise<PendingPage243> {
  if (cursor !== null && !validCursor243(cursor)) throw new Error("Con trỏ không hợp lệ");
  if (filters.report_type !== undefined && !["regular", "adhoc"].includes(filters.report_type)
    || filters.period_id !== undefined && !validCursor243(filters.period_id)
    || filters.department !== undefined && !/^[a-z][a-z0-9_]{0,39}$/.test(filters.department)) throw new Error("Bộ lọc không hợp lệ");
  const result = await unwrap(api.GET("/v1/extension/khtc/pending-review", {
    params: { query: { ...filters, ...(cursor ? { before: cursor } : {}) } }, signal, cache: "no-store",
  }));
  if (!Array.isArray(result.rows) || result.next_cursor !== null && (!validCursor243(result.next_cursor) || cursor !== null && result.next_cursor.toLowerCase() >= cursor.toLowerCase()))
    throw new Error("Trang phiếu không hợp lệ");
  const seen = new Set<string>();
  for (const row of result.rows) {
    const key = `${row.period_id}:${row.department}`;
    if (filters.period_id && row.period_id.toLowerCase() !== filters.period_id.toLowerCase() || filters.department && row.department !== filters.department
      || !validCursor243(row.period_id) || !validCursor243(row.submission_id) || !/^[a-z][a-z0-9_]{0,39}$/.test(row.department)
      || !Number.isSafeInteger(row.attempt) || row.attempt < 1 || !Number.isFinite(Date.parse(row.submitted_at)) || seen.has(key))
      throw new Error("Lần gửi trong hộp phiếu không hợp lệ");
    seen.add(key);
  }
  return result;
}
