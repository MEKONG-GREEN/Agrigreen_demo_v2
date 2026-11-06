"use client";

import Link from "next/link";
import { useId, useState, useSyncExternalStore, type FormEvent } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";

/** Mã trong liên kết email nằm sau '#token=' — không tới máy chủ web, không vào log hay Referer. */
function tokenFromHash(hash: string): string {
  return new URLSearchParams(hash.replace(/^#/u, "")).get("token") ?? "";
}

const noSubscribe = () => () => {};
const hashToken = () => tokenFromHash(window.location.hash);
/** Kết xuất ở máy chủ: chưa biết mã (null) — tránh lệch khi hydrate. */
const serverToken = () => null;

/** Xóa mã khỏi thanh địa chỉ và lịch sử khi liên kết đã dùng xong hoặc hết hiệu lực. */
function forgetToken() {
  window.history.replaceState(null, "", window.location.pathname);
}

/**
 * Đặt mật khẩu mới từ liên kết email (BRD 2.0 FN-CH-00.01, #255). Thành công: mọi phiên cũ bị thu hồi; tài khoản bắt MFA
 * vẫn nhập OTP khi đăng nhập lại.
 */
export default function ResetPasswordPage() {
  const { api } = useAuth();
  const id = useId();
  const token = useSyncExternalStore(noSubscribe, hashToken, serverToken);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expired, setExpired] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if ([...password].length < 12) return setError("Mật khẩu tối thiểu 12 ký tự.");
    if (password !== confirm) return setError("Hai lần nhập mật khẩu không khớp.");
    setBusy(true);
    try {
      const { response, error: problem } = await api.POST("/v1/public/password-reset/confirm", { body: { token: token ?? "", password } });
      if (response.status === 204 || response.status === 410) forgetToken();
      if (response.status === 204) setDone(true);
      else if (response.status === 410) setExpired(true);
      else if (response.status === 422) {
        const p = problem as { title?: string } | undefined;
        setError(p?.title ?? "Mật khẩu chưa đạt yêu cầu.");
      } else setError("Không đặt được mật khẩu. Vui lòng thử lại sau.");
    } catch {
      setError("Không kết nối được máy chủ. Vui lòng thử lại sau.");
    }
    setBusy(false);
  };

  const invalid = expired || token === "";
  return (
    <div className="auth-bg">
      <main className="login" style={{ gridTemplateColumns: "minmax(0, 560px)", justifyContent: "center" }}>
        <section className="login-card">
          <div className="brand-row" style={{ color: "var(--slate-900)" }}>
            <BrandMark size={32} />
            {BRAND.productName}
          </div>
          <h1>Đặt lại mật khẩu</h1>
          {done ? (
            <>
              <p role="status">Đã đặt mật khẩu mới. Mọi phiên đăng nhập cũ đã bị đăng xuất. Đăng nhập lại bằng mật khẩu mới.</p>
              <Link className="btn btn-p btn-block" href="/login">
                Đăng nhập
              </Link>
            </>
          ) : invalid ? (
            <>
              <p role="alert">Liên kết đặt lại mật khẩu không hợp lệ, đã dùng hoặc đã hết hạn (hiệu lực 30 phút). Hãy yêu cầu liên kết mới.</p>
              <Link className="btn btn-p btn-block" href="/login/quen-mat-khau">
                Yêu cầu liên kết mới
              </Link>
            </>
          ) : (
            <form onSubmit={submit} noValidate>
              <p className="sub">Mật khẩu mới tối thiểu 12 ký tự, khác tên đăng nhập và không trùng 5 mật khẩu gần nhất.</p>
              <div className="field">
                <label htmlFor={`${id}-p`} className="req">
                  Mật khẩu mới
                </label>
                <div className="pw-wrap">
                  <input
                    id={`${id}-p`}
                    className="input"
                    name="new-password"
                    type={show ? "text" : "password"}
                    autoComplete="new-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                  <button type="button" className="pw-toggle" onClick={() => setShow((v) => !v)} aria-pressed={show} aria-controls={`${id}-p`}>
                    {show ? "Ẩn" : "Hiện"}
                  </button>
                </div>
              </div>
              <div className="field">
                <label htmlFor={`${id}-c`} className="req">
                  Nhập lại mật khẩu mới
                </label>
                <input
                  id={`${id}-c`}
                  className="input"
                  name="confirm-password"
                  type={show ? "text" : "password"}
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
              </div>
              <button type="submit" className="btn btn-p btn-block" disabled={busy || token === null}>
                {busy ? "Đang lưu…" : "Đặt mật khẩu mới"}
              </button>
              {error ? (
                <div className="login-error" role="alert">
                  <Icon name="xCircle" />
                  <span>{error}</span>
                </div>
              ) : null}
            </form>
          )}
        </section>
      </main>
    </div>
  );
}
