"use client";

import Link from "next/link";
import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { uuidv7 } from "@/api/idempotency";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { TextField, validateForm } from "@/components/form";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";

const empty = { name: "", contact: "", message: "" };

/** Q10/FN-CH-00.01: public create only; form content stays in component memory and is cleared after committed success. */
export default function AccountSupportForm() {
  const { api } = useAuth();
  const [values, setValues] = useState(empty);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [locked, setLocked] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const key = useRef<string | null>(null);
  const submitting = useRef(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (submitting.current) return;
    const checked = validateForm(values, {
      name: { kind: "text", required: true, maxLength: 100 },
      contact: { kind: "text", required: true, maxLength: 254 },
      message: { kind: "text", required: true, maxLength: 2000 },
    });
    if (values.message.trim().length < 10) checked.errors.message = "Nhập ít nhất 10 ký tự.";
    if (!/^\+?[0-9]{8,15}$/u.test(values.contact.trim()) && !/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/u.test(values.contact.trim())) checked.errors.contact = "Nhập email hoặc số điện thoại hợp lệ.";
    setFields(checked.errors);
    if (Object.keys(checked.errors).length) return;
    key.current ??= uuidv7();
    submitting.current = true; setBusy(true); setLocked(true); setError(null);
    try {
      await unwrap(api.POST("/v1/public/contacts/support-requests", {
        params: { header: { "Idempotency-Key": key.current } },
        body: { name: values.name.trim(), contact: values.contact.trim(), message: values.message.trim() },
        cache: "no-store",
      }));
      setValues(empty); setSent(true); key.current = null;
    } catch (e) {
      if (e instanceof ApiProblem && e.status === 422 && e.code === "validation_failed") {
        setFields(fieldErrorMap(e)); setLocked(false); key.current = null;
      }
      setError(e instanceof ApiProblem && e.status === 422 && e.code === "validation_failed"
        ? "Dữ liệu chưa hợp lệ. Kiểm tra các trường được đánh dấu."
        : e instanceof ApiProblem && e.status === 429
        ? "Bạn gửi quá nhiều lần. Vui lòng đợi ít phút rồi thử lại."
        : e instanceof ApiProblem && e.status === 503
          ? "Bộ phận hỗ trợ tạm thời chưa tiếp nhận được. Vui lòng thử lại sau."
          : "Chưa xác nhận được việc tiếp nhận. Bấm Thử lại để gửi lại cùng yêu cầu.");
    } finally { submitting.current = false; setBusy(false); }
  }

  return <div className="auth-bg"><main className="login" style={{ gridTemplateColumns: "minmax(0, 560px)", justifyContent: "center" }}>
    <section className="login-card">
      <div className="brand-row" style={{ color: "var(--slate-900)" }}><BrandMark size={32} />{BRAND.productName}</div>
      <h1>Liên hệ quản trị viên</h1>
      {sent ? <div role="status"><p>Yêu cầu đã được tiếp nhận trong hệ thống Khuyến nông. Quản trị viên sẽ xem và xử lý.</p>
        <Link className="btn btn-p btn-block" href="/login">Quay lại đăng nhập</Link></div>
        : <form onSubmit={submit} noValidate aria-label="Yêu cầu hỗ trợ tài khoản" aria-busy={busy}>
          <p>Cần hỗ trợ tài khoản hoặc chưa có vai trò, phòng ban đang áp dụng? Gửi yêu cầu đến Admin Khuyến nông.</p>
          <p className="login-note">Thông tin liên hệ chỉ dùng để xử lý yêu cầu này. Không nhập mật khẩu, mã OTP hoặc thông tin nhạy cảm khác.</p>
          <TextField label="Họ tên" name="name" required maxLength={100} value={values.name} onChange={(name) => setValues((v) => ({ ...v, name }))} error={fields.name} disabled={locked} />
          <TextField label="Email hoặc số điện thoại liên hệ" name="contact" required maxLength={254} value={values.contact} onChange={(contact) => setValues((v) => ({ ...v, contact }))} error={fields.contact} disabled={locked} />
          <TextField label="Nội dung cần hỗ trợ" name="message" required multiline maxLength={2000} value={values.message} onChange={(message) => setValues((v) => ({ ...v, message }))} error={fields.message} disabled={locked} />
          {error ? <p className="login-error" role="alert">{error}</p> : null}
          <button type="submit" className="btn btn-p btn-block" disabled={busy}>{busy ? "Đang gửi…" : error ? "Thử lại" : "Gửi yêu cầu"}</button>
          <p className="login-help"><Link href="/login">Quay lại đăng nhập</Link></p>
        </form>}
    </section>
  </main></div>;
}
