// Client có kiểu cho Hồ sơ nhân sự (W11.22, dom/hrm — /v1/hrm/access|units|staff*|permissions*) trên openapi-fetch sinh từ
// hợp đồng. Dữ liệu cá nhân chỉ đi qua bộ nhớ (máy chủ đọc từ kho định danh, phản hồi no-store) — không lưu vào
// localStorage/sessionStorage.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type HrmMyAccess = S["HrmMyAccess"];
export type HrmStaffItem = S["HrmStaffItem"];
export type HrmStaffDetail = S["HrmStaffDetail"];
export type HrmStaffEvent = S["HrmStaffEvent"];
export type HrmSnapshot = S["HrmSnapshot"];
export type HrmStaffCreate = S["HrmStaffCreate"];
export type HrmProfileUpdate = S["HrmProfileUpdate"];
export type HrmEventInput = S["HrmEventInput"];
export type HrmSensitiveFields = S["HrmSensitiveFields"];
export type HrmStaffWrite = S["HrmStaffWrite"];
export type HrmPermEntry = S["HrmPermEntry"];
export type HrmPermMatrix = S["HrmPermMatrix"];
export type HrmAccessEntry = S["HrmAccessEntry"];
export type HrmScope = S["HrmScope"];
export type HrmStaffType = S["HrmStaffType"];
export type HrmEventKind = HrmEventInput["kind"];

export interface StaffFilter {
  at?: string;
  type?: HrmStaffType;
  unit?: string;
  status?: "active" | "left" | "none";
  position?: string;
  grade?: string;
  q?: string;
  offset?: number;
  limit?: number;
}

export const STAFF_TYPE_LABEL: Record<HrmStaffType, string> = {
  vien_chuc: "Viên chức",
  hop_dong: "Hợp đồng lao động",
  can_bo_xa: "Cán bộ khuyến nông cấp xã",
};
export const STATUS_LABEL: Record<string, string> = {
  active: "Đang công tác",
  left: "Đã nghỉ việc",
  none: "Chưa vào làm",
};
export const EVENT_LABEL: Record<HrmEventKind, string> = {
  work_start: "Vào làm",
  work_end: "Nghỉ việc",
  unit: "Đơn vị công tác",
  position: "Chức vụ",
  secondment: "Tăng cường",
  grade: "Ngạch - Bậc",
  step: "Nâng bậc",
  degree: "Văn bằng",
  training: "Bồi dưỡng, trình độ",
  award: "Khen thưởng",
  discipline: "Kỷ luật",
};
export const PERM_LABEL: Record<string, string> = {
  "profile.view": "Xem hồ sơ",
  "profile.edit": "Thêm, sửa hồ sơ",
  "sensitive.view": "Thẻ Thống kê BC Sở",
  export: "Xuất danh sách",
  "audit.view": "Xem nhật ký xem/xuất",
  "docs.view": "Xem tài liệu", // W11.24 kho tài liệu (FN-14)
  "docs.manage": "Quản lý tài liệu",
  assessment: "Đánh giá viên chức",
};
export const SCOPE_LABEL: Record<HrmScope, string> = {
  none: "Không",
  self: "Bản thân",
  unit: "Đơn vị mình",
  all: "Toàn Trung tâm",
};
export const GROUP_LABEL: Record<string, string> = {
  admin: "Admin",
  hcth: "Cấp Tỉnh – HCTH",
  manager: "Lãnh đạo các phòng",
  tinh: "Cấp Tỉnh (viên chức)",
  xa: "Cấp Xã",
  tw: "Cấp TW",
};
export const SENSITIVE_LABEL: Record<string, string> = {
  ethnicity: "Dân tộc (TK-01)",
  religion: "Tôn giáo (TK-02)",
  party_date: "Ngày vào Đảng (TK-03)",
  hometown: "Quê quán (TK-04)",
  birthplace: "Nơi sinh (TK-05)",
  revolution_date: "Ngày TGCM (TK-06)",
};

/** Thẻ của màn chi tiết (FN-07 B3) → loại quá trình hiển thị trong thẻ. */
export const TAB_EVENTS: Record<string, HrmEventKind[]> = {
  cong_tac: ["unit", "position", "secondment"],
  trinh_do: ["degree", "training"],
  khen_thuong: ["award", "discipline"],
  ngach_bac: ["grade", "step"],
  lam_viec: ["work_start", "work_end"],
};

