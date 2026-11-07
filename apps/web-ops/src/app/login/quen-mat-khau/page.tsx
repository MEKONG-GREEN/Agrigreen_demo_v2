"use client";

import Link from "next/link";
import { useId, useState, type FormEvent } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";

/**
 * "Quên mật khẩu?" (BRD 2.0 FN-CH-00.01, #255): nhập email, nhận liên kết đặt lại (hiệu lực 30 phút, dùng một lần, tối đa
 * 5 yêu cầu mỗi giờ cho một tài khoản) và tự đặt mật khẩu mới, không qua Admin. Thông báo giống nhau dù email có tài khoản
 * hay không (không lộ email có tồn tại).
 */
export default function ForgotPasswordPage() {
  const { api } = useAuth();
  const id = useId();
  const [email, setEmail] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const missing = touched && !email.trim();

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!email.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const { response } = await api.POST("/v1/public/password-reset", { body: { email: email.trim() } });
      if (response.status === 202) setSent(true);
      else if (response.status === 422) setError("Email không hợp lệ. Kiểm tra lại địa chỉ email.");
      else if (response.status === 429) setError("Bạn đã yêu cầu quá nhiều lần. Vui lòng thử lại sau ít phút.");
      else setError("Không gửi được yêu cầu. Vui lòng thử lại sau.");
    } catch {
      setError("Không kết nối được máy chủ. Vui lòng thử lại sau.");
    }
    setBusy(false);
  };

  return (
    <div className="auth-bg">
      <main className="login" style={{ gridTemplateColumns: "minmax(0, 560px)", justifyContent: "center" }}>
        <section className="login-card">
          <div className="brand-row" style={{ color: "var(--slate-900)" }}>
            <BrandMark size={32} />
            {BRAND.productName}
          </div>
          <h1>Quên mật khẩu</h1>
          {sent ? (
            <>
              <p role="status">
                Nếu email này thuộc một tài khoản đang hoạt động, hệ thống đã gửi liên kết đặt lại mật khẩu. Liên kết có hiệu lực
                30 phút và chỉ dùng được một lần. Kiểm tra cả thư mục thư rác.
              </p>
              <Link className="btn btn-p btn-block" href="/login">
                Quay lại đăng nhập
              </Link>
            </>
          ) : (
            <form onSubmit={submit} noValidate aria-labelledby={`${id}-h`}>
              <p className="sub" id={`${id}-h`}>
                Nhập email của tài khoản. Hệ thống gửi liên kết để bạn tự đặt mật khẩu mới.
              </p>
              <div className="field">
                <label htmlFor={`${id}-e`} className="req">
                  Email
                </label>
                <input
                  id={`${id}-e`}
                  className="input"
                  name="email"
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  aria-invalid={missing || undefined}
                  aria-describedby={missing ? `${id}-ee` : undefined}
                />
                {missing ? (
                  <span className="err" id={`${id}-ee`}>
                    Nhập email.
                  </span>
                ) : null}
              </div>
              <button type="submit" className="btn btn-p btn-block" disabled={busy}>
                {busy ? "Đang gửi…" : "Gửi liên kết đặt lại"}
              </button>
              {error ? (
                <div className="login-error" role="alert">
                  <Icon name="xCircle" />
                  <span>{error}</span>
                </div>
              ) : null}
              <p className="login-help">
                <Link href="/login">Quay lại đăng nhập</Link> · <Link href="/login/lien-he">Liên hệ quản trị viên</Link>
              </p>
            </form>
          )}
        </section>
      </main>
    </div>
  );
}
