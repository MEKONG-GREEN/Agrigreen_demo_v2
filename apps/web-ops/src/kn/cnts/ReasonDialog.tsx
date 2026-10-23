"use client";

// Pop-up nhập lý do ngắn (BF-SWEEP-CNTS #101, UI-KIT): thay `window.prompt` khi mở lại / hủy kỳ, ngừng dùng mục danh mục…
// Pop-up chỉ còn cho lý do ngắn và xác nhận (R-A2). Kiểm độ dài như máy chủ trước khi gọi API (R-A4).
import { useState, type ReactNode } from "react";
import { Dialog } from "@/components/Dialog";
import { TextField } from "@/components/form";

export function reasonError(reason: string, min: number, max: number): string | undefined {
  const n = [...reason.trim()].length;
  if (n === 0) return "Bắt buộc nhập.";
  if (n < min) return `Tối thiểu ${min} ký tự.`;
  if (n > max) return `Tối đa ${max} ký tự.`;
  return undefined;
}

export function ReasonDialog({
  title,
  label = "Lý do",
  confirmLabel = "Xác nhận",
  min = 10,
  max = 500,
  busy,
  children,
  onConfirm,
  onClose,
}: {
  title: string;
  label?: string;
  confirmLabel?: string;
  min?: number;
  max?: number;
  busy?: boolean;
  children?: ReactNode;
  onConfirm: (reason: string) => void;
  onClose: () => void;
}) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | undefined>();
  const confirm = () => {
    const e = reasonError(reason, min, max);
    setError(e);
    if (!e) onConfirm(reason.trim());
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={confirm}>
          {confirmLabel}
        </button>
      }
    >
      {children}
      <TextField label={`${label} (${min}–${max} ký tự)`} required multiline maxLength={max} value={reason} onChange={setReason} error={error} />
    </Dialog>
  );
}
