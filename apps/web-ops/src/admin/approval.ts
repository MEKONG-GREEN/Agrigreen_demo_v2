import type { ParamVersion } from "@/api/types";
import { hasRole, ROLES, type Principal } from "@/auth/principal";

/**
 * Lý do người dùng KHÔNG được phê duyệt/từ chối một phiên bản tham số, hoặc null nếu được.
 * Chỉ để hiển thị — backend vẫn chặn bằng authz.EnsureDistinctApprover (R-56, 403 self_approval_forbidden).
 */
export function approvalBlockReason(v: Pick<ParamVersion, "setter_ref" | "status">, p: Principal | null): string | null {
  if (!p) return "Chưa đăng nhập.";
  if (v.status !== "submitted") return v.status === "draft" ? "Phiên bản còn là nháp — người đặt phải gửi duyệt trước." : "Phiên bản đã được xử lý.";
  if (!hasRole(p, ROLES.paramApprover)) return "Cần vai Người phê duyệt tham số (param_approver).";
  if (v.setter_ref === p.subject) return "Người phê duyệt phải khác người đề xuất (R-56).";
  if (!p.mfa) return "Cần đăng nhập có xác thực hai yếu tố (OTP) để dùng chức năng quản trị.";
  return null;
}

export function proposeBlockReason(p: Principal | null): string | null {
  if (!p) return "Chưa đăng nhập.";
  if (!hasRole(p, ROLES.paramSetter)) return "Cần vai Người đặt tham số (param_setter).";
  if (!p.mfa) return "Cần đăng nhập có xác thực hai yếu tố (OTP) để dùng chức năng quản trị.";
  return null;
}

/** Chỉ người đặt được gửi duyệt bản nháp của mình (masterdata.param_not_setter). */
export function submitBlockReason(v: Pick<ParamVersion, "setter_ref" | "status">, p: Principal | null): string | null {
  if (!p) return "Chưa đăng nhập.";
  if (v.status !== "draft") return "Chỉ gửi duyệt được bản nháp.";
  if (v.setter_ref !== p.subject) return "Chỉ người đặt mới gửi duyệt được bản nháp này.";
  if (!p.mfa) return "Cần đăng nhập có xác thực hai yếu tố (OTP) để dùng chức năng quản trị.";
  return null;
}
