// Client Chiến dịch điều tra mở rộng CNTS (W11.50 — BRD CNTS v3.7 FN-21, UR-TINH-26, UR-XA-14): đợt Nháp → Đang thu thập
// → Đã đóng (/v1/extension/cnts/campaigns*). Đợt phát hành thành một kỳ native kn_cnts loại `adhoc`: cán bộ xã nhập ở màn
// "Nhập biểu kỳ CNTS" (công việc /v1/forms/tasks). Số liệu đợt tách khỏi lũy kế báo cáo định kỳ.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";

type S = components["schemas"];
export type Campaign = S["CNTSCampaign"];
export type CampaignInput = S["CNTSCampaignInput"];
export type CampaignResults = S["CNTSCampaignResults"];
export type CampaignState = Campaign["state"];
export type FormsTemplate = S["FormsTemplate"];

export const STATE_LABEL: Record<CampaignState, string> = { draft: "Nháp", collecting: "Đang thu thập", closed: "Đã đóng" };
export const STATE_TONE: Record<CampaignState, "neutral" | "ok" | "warn" | "bad" | "info"> = { draft: "neutral", collecting: "info", closed: "ok" };
export const FIELD_LABEL: Record<CampaignInput["field"], string> = { chan_nuoi: "Chăn nuôi", thu_y: "Thú y", thuy_san: "Thủy sản", khac: "Khác" };
export const EVENT_LABEL: Record<string, string> = {
  published: "Phát hành",
  extended: "Gia hạn",
  closed: "Đóng đợt",
  reopened: "Mở lại",
  reminded: "Nhắc xã chưa nộp",
};
export const UNIT_STATUS_LABEL: Record<string, string> = {
  not_submitted: "Chưa nhập",
  overdue: "Trễ hạn",
  submitted: "Đã nộp",
  accepted: "Đã xác thực",
  returned: "Yêu cầu điều chỉnh",
  missed: "Không nộp",
  closed_unadjusted: "Đã chốt – chưa điều chỉnh",
  awaiting_reopen: "Chờ mở kỳ",
};

/** Biểu nhập "+ Tạo đợt điều tra" (chuỗi như ô nhập). */
export interface CampaignForm {
  name: string;
  goal: string;
  field: CampaignInput["field"];
  template_code: string;
  scope_mode: CampaignInput["scope_mode"];
  regions: string;
  communes: string; // mã xã, cách nhau bởi dấu phẩy/khoảng trắng
  from_date: string;
  to_date: string;
  due_at: string; // datetime-local (giờ máy)
  remind_before_hours: string;
}

export function emptyForm(): CampaignForm {
  return { name: "", goal: "", field: "chan_nuoi", template_code: "", scope_mode: "communes", regions: "", communes: "", from_date: "", to_date: "",
    due_at: "", remind_before_hours: "24" };
}

const split = (s: string) => s.split(/[\s,;]+/).map((x) => x.trim()).filter(Boolean);

