// Gói ORGUI — logic thuần của hộp thoại "Tạo tổ chức" (Admin POST /v1/orgs, partyCreateOrg).
// Quy tắc khớp internal/platform/party/domain/org.go (KnownKind, UnitLevel, AllowedParent,
// ValidateOrgBasics) và app/org.go (unitWithin). Server vẫn là nguồn sự thật: mọi 409/422 hiển thị lại.
import type { components } from "@/api/generated/admin";
import type { ApiProblem } from "@/api/problem";
import type { AdminUnit, Org, OrgKind } from "@/api/types";
import { ROLES } from "@/auth/principal";

export type PartyOrgInput = components["schemas"]["PartyOrgInput"];

/** Vai được mở hộp thoại tạo. KN admin (kn_province_admin…) tạm chưa được tạo — quyết định nhóm (ORGUI). */
export const ORG_CREATE_ROLES: readonly string[] = [
  ROLES.platformAdmin,
  ROLES.registryAdmin,
];

export function canCreateOrg(
  roles: readonly string[] | undefined | null,
): boolean {
  return !!roles && roles.some((r) => ORG_CREATE_ROLES.includes(r));
}

/** Cấp đơn vị hành chính phải gắn: none = không gắn; any = tuỳ chọn tỉnh hoặc xã. */
export type UnitRule = "none" | "province" | "commune" | "any";

export interface KindRule {
  label: string;
  unit: UnitRule;
  /** Loại cha hợp lệ; `root` = được đứng ở gốc (không cha). */
  parents: readonly OrgKind[];
  root: boolean;
  /** HTX không tạo ở đây: chuyển sang luồng neo HTX theo MST (ADR-0052, D2). */
  redirect?: string;
}

export const NEO_HTX_HREF = "/quan-tri/neo-htx";

export const KIND_RULES: Readonly<Record<OrgKind, KindRule>> = {
  ttkn_national: {
    label: "TTKN Quốc gia",
    unit: "none",
    parents: [],
    root: true,
  },
  ttkn_province: {
    label: "TTKN tỉnh",
    unit: "province",
    parents: ["ttkn_national"],
    root: true,
  },
  extension_team: {
    label: "Tổ KN / KNV xã",
    unit: "commune",
    parents: ["ttkn_province"],
    root: false,
  },
  dcrd: { label: "Cục KTHT", unit: "none", parents: [], root: true },
  mkg: { label: "Mekong Green", unit: "none", parents: [], root: true },
  partner: { label: "Đối tác", unit: "any", parents: [], root: true },
  cooperative: {
    label: "HTX / THT",
    unit: "commune",
    parents: ["extension_team", "ttkn_province"],
    root: true,
    redirect: NEO_HTX_HREF,
  },
};

/** Thứ tự trong ô chọn loại. */
export const CREATE_KIND_ORDER: readonly OrgKind[] = [
  "ttkn_province",
  "extension_team",
  "ttkn_national",
  "dcrd",
  "mkg",
  "partner",
  "cooperative",
];

/** Mã tổ chức: 1–40 ký tự [A-Za-z0-9._-] (domain.codeRe). */
export const ORG_CODE_RE = /^[A-Za-z0-9._-]{1,40}$/;

export function codeError(code: string): string | null {
  const c = code.trim();
  if (!c) return "Nhập mã tổ chức.";
  if (!ORG_CODE_RE.test(c))
    return "Mã 1–40 ký tự, chỉ gồm chữ không dấu, số và . _ -";
  return null;
}

export function nameError(name: string): string | null {
  const n = [...name.trim()].length;
  if (n === 0) return "Nhập tên tổ chức.";
  if (n > 200) return "Tên tối đa 200 ký tự.";
  return null;
}

/** Ngày YYYY-MM-DD theo giờ Việt Nam (ngày hiệu lực của ĐVHC tính theo lịch VN). */
export function vnToday(now: Date = new Date()): string {
  return new Date(now.getTime() + 7 * 3_600_000).toISOString().slice(0, 10);
}

/** ĐVHC còn hiệu lực ngày `day`: valid_from ≤ day < valid_to (valid_to không bao gồm; null = còn hiệu lực). */
export function unitValidOn(
  u: Pick<AdminUnit, "valid_from" | "valid_to">,
  day: string,
): boolean {
  return u.valid_from <= day && (u.valid_to === null || day < u.valid_to);
}

/** Cha hợp lệ cho loại `kind`: đang hoạt động và đúng loại cấp trên (domain.AllowedParent). */
export function parentOptions(kind: OrgKind, orgs: readonly Org[]): Org[] {
  const rule = KIND_RULES[kind];
  return orgs
    .filter((o) => o.active && rule.parents.includes(o.kind))
    .sort((a, b) => a.name.localeCompare(b.name, "vi"));
}

