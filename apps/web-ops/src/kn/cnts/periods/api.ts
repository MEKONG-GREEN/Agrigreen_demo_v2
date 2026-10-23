// Client biểu kỳ Cấp Xã CNTS (W11.42 — BRD CNTS v3.7 UR-XA-06…09, 13): danh sách/cài biểu và phát hành lô kỳ
// (/v1/extension/cnts/*), công việc, nộp, sao chép kỳ trước, chốt/mở lại/hủy kỳ (/v1/forms/*, profile kn_cnts).
// openapi-fetch sinh từ contracts/openapi/application.yaml; Bearer, X-AG-Context, Idempotency-Key do createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseNumberInput, type NumberRule } from "@/components/form";
import { normalizeNumberInput } from "@/kn/grid/decimal";

type S = components["schemas"];
export type CntsForm = S["CNTSPeriodForm"];
export type CntsSeries = S["CNTSSeries"];
export type CntsSeriesInput = S["CNTSSeriesInput"];
export type FormsTask = S["FormsTask"];
export type FormsField = S["FormsField"];
export type FormsSubmission = S["FormsSubmission"];
export type FormsPeriodView = S["FormsPeriodView"];
export type FormsPrevious = S["FormsPrevious"];
export type TaskStatus = FormsTask["status"];

export const PROFILE = "kn_cnts";

export const STATUS_LABEL: Record<TaskStatus, string> = {
  not_submitted: "Chưa nhập",
  overdue: "Trễ hạn",
  submitted: "Đã nộp",
  accepted: "Đã xác thực",
  returned: "Yêu cầu điều chỉnh",
  missed: "Không nộp",
  closed_unadjusted: "Đã chốt – chưa điều chỉnh",
  awaiting_reopen: "Chờ mở kỳ",
};

export const STATUS_TONE: Record<TaskStatus, "neutral" | "ok" | "warn" | "bad" | "info"> = {
  not_submitted: "neutral",
  overdue: "bad",
  submitted: "info",
  accepted: "ok",
  returned: "warn",
  missed: "bad",
  closed_unadjusted: "warn",
  awaiting_reopen: "warn",
};

/** Nhóm trường theo `group` (giữ thứ tự xuất hiện). */
export function groupFields(fields: FormsField[]): { group: string; fields: FormsField[] }[] {
  const out: { group: string; fields: FormsField[] }[] = [];
  for (const f of fields) {
    const g = f.group ?? "";
    const last = out.find((x) => x.group === g);
    if (last) last.fields.push(f);
    else out.push({ group: g, fields: [f] });
  }
  return out;
}

const isNumeric = (f: FormsField) => f.type === "integer" || f.type === "decimal";

/** Quy tắc ô số của biểu (R-A1, UI-KIT): không âm (mọi trường số CNTS đều `non_negative`), nguyên/thập phân ≤ 6 số lẻ. */
export function numberRule(f: FormsField): NumberRule {
  return { required: !!f.required, integer: f.type === "integer", maxDecimals: f.type === "decimal" ? 6 : undefined };
}

/** Lỗi phía máy khách trước khi gửi (máy chủ kiểm lại): bắt buộc, kiểu số, không âm. Khóa trường → lý do. */
export function fieldErrors(fields: FormsField[], values: Record<string, string>, nilReport: boolean): Record<string, string> {
  const errs: Record<string, string> = {};
  for (const f of fields) {
    const v = (values[f.key] ?? "").trim();
    if (nilReport && isNumeric(f)) {
      if (v !== "" && Number(normalizeNumberInput(v)) !== 0) errs[f.key] = "Báo không phát sinh: phải bằng 0.";
      continue;
    }
    if (isNumeric(f)) {
      const p = parseNumberInput(v, numberRule(f));
      if (!p.ok) errs[f.key] = p.error;
      continue;
    }
    if (!v) {
      if (f.required) errs[f.key] = "Bắt buộc nhập.";
      continue;
    }
    if (f.type === "choice" && !(f.options ?? []).includes(v)) errs[f.key] = "Chọn trong danh sách.";
  }
  return errs;
}

