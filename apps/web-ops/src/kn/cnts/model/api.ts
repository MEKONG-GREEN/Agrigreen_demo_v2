// Client CT/DA, điểm trình diễn, hộ mô hình, cấp quyền CNTS (W11.47 — BRD CNTS v3.7 FN-10 phần hộ, FN-14, FN-19 phần CT/DA,
// UR-XA-04, UR-TINH-09, UR-TINH-19): /v1/extension/cnts/projects*, /demo-sites*, /model-households*, /grants*. Quy tắc thuần ở
// đây để kiểm thử đơn vị; máy chủ kiểm lại.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type Project = S["CNTSProject"];
export type ProjectDetail = S["CNTSProjectDetail"];
export type ProjectSave = S["CNTSProjectSave"];
export type Site = S["CNTSSite"];
export type SiteDetail = S["CNTSSiteDetail"];
export type SiteSave = S["CNTSSiteSave"];
export type Household = S["CNTSHousehold"];
export type HouseholdDetail = S["CNTSHouseholdDetail"];
export type HouseholdSave = S["CNTSHouseholdSave"];
export type Grant = S["CNTSGrant"];
export type GrantDetail = S["CNTSGrantDetail"];
export type GrantSave = S["CNTSGrantSave"];
export type GrantRole = Grant["role"];
export type Candidate = S["CNTSGrantCandidate"];
export type KhtcProgram = S["CNTSKHTCProgram"];

export const KIND_LABEL: Record<Project["kind"], string> = { tinh: "Khuyến nông tỉnh", knqg: "Khuyến nông quốc gia", hop_tac: "Hợp tác" };
export const ROLE_LABEL: Record<GrantRole, string> = {
  tang_cuong: "Cán bộ tăng cường",
  ksgm: "Viên chức trực KSGM",
  theo_doi_mo_hinh: "Cán bộ theo dõi mô hình",
  nong_ho: "Nông hộ",
};
export const GRANT_STATUS_LABEL: Record<Grant["status"], string> = { active: "Đang hiệu lực", pending: "Chưa tới ngày", expired: "Hết hiệu lực", revoked: "Đã thu hồi" };
export const GRANT_TONE: Record<Grant["status"], "ok" | "info" | "neutral" | "bad"> = { active: "ok", pending: "info", expired: "neutral", revoked: "bad" };
export const SCALE_UNITS = ["con", "m2", "m3", "ha", "long", "be"] as const;
export const DIARY_FORMS = ["BM-08", "BM-09", "BM-19"] as const;
/** Biểu mẫu CNTS giao được (BM-01…BM-19). */
export const FORM_CODES = Array.from({ length: 19 }, (_, i) => `BM-${String(i + 1).padStart(2, "0")}`);

/** Đồng → chuỗi có dấu chấm nhóm nghìn. */
export function money(v: string | null | undefined): string {
  if (!v) return "—";
  return v.replace(/\B(?=(\d{3})+(?!\d))/g, ".") + " đ";
}

/** Ngày YYYY-MM-DD → DD/MM/YYYY. */
export const dmy = (d: string | null | undefined) => (d ? d.split("-").reverse().join("/") : "—");

/** Lỗi phía máy khách của biểu CT/DA. */
export function projectErrors(p: Partial<ProjectSave>): Record<string, string> {
  const e: Record<string, string> = {};
  if (!p.code?.trim() || !/^[A-Za-z0-9._/-]{1,40}$/.test(p.code.trim())) e.code = "Mã 1–40 ký tự chữ, số, . _ / -";
  if (!p.name?.trim()) e.name = "Nhập tên";
  if (!p.kind) e.kind = "Chọn loại";
  if (!p.start_date) e.start_date = "Nhập ngày bắt đầu";
  if (p.end_date && p.start_date && p.end_date < p.start_date) e.end_date = "Không trước ngày bắt đầu";
  if (p.budget && !/^\d+$/.test(p.budget)) e.budget = "Số đồng nguyên";
  return e;
}

/** Lỗi phía máy khách của quy mô (cả hai hoặc không). */
export function scaleError(scale: string, unit: string): string | null {
  if (!scale && !unit) return null;
  if (!scale || !unit) return "Nhập cả quy mô và đơn vị";
  return /^\d+(\.\d{1,2})?$/.test(scale) && Number(scale) > 0 ? null : "Quy mô là số dương";
}

/** Tọa độ nhập tay "vĩ độ, kinh độ" → điểm (null nếu trống; lỗi nếu sai). */
export function parseLatLng(s: string): { lat: number; lng: number } | null | "invalid" {
  const t = s.trim();
  if (!t) return null;
  const m = /^(-?\d+(?:\.\d+)?)\s*[,;]\s*(-?\d+(?:\.\d+)?)$/.exec(t);
  if (!m) return "invalid";
  const lat = Number(m[1]), lng = Number(m[2]);
  return lat < 5 || lat > 25 || lng < 100 || lng > 118 ? "invalid" : { lat, lng };
}

