"use client";

import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "./Icon";

/**
 * Hướng dẫn khi phiên chưa qua xác thực hai yếu tố: mọi màn hình gọi Admin API (host riêng) sẽ bị
 * từ chối 403 mfa_required. Chỉ để báo trước — máy chủ vẫn là nơi quyết định.
 */
export function MfaNotice({ what = "Admin API" }: { what?: string }) {
  const { principal } = useAuth();
  if (!principal || principal.mfa) return null;
  return (
    <div className="alert alert-warn" style={{ marginBottom: 16 }} role="status" data-testid="mfa-notice">
      <Icon name="alert" />
      <span>
        <b>Cần đăng nhập lại có mã OTP.</b> Phiên hiện tại chưa qua xác thực hai yếu tố — {what} sẽ từ chối mọi thao tác
        (<code>mfa_required</code>). Đăng xuất, đăng nhập lại và nhập mã OTP từ ứng dụng xác thực; nếu chưa cài OTP, liên hệ quản trị để
        được gửi liên kết cấu hình.
      </span>
    </div>
  );
}