export function allowedParent(kind: OrgKind, parent: Org | null): boolean {
  const rule = KIND_RULES[kind];
  return parent === null
    ? rule.root
    : parent.active && rule.parents.includes(parent.kind);
}

/** ĐVHC chọn được: đúng cấp, còn hiệu lực hôm nay, và nằm trong địa bàn của cha (app.unitWithin). */
export function unitOptions(
  kind: OrgKind,
  units: readonly AdminUnit[],
  parent: Org | null,
  day: string,
): AdminUnit[] {
  const level = KIND_RULES[kind].unit;
  if (level === "none") return [];
  const within = parent?.admin_unit_code ?? null;
  return units
    .filter((u) =>
      level === "any"
        ? u.level === "province" || u.level === "commune"
        : u.level === level,
    )
    .filter((u) => unitValidOn(u, day))
    .filter((u) => !within || u.code === within || u.parent_code === within)
    .sort((a, b) => a.name.localeCompare(b.name, "vi"));
}

export interface OrgForm {
  kind: OrgKind;
  code: string;
  name: string;
  admin_unit_code: string;
  parent_ref: string;
}

export const EMPTY_FORM: OrgForm = {
  kind: "ttkn_province",
  code: "",
  name: "",
  admin_unit_code: "",
  parent_ref: "",
};

export type FieldErrors = Partial<
  Record<"kind" | "code" | "name" | "admin_unit_code" | "parent_ref", string>
>;

/** Kiểm phía client trước khi gửi (server kiểm lại). */
export function validateForm(f: OrgForm, orgs: readonly Org[]): FieldErrors {
  const rule = KIND_RULES[f.kind];
  const errs: FieldErrors = {};
  if (rule.redirect) {
    errs.kind = "HTX tạo qua màn Neo HTX theo mã số thuế.";
    return errs;
  }
  const c = codeError(f.code);
  if (c) errs.code = c;
  const n = nameError(f.name);
  if (n) errs.name = n;
  const parent = f.parent_ref
    ? (orgs.find((o) => o.ref === f.parent_ref) ?? null)
    : null;
  if (f.parent_ref && !parent)
    errs.parent_ref = "Không tìm thấy tổ chức cấp trên.";
  else if (!allowedParent(f.kind, parent))
    errs.parent_ref = parent
      ? "Tổ chức cấp trên không hợp lệ cho loại này."
      : "Chọn tổ chức cấp trên.";
  if (
    (rule.unit === "province" || rule.unit === "commune") &&
    !f.admin_unit_code.trim()
  )
    errs.admin_unit_code = "Chọn đơn vị hành chính.";
  return errs;
}

/** Thân POST /v1/orgs theo loại: chỉ gửi trường loại đó dùng. */
export function buildOrgInput(f: OrgForm): PartyOrgInput {
  const rule = KIND_RULES[f.kind];
  const body: PartyOrgInput = {
    kind: f.kind,
    code: f.code.trim(),
    name: f.name.trim(),
  };
  const unit = f.admin_unit_code.trim();
  if (rule.unit !== "none" && unit) body.admin_unit_code = unit;
  if (f.parent_ref) body.parent_ref = f.parent_ref;
  return body;
}

const FIELD_ALIASES: Record<string, keyof FieldErrors> = {
  kind: "kind",
  code: "code",
  name: "name",
  admin_unit_code: "admin_unit_code",
  parent_ref: "parent_ref",
};

/**
 * Lỗi server → lỗi theo trường. 409 party.org_code_taken → ô Mã; 422 party.invalid_hierarchy → ô Cấp trên;
 * 422 validation_failed → theo `errors[].field`. Trả null khi không gắn được trường nào (hiện khung lỗi chung).
 */
export function fieldErrorsFromProblem(p: ApiProblem): FieldErrors | null {
  const d = p.problem;
  if (d.code === "party.org_code_taken")
    return {
      code: "Mã tổ chức đã được dùng — chọn mã khác (mã không tái sử dụng).",
    };
  if (d.code === "party.invalid_hierarchy")
    return {
      parent_ref: d.detail || "Vị trí trong cây phân cấp không hợp lệ.",
    };
  if (d.code === "validation_failed" && p.fieldErrors.length) {
    const out: FieldErrors = {};
    for (const e of p.fieldErrors) {
      const k = e.field ? FIELD_ALIASES[e.field] : undefined;
      if (k && !out[k]) out[k] = e.reason || "Không hợp lệ.";
    }
    return Object.keys(out).length ? out : null;
  }
  return null;
}
