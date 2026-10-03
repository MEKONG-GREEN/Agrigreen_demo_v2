"use client";

import { useState } from "react";
import { G5_REF_NAMES } from "@/api/mock/g5";
import type { ApiProblem } from "@/api/problem";
import type { Principal } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { config } from "@/config";
import { toApiProblem } from "@/lib/useAsync";
import { reasonError, shortRef } from "./rules";

/** Tên hiển thị của người/tenant: “Bạn” cho chính mình; tên giả lập ở chế độ mock; còn lại mã rút gọn. */
export function who(ref: string | null | undefined, p: Principal | null): string {
  if (!ref) return "—";
  if (p && ref === p.subject) return `Bạn (${p.displayName})`;
  if (config.mock && G5_REF_NAMES[ref]) return G5_REF_NAMES[ref];
  return shortRef(ref);
}

/** Chip trạng thái. */
export function Chip({ s }: { s: { label: string; chip: string } }) {
  return <span className={`chip ${s.chip}`}>{s.label}</span>;
}

/** Nút có gợi ý vì sao bị khóa (aria-describedby) — gợi ý chỉ phản ánh quy tắc của máy chủ (R-56). */
export function GuardedButton({
  id,
  label,
  busyLabel,
  block,
  busy,
  onClick,
  variant = "btn-p",
}: {
  id: string;
  label: string;
  busyLabel?: string;
  block: string | null;
  busy: boolean;
  onClick: () => void;
  variant?: string;
}) {
  return (
    <span className="stack" style={{ gap: 2, display: "inline-flex" }}>
      <button type="button" className={`btn ${variant} btn-sm`} disabled={!!block || busy} aria-describedby={block ? `${id}-why` : undefined} onClick={onClick}>
        {busy ? (busyLabel ?? "Đang xử lý…") : label}
      </button>
      {block ? (
        <span id={`${id}-why`} className="small mute" data-testid="sod-hint">
          {block}
        </span>
      ) : null}
    </span>
  );
}

/** Hộp thoại nhập lý do (10–500 ký tự, không ghi dữ liệu cá nhân) cho thao tác tạm ngưng/thu hồi. */
export function ReasonDialog({
  title,
  intro,
  confirmLabel,
  onClose,
  onConfirm,
}: {
  title: string;
  intro: string;
  confirmLabel: string;
  onClose: () => void;
  onConfirm: (reason: string) => Promise<void>;
}) {
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const err = reasonError(reason);
  const submit = async () => {
    setTouched(true);
    if (err) return;
    setBusy(true);
    setProblem(null);
    try {
      await onConfirm(reason.trim());
    } catch (e) {
      setProblem(toApiProblem(e));
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-d btn-sm" disabled={busy} onClick={() => void submit()}>
          {busy ? "Đang gửi…" : confirmLabel}
        </button>
      }
    >
      <p className="small">{intro}</p>
      <div className="field" style={{ marginTop: 8 }}>
        <label htmlFor="g5-reason">Lý do (10–500 ký tự, không ghi tên, số điện thoại)</label>
        <textarea id="g5-reason" className="input" rows={3} value={reason} onChange={(e) => setReason(e.target.value)} onBlur={() => setTouched(true)} />
        {touched && err ? <span className="err">{err}</span> : null}
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </Dialog>
  );
}