export function createStaffApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const clean = (f: StaffFilter) =>
    Object.fromEntries(
      Object.entries(f).filter(([, v]) => v !== undefined && v !== ""),
    ) as StaffFilter;
  return {
    access: (): Promise<HrmMyAccess> => unwrap(api.GET("/v1/hrm/access")),
    units: (): Promise<{
      items: { code: string; name: string; kind: string; active: boolean }[];
    }> => unwrap(api.GET("/v1/hrm/units")),
    list: (
      f: StaffFilter,
    ): Promise<{
      day: string;
      total: number;
      scope: HrmScope;
      items: HrmStaffItem[];
    }> => unwrap(api.GET("/v1/hrm/staff", { params: { query: clean(f) } })),
    get: (id: string, at?: string): Promise<HrmStaffDetail> =>
      unwrap(
        api.GET("/v1/hrm/staff/{id}", {
          params: { path: { id }, query: at ? { at } : {} },
        }),
      ),
    create: (body: HrmStaffCreate): Promise<HrmStaffWrite> =>
      unwrap(api.POST("/v1/hrm/staff", { params: { header: header() }, body })),
    updateProfile: (
      id: string,
      body: HrmProfileUpdate,
    ): Promise<HrmStaffWrite> =>
      unwrap(
        api.POST("/v1/hrm/staff/{id}/profile", {
          params: { header: header(), path: { id } },
          body,
        }),
      ),
    sensitive: (
      id: string,
    ): Promise<{ id: string; fields: HrmSensitiveFields }> =>
      unwrap(
        api.GET("/v1/hrm/staff/{id}/sensitive", { params: { path: { id } } }),
      ),
    updateSensitive: (
      id: string,
      fields: HrmSensitiveFields,
    ): Promise<HrmStaffWrite> =>
      unwrap(
        api.POST("/v1/hrm/staff/{id}/sensitive", {
          params: { header: header(), path: { id } },
          body: { fields },
        }),
      ),
    addEvent: (
      id: string,
      body: HrmEventInput,
    ): Promise<{ id: string; version: number }> =>
      unwrap(
        api.POST("/v1/hrm/staff/{id}/events", {
          params: { header: header(), path: { id } },
          body,
        }),
      ),
    replaceEvent: (
      id: string,
      event: string,
      body: HrmEventInput,
    ): Promise<{ id: string; version: number }> =>
      unwrap(
        api.POST("/v1/hrm/staff/{id}/events/{event}/replace", {
          params: { header: header(), path: { id, event } },
          body,
        }),
      ),
    voidEvent: (
      id: string,
      event: string,
      reason: string,
    ): Promise<{ id: string; version: number }> =>
      unwrap(
        api.POST("/v1/hrm/staff/{id}/events/{event}/void", {
          params: { header: header(), path: { id, event } },
          body: { reason },
        }),
      ),
    accessLog: (id: string): Promise<{ items: HrmAccessEntry[] }> =>
      unwrap(
        api.GET("/v1/hrm/staff/{id}/access-log", { params: { path: { id } } }),
      ),
    /** Xuất CSV danh sách đang lọc (máy chủ ghi nhật ký xuất) — trả tệp và tên tệp từ Content-Disposition. */
    async exportCsv(f: StaffFilter): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/hrm/staff/export", {
        params: { query: clean(f) },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      return {
        blob: res.data as Blob,
        name: filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          "ho-so-nhan-su.csv",
        ),
      };
    },
    permissions: (): Promise<{
      perms: string[];
      scopes: HrmScope[];
      groups: string[];
      items: HrmPermEntry[];
    }> => unwrap(api.GET("/v1/hrm/permissions")),
    permissionHistory: (): Promise<{ items: HrmPermEntry[] }> =>
      unwrap(api.GET("/v1/hrm/permissions/history")),
    setPermission: (body: {
      grantee_kind: "role" | "user";
      grantee: string;
      perms: HrmPermMatrix;
      note?: string;
    }): Promise<{ version: number }> =>
      unwrap(
        api.POST("/v1/hrm/permissions", { params: { header: header() }, body }),
      ),
  };
}

export type StaffApi = ReturnType<typeof createStaffApi>;

/** Hiển thị ngày theo mức chính xác (BR-05): YYYY → yyyy; YYYY-MM → mm/yyyy; YYYY-MM-DD → dd/mm/yyyy. */
export function vnDate(s: string | null | undefined): string {
  if (!s) return "—";
  const p = s.slice(0, 10).split("-");
  if (p.length === 1) return p[0]!;
  if (p.length === 2) return `${p[1]}/${p[0]}`;
  return `${p[2]}/${p[1]}/${p[0]}`;
}
