"use client";

// Thành phần dùng chung của các màn vòng đời báo cáo KN v7.7 (P6s): tải dữ liệu, nhãn trạng thái, định dạng.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { ApiProblem } from "@/api/problem";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { StatusPill, type PillTone } from "@/components/patterns/list";
import type { LogEntry } from "@/components/patterns/detail";
import { formatDecimalVi } from "@/kn/grid/decimal";
import type { RcLogEntry, RcTask } from "./api";

export const TASK_TONE: Record<RcTask["status"], PillTone> = {
  not_started: "neutral",
  draft: "info",
  submitted: "ok",
  overdue: "bad",
  adjustment_requested: "warn",
  confirmed: "ok",
  closed: "neutral",
};

export const TASK_STATUS_LABEL: Record<RcTask["status"], string> = {
  not_started: "Chưa nhập",
  draft: "Lưu nháp",
  submitted: "Đã nộp",
  overdue: "Trễ hạn",
  adjustment_requested: "Yêu cầu điều chỉnh",
  confirmed: "Đã xác nhận",
  closed: "Đã chốt",
};

export function TaskPill({ status, label }: { status: RcTask["status"]; label?: string }) {
  return <StatusPill tone={TASK_TONE[status]}>{label ?? TASK_STATUS_LABEL[status]}</StatusPill>;
}

/** Thao tác theo trạng thái (Hình 13): Nhập / Tiếp tục / Sửa / Xem. */
export function taskAction(t: Pick<RcTask, "stored_status" | "open">): "Nhập" | "Tiếp tục" | "Sửa" | "Xem" {
  if (!t.open) return "Xem";
  if (t.stored_status === "not_started") return "Nhập";
  if (t.stored_status === "draft") return "Tiếp tục";
  if (t.stored_status === "adjustment_requested" || t.stored_status === "submitted") return "Sửa";
  return "Xem";
}

export const REPORT_TYPE_OPTIONS = [
  { value: "weekly", label: "Tuần", period: "week" },
  { value: "monthly", label: "Tháng", period: "month" },
  { value: "seasonal", label: "Hàng vụ", period: "season" },
  { value: "annual", label: "Năm", period: "year" },
] as const;

