"use client";

// Mảnh giao diện dùng chung của các màn hình App HTX.
import { useState, type ReactNode } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { CatalogItem, CatalogName } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { locationLabel, reasonError } from "./rules";

/** Danh mục đang dùng (giống lúa, vụ, máy) — GET /v1/masterdata/catalogs/{catalog}. */
export function useCatalog(catalog: CatalogName): CatalogItem[] {
  const { api } = useAuth();
  const res = useAsync(() => unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog }, query: { limit: 200 } } })), [api, catalog]);
  return res.data?.items ?? [];
}

export function LocationChip({ status, distance }: { status: string | null | undefined; distance?: string | null }) {
  const l = locationLabel(status);
  return (
    <span className={`chip ${l.cls}`} title={distance ? `Cách ranh giới thửa ${distance} m` : undefined}>
      {l.text}
    </span>
  );
}

export function StatusChip({ map, value }: { map: Record<string, { text: string; cls: string }>; value: string | null | undefined }) {
  const s = map[value ?? ""] ?? { text: value ?? "—", cls: "c-mute" };
  return <span className={`chip ${s.cls}`}>{s.text}</span>;
}

/**
 * Hộp thoại nhập lý do (≥ min ký tự) rồi gọi `run(reason, idemKey)`. Khóa Idempotency-Key giữ nguyên
 * khi thử lại sau lỗi mạng, đổi khi người dùng sửa nội dung.
 */
export function ReasonDialog({
  title,
  label,
  min = 10,
  confirmText,
  danger,
  children,
  onClose,
  run,
}: {
  title: string;
  label: string;
  min?: number;
  confirmText: string;
  danger?: boolean;
  children?: ReactNode;
  onClose: () => void;
  run: (reason: string, idemKey: string) => Promise<void>;
}) {
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const err = reasonError(reason, min);
  const submit = async () => {
    setTouched(true);
    if (err) return;
    setBusy(true);
    setProblem(null);
    try {
      await run(reason.trim(), key);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button type="button" className={`btn ${danger ? "btn-d" : "btn-p"} btn-sm`} disabled={busy} onClick={() => void submit()}>
          {busy ? "Đang lưu…" : confirmText}
        </button>
      }
    >
      <div className="stack" style={{ gap: 10 }}>
        {children}
        <div className="field">
          <label htmlFor="dlg-reason">{label}</label>
          <textarea
            id="dlg-reason"
            className="textarea"
            value={reason}
            aria-invalid={touched && !!err}
            aria-describedby="dlg-reason-err"
            onChange={(e) => {
              setReason(e.target.value);
              setKey(newIdempotencyKey());
            }}
          />
          {touched && err ? (
            <span id="dlg-reason-err" className="err">
              {err}
            </span>
          ) : null}
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </div>
    </Dialog>
  );
}

/** Bảng kết quả theo dòng/lô (bỏ qua kèm lý do) — HTX US-SEASON-02/04, US-LOG-03, US-YIELD-02, US-ACC-02, US-CAT-02. */
export function ResultList({ caption, rows }: { caption: string; rows: { key: string; label: ReactNode; ok: boolean; status: string; reason?: ReactNode }[] }) {
  if (rows.length === 0) return null;
  return (
    <div className="tbl-wrap" style={{ marginTop: 10 }}>
      <table className="tbl">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr>
            <th scope="col">Dòng / lô</th>
            <th scope="col">Kết quả</th>
            <th scope="col">Lý do</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.key}>
              <td>{r.label}</td>
              <td>
                <span className={`chip ${r.ok ? "c-ok" : "c-bad"}`}>{r.status}</span>
              </td>
              <td className="small">{r.reason ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