/** Lỗi phía máy khách của biểu cấp quyền theo vai (FN-14). */
export function grantErrors(g: Partial<GrantSave>): Record<string, string> {
  const e: Record<string, string> = {};
  if (!g.grantee_ref) e.grantee_ref = "Chọn tài khoản";
  if (!g.role) e.role = "Chọn vai";
  if (!g.commune_codes?.length) e.commune_codes = "Chọn ít nhất một xã";
  const sites = g.site_refs?.length ?? 0, hhs = g.household_refs?.length ?? 0, facs = g.facility_refs?.length ?? 0;
  if (g.role === "theo_doi_mo_hinh" && !sites && !hhs) e.site_refs = "Chọn điểm hoặc hộ được theo dõi";
  if ((g.role === "ksgm" || g.role === "tang_cuong") && (sites || hhs)) e.site_refs = "Vai này giao theo xã/cơ sở, không theo điểm/hộ";
  if (g.role === "nong_ho" && (hhs !== 1 || facs || sites)) e.household_refs = "Nông hộ được giao đúng một hộ mô hình";
  if (g.valid_to && g.valid_from && g.valid_to < g.valid_from) e.valid_to = "Không trước ngày hiệu lực";
  return e;
}

/** Bật/tắt một phần tử trong danh sách (giữ thứ tự sắp xếp). */
export function toggle<T extends string>(list: readonly T[], v: T): T[] {
  return (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]).sort();
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createCntsModelApi(api: ApiClient) {
  return {
    projects: (q: { status?: "active" | "closed" } = {}) => unwrap(api.GET("/v1/extension/cnts/projects", { params: { query: q } })),
    project: (ref: string): Promise<ProjectDetail> => unwrap(api.GET("/v1/extension/cnts/projects/{ref}", { params: { path: { ref } } })),
    createProject: (body: ProjectSave): Promise<ProjectDetail> => unwrap(api.POST("/v1/extension/cnts/projects", { params: idem(), body })),
    updateProject: (ref: string, body: ProjectSave): Promise<ProjectDetail> =>
      unwrap(api.POST("/v1/extension/cnts/projects/{ref}", { params: { path: { ref }, ...idem() }, body })),
    projectStatus: (ref: string, close: boolean, base_version: number, reason: string): Promise<ProjectDetail> =>
      close
        ? unwrap(api.POST("/v1/extension/cnts/projects/{ref}/close", { params: { path: { ref }, ...idem() }, body: { base_version, reason } }))
        : unwrap(api.POST("/v1/extension/cnts/projects/{ref}/reopen", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
    programs: async (): Promise<KhtcProgram[]> => (await unwrap(api.GET("/v1/extension/cnts/khtc-programs"))).items,
    sites: async (q: { project_ref?: string; status?: "active" | "closed" } = {}): Promise<Site[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/demo-sites", { params: { query: q } }))).items,
    site: (ref: string): Promise<SiteDetail> => unwrap(api.GET("/v1/extension/cnts/demo-sites/{ref}", { params: { path: { ref } } })),
    createSite: (body: SiteSave): Promise<SiteDetail> => unwrap(api.POST("/v1/extension/cnts/demo-sites", { params: idem(), body })),
    updateSite: (ref: string, body: SiteSave): Promise<SiteDetail> =>
      unwrap(api.POST("/v1/extension/cnts/demo-sites/{ref}", { params: { path: { ref }, ...idem() }, body })),
    siteStatus: (ref: string, close: boolean, base_version: number, reason: string): Promise<SiteDetail> =>
      close
        ? unwrap(api.POST("/v1/extension/cnts/demo-sites/{ref}/close", { params: { path: { ref }, ...idem() }, body: { base_version, reason } }))
        : unwrap(api.POST("/v1/extension/cnts/demo-sites/{ref}/reopen", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
    households: async (q: { site_ref?: string; status?: "active" | "withdrawn" } = {}): Promise<Household[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/model-households", { params: { query: q } }))).items,
    createHousehold: (body: HouseholdSave): Promise<HouseholdDetail> => unwrap(api.POST("/v1/extension/cnts/model-households", { params: idem(), body })),
    updateHousehold: (ref: string, body: HouseholdSave): Promise<HouseholdDetail> =>
      unwrap(api.POST("/v1/extension/cnts/model-households/{ref}", { params: { path: { ref }, ...idem() }, body })),
    householdStatus: (ref: string, withdraw: boolean, base_version: number, reason: string): Promise<HouseholdDetail> =>
      withdraw
        ? unwrap(api.POST("/v1/extension/cnts/model-households/{ref}/withdraw", { params: { path: { ref }, ...idem() }, body: { base_version, reason } }))
        : unwrap(api.POST("/v1/extension/cnts/model-households/{ref}/reinstate", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
    grants: (q: { mine?: boolean; status?: Grant["status"] } = {}) => unwrap(api.GET("/v1/extension/cnts/grants", { params: { query: q } })),
    grant: (ref: string): Promise<GrantDetail> => unwrap(api.GET("/v1/extension/cnts/grants/{ref}", { params: { path: { ref } } })),
    createGrant: (body: GrantSave): Promise<GrantDetail> => unwrap(api.POST("/v1/extension/cnts/grants", { params: idem(), body })),
    updateGrant: (ref: string, body: GrantSave): Promise<GrantDetail> =>
      unwrap(api.POST("/v1/extension/cnts/grants/{ref}", { params: { path: { ref }, ...idem() }, body })),
    revokeGrant: (ref: string, body: S["CNTSGrantRevoke"]): Promise<GrantDetail> =>
      unwrap(api.POST("/v1/extension/cnts/grants/{ref}/revoke", { params: { path: { ref }, ...idem() }, body })),
    candidates: async (): Promise<Candidate[]> => (await unwrap(api.GET("/v1/extension/cnts/grant-candidates"))).items,
  };
}

export type CntsModelApi = ReturnType<typeof createCntsModelApi>;
