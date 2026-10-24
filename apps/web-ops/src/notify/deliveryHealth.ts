// Tình trạng kênh gửi email/đẩy (P0-NOTIFY-FAILCLOSED, #127) — logic thuần cho cảnh báo quản trị.
// Admin API GET /v1/notify/delivery-health: kênh chưa cấu hình (SMTP/FCM/URL web-ops) là kênh TẮT — bản gửi ghi
// "dropped", không bao giờ "sent"; quản trị phải thấy rõ thông báo KHÔNG được gửi qua email/đẩy.
import type { AdminGet } from "@/api/types";
import { ROLES } from "@/auth/principal";

export type DeliveryHealth = AdminGet<"/v1/notify/delivery-health">;
export type ChannelHealth = DeliveryHealth["email"];

/** Vai được xem tình trạng kênh gửi (khớp port.DeliveryHealthRoles ở backend). */
export const DELIVERY_HEALTH_ROLES: readonly string[] = [ROLES.platformAdmin, ROLES.knNationalAdmin, ROLES.knProvinceAdmin, "admin"];

/** Lý do kênh TẮT (disabled_reason) → nhãn tiếng Việt ngắn. */
export const DISABLED_REASON_LABEL: Record<string, string> = {
  smtp_not_configured: "chưa cấu hình SMTP",
  link_base_missing: "chưa cấu hình URL web-ops cho liên kết trong email",
  fcm_not_configured: "chưa cấu hình FCM",
  disabled_by_config: "đã tắt bằng cấu hình",
};

export interface ChannelAlert {
  channel: "email" | "push";
  text: string;
}

const CHANNELS: readonly { key: ChannelAlert["channel"]; name: string }[] = [
  { key: "email", name: "Email" },
  { key: "push", name: "Thông báo đẩy" },
];

/** Câu cảnh báo cho một kênh, null khi kênh bình thường/chưa có lần gửi. */
export function channelAlert(name: string, h: ChannelHealth): string | null {
  if (h.status === "disabled") {
    const reason = (h.disabled_reason && DISABLED_REASON_LABEL[h.disabled_reason]) ?? "chưa cấu hình kênh gửi";
    return `${name} đang TẮT: ${reason} — thông báo không được gửi`;
  }
  if (h.status === "failing") return h.warning ?? `${name} đang gửi lỗi — thông báo không đến được người nhận`;
  return null;
}

/** Các kênh cần cảnh báo (TẮT hoặc gửi lỗi). */
export function deliveryHealthAlerts(h: DeliveryHealth | null | undefined): ChannelAlert[] {
  if (!h) return [];
  return CHANNELS.flatMap(({ key, name }) => {
    const text = channelAlert(name, h[key]);
    return text ? [{ channel: key, text }] : [];
  });
}
