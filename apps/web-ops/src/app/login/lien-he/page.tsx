"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";

/**
 * "Quên mật khẩu?" và "Liên hệ quản trị viên" dẫn tới Admin (FN-CH-00.01): không có tự đặt lại mật khẩu
 * công khai. Đầu mối liên hệ là TẠM — chờ khách cung cấp (Q-03, Q-19).
 */
function ContactInner() {
  const forgot = useSearchParams().get("ly-do") === "quen-mat-khau";
  return (
    <div className="auth-bg">
      <main className="login" style={{ gridTemplateColumns: "minmax(0, 560px)", justifyContent: "center" }}>
        <section className="login-card">
          <div className="brand-row" style={{ color: "var(--slate-900)" }}>
            <BrandMark size={32} />
            {BRAND.productName}
          </div>
          <h1>{forgot ? "Quên mật khẩu" : "Liên hệ quản trị viên"}</h1>
          <p>
            {forgot
              ? "Mật khẩu do quản trị viên hệ thống đặt lại. Liên hệ quản trị viên của đơn vị để nhận liên kết đặt mật khẩu mới."
              : "Cần hỗ trợ tài khoản (chưa có vai trò, phòng ban; tài khoản bị khóa; đổi đơn vị công tác)? Liên hệ quản trị viên hệ thống."}
          </p>
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

export default function ContactAdminPage() {
  return (
    <Suspense fallback={null}>
      <ContactInner />
    </Suspense>
  );
}