export function toLocalInput(iso: string): string {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

export function formOf(c: Campaign): CampaignForm {
  return { name: c.name, goal: c.goal ?? "", field: c.field as CampaignInput["field"], template_code: c.template_code, scope_mode: c.scope_mode,
    regions: c.regions.join(", "), communes: c.scope_mode === "communes" ? c.communes.join(", ") : "", from_date: c.from_date, to_date: c.to_date,
    due_at: toLocalInput(c.due_at), remind_before_hours: String(c.remind_before_hours) };
}

/** Lỗi phía máy khách (máy chủ kiểm lại). */
export function formErrors(f: CampaignForm): Record<string, string> {
  const e: Record<string, string> = {};
  if (!f.name.trim()) e.name = "Bắt buộc";
  if (!f.template_code) e.template_code = "Chọn biểu điều tra";
  if (f.scope_mode === "communes" && split(f.communes).length === 0) e.communes = "Nhập ít nhất một mã xã";
  if (f.scope_mode === "regions" && split(f.regions).length === 0) e.regions = "Nhập ít nhất một khu vực";
  if (!f.from_date) e.from_date = "Bắt buộc";
  if (!f.to_date) e.to_date = "Bắt buộc";
  else if (f.from_date && f.to_date < f.from_date) e.to_date = "Đến ngày ≥ từ ngày";
  if (!f.due_at) e.due_at = "Bắt buộc";
  const h = Number(f.remind_before_hours || "0");
  if (!Number.isInteger(h) || h < 0 || h > 168) e.remind_before_hours = "0–168 giờ";
  return e;
}

export function payload(f: CampaignForm): CampaignInput {
  return {
    name: f.name.trim(),
    ...(f.goal.trim() ? { goal: f.goal.trim() } : {}),
    field: f.field,
    template_code: f.template_code,
    scope_mode: f.scope_mode,
    regions: f.scope_mode === "regions" ? split(f.regions) : [],
    communes: f.scope_mode === "communes" ? split(f.communes) : [],
    from_date: f.from_date,
    to_date: f.to_date,
    due_at: new Date(f.due_at).toISOString(),
    remind_before_hours: Number(f.remind_before_hours || "0"),
  };
}

/** Lý do gia hạn / mở lại: 10–500 ký tự. */
export const reasonOk = (r: string) => r.trim().length >= 10 && r.trim().length <= 500;

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createCampaignApi(api: ApiClient) {
  const path = (id: string) => ({ path: { id } });
  return {
    list: async (): Promise<Campaign[]> => (await unwrap(api.GET("/v1/extension/cnts/campaigns"))).items,
    get: (id: string): Promise<Campaign> => unwrap(api.GET("/v1/extension/cnts/campaigns/{id}", { params: path(id) })),
    create: (body: CampaignInput): Promise<Campaign> => unwrap(api.POST("/v1/extension/cnts/campaigns", { params: idem(), body })),
    update: (id: string, body: CampaignInput): Promise<Campaign> =>
      unwrap(api.POST("/v1/extension/cnts/campaigns/{id}/draft", { params: { ...path(id), ...idem() }, body })),
    publish: (id: string): Promise<Campaign> => unwrap(api.POST("/v1/extension/cnts/campaigns/{id}/publish", { params: { ...path(id), ...idem() } })),
    extend: (id: string, due_at: string, reason: string): Promise<Campaign> =>
      unwrap(api.POST("/v1/extension/cnts/campaigns/{id}/extend", { params: { ...path(id), ...idem() }, body: { due_at, reason } })),
    close: (id: string): Promise<Campaign> => unwrap(api.POST("/v1/extension/cnts/campaigns/{id}/close", { params: { ...path(id), ...idem() } })),
    reopen: (id: string, due_at: string, reason: string): Promise<Campaign> =>
      unwrap(api.POST("/v1/extension/cnts/campaigns/{id}/reopen", { params: { ...path(id), ...idem() }, body: { due_at, reason } })),
    remind: (id: string) => unwrap(api.POST("/v1/extension/cnts/campaigns/{id}/remind", { params: { ...path(id), ...idem() } })),
    results: (id: string): Promise<CampaignResults> => unwrap(api.GET("/v1/extension/cnts/campaigns/{id}/results", { params: path(id) })),
    download: async (id: string): Promise<Blob> => {
      const res = await api.GET("/v1/extension/cnts/campaigns/{id}/results/xlsx", { params: path(id), parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return res.data as Blob;
    },
    templates: async (): Promise<FormsTemplate[]> =>
      (await unwrap(api.GET("/v1/forms/templates", { params: { query: { profile: "kn_cnts" } } }))).items,
  };
}

export type CampaignApi = ReturnType<typeof createCampaignApi>;

/** Tỷ lệ đã nộp (0–100) của đợt đã phát hành. */
export function percent(c: Pick<Campaign, "progress">): number {
  const p = c.progress;
  if (!p || p.total === 0) return 0;
  return Math.round((p.submitted * 100) / p.total);
}
