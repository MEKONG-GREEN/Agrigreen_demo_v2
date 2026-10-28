// GIẢ LẬP tình trạng kênh gửi (P0-NOTIFY-FAILCLOSED, #127) — Admin API GET /v1/notify/delivery-health.
// Máy demo giả lập: email gửi bình thường, thông báo đẩy TẮT vì chưa cấu hình FCM (bản gửi "dropped", không "sent") —
// để màn quản trị thấy cảnh báo như trên VPS thiếu AG_FCM_*.
import type { AdminGet } from "../types";
import { hasRole, json, problem, type Route } from "./http";

type Health = AdminGet<"/v1/notify/delivery-health">;

const HOUR = 3_600_000;

export function mockDeliveryHealth(now: number = Date.now()): Health {
  const at = (h: number) => new Date(now - h * HOUR).toISOString();
  return {
    window_days: 7,
    email: { status: "ok", sent: 42, failed: 0, dropped: 0, pending: 0, disabled: 0, disabled_reason: null, last_disabled_at: null, last_sent_at: at(2), warning: null },
    push: {
      status: "disabled",
      sent: 0,
      failed: 0,
      dropped: 18,
      pending: 0,
      disabled: 18,
      disabled_reason: "fcm_not_configured",
      last_disabled_at: at(1),
      last_sent_at: null,
      warning: "Thông báo đẩy KHÔNG được gửi: chưa cấu hình dịch vụ đẩy FCM (AG_FCM_*). Thông báo chỉ hiển thị trong ứng dụng.",
    },
  };
}

export const notifyHealthAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/notify\/delivery-health$/,
    handler: ({ persona }) => {
      if (!hasRole(persona, "platform_admin", "kn_national_admin", "kn_province_admin", "admin")) return problem(403, "forbidden", "Forbidden");
      return json(mockDeliveryHealth());
    },
  },
];
