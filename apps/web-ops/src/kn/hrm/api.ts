// Client có kiểu cho Danh mục nhân sự (W11.21, dom/hrm — /v1/hrm/*) trên openapi-fetch sinh từ hợp đồng. Bearer,
// X-AG-Context và Idempotency-Key do middleware của createApiClient gắn (POST nào cũng có khóa mới).
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type HrmStatus = S["HrmStatus"];
export type HrmItem = S["HrmCatalogItem"];
export type HrmAttrs = S["HrmCatalogAttrs"];
export type HrmItemInput = S["HrmCatalogItemInput"];
export type HrmScale = S["HrmGradeScale"];
export type HrmScaleInput = S["HrmGradeScaleInput"];
export type HrmRule = S["HrmRule"];
export type HrmRuleView = S["HrmRuleView"];
export type HrmWriteResult = S["HrmWriteResult"];
export type HrmPromotionRules = S["HrmPromotionRules"];
export type HrmAchievementConversion = S["HrmAchievementConversion"];

export type HrmKind =
  | "position"
  | "grade"
  | "education_level"
  | "major"
  | "major_group"
  | "ethnicity"
  | "religion"
  | "political_theory"
  | "state_mgmt"
  | "foreign_language"
  | "language_level"
  | "it_level"
  | "award_form"
  | "discipline_form"
  | "change_type"
  | "leave_reason";
export type HrmRuleKey = "promotion" | "achievement_conversion";

/** Bốn danh mục Admin cấu hình (FN-19 menu Master data). */
export const MAIN_KINDS: HrmKind[] = [
  "position",
  "grade",
  "education_level",
  "major",
];
/** Danh sách khai báo sẵn khi triển khai (FN-19 mô tả). */
export const FIXED_KINDS: HrmKind[] = [
  "major_group",
  "ethnicity",
  "religion",
  "political_theory",
  "state_mgmt",
  "foreign_language",
  "language_level",
  "it_level",
  "award_form",
  "discipline_form",
  "change_type",
  "leave_reason",
];
export const KIND_LABEL: Record<HrmKind, string> = {
  position: "Chức vụ",
  grade: "Ngạch/CDNN",
  education_level: "Trình độ chuyên môn",
  major: "Chuyên ngành",
  major_group: "Nhóm chuyên ngành",
  ethnicity: "Dân tộc",
  religion: "Tôn giáo",
  political_theory: "Lý luận chính trị",
  state_mgmt: "Quản lý nhà nước",
  foreign_language: "Ngoại ngữ",
  language_level: "Trình độ ngoại ngữ",
  it_level: "Tin học",
  award_form: "Hình thức khen thưởng",
  discipline_form: "Hình thức kỷ luật",
  change_type: "Loại biến động",
  leave_reason: "Lý do nghỉ việc",
};
export const RULE_LABEL: Record<HrmRuleKey, string> = {
  promotion: "Quy tắc nâng bậc",
  achievement_conversion: "Quy đổi thành tích",
};
export const EQUIV_GROUPS = ["CVCC", "CVC", "CV", "CS", "CON_LAI"] as const;
export const EQUIV_LABEL: Record<string, string> = {
  CVCC: "CVCC và tương đương",
  CVC: "CVC và tương đương",
  CV: "CV và tương đương",
  CS: "Cán sự và tương đương",
  CON_LAI: "Còn lại",
};
export const GRADE_TYPE_LABEL: Record<string, string> = {
  cong_chuc: "Ngạch công chức",
  vien_chuc: "Chức danh nghề nghiệp",
};

/** Ngày YYYY-MM-DD → dd/mm/yyyy. */
export function dmy(d: string | null | undefined): string {
  if (!d) return "—";
  const [y, m, day] = d.slice(0, 10).split("-");
  return `${day}/${m}/${y}`;
}

/** Hệ số "2.34" → "2,34" (hiển thị kiểu Việt Nam; không qua số thực). */
export function vnDecimal(s: string | null | undefined): string {
  return s ? s.replace(".", ",") : "—";
}

/** Chuẩn hóa ô nhập hệ số: "2,34" → "2.34", bỏ khoảng trắng. */
export function normDecimal(s: string): string {
  return s.replace(/\s+/g, "").replace(",", ".");
}

/** Mã ngạch như máy chủ chuẩn hóa (FN-19 AC-03): bỏ mọi khoảng trắng, xuống dòng. */
export function normGradeCode(s: string): string {
  return s.replace(/\s+/g, "");
}

export function createHrmApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const at = (d?: string) => (d ? { at: d } : {});
  return {
    status: (): Promise<HrmStatus> => unwrap(api.GET("/v1/hrm/status")),
    installDefaults: (): Promise<{
      items: number;
      scales: number;
      rules: number;
    }> =>
      unwrap(api.POST("/v1/hrm/defaults", { params: { header: header() } })),
    list: (
      kind: HrmKind,
      includeRetired = false,
    ): Promise<{ kind: string; items: HrmItem[] }> =>
      unwrap(
        api.GET("/v1/hrm/catalogs/{kind}", {
          params: {
            path: { kind },
            query: includeRetired ? { include_retired: true } : {},
          },
        }),
      ),
    history: (kind: HrmKind, code: string): Promise<{ versions: HrmItem[] }> =>
      unwrap(
        api.GET("/v1/hrm/catalogs/{kind}/{code}/history", {
          params: { path: { kind, code } },
        }),
      ),
    create: (kind: HrmKind, body: HrmItemInput): Promise<HrmWriteResult> =>
      unwrap(
        api.POST("/v1/hrm/catalogs/{kind}", {
          params: { header: header(), path: { kind } },
          body,
        }),
      ),
    revise: (
      kind: HrmKind,
      code: string,
      body: HrmItemInput,
    ): Promise<HrmWriteResult> =>
      unwrap(
        api.POST("/v1/hrm/catalogs/{kind}/{code}/revise", {
          params: { header: header(), path: { kind, code } },
          body,
        }),
      ),
    scales: (
      day?: string,
    ): Promise<{
      day: string;
      grades: { grade: HrmItem; scale: HrmScale | null }[];
    }> =>
      unwrap(api.GET("/v1/hrm/grade-scales", { params: { query: at(day) } })),
    scaleHistory: (grade: string): Promise<{ versions: HrmScale[] }> =>
      unwrap(
        api.GET("/v1/hrm/grade-scales/{grade}/history", {
          params: { path: { grade } },
        }),
      ),
    addScale: (grade: string, body: HrmScaleInput): Promise<HrmWriteResult> =>
      unwrap(
        api.POST("/v1/hrm/grade-scales/{grade}", {
          params: { header: header(), path: { grade } },
          body,
        }),
      ),
    rule: (key: HrmRuleKey, day?: string): Promise<HrmRuleView> =>
      unwrap(
        api.GET("/v1/hrm/rules/{key}", {
          params: { path: { key }, query: at(day) },
        }),
      ),
    addRule: (
      key: HrmRuleKey,
      body: {
        valid_from: string;
        payload: HrmPromotionRules | HrmAchievementConversion;
        note?: string;
      },
    ): Promise<HrmWriteResult> =>
      unwrap(
        api.POST("/v1/hrm/rules/{key}", {
          params: { header: header(), path: { key } },
          body,
        }),
      ),
  };
}

export type HrmApi = ReturnType<typeof createHrmApi>;
