// pf/notify — hộp thư trong app (ADR-0015: nội dung chỉ đọc qua API của ta; web không dùng websocket,
// huy hiệu hỏi lại mỗi 60 giây).
import type { ApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { Notification } from "@/api/types";

export const NOTIFY_POLL_MS = 60_000;

/** Sự kiện trình duyệt báo huy hiệu cần tải lại (sau khi đọc ở trang /thong-bao). */
export const NOTIFY_CHANGED_EVENT = "ag:notifications-changed";

export function announceNotificationsChanged(): void {
  if (typeof window !== "undefined") window.dispatchEvent(new Event(NOTIFY_CHANGED_EVENT));
}

export const KIND_LABEL: Record<Notification["kind"], string> = {
  directive: "Chỉ đạo",
  alert: "Cảnh báo",
  document: "Tài liệu mới",
  task_escalated: "Nhiệm vụ leo thang",
  sync_conflict: "Xung đột đồng bộ",
  org_deactivated: "Tổ chức ngừng hoạt động",
  household_registered: "Nông hộ mới từ App HTX",
  household_membership_changed: "Nông hộ đổi HTX",
  task_assigned: "Nhiệm vụ hỗ trợ mới",
  farm_log_pending: "Nhật ký chờ duyệt",
  farm_log_reviewed: "Kết quả duyệt nhật ký",
  season_opened: "Nông dân mở vụ",
  integration_dead_lettered: "Lỗi tích hợp (DLQ)",
  area_feed_stale: "Dữ liệu diện tích CGH quá hạn",
};

export function unreadCount(api: ApiClient): Promise<number> {
  return unwrap(api.GET("/v1/notifications/unread-count")).then((r) => r.unread);
}

export function markRead(api: ApiClient, id: string): Promise<Notification> {
  return unwrap(api.POST("/v1/notifications/{id}/read", { params: { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
}

export function markAllRead(api: ApiClient): Promise<number> {
  return unwrap(api.POST("/v1/notifications/read-all", { params: { header: { "Idempotency-Key": newIdempotencyKey() } } })).then((r) => r.marked);
}

/** "3 giờ trước" — thời gian tương đối cho hộp thư. */
export function relativeTime(iso: string, now: number = Date.now()): string {
  const diff = Math.max(0, now - Date.parse(iso));
  const m = Math.floor(diff / 60_000);
  if (m < 1) return "vừa xong";
  if (m < 60) return `${m} phút trước`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} giờ trước`;
  return `${Math.floor(h / 24)} ngày trước`;
}
