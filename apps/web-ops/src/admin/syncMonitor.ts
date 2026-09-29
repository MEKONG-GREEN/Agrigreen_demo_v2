// Giám sát đồng bộ tích hợp (GIS US-SYNC-01/02) — logic thuần cho màn /quan-tri/giam-sat-dong-bo.
import type { AdminGet } from "@/api/types";

export type SyncIntegration = AdminGet<"/v1/sync/integrations">["items"][number];
export type SyncLogEntry = AdminGet<"/v1/sync/integrations/log">["items"][number];

export const SYNC_STATUS: Record<SyncIntegration["status"], { label: string; cls: string }> = {
  ok: { label: "Bình thường", cls: "c-ok" },
  error: { label: "Lỗi", cls: "c-bad" },
  idle: { label: "Chưa có dữ liệu", cls: "c-mute" },
};

export const LOG_STATUS: Record<SyncLogEntry["status"], { label: string; cls: string }> = {
  ok: { label: "Thành công", cls: "c-ok" },
  error: { label: "Lỗi", cls: "c-bad" },
};

export const WINDOW_OPTIONS: readonly { hours: number; label: string }[] = [
  { hours: 24, label: "24 giờ qua" },
  { hours: 72, label: "3 ngày qua" },
  { hours: 168, label: "7 ngày qua" },
  { hours: 720, label: "30 ngày qua" },
];

/** Độ sâu hàng chờ lỗi: null = không đo được (chưa cấu hình management API). */
export function dlqLabel(depth: number | null | undefined): string {
  return depth === null || depth === undefined ? "không đo được" : String(depth);
}

/** Nguồn có sự kiện trong hàng chờ lỗi (DLQ hiện tại > 0 hoặc có sự kiện đã vào DLQ trong cửa sổ). */
export function dlqAlerts(items: readonly SyncIntegration[]): SyncIntegration[] {
  return items.filter((i) => (i.dlq_depth ?? 0) > 0 || i.dead_lettered_count > 0);
}

/** Ngày YYYY-MM-DD (giờ Việt Nam) → mốc RFC 3339 đầu/cuối ngày. Rỗng → undefined. */
export function dayBound(day: string, end: boolean): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return undefined;
  return `${day}T${end ? "23:59:59" : "00:00:00"}+07:00`;
}
