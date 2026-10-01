"use client";

import Link from "next/link";
import { useId, useState, type FormEvent } from "react";
import { Icon } from "@/components/Icon";

export type LoginError =
  | { kind: "invalid"; remaining: number }
  | { kind: "locked"; minutes: number }
  | { kind: "message"; text: string };

export interface LoginCardProps {
  /**
   * "password": thẻ đủ trường (chế độ giả lập). "oidc": chỉ email/SĐT — mật khẩu nhập ở trang xác thực
   * Keycloak (theme agrigreen của P1), web không bao giờ nhận mật khẩu (OIDC + PKCE).
   */
  mode: "password" | "oidc";
  busy?: boolean;
  error?: LoginError | null;
  initialIdentifier?: string;
  initialRemember?: boolean;
  onSubmit: (v: { identifier: string; password: string; remember: boolean }) => void;
}

const HELP_HREF = "/login/lien-he";

/** Thẻ "Đăng nhập" (Hình 3a): email/SĐT, mật khẩu + "Hiện", Ghi nhớ, Quên mật khẩu, banner lỗi, Liên hệ quản trị viên. */
export function LoginCard({ mode, busy, error, initialIdentifier = "", initialRemember = false, onSubmit }: LoginCardProps) {
  const id = useId();
  const [identifier, setIdentifier] = useState(initialIdentifier);
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [remember, setRemember] = useState(initialRemember);
  const [touched, setTouched] = useState(false);

  const idMissing = touched && !identifier.trim();
  const pwMissing = touched && mode === "password" && !password;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!identifier.trim() || (mode === "password" && !password)) return;
    onSubmit({ identifier: identifier.trim(), password, remember });
  };

  return (
    <form className="login-card" onSubmit={submit} noValidate aria-labelledby={`${id}-h`}>
      <h1 id={`${id}-h`}>Đăng nhập</h1>
      <p className="sub">Nhập thông tin tài khoản để tiếp tục.</p>
      <div className="field">
        <label htmlFor={`${id}-u`} className="req">
          Email hoặc số điện thoại
        </label>
        <input
          id={`${id}-u`}
          className="input"
          name="username"
          autoComplete="username"
          inputMode="email"
          value={identifier}
          onChange={(e) => setIdentifier(e.target.value)}
          aria-invalid={idMissing || undefined}
          aria-describedby={idMissing ? `${id}-ue` : undefined}
        />
        {idMissing ? (
          <span className="err" id={`${id}-ue`}>
            Nhập email hoặc số điện thoại.
          </span>
        ) : null}
      </div>
      {mode === "password" ? (
        <div className="field">
          <label htmlFor={`${id}-p`} className="req">
            Mật khẩu
          </label>
          <div className="pw-wrap">
            <input
              id={`${id}-p`}
              className="input"
              name="password"
              type={show ? "text" : "password"}
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              aria-invalid={pwMissing || undefined}
              aria-describedby={pwMissing ? `${id}-pe` : undefined}
            />
            <button type="button" className="pw-toggle" onClick={() => setShow((v) => !v)} aria-pressed={show} aria-controls={`${id}-p`}>
              {show ? "Ẩn" : "Hiện"}
            </button>
          </div>
          {pwMissing ? (
            <span className="err" id={`${id}-pe`}>
              Nhập mật khẩu.
            </span>
          ) : null}
        </div>
      ) : (
        <p className="login-note">Mật khẩu (và mã OTP với tài khoản Admin) nhập ở trang xác thực của hệ thống sau khi bấm Đăng nhập.</p>
      )}
      <div className="login-opts">
        <label>
          <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
          Ghi nhớ đăng nhập
        </label>
        <Link href={`${HELP_HREF}?ly-do=quen-mat-khau`}>Quên mật khẩu?</Link>
      </div>
      <button type="submit" className="btn btn-p btn-block" disabled={busy}>
        {busy ? "Đang đăng nhập…" : "Đăng nhập"}
      </button>
      {error ? (
        <div className="login-error" role="alert">
          <Icon name="xCircle" />
          {error.kind === "invalid" ? (
            <span>
              <b>Sai tài khoản hoặc mật khẩu.</b>
              Kiểm tra lại thông tin. Còn {error.remaining} lần thử.
            </span>
          ) : error.kind === "locked" ? (
            <span>
              <b>Tài khoản tạm khóa {error.minutes} phút.</b>
              Đã nhập sai 5 lần. Thử lại sau hoặc liên hệ quản trị viên.
            </span>
          ) : (
            <span>{error.text}</span>
          )}
        </div>
      ) : null}
      <p className="login-help">
        Cần hỗ trợ? <Link href={HELP_HREF}>Liên hệ quản trị viên</Link>
      </p>
    </form>
  );
}
