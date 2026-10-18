// Client cảnh báo CNTS (W11.43 — BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5): danh sách, chi tiết + lịch sử,
// giao / nhận / đóng xử lý (khóa lạc quan base_seq), cấu hình nhắc nhở theo biểu, xử lý theo loại, ngưỡng.
// openapi-fetch sinh từ contracts/openapi/application.yaml; Bearer, X-AG-Context, Idempotency-Key do createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type Alert = S["CNTSAlert"];
export type AlertList = S["CNTSAlertList"];
export type AlertDetail = S["CNTSAlertDetail"];
export type AlertConfig = S["CNTSAlertConfig"];
export type FormReminder = S["CNTSFormReminder"];
export type AlertSetting = S["CNTSAlertSetting"];
export type Assignee = S["CNTSAlertAssignees"]["items"][number];
export type AlertStatus = Alert["status"];
export type AlertLevel = Alert["level"];

export const STATUS_LABEL: Record<AlertStatus, string> = { new: "Mới", assigned: "Đã giao", in_progress: "Đang xử lý", closed: "Đã đóng" };
export const STATUS_TONE: Record<AlertStatus, "neutral" | "ok" | "warn" | "bad" | "info"> = {
  new: "warn",
  assigned: "info",
  in_progress: "info",
  closed: "ok",
};
export const LEVEL_LABEL: Record<AlertLevel, string> = { urgent: "Khẩn", attention: "Cần chú ý" };
export const ACTION_LABEL: Record<AlertDetail["history"][number]["action"], string> = {
  raised: "Hệ thống tạo cảnh báo",
  assigned: "Giao xử lý",
  started: "Nhận xử lý",
  closed: "Đóng (ghi kết quả)",
  auto_closed: "Tự đóng — số liệu hết vi phạm",
  overdue: "Quá hạn xử lý — báo Lãnh đạo phòng",
};
export const CHANNEL_LABEL: Record<FormReminder["channels"][number], string> = { in_app: "Trong ứng dụng", push: "Đẩy thông báo Mobile", email: "Email" };
export const RECIPIENT_LABEL: Record<string, string> = {
  form_owner: "Người phụ trách biểu",
  submitter: "Người nhập",
  commune: "Cán bộ xã",
  manager: "Lãnh đạo phòng",
  province: "Cán bộ tỉnh",
  program_lead: "Chủ nhiệm",
  site_officer: "Cán bộ theo dõi điểm",
  farmer: "Nông hộ",
};

/** Bộ lọc trạng thái của danh sách (tab). */
export const STATUS_TABS: { key: "" | "open" | "overdue" | AlertStatus; label: string }[] = [
  { key: "open", label: "Chưa đóng" },
  { key: "overdue", label: "Quá hạn" },
  { key: "new", label: "Mới" },
  { key: "assigned", label: "Đã giao" },
  { key: "in_progress", label: "Đang xử lý" },
  { key: "closed", label: "Đã đóng" },
  { key: "", label: "Tất cả" },
];

export interface Finding {
  field: string;
  label: string;
  current?: string;
  compare?: string;
  change_pct?: string;
  reason: string;
  new_entry?: boolean;
}

/** Các vi phạm ghi trong detail.findings (dòng 1, 2, 4, 5). */
export function findingsOf(a: Pick<Alert, "detail">): Finding[] {
  const f = (a.detail as { findings?: unknown }).findings;
  return Array.isArray(f) ? (f as Finding[]) : [];
}

/** Lý do / tóm tắt ngắn của cảnh báo. */
export function reasonOf(a: Pick<Alert, "detail">): string {
  const d = a.detail as { summary?: string; reason?: string };
  return d.summary ?? d.reason ?? "";
}

/** Kiểm cấu hình nhắc phía máy khách (trùng quy tắc máy chủ). */
export function reminderErrors(r: FormReminder): Record<string, string> {
  const e: Record<string, string> = {};
  if (!Number.isInteger(r.before_due_hours) || r.before_due_hours < 0 || r.before_due_hours > 168) e.before_due_hours = "0–168 giờ";
  if (r.repeat_daily && !/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(r.repeat_at)) e.repeat_at = "giờ dạng HH:MM";
  if (!Number.isInteger(r.late_streak) || r.late_streak < 0 || r.late_streak > 12) e.late_streak = "0–12 kỳ";
  if ([...r.message].length > 500) e.message = "tối đa 500 ký tự";
  return e;
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export interface AlertQuery {
  status?: "open" | "overdue" | AlertStatus;
  kind?: string;
  level?: AlertLevel;
  template_code?: string;
  mine?: boolean;
}

export function createAlertApi(api: ApiClient) {
  return {
    list: (q: AlertQuery = {}): Promise<AlertList> => unwrap(api.GET("/v1/extension/cnts/alerts", { params: { query: q } })),
    get: (ref: string): Promise<AlertDetail> => unwrap(api.GET("/v1/extension/cnts/alerts/{ref}", { params: { path: { ref } } })),
    assign: (ref: string, base_seq: number, assignee_ref: string, due_at?: string, note?: string): Promise<AlertDetail> =>
      unwrap(api.POST("/v1/extension/cnts/alerts/{ref}/assign", {
        params: { path: { ref }, ...idem() },
        body: { base_seq, assignee_ref, ...(due_at ? { due_at } : {}), ...(note ? { note } : {}) },
      })),
    start: (ref: string, base_seq: number): Promise<AlertDetail> =>
      unwrap(api.POST("/v1/extension/cnts/alerts/{ref}/start", { params: { path: { ref }, ...idem() }, body: { base_seq } })),
    close: (ref: string, base_seq: number, result: string): Promise<AlertDetail> =>
      unwrap(api.POST("/v1/extension/cnts/alerts/{ref}/close", { params: { path: { ref }, ...idem() }, body: { base_seq, result } })),
    assignees: async (commune?: string | null): Promise<Assignee[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/alert-assignees", { params: { query: commune ? { commune_code: commune } : {} } }))).items,
    config: (): Promise<AlertConfig> => unwrap(api.GET("/v1/extension/cnts/alert-config")),
    putForm: (code: string, body: FormReminder): Promise<AlertConfig> =>
      unwrap(api.PUT("/v1/extension/cnts/alert-config/forms/{code}", { params: { path: { code: code as never }, ...idem() }, body })),
    putKind: (kind: string, body: AlertSetting): Promise<AlertConfig> =>
      unwrap(api.PUT("/v1/extension/cnts/alert-config/kinds/{kind}", { params: { path: { kind }, ...idem() }, body })),
  };
}

export type AlertApi = ReturnType<typeof createAlertApi>;
