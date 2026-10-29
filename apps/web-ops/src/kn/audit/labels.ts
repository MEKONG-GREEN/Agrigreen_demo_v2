// AUDIT-LOG-KN (#73): nhãn tiếng Việt của nhật ký hệ thống (GET /v1/accounts/audit). Máy chủ chỉ trả mã/ref; nhãn ở đây.
// Mã chưa có nhãn hiện nguyên mã (không đoán nghĩa).
import type { AuditChange, AuditItem } from "../cycle/api";

export const AUDIT_MODULES: { value: string; label: string }[] = [
  { value: "accounts", label: "Tài khoản" },
  { value: "kn.audit", label: "Lượt xem nhật ký" },
  { value: "kn.records", label: "Hồ sơ HTX / Nông hộ / THT" },
  { value: "kn.report", label: "Kỳ và duyệt báo cáo" },
  { value: "kn.khtc", label: "Kỳ KHTC" },
  { value: "kn.cnts", label: "Cấp quyền CNTS" },
];

export const AUDIT_SOURCES: Record<string, string> = {
  "accounts.account_event": "Sổ tài khoản",
  "kn.audit_view": "Sổ lượt xem nhật ký",
  "kn.producer": "Sổ hồ sơ KN",
  "kn.period_event": "Sổ kỳ báo cáo",
  "kn.review_event": "Sổ duyệt báo cáo",
  "kn.khtc_period_event": "Sổ kỳ KHTC",
  "kn.cnts_grant": "Sổ cấp quyền CNTS",
};

/** Mã thao tác theo module (bộ lọc "Thao tác" chỉ liệt kê mã của module đã chọn). */
export const AUDIT_ACTIONS: Record<string, { value: string; label: string; module: string }[]> = {};
const ACTIONS: [string, string, string][] = [
  ["accounts", "account.created", "Tạo tài khoản"],
  ["accounts", "account.locked", "Khóa tài khoản"],
  ["accounts", "account.unlocked", "Mở khóa tài khoản"],
  ["accounts", "account.assignment_changed", "Đổi vai trò / phân công"],
  ["accounts", "account.actions_sent", "Gửi email kích hoạt / yêu cầu"],
  ["accounts", "account.dormant_warned", "Cảnh báo tài khoản không hoạt động"],
  ["accounts", "account.totp_reset", "Đặt lại TOTP"],
  ["kn.audit", "audit.viewed", "Xem nhật ký hệ thống"],
  ["kn.audit", "audit.denied", "Bị từ chối xem nhật ký"],
  ["kn.records", "producer.created", "Tạo hồ sơ"],
  ["kn.records", "producer.updated", "Sửa hồ sơ"],
  ["kn.records", "producer.deactivated", "Vô hiệu hóa hồ sơ"],
  ["kn.records", "producer.reactivated", "Kích hoạt lại hồ sơ"],
  ["kn.report", "period.closed", "Chốt kỳ báo cáo"],
  ["kn.report", "period.auto_closed", "Tự chốt kỳ báo cáo"],
  ["kn.report", "period.reopened", "Mở lại kỳ báo cáo"],
  ["kn.report", "period.reclosed", "Chốt lại kỳ báo cáo"],
  ["kn.report", "review.adjustment_requested", "Yêu cầu điều chỉnh báo cáo"],
  ["kn.report", "review.confirmed", "Xác nhận báo cáo"],
  ["kn.khtc", "khtc_period.deadline_changed", "Đổi hạn kỳ KHTC"],
  ["kn.khtc", "khtc_period.closed", "Đóng kỳ KHTC"],
  ["kn.khtc", "khtc_period.cancelled", "Hủy kỳ KHTC"],
  ["kn.cnts", "cnts_grant.granted", "Cấp quyền CNTS"],
  ["kn.cnts", "cnts_grant.updated", "Sửa quyền CNTS"],
  ["kn.cnts", "cnts_grant.revoked", "Thu hồi quyền CNTS"],
];
const ACTION_LABEL: Record<string, string> = {};
for (const [module, value, label] of ACTIONS) {
  (AUDIT_ACTIONS[module] ??= []).push({ value, label, module });
  ACTION_LABEL[value] = label;
}

export const actionLabel = (code: string): string => ACTION_LABEL[code] ?? code;
export const moduleLabel = (code: string): string => AUDIT_MODULES.find((m) => m.value === code)?.label ?? code;
export const sourceLabel = (code: string): string => AUDIT_SOURCES[code] ?? code;

export const OUTCOME: Record<AuditItem["outcome"], { label: string; tone: "ok" | "bad" | "warn" | "neutral" }> = {
  succeeded: { label: "Thành công", tone: "ok" },
  denied: { label: "Bị từ chối", tone: "bad" },
  failed: { label: "Lỗi", tone: "warn" },
  unknown: { label: "Không ghi nhận", tone: "neutral" },
};

const FIELD_LABEL: Record<string, string> = {
  enabled: "Đang hoạt động",
  roles: "Vai trò",
  org_ref: "Đơn vị",
  lock_cause: "Nguyên nhân khóa",
  denial: "Mã từ chối",
  sources: "Nguồn đã xem",
  filter_actor_ref: "Lọc theo người thao tác",
  filter_module: "Lọc theo module",
  filter_action: "Lọc theo thao tác",
  filter_department: "Lọc theo phòng ban",
  kind: "Loại hồ sơ",
  parent_ref: "Thuộc hồ sơ",
  htx_ref: "HTX đã công bố",
  on_behalf_of: "Thao tác thay cho",
  batch_ref: "Đợt báo cáo",
  submission_ref: "Bài nộp",
  forced: "Đóng ngoại lệ",
  period_state: "Trạng thái kỳ",
  grantee_ref: "Người được cấp",
  role: "Vai CNTS",
};
export const fieldLabel = (f: string): string => FIELD_LABEL[f] ?? f;

/** Mã tham chiếu rút gọn (8 ký tự đầu) khi không có tên hiển thị. */
export const shortRef = (id: string): string => id.slice(0, 8);

/** Giá trị một trường thay đổi: ref → tên (nếu biết) hoặc mã rút gọn, mã → nhãn nguồn/thao tác nếu có. */
export function valueText(v: AuditChange["after"], names: Map<string, string>): string {
  if (!v) return "—";
  if (v.kind === "boolean") return v.boolean ? "Có" : "Không";
  if (v.kind === "ref") return v.ref ? (names.get(v.ref) ?? shortRef(v.ref)) : "—";
  const codes = v.codes ?? [];
  return codes.length ? codes.map((c) => AUDIT_SOURCES[c] ?? ACTION_LABEL[c] ?? c).join(", ") : "—";
}
