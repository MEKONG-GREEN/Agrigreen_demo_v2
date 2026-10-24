"use client";

import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { useAsync } from "@/lib/useAsync";
import { DELIVERY_HEALTH_ROLES, deliveryHealthAlerts } from "./deliveryHealth";

/**
 * Cảnh báo quản trị khi kênh email/đẩy TẮT hoặc gửi lỗi (P0-NOTIFY-FAILCLOSED, #127). Chỉ hiện cho vai được xem
 * GET /v1/notify/delivery-health; không có quyền/chưa qua MFA/lỗi mạng → không hiện gì (không chặn trang).
 */
export function DeliveryHealthBanner() {
  const { adminApi, principal } = useAuth();
  // Admin API bắt buộc MFA: chưa qua MFA thì không gọi (máy chủ sẽ từ chối mfa_required).
  const allowed = !!principal?.mfa && hasRole(principal, ...DELIVERY_HEALTH_ROLES);
  const health = useAsync(() => (allowed ? unwrap(adminApi.GET("/v1/notify/delivery-health")) : Promise.resolve(null)), [adminApi, allowed]);
  const alerts = deliveryHealthAlerts(health.data);
  if (alerts.length === 0) return null;
  return (
    <div className="alert alert-bad" role="alert" data-testid="delivery-health-banner" style={{ marginBottom: 16 }}>
      <Icon name="alert" />
      <div>
        <b>Kênh gửi thông báo ra ngoài đang có vấn đề</b>
        <ul style={{ margin: "6px 0 0 18px" }}>
          {alerts.map((a) => (
            <li key={a.channel}>{a.text}</li>
          ))}
        </ul>
        <p className="small" style={{ marginTop: 6 }}>
          Thông báo vẫn hiển thị trong ứng dụng. Cấu hình kênh theo runbook <code>docs/runbooks/vps-deploy.md</code> (§3a).
        </p>
      </div>
    </div>
  );
}