/** Giá trị gửi đi: bỏ ô trống; ô số chuẩn hóa ("1.234,5" → "1234.5"); báo không phát sinh → mọi ô số = 0. */
export function payloadValues(fields: FormsField[], values: Record<string, string>, nilReport: boolean): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of fields) {
    let v = nilReport && isNumeric(f) ? "0" : (values[f.key] ?? "").trim();
    if (v !== "" && isNumeric(f)) {
      const p = parseNumberInput(v, numberRule(f));
      if (p.ok) v = p.value;
    }
    if (v !== "") out[f.key] = v;
  }
  return out;
}

/** Gửi sau hạn nộp (giờ máy) — cần lý do trễ (H09). */
export function isLate(task: Pick<FormsTask, "due_at">, now = Date.now()): boolean {
  return now > Date.parse(task.due_at);
}

export interface SubmitBody {
  unit: string;
  values: Record<string, string>;
  base_version: number;
  nil_report: boolean;
  note?: string;
  explanation?: string;
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createCntsPeriodApi(api: ApiClient) {
  const query = { profile: PROFILE };
  return {
    forms: async (): Promise<CntsForm[]> => (await unwrap(api.GET("/v1/extension/cnts/period-forms"))).items,
    install: async (): Promise<CntsForm[]> => (await unwrap(api.POST("/v1/extension/cnts/period-forms/install", { params: idem() }))).items,
    preview: (body: CntsSeriesInput): Promise<CntsSeries> => unwrap(api.POST("/v1/extension/cnts/period-series/preview", { body })),
    publish: (body: CntsSeriesInput): Promise<CntsSeries> => unwrap(api.POST("/v1/extension/cnts/period-series", { params: idem(), body })),
    tasks: async (code?: string): Promise<FormsTask[]> =>
      (await unwrap(api.GET("/v1/forms/tasks", { params: { query: { ...query, ...(code ? { code } : {}) } } }))).items,
    period: (id: string): Promise<FormsPeriodView> => unwrap(api.GET("/v1/forms/periods/{id}", { params: { path: { id }, query } })),
    previous: async (periodId: string, unit: string): Promise<FormsPrevious | null> =>
      (await unwrap(api.GET("/v1/forms/periods/{id}/previous", { params: { path: { id: periodId }, query: { ...query, unit } } }))).previous,
    submit: (periodId: string, body: SubmitBody): Promise<FormsSubmission> =>
      unwrap(api.POST("/v1/forms/periods/{id}/submissions", { params: { path: { id: periodId }, query, ...idem() }, body })),
    close: (periodId: string): Promise<FormsPeriodView> =>
      unwrap(api.POST("/v1/forms/periods/{id}/close", { params: { path: { id: periodId }, query, ...idem() } })),
    reopen: (periodId: string, reason: string, units: string[] = []): Promise<FormsPeriodView> =>
      unwrap(api.POST("/v1/forms/periods/{id}/reopen", { params: { path: { id: periodId }, query, ...idem() }, body: { reason, units } })),
    cancel: (periodId: string, reason: string): Promise<FormsPeriodView> =>
      unwrap(api.POST("/v1/forms/periods/{id}/cancel", { params: { path: { id: periodId }, query, ...idem() }, body: { reason } })),
  };
}

export type CntsPeriodApi = ReturnType<typeof createCntsPeriodApi>;

/** Gom công việc theo kỳ (màn theo dõi Cấp Tỉnh). */
export interface PeriodRow {
  periodId: string;
  code: string;
  title: string;
  label: string;
  dueAt: string;
  periodStatus: FormsTask["period_status"];
  total: number;
  submitted: number;
  overdue: number;
  missing: number;
}

export function periodRows(tasks: FormsTask[]): PeriodRow[] {
  const by = new Map<string, PeriodRow>();
  for (const t of tasks) {
    let r = by.get(t.period_id);
    if (!r) {
      r = { periodId: t.period_id, code: t.template_code, title: t.template_title, label: t.label, dueAt: t.due_at, periodStatus: t.period_status,
        total: 0, submitted: 0, overdue: 0, missing: 0 };
      by.set(t.period_id, r);
    }
    r.total++;
    if (t.version > 0) r.submitted++;
    if (t.status === "overdue") r.overdue++;
    if (t.status === "missed") r.missing++;
  }
  return [...by.values()];
}