const pad = (n: number) => String(n).padStart(2, "0");
/** "07/09/2026 15:20" giờ Việt Nam (môi trường chạy đặt Asia/Ho_Chi_Minh). */
export function dt(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
export function ddmm(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
export const num = (v: string | null | undefined, maxDecimals = 1) => (v === null || v === undefined || v === "" ? "—" : formatDecimalVi(v, { maxDecimals, group: true }));

/** Nhãn tiếng Việt của sự kiện Log (P4 handler: batch.*, task.*, period.*, dispatch.*). */
export const LOG_LABEL: Record<string, string> = {
  "batch.issued": "Phát hành công việc",
  "batch.withdrawn": "Thu hồi đợt",
  "batch.deadline_changed": "Sửa hạn nộp",
  "task.draft_saved": "Lưu nháp",
  "task.submitted": "Gửi báo cáo",
  "task.adjustment_requested": "Yêu cầu điều chỉnh",
  "task.confirmed": "Xác nhận",
  "task.reminded.manual": "Nhắc nhở",
  "task.reminded.auto_before": "Tự nhắc trước hạn",
  "task.reminded.auto_overdue": "Tự nhắc khi trễ hạn",
  "period.closed": "Chốt kỳ",
  "period.auto_closed": "Hệ thống tự chốt kỳ",
  "period.reopened": "Mở lại kỳ",
  "period.reclosed": "Chốt lại kỳ",
  "dispatch.exported": "Xuất file",
  "dispatch.send_requested": "Gửi email",
  "dispatch.recipient_mailed": "Đã gửi tới một người nhận",
  "dispatch.sent": "Gửi email thành công",
  "dispatch.failed": "Gửi lỗi",
};

export function toLog(items: (RcLogEntry & { actor_display?: string })[]): LogEntry[] {
  return items.map((l) => ({
    event: (LOG_LABEL[l.event] ?? l.event) + (l.on_behalf_of ? " (Admin nhập thay)" : ""),
    actor_display: l.actor_display || "—",
    at: l.at,
    changes: (l.changes as { field: string; old: string | null; new: string | null }[]).filter((c) => c.field !== "submission_id"),
    reason: l.reason,
  }));
}

export interface Loaded<T> {
  data: T | null;
  problem: ApiProblem | null;
  loading: boolean;
  reload: () => void;
}

/** Tải dữ liệu theo khóa; đổi khóa → tải lại; kết quả cũ về muộn bị bỏ qua. */
export function useLoad<T>(load: (() => Promise<T>) | null, deps: unknown[]): Loaded<T> {
  const [state, setState] = useState<{ data: T | null; problem: ApiProblem | null; loading: boolean }>({ data: null, problem: null, loading: !!load });
  const seq = useRef(0);
  const [nonce, setNonce] = useState(0);
  const fn = useRef(load);
  useEffect(() => {
    fn.current = load;
  });
  useEffect(() => {
    const f = fn.current;
    if (!f) return;
    const my = ++seq.current;
    setState((s) => ({ ...s, loading: true }));
    f().then(
      (data) => my === seq.current && setState({ data, problem: null, loading: false }),
      (e: unknown) => my === seq.current && setState({ data: null, problem: e instanceof ApiProblem ? e : null, loading: false }),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce]);
  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { ...state, reload };
}

export function LoadState<T>({ s, children }: { s: Loaded<T>; children: (d: T) => ReactNode }) {
  if (s.problem) return <ProblemAlert problem={s.problem} onRetry={s.reload} />;
  if (!s.data) return <Loading />;
  return <>{children(s.data)}</>;
}

/** Thao tác có trạng thái chờ + lỗi + thông báo. */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const run = useCallback(async <T,>(f: () => Promise<T>, ok?: string | ((r: T) => string)): Promise<T | null> => {
    setBusy(true);
    setProblem(null);
    setNote(null);
    try {
      const r = await f();
      if (ok) setNote(typeof ok === "function" ? ok(r) : ok);
      return r;
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
      return null;
    } finally {
      setBusy(false);
    }
  }, []);
  const view = (
    <>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {note ? (
        <div className="alert alert-info" role="status">
          <Icon name="check" />
          <span>{note}</span>
        </div>
      ) : null}
    </>
  );
  return { busy, problem, note, run, view, setNote };
}

export function ReadOnlyNotice({ children }: { children?: ReactNode }) {
  return (
    <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
      <Icon name="eye" />
      <span>{children ?? "Chỉ xem — số liệu lấy từ lần cập nhật gần nhất của Báo cáo tổng hợp các xã; không có thao tác thêm, sửa, xóa."}</span>
    </div>
  );
}

/** Ngày yyyy-mm-dd theo giờ địa phương. */
export function ymdLocal(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
export function mondayLocal(d = new Date()): Date {
  const x = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  x.setDate(x.getDate() - ((x.getDay() + 6) % 7));
  return x;
}

// ---------------------------------------------------------------------------
// Tên xã/phường theo mã ĐVHC (GET /v1/masterdata/admin-units/{code}) — bộ nhớ đệm dùng chung
// ---------------------------------------------------------------------------
const NAMES = new Map<string, string>();
const PENDING = new Map<string, Promise<void>>();

export function useCommuneNames(codes: readonly string[]): (code: string) => string {
  const { api, kn } = useAuth();
  const [, bump] = useState(0);
  for (const c of kn.context?.communes ?? []) NAMES.set(c.code, c.name);
  const key = [...new Set(codes)].sort().join(",");
  useEffect(() => {
    const missing = key ? key.split(",").filter((c) => c && !NAMES.has(c)) : [];
    if (!missing.length) return;
    let alive = true;
    void Promise.all(
      missing.map((c) => {
        let p = PENDING.get(c);
        if (!p) {
          p = unwrap(api.GET("/v1/masterdata/admin-units/{code}", { params: { path: { code: c } } }))
            .then((u) => void NAMES.set(c, u.name))
            .catch(() => void NAMES.set(c, c));
          PENDING.set(c, p);
        }
        return p;
      }),
    ).then(() => alive && bump((n) => n + 1));
    return () => {
      alive = false;
    };
  }, [key, api]);
  return (code: string) => NAMES.get(code) ?? code;
}

/** Chỉ xem dữ liệu vòng đời báo cáo: Cấp Quốc gia (BRD §4.2) và Admin ngoài chế độ hỗ trợ (ghi qua Menu các cấp). */
export function useCycleReadOnly(): boolean {
  const { kn } = useAuth();
  return kn.context?.role === "kn_tw" || kn.context?.role === "admin";
}
