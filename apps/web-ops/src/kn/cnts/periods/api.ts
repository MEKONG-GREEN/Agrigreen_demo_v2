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
export type CntsTracking = S["CNTSTracking"];
export type CntsDraft = S["CNTSPeriodDraft"];
export type CntsTarget = NonNullable<CntsSeriesInput["target"]>;

/** Đối tượng phát hành (CN-07). */
export const TARGET_LABEL: Record<CntsTarget, string> = {
  province: "Tất cả xã của tỉnh",
  regions: "Theo khu vực",
  aquaculture: "Xã có nuôi thủy sản",
  model: "Xã có mô hình",
  communes: "Chọn từng xã",
  facilities: "Cơ sở giết mổ",
};

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
    /** Lịch sử phiên bản bài nộp của đơn vị (mới trước) — khung Log (BF-CN-WEEKLY). */
    submissions: async (periodId: string, unit: string): Promise<FormsSubmission[]> =>
      (await unwrap(api.GET("/v1/forms/periods/{id}/submissions", { params: { path: { id: periodId }, query: { ...query, unit } } }))).items,
    submit: (periodId: string, body: SubmitBody): Promise<FormsSubmission> =>
      unwrap(api.POST("/v1/forms/periods/{id}/submissions", { params: { path: { id: periodId }, query, ...idem() }, body })),
    close: (periodId: string): Promise<FormsPeriodView> =>
      unwrap(api.POST("/v1/forms/periods/{id}/close", { params: { path: { id: periodId }, query, ...idem() } })),
    reopen: (periodId: string, reason: string, units: string[] = []): Promise<FormsPeriodView> =>
      unwrap(api.POST("/v1/forms/periods/{id}/reopen", { params: { path: { id: periodId }, query, ...idem() }, body: { reason, units } })),
    cancel: (periodId: string, reason: string): Promise<FormsPeriodView> =>
      unwrap(api.POST("/v1/forms/periods/{id}/cancel", { params: { path: { id: periodId }, query, ...idem() }, body: { reason } })),
    // BF-CN-PUBLISH (CN-07): người phụ trách, giờ chốt, dấu nháp; nháp của xã; nút Nhắc.
    tracking: (code?: string, periodIds: string[] = []): Promise<CntsTracking> =>
      unwrap(api.GET("/v1/extension/cnts/period-tracking", {
        params: { query: { ...(code ? { code } : {}), ...(periodIds.length ? { period_ids: periodIds.slice(0, 400).join(",") } : {}) } },
      })),
    draft: async (periodId: string, unit: string): Promise<CntsDraft | null> =>
      (await unwrap(api.GET("/v1/extension/cnts/period-drafts/{id}", { params: { path: { id: periodId }, query: { unit } } }))).draft ?? null,
    saveDraft: (periodId: string, body: { unit: string; values: Record<string, string>; nil_report: boolean; note?: string }): Promise<CntsDraft> =>
      unwrap(api.PUT("/v1/extension/cnts/period-drafts/{id}", { params: { path: { id: periodId } }, body })),
    remind: (periodId: string, units: string[] = []): Promise<{ reminded: number; skipped: string[] }> =>
      unwrap(api.POST("/v1/extension/cnts/period-reminders", { params: idem(), body: { period_id: periodId, ...(units.length ? { units } : {}) } })),
  };
}

export type CntsPeriodApi = ReturnType<typeof createCntsPeriodApi>;

/** Trạng thái theo dõi của một đơn vị (CN-07): đã nộp / nháp / chưa nhập / trễ / trả lại / không nộp. */
export type TrackState = "submitted" | "draft" | "not_entered" | "overdue" | "returned" | "missed";

export const TRACK_LABEL: Record<TrackState, string> = {
  submitted: "Đã nộp",
  draft: "Nháp",
  not_entered: "Chưa nhập",
  overdue: "Trễ hạn",
  returned: "Trả lại",
  missed: "Không nộp",
};

export const TRACK_TONE: Record<TrackState, "neutral" | "ok" | "warn" | "bad" | "info"> = {
  submitted: "ok",
  draft: "info",
  not_entered: "neutral",
  overdue: "bad",
  returned: "warn",
  missed: "bad",
};

/** Khóa dấu nháp (kỳ|đơn vị) → thời điểm lưu. */
export function draftIndex(t: Pick<CntsTracking, "drafts"> | null | undefined): Map<string, string> {
  return new Map((t?.drafts ?? []).map((d) => [`${d.period_id}|${d.unit}`, d.saved_at]));
}

/**
 * Trạng thái theo dõi của một công việc. Trả lại (yêu cầu điều chỉnh) ưu tiên; chưa gửi lần nào: Trễ hạn khi quá hạn
 * (kể cả đã có nháp — trễ là việc cần xử lý), Nháp khi có nháp, còn lại Chưa nhập. Nháp cũ hơn lần gửi cuối bị bỏ qua.
 */
export function trackState(t: Pick<FormsTask, "status" | "version" | "submitted_at" | "period_id" | "unit">, drafts: Map<string, string>): TrackState {
  if (t.status === "returned") return "returned";
  if (t.status === "missed") return "missed";
  if (t.version > 0) return "submitted";
  if (t.status === "overdue") return "overdue";
  const saved = drafts.get(`${t.period_id}|${t.unit}`);
  if (saved && (!t.submitted_at || Date.parse(saved) > Date.parse(t.submitted_at))) return "draft";
  return "not_entered";
}

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
  draft: number;
  notEntered: number;
  overdue: number;
  returned: number;
  missing: number;
  /** Số đơn vị cần nhắc: chưa gửi (chưa nhập, nháp, trễ) hoặc bị trả lại. */
  pending: number;
  units: { unit: string; parent: string; state: TrackState; returnNote: string | null; submittedAt: string | null }[];
  ownerName: string | null;
  /** Giờ chốt (ISO) = hạn + số giờ đã đặt khi phát hành; null = không đặt. */
  closeAt: string | null;
}

export function periodRows(tasks: FormsTask[], tracking?: CntsTracking | null): PeriodRow[] {
  const drafts = draftIndex(tracking);
  const meta = new Map((tracking?.periods ?? []).map((p) => [p.period_id, p]));
  const by = new Map<string, PeriodRow>();
  for (const t of tasks) {
    let r = by.get(t.period_id);
    if (!r) {
      const m = meta.get(t.period_id);
      const closeAt = m && m.close_after_hours != null ? new Date(Date.parse(t.due_at) + m.close_after_hours * 3_600_000).toISOString() : null;
      r = { periodId: t.period_id, code: t.template_code, title: t.template_title, label: t.label, dueAt: t.due_at, periodStatus: t.period_status,
        total: 0, submitted: 0, draft: 0, notEntered: 0, overdue: 0, returned: 0, missing: 0, pending: 0, units: [],
        ownerName: m?.owner_name ?? (m?.owner_ref ? m.owner_ref : null), closeAt };
      by.set(t.period_id, r);
    }
    const st = trackState(t, drafts);
    r.total++;
    if (st === "submitted") r.submitted++;
    if (st === "draft") r.draft++;
    if (st === "not_entered") r.notEntered++;
    if (st === "overdue") r.overdue++;
    if (st === "returned") r.returned++;
    if (st === "missed") r.missing++;
    if (st === "draft" || st === "not_entered" || st === "overdue" || st === "returned") r.pending++;
    r.units.push({ unit: t.unit, parent: t.parent, state: st, returnNote: t.return_note, submittedAt: t.submitted_at });
  }
  return [...by.values()];
}
