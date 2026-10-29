import { validateForm, type FieldRule } from "@/components/form";
import { EQUIV_GROUPS, FIXED_KINDS, MAIN_KINDS, normGradeCode, type HrmAttrs, type HrmItemInput, type HrmKind } from "./api";

export function isHrmKind(value: string | undefined): value is HrmKind {
  return [...MAIN_KINDS, ...FIXED_KINDS].includes(value as HrmKind);
}

/** Giữ alias phòng ban khi mở trang con; không đổi menu hay quyền của trang danh sách. */
export function catalogBase(pathname: string): string {
  return /^\/kn\/phong-ban\/[^/]+\/danh-muc-nhan-su(?:\/|$)/.exec(pathname)?.[0].replace(/\/$/, "") ?? "/kn/nhan-su/danh-muc";
}

export function catalogItemPath(base: string, kind: HrmKind, code: string): string {
  return `${base}/${kind}/muc/${encodeURIComponent(code)}`;
}

export type CatalogValues = { code: string; name: string; order: string; allowance: string; rank: string };

/** Giới hạn chuỗi TRƯỚC validateForm/so sánh BigInt; biên khớp domain/hrm, không xóa ký tự sai thành số đúng. */
export function validateCatalogItem(kind: HrmKind, values: CatalogValues, attrs: HrmAttrs) {
  const numeric: (keyof CatalogValues)[] = ["order", ...(kind === "position" && attrs.leader ? ["allowance" as const] : []), ...(kind === "education_level" ? ["rank" as const] : [])];
  const errors: Record<string, string> = {};
  for (const key of numeric) {
    if (values[key].length > 32) errors[key] = "Tối đa 32 ký tự số.";
    else {
      const raw = values[key].replace(/\s+/g, "");
      const decimal = raw.includes(",") ? raw.replace(/\./g, "").replace(",", ".") : raw;
      if (raw && !/^-?\d+(\.\d+)?$/.test(decimal)) errors[key] = "Chỉ nhập số thập phân thường, không dùng số mũ.";
    }
  }
  // Không đưa một chuỗi quá dài vào bộ so sánh số của UI-KIT, kể cả khi trường khác cũng sai.
  if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
  const rules: Partial<Record<keyof CatalogValues, FieldRule>> = {
    code: { kind: "text", required: true },
    name: { kind: "text", required: true, maxLength: 200 },
    order: { kind: "number", required: true, integer: true, max: 100000 },
  };
  if (kind === "position" && attrs.leader) rules.allowance = { kind: "number", positive: true, max: "10", maxDecimals: 2 };
  if (kind === "education_level") rules.rank = { kind: "number", required: true, integer: true, positive: true, max: 100 };
  const checked = validateForm({ ...values, name: values.name.trim() }, rules);
  Object.assign(errors, checked.errors);
  const code = kind === "grade" ? normGradeCode(values.code) : values.code.replace(/\s+/g, "").toLowerCase();
  if (values.code.trim() && !(kind === "grade" ? /^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$/ : /^[a-z][a-z0-9_]{0,39}$/).test(code)) errors.code = kind === "grade" ? "Mã ngạch tối đa 40 ký tự: chữ, số, dấu chấm, gạch." : "Mã tối đa 40 ký tự: chữ không dấu, số, gạch dưới; bắt đầu bằng chữ.";
  if (kind === "grade") {
    if (!EQUIV_GROUPS.includes(attrs.equiv_group as typeof EQUIV_GROUPS[number])) errors.equiv_group = "Bắt buộc chọn nhóm tương đương.";
    if (!["cong_chuc", "vien_chuc"].includes(attrs.grade_type ?? "")) errors.grade_type = "Bắt buộc chọn loại ngạch.";
    if ((attrs.salary_class?.length ?? 0) > 20) errors.salary_class = "Tối đa 20 ký tự.";
  }
  if (kind === "major_group" && !attrs.level) errors.level = "Bắt buộc chọn trình độ.";
  if (Object.keys(errors).length) return { valid: false as const, errors, body: null };
  const out: HrmAttrs = { ...attrs };
  if (kind === "position") {
    if (attrs.leader && checked.values.allowance) out.allowance = checked.values.allowance;
    else delete out.allowance;
    if (!out.mgmt_level) delete out.mgmt_level;
  }
  if (kind === "grade" && !out.salary_class) delete out.salary_class;
  if (kind === "education_level") out.rank = Number(checked.values.rank);
  if (out.groups) out.groups = Object.fromEntries(Object.entries(out.groups).filter(([, group]) => group));
  const body: HrmItemInput = { code, name: checked.values.name, sort_order: Number(checked.values.order), attrs: out };
  return { valid: true as const, errors, body };
}
