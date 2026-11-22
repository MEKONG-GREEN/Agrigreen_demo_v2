"use client";

import Link from "next/link";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";

/**
 * "Liên hệ quản trị viên" (FN-CH-00.01): yêu cầu hỗ trợ tài khoản. Quên mật khẩu tự đặt lại qua email ở /login/quen-mat-khau
 * (#255). Đầu mối liên hệ là TẠM — chờ khách cung cấp (Q-03, Q-19).
 */
export default function ContactAdminPage() {
  return (
    <div className="auth-bg">
      <main className="login" style={{ gridTemplateColumns: "minmax(0, 560px)", justifyContent: "center" }}>
        <section className="login-card">
          <div className="brand-row" style={{ color: "var(--slate-900)" }}>
            <BrandMark size={32} />
            {BRAND.productName}
          </div>
          <h1>Liên hệ quản trị viên</h1>
          <p>Cần hỗ trợ tài khoản (chưa có vai trò, phòng ban; tài khoản bị khóa; đổi đơn vị công tác)? Liên hệ quản trị viên hệ thống.</p>
          <p className="login-note">
            Đầu mối hỗ trợ (tạm, chờ khách): quản trị viên Mekong Green và cán bộ phụ trách tài khoản của {BRAND.orgFull}. Khi gọi, cung cấp email
            hoặc số điện thoại đăng nhập — không bao giờ đọc mật khẩu cho người khác.
          </p>
          <Link className="btn btn-p btn-block" href="/login">
            Quay lại đăng nhập
          </Link>
        </section>
      </main>
    </div>
  );
}
