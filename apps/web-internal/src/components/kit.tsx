"use client";

import { useCallback, useId, useState, type ReactNode } from "react";
import { unwrap } from "@/api/client";
import type { ApiProblem } from "@/api/problem";
import type { Facility } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { isSelf, shortRef, VERSION_STATE_LABEL } from "@/lib/rules";
import { recent, type RecentKind } from "@/lib/recent";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { Icon } from "./Icon";
import { ProblemAlert } from "./ProblemAlert";

// ---------------------------------------------------------------------------
// Thao tác ghi: trạng thái bận / lỗi RFC 9457 / thông báo thành công
// ---------------------------------------------------------------------------
export interface ActionState {
  busy: boolean;
  error: ApiProblem | null;
  ok: string | null;
  run: <T>(fn: () => Promise<T>, okMsg?: string | ((t: T) => string)) => Promise<T | undefined>;
  clear: () => void;
}

export function useAction(): ActionState {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const run = useCallback(async <T,>(fn: () => Promise<T>, okMsg?: string | ((t: T) => string)) => {
    setBusy(true);
    setError(null);
    setOk(null);
    try {
      const t = await fn();
      if (okMsg) setOk(typeof okMsg === "function" ? okMsg(t) : okMsg);
      return t;
    } catch (e) {
      setError(toApiProblem(e));
      return undefined;
    } finally {
      setBusy(false);
    }
  }, []);
  const clear = useCallback(() => {
    setError(null);
    setOk(null);
  }, []);
  return { busy, error, ok, run, clear };
}

export function ActionStatus({ a }: { a: Pick<ActionState, "error" | "ok"> }) {
  if (a.error) return <ProblemAlert problem={a.error} />;
  if (a.ok)
    return (
      <div className="alert alert-ok" role="status">
        <Icon name="check" />
        <span>{a.ok}</span>
      </div>
    );
  return null;
}

// ---------------------------------------------------------------------------
// Trường nhập có nhãn (a11y: label for/id)
// ---------------------------------------------------------------------------
export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children(id)}
      {hint ? <span className="small mute">{hint}</span> : null}
    </div>
  );
}

export function TextField({
  label,
  value,
  onChange,
  hint,
  type = "text",
  placeholder,
  multiline,
  inputMode,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  hint?: ReactNode;
  type?: string;
  placeholder?: string;
  multiline?: boolean;
  inputMode?: "decimal" | "numeric" | "text";
}) {
  return (
    <Field label={label} hint={hint}>
      {(id) =>
        multiline ? (
          <textarea id={id} className="textarea" value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
        ) : (
          <input id={id} className="input" type={type} value={value} placeholder={placeholder} inputMode={inputMode} onChange={(e) => onChange(e.target.value)} />
        )
      }
    </Field>
  );
}

