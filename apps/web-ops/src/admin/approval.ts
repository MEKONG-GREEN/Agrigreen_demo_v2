import type { ParamProposal } from "@/api/pending/masterdata";
import { hasRole, ROLES, type Principal } from "@/auth/principal";

/**
 * Lý do người dùng KHÔNG được phê duyệt/từ chối một đề xuất, hoặc null nếu được.
 * Chỉ để hiển thị — backend vẫn chặn bằng authz.EnsureDistinctApprover (R-56, 403 self_approval_forbidden).
 */
export function approvalBlockReason(proposal: Pick<ParamProposal, "proposed_by" | "status">, p: Principal | null): string | null {
  if (!p) return "Chưa đăng nhập.";
  if (proposal.status !== "pending") return "Đề xuất đã được xử lý.";
  if (!hasRole(p, ROLES.paramApprover)) return "Cần vai Người phê duyệt tham số (param_approver).";
  if (proposal.proposed_by === p.subject) return "Người phê duyệt phải khác người đề xuất (R-56).";
  if (!p.mfa) return "Cần đăng nhập có xác thực hai yếu tố (OTP) để dùng chức năng quản trị.";
  return null;
}

export function proposeBlockReason(p: Principal | null): string | null {
  if (!p) return "Chưa đăng nhập.";
  if (!hasRole(p, ROLES.paramSetter)) return "Cần vai Người đặt tham số (param_setter).";
  if (!p.mfa) return "Cần đăng nhập có xác thực hai yếu tố (OTP) để dùng chức năng quản trị.";
  return null;
}
