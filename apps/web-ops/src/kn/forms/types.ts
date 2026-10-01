// Kiểu hợp đồng mẫu báo cáo dạng lưới KN v7.7 (form-template.v1) — lấy từ OpenAPI sinh tự động
// (contracts/openapi/application.yaml, admin.yaml mục pf/reports). Không khai báo lại bằng tay.
import type { paths as AdminPaths } from "@/api/generated/admin";
import type { components } from "@/api/generated/application";

type S = components["schemas"];

export type FormDefinition = S["FormDefinition"];
export type FormColumn = S["FormColumn"];
export type FormColumnKind = FormColumn["kind"];
export type FormDType = FormColumn["dtype"];
export type FormHeaderCell = S["FormHeaderCell"];
export type FormPeriodBlock = S["FormPeriodBlock"];
export type FormRule = S["FormRule"];
export type FormTemplate = S["FormTemplate"];
export type FormVersion = S["FormVersion"];
export type FormAssignment = S["FormAssignment"];
export type FormReportType = S["FormReportType"];
export type FormEvalInput = S["FormEvalInput"];
export type FormEvalResult = S["FormEvalResult"];
export type FormEvalRow = FormEvalInput["rows"][number];
export type FormEvalRowResult = FormEvalResult["rows"][number];
export type FormCellIssue = FormEvalResult["issues"][number];
export type FormTotalRow = FormEvalResult["totals"][number];
export type FormIndicator = FormDefinition["rows"]["indicators"][number];
export type FormTotalDef = FormDefinition["totals"][number];
export type FormGroup = FormDefinition["groups"][number];
export type FormPendingItem = FormDefinition["pending"][number];

type OpsBody = AdminPaths["/v1/reports/forms/{id}/versions/{v}/ops"]["post"]["requestBody"];
/** Một thao tác của trình thiết kế (16 loại, admin.yaml adminApplyFormOps). */
export type FormOp = NonNullable<OpsBody>["content"]["application/json"]["ops"][number];
export type FormOpName = FormOp["op"];
export type FormMeta = NonNullable<FormOp["meta"]>;

export type FormLogEntry = NonNullable<
  AdminPaths["/v1/reports/forms/{id}/log"]["get"]["responses"]["200"]["content"]["application/json"]
>["items"][number];
export type FormImportReport = AdminPaths["/v1/reports/forms/import"]["post"]["responses"]["201"]["content"]["application/json"]["report"];
export type FormSummary = AdminPaths["/v1/reports/forms"]["get"]["responses"]["200"]["content"]["application/json"]["items"][number];

export const KIND_LABEL: Record<FormColumnKind, string> = {
  fixed: "Cố định",
  provincial: "Tỉnh giao",
  input: "Nhập",
  per_period: "Cột theo kỳ",
  formula: "Công thức",
};

/** Nhãn ngắn ở dòng loại cột của lưới thiết kế (Hình 27: Cố định, Tỉnh giao, Nhập, Kỳ, ƒx). */
export const KIND_SHORT: Record<FormColumnKind, string> = {
  fixed: "Cố định",
  provincial: "Tỉnh giao",
  input: "Nhập",
  per_period: "Kỳ",
  formula: "ƒx",
};

export const DTYPE_LABEL: Record<FormDType, string> = {
  number: "Số",
  integer: "Số nguyên",
  text: "Chữ",
  date: "Ngày",
  catalog: "Danh mục chọn",
};

export const REPORT_TYPE_LABEL: Record<FormReportType, string> = {
  weekly: "Báo cáo tuần",
  monthly: "Báo cáo tháng",
  seasonal: "Báo cáo hàng vụ",
  annual: "Báo cáo năm",
  adhoc: "Báo cáo đột xuất",
  dispatch: "Báo cáo gửi Chi cục",
};

export const LEVEL_LABEL: Record<FormTemplate["entry_level"], string> = {
  commune: "Cấp Xã",
  province: "Cấp Tỉnh",
};

export const VERSION_STATUS_LABEL: Record<FormVersion["status"], string> = {
  draft: "Nháp",
  proposed: "Chờ phát hành",
  published: "Đã phát hành",
  retired: "Ngừng dùng",
};

export const COMPLETENESS_LABEL: Record<FormDefinition["completeness"], string> = {
  complete: "Đủ theo tệp gốc",
  partial: "Một phần (chờ khách)",
  placeholder: "Tạm (chờ khách Q-01)",
};