export function SelectField({ label, value, onChange, options, hint }: { label: string; value: string; onChange: (v: string) => void; options: { value: string; label: string }[]; hint?: ReactNode }) {
  return (
    <Field label={label} hint={hint}>
      {(id) => (
        <select id={id} className="select input" value={value} onChange={(e) => onChange(e.target.value)}>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}

// ---------------------------------------------------------------------------
// Hiển thị
// ---------------------------------------------------------------------------
const CHIP: Record<string, string> = {
  pending: "c-warn",
  approved: "c-ok",
  active: "c-ok",
  rejected: "c-bad",
  revoked: "c-bad",
  superseded: "c-mute",
  within: "c-ok",
  within_tolerance: "c-ok",
  normal: "c-ok",
  match: "c-ok",
  flag: "c-warn",
  loss_alert: "c-warn",
  abnormal: "c-warn",
  mismatch: "c-bad",
  critical: "c-bad",
  dispute_opened: "c-bad",
  gain: "c-bad",
  open: "c-warn",
  under_review: "c-info",
  resolved: "c-ok",
  cancelled: "c-mute",
  informational: "c-info",
  pending_rolls: "c-mute",
  not_applicable: "c-mute",
  direct_aggregate: "c-info",
  in_transit: "c-info",
  received: "c-ok",
  planned: "c-warn",
  assigned: "c-info",
  closed: "c-mute",
};

export const STATUS_LABEL: Record<string, string> = {
  ...VERSION_STATE_LABEL,
  within: "Trong dung sai",
  within_tolerance: "Trong dung sai",
  normal: "Bình thường",
  match: "Khớp",
  flag: "Gắn cờ",
  loss_alert: "Cảnh báo hao hụt",
  abnormal: "Hao hụt bất thường",
  mismatch: "Lệch",
  critical: "Nghiêm trọng",
  dispute_opened: "Mở tranh chấp",
  gain: "Tăng bất thường",
  open: "Đang mở",
  under_review: "Đang xem xét",
  resolved: "Đã giải quyết",
  cancelled: "Đã huỷ",
  informational: "Chỉ tham khảo (theo cuộn)",
  pending_rolls: "Chờ số cuộn",
  not_applicable: "Không áp dụng",
  direct_aggregate: "Giao thẳng — gộp",
  in_transit: "Đang đi đường",
  received: "Đã nhận",
  planned: "Chờ duyệt",
  assigned: "Đã giao tổ",
  closed: "Đã đóng",
};

export function Chip({ status, label }: { status: string | null | undefined; label?: string }) {
  const s = status ?? "";
  return <span className={`chip ${CHIP[s] ?? "c-mute"}`}>{label ?? STATUS_LABEL[s] ?? (s || "—")}</span>;
}

/** Mã tham chiếu rút gọn, đầy đủ trong title (UUID). */
export function Ref({ id, label }: { id: string | null | undefined; label?: string }) {
  if (!id) return <span className="mute">—</span>;
  return (
    <span className="mono" title={id}>
      {label ?? shortRef(id)}
    </span>
  );
}

/** Gợi ý R-56 khi người đang xem là người đề xuất (máy chủ vẫn chặn). */
export function SelfHint({ proposer }: { proposer: string | null | undefined }) {
  const { principal } = useAuth();
  if (!isSelf(principal?.subject, proposer)) return null;
  return <span className="notice">Bạn là người đề xuất — cần một người phê duyệt khác (R-56)</span>;
}

/** Nhãn "tạm, chờ khách" cho tham số/quy tắc chưa được khách chốt. */
export function Provisional({ what = "C-4" }: { what?: string }) {
  return (
    <span className="chip c-warn" title={`Mặc định tạm theo prototype — chờ khách chốt (${what})`}>
      Tạm, chờ khách {what}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Chọn cơ sở / chọn mã gần đây
// ---------------------------------------------------------------------------
export function useFacilities() {
  const { api } = useAuth();
  return useAsync(async () => (await unwrap(api.GET("/v1/hub/facilities", { params: { query: { limit: 200 } } }))).items ?? [], [api]);
}

export function FacilitySelect({ value, onChange, label = "Cơ sở", facilities, exclude }: { value: string; onChange: (v: string) => void; label?: string; facilities: Facility[]; exclude?: string }) {
  return (
    <SelectField
      label={label}
      value={value}
      onChange={onChange}
      options={[{ value: "", label: "— Chọn cơ sở —" }, ...facilities.filter((f) => f.id !== exclude).map((f) => ({ value: f.id ?? "", label: `${f.code} · ${f.name}` }))]}
    />
  );
}

/**
 * Chọn mã (lô, phiên cân, phiếu chuyển, HTX): danh sách gợi ý gồm mã đã dùng gần đây trong tab + mã bổ sung,
 * vẫn cho nhập UUID bất kỳ (api-internal chưa có route liệt kê — xem README).
 */
export function RefPicker({ label, kind, value, onChange, extra = [], hint }: { label: string; kind: RecentKind; value: string; onChange: (v: string) => void; extra?: { id: string; label: string }[]; hint?: ReactNode }) {
  const listId = useId();
  const opts = [...extra, ...recent(kind).filter((r) => !extra.some((e) => e.id === r.id))];
  return (
    <Field label={label} hint={hint ?? "Chọn từ gợi ý hoặc dán mã UUID"}>
      {(id) => (
        <>
          <input id={id} className="input mono" list={listId} value={value} onChange={(e) => onChange(e.target.value.trim())} placeholder="0192f…" />
          <datalist id={listId}>
            {opts.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </datalist>
          {opts.length ? (
            <div className="row" role="group" aria-label={`Gợi ý ${label.toLowerCase()}`}>
              {opts.slice(0, 12).map((o) => (
                <button key={o.id} type="button" className="btn btn-o btn-sm" aria-pressed={o.id === value} onClick={() => onChange(o.id)}>
                  {o.label}
                </button>
              ))}
            </div>
          ) : null}
        </>
      )}
    </Field>
  );
}

export function Section({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="card sec" aria-label={title}>
      <div className="sec-h">
        <h2>{title}</h2>
        {actions ? <div className="row">{actions}</div> : null}
      </div>
      {children}
    </section>
  );
}

export function nowLocalInput(): string {
  const d = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000);
  return d.toISOString().slice(0, 16);
}

/** Giá trị input datetime-local → ISO 8601 (múi giờ trình duyệt). */
export function localToIso(v: string): string {
  const t = Date.parse(v);
  return Number.isFinite(t) ? new Date(t).toISOString() : v;
}
