"use client";

import Link from "next/link";
import { useAuth } from "@/auth/AuthProvider";

export default function NoAccessPage() {
  const { logout, principal } = useAuth();
  const knAccount = principal?.roles.some((role) => role.startsWith("kn_") || role === "platform_admin");
  return (
    <div className="card sec" role="alert">
      <h1 style={{ fontSize: 20, marginBottom: 6 }}>Tài khoản chưa được phân quyền</h1>
      <p className="mute">
        Tài khoản của bạn chưa gắn với đơn vị công tác hoặc vai nào. Liên hệ quản trị đơn vị để được cấp quyền, sau đó đăng
        nhập lại.
      </p>
      {knAccount ? <p style={{ marginTop: 12 }}><Link className="btn btn-p btn-sm" href="/login/lien-he">Liên hệ quản trị viên</Link></p> : null}
      <p style={{ marginTop: 12 }}>
        <button type="button" className="btn btn-o btn-sm" onClick={() => void logout()}>
          Đăng xuất
        </button>
      </p>
    </div>
  );
}
