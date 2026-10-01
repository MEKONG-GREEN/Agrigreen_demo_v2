// Quy tắc chọn vai trò & phòng ban (FN-CH-00.01 B3) — hàm thuần, kiểm thử được.
import { KN_ROLE_LABEL, type DepartmentRef, type RoleAssignment, type SelectContextRequest, type SessionContext } from "./contract";

export interface ContextOption {
  assignment: RoleAssignment;
  /** null với Admin (không chọn phòng ban). */
  department: DepartmentRef | null;
}

/** Các cặp vai trò × phòng ban vào được: phòng ban "Đang áp dụng"; Admin một lựa chọn, không phòng ban. */
export function enterableOptions(assignments: readonly RoleAssignment[]): ContextOption[] {
  return assignments.flatMap((a): ContextOption[] =>
    a.role === "admin" ? [{ assignment: a, department: null }] : a.departments.filter((d) => d.status === "active").map((d) => ({ assignment: a, department: d })),
  );
}

/** Chỉ 1 vai trò + 1 phòng ban đang áp dụng → vào thẳng (không hiện màn chọn). */
export function autoSelection(assignments: readonly RoleAssignment[]): SelectContextRequest | null {
  const opts = enterableOptions(assignments);
  if (opts.length !== 1) return null;
  const o = opts[0]!;
  return { assignment_id: o.assignment.assignment_id, department_code: o.department?.code ?? null };
}

export function selectionOf(o: ContextOption): SelectContextRequest {
  return { assignment_id: o.assignment.assignment_id, department_code: o.department?.code ?? null };
}

/** Lựa chọn đã lưu còn hợp lệ (vai trò còn, phòng ban vẫn đang áp dụng)? */
export function isValidSelection(assignments: readonly RoleAssignment[], sel: SelectContextRequest | null): sel is SelectContextRequest {
  if (!sel) return false;
  return enterableOptions(assignments).some((o) => o.assignment.assignment_id === sel.assignment_id && (o.department?.code ?? null) === sel.department_code);
}

/** "Phòng Trồng trọt" → "Trồng trọt" (dòng phụ trên thanh trên, Hình 3d–3f). */
export function shortDepartment(name: string | null): string {
  return (name ?? "").replace(/^Phòng\s+/u, "");
}

/** Nhãn đầy đủ: "Cấp Tỉnh · Phòng Trồng trọt"; Admin: "Admin · Tất cả phòng ban". */
export function optionLabel(o: ContextOption): string {
  return o.department ? `${KN_ROLE_LABEL[o.assignment.role]} · ${o.department.name}` : `${KN_ROLE_LABEL[o.assignment.role]} · Tất cả phòng ban`;
}

export function contextLabel(ctx: Pick<SessionContext, "role" | "department_name">): string {
  return ctx.department_name ? `${KN_ROLE_LABEL[ctx.role]} · ${ctx.department_name}` : `${KN_ROLE_LABEL[ctx.role]} · Tất cả phòng ban`;
}

export function contextShortLabel(ctx: Pick<SessionContext, "role" | "department_name">): string {
  return ctx.department_name ? `${KN_ROLE_LABEL[ctx.role]} · ${shortDepartment(ctx.department_name)}` : `${KN_ROLE_LABEL[ctx.role]} · Tất cả phòng ban`;
}

/** Các cặp khác cặp đang dùng — mục "Đổi vai trò / phòng ban" trong menu tài khoản. */
export function otherOptions(assignments: readonly RoleAssignment[], ctx: Pick<SessionContext, "assignment_id" | "department_code"> | null): ContextOption[] {
  return enterableOptions(assignments).filter(
    (o) => !ctx || o.assignment.assignment_id !== ctx.assignment_id || (o.department?.code ?? null) !== ctx.department_code,
  );
}

/** Dòng phụ thẻ vai trò (Hình 3b): "TTKN An Giang · Mỹ Hòa Hưng, Bình Đức". */
export function assignmentSubtitle(a: RoleAssignment): string {
  if (a.role === "admin") return "Toàn hệ thống · không chọn phòng ban";
  const org = a.org_name.replace(/^TTKN tỉnh /u, "TTKN ");
  if (a.role === "kn_xa" && a.communes.length) return `${org} · ${a.communes.map((c) => c.name).join(", ")}`;
  return a.org_name;
}

export function initials(name: string): string {
  const clean = name.replace(/\(.*?\)/g, "").trim();
  if (!clean) return "AG";
  // "admin.mekong" → "AD" (Hình 3c); "Nguyễn Văn An" → "NA" (họ + tên).
  const parts = clean.split(/\s+/);
  if (parts.length === 1) return clean.slice(0, 2).toUpperCase();
  return ((parts[0]?.[0] ?? "") + (parts[parts.length - 1]?.[0] ?? "")).toUpperCase();
}
