// Quy tắc hiển thị của màn hình khách hàng Public API và bảng kê (G5). CHỈ để ẩn/hiện và gợi ý —
// máy chủ là nơi quyết định (pf/apiclient app/ + CHECK DB, R-56: 403 apiclient.self_approval /
// metering.self_approval). Không có quy tắc nào ở đây đọc hay giữ bí mật khóa API.
import type {
  ApiClientProposal,
  ApiClientRecord,
  ApiClientStatus,
  ApiDataProduct,
  ApiKeyRecord,
  ApiQuotaPlan,
  ApiQuotaPlanProposal,
  ApiSubscription,
  EgressChannel,
  MeteringStatement,
} from "@/api/types";
import { hasRole, ROLES, type Principal } from "@/auth/principal";

export const CLIENT_STATUS: Record<ApiClientStatus, { label: string; chip: string }> = {
  proposed: { label: "Chờ duyệt", chip: "c-warn" },
  active: { label: "Đang hoạt động", chip: "c-ok" },
  suspended: { label: "Tạm ngưng", chip: "c-bad" },
  revoked: { label: "Đã thu hồi", chip: "c-mute" },
};

export const KEY_STATUS: Record<ApiKeyRecord["status"], { label: string; chip: string }> = {
  active: { label: "Hiệu lực", chip: "c-ok" },
  rotated: { label: "Đã xoay (còn chồng lấn)", chip: "c-warn" },
  revoked: { label: "Đã thu hồi", chip: "c-mute" },
  expired: { label: "Hết hạn", chip: "c-mute" },
};

export const SUB_STATUS: Record<ApiSubscription["status"], { label: string; chip: string }> = {
  proposed: { label: "Chờ duyệt", chip: "c-warn" },
  active: { label: "Hiệu lực", chip: "c-ok" },
  revoked: { label: "Đã thu hồi", chip: "c-mute" },
};

export const PLAN_STATUS: Record<ApiQuotaPlan["status"], { label: string; chip: string }> = {
  proposed: { label: "Chờ duyệt", chip: "c-warn" },
  approved: { label: "Đã duyệt", chip: "c-ok" },
};

export const STATEMENT_STATUS: Record<MeteringStatement["status"], { label: string; chip: string }> = {
  draft: { label: "Nháp — chờ duyệt", chip: "c-warn" },
  approved: { label: "Đã duyệt (đóng băng)", chip: "c-ok" },
};

/** Sản phẩm dữ liệu ban đầu (g5-conventions §6) và scope của hợp đồng `contracts/dataproduct`. */
export const PRODUCTS: Record<ApiDataProduct, { label: string; scopes: readonly string[]; note?: string }> = {
  collection_volume: {
    label: "Sản lượng thu gom theo xã × vụ × tháng",
    scopes: ["dp:collection_volume:read", "dp:collection_volume:export", "usage:read"],
    note: "Ô dưới k nông hộ bị ẩn (publicdata.k_min, tạm 10).",
  },
  hub_capacity: {
    label: "Bậc sử dụng sức chứa Hub",
    scopes: ["dp:hub_capacity:read", "usage:read"],
    note: "Cần MKG đồng ý mục đích mới (Q5-6).",
  },
  mrv_package_status: {
    label: "Trạng thái gói bằng chứng MRV (đơn vị thẩm định)",
    scopes: ["dp:mrv_package_status:read", "usage:read"],
    note: "Chỉ xem (KN US-MRV-05); tải gói chưa bật — chờ C-1.",
  },
};

export const PRODUCT_LABEL = (p: string): string => PRODUCTS[p as ApiDataProduct]?.label ?? p;

export const SCOPE_LABEL: Record<string, string> = {
  "usage:read": "Xem sử dụng của chính mình",
};
export function scopeLabel(scope: string): string {
  const m = /^dp:([a-z_]+):(read|export)$/.exec(scope);
  if (m) return `${m[2] === "read" ? "Đọc" : "Xuất tệp"} ${m[1]}`;
  return SCOPE_LABEL[scope] ?? scope;
}

export const CHANNEL_LABEL: Record<EgressChannel, string> = {
  public_api: "Public API",
  mrv_package: "Gói bằng chứng MRV",
  report_export: "Xuất báo cáo",
  valve: "Van MKG → nền tảng",
  partner_share: "Chia sẻ đối tác (ADR-0020)",
};

export const COUNTERPARTY_LABEL: Record<string, string> = {
  api_client: "Khách hàng API",
  tenant: "Tenant",
  actor: "Người dùng",
};

/** Hạn mức mặc định của gói (apiclientport.Default*) — tạm, chờ khách (Q5-5). */
export const DEFAULT_PLAN = { requests_per_minute: 60, daily_units: 10_000, monthly_units: 200_000, max_export_rows: 100_000 } as const;

const MFA_MSG = "Cần đăng nhập có xác thực hai yếu tố (OTP) để dùng chức năng quản trị.";
const SOD_MSG = "Người duyệt phải khác người đề xuất (R-56).";

function base(p: Principal | null, role: string, roleText: string): string | null {
  if (!p) return "Chưa đăng nhập.";
  if (!hasRole(p, role)) return `Cần vai ${roleText} (${role}).`;
  if (!p.mfa) return MFA_MSG;
  return null;
}

/** Ai được đề xuất / thao tác quản trị (api_client_admin). */
export function adminBlock(p: Principal | null): string | null {
  return base(p, ROLES.apiClientAdmin, "Quản trị khách hàng Public API");
}

/** Duyệt một bản ghi (khách hàng, đăng ký, gói): api_client_approver ≠ người đề xuất. */
export function approveBlock(rec: { status: string; proposed_by: string }, p: Principal | null): string | null {
  if (rec.status !== "proposed") return "Bản ghi không còn chờ duyệt.";
  const b = base(p, ROLES.apiClientApprover, "Duyệt khách hàng Public API");
  if (b) return b;
  if (rec.proposed_by === p?.subject) return SOD_MSG;
  return null;
}

export function suspendBlock(c: Pick<ApiClientRecord, "status">, p: Principal | null): string | null {
  if (c.status !== "active") return "Chỉ tạm ngưng được khách hàng đang hoạt động.";
  return adminBlock(p);
}

export function reactivateBlock(c: Pick<ApiClientRecord, "status">, p: Principal | null): string | null {
  if (c.status !== "suspended") return "Chỉ bật lại được khách hàng đang tạm ngưng.";
  return base(p, ROLES.apiClientApprover, "Duyệt khách hàng Public API");
}

export function revokeClientBlock(c: Pick<ApiClientRecord, "status">, p: Principal | null): string | null {
  if (c.status === "revoked") return "Khách hàng đã bị thu hồi.";
  return adminBlock(p);
}

/** Khóa còn hiệu lực xác thực: active, hoặc rotated còn trong thời gian chồng lấn. */
export function isKeyEffective(k: Pick<ApiKeyRecord, "status" | "grace_until" | "expires_at">, now: number = Date.now()): boolean {
  if (Date.parse(k.expires_at) <= now) return false;
  if (k.status === "active") return true;
  return k.status === "rotated" && !!k.grace_until && Date.parse(k.grace_until) > now;
}

export const MAX_EFFECTIVE_KEYS = 2;

export function issueKeyBlock(c: Pick<ApiClientRecord, "status">, keys: readonly ApiKeyRecord[], p: Principal | null, now: number = Date.now()): string | null {
  if (c.status !== "active") return "Chỉ phát hành khóa cho khách hàng đang hoạt động.";
  if (keys.filter((k) => isKeyEffective(k, now)).length >= MAX_EFFECTIVE_KEYS) return "Đã có tối đa 2 khóa hiệu lực — xoay hoặc thu hồi một khóa trước.";
  return adminBlock(p);
}

/** Lý do (suspend/revoke) 10–500 ký tự, không ghi dữ liệu cá nhân. */
export function reasonError(reason: string): string | null {
  const n = reason.trim().length;
  if (n < 10) return "Lý do tối thiểu 10 ký tự.";
  if (n > 500) return "Lý do tối đa 500 ký tự.";
  return null;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export const isUuid = (s: string): boolean => UUID.test(s.trim());

export interface FieldError {
  field: string;
  reason: string;
}

/** Kiểm đề xuất khách hàng như hợp đồng Admin API (additionalProperties: false). */
export function validateClientProposal(d: ApiClientProposal): FieldError[] {
  const out: FieldError[] = [];
  if (!isUuid(d.client_tenant_id)) out.push({ field: "client_tenant_id", reason: "Mã tenant bên thứ ba phải là UUID." });
  const name = d.display_name.trim();
  if (name.length < 2 || name.length > 200) out.push({ field: "display_name", reason: "Tên tổ chức 2–200 ký tự." });
  const purpose = d.purpose.trim();
  if (purpose.length < 10 || purpose.length > 500) out.push({ field: "purpose", reason: "Mục đích sử dụng 10–500 ký tự." });
  if (!isUuid(d.contact_ref)) out.push({ field: "contact_ref", reason: "Mã đầu mối phải là UUID (không nhập tên/điện thoại)." });
  const methods = d.auth_methods ?? [];
  if (methods.length === 0) out.push({ field: "auth_methods", reason: "Chọn ít nhất một cách xác thực." });
  if (methods.includes("oauth_client_credentials") && !/^[A-Za-z0-9._-]{3,100}$/.test(d.oauth_client_id ?? ""))
    out.push({ field: "oauth_client_id", reason: "Mã client OAuth 3–100 ký tự (chữ, số, . _ -)." });
  return out;
}

export function validateSubscription(product: ApiDataProduct, scopes: readonly string[]): FieldError[] {
  const allowed = PRODUCTS[product]?.scopes ?? [];
  const out: FieldError[] = [];
  if (scopes.length === 0 || scopes.length > 3) out.push({ field: "scopes", reason: "Chọn 1–3 phạm vi." });
  for (const s of scopes) if (!allowed.includes(s)) out.push({ field: "scopes", reason: `Phạm vi ${s} không có trong hợp đồng của sản phẩm.` });
  return out;
}

export function validatePlan(d: ApiQuotaPlanProposal): FieldError[] {
  const out: FieldError[] = [];
  const int = (v: number, min: number, max: number) => Number.isInteger(v) && v >= min && v <= max;
  if (!int(d.requests_per_minute, 1, 100_000)) out.push({ field: "requests_per_minute", reason: "1–100.000 lời gọi/phút." });
  if (!int(d.daily_units, 1, Number.MAX_SAFE_INTEGER)) out.push({ field: "daily_units", reason: "Số nguyên ≥ 1." });
  if (!int(d.monthly_units, 1, Number.MAX_SAFE_INTEGER)) out.push({ field: "monthly_units", reason: "Số nguyên ≥ 1." });
  if (int(d.daily_units, 1, Number.MAX_SAFE_INTEGER) && int(d.monthly_units, 1, Number.MAX_SAFE_INTEGER) && d.monthly_units < d.daily_units)
    out.push({ field: "monthly_units", reason: "Hạn mức tháng không nhỏ hơn hạn mức ngày." });
  if (!int(d.max_export_rows, 1, 100_000_000)) out.push({ field: "max_export_rows", reason: "1–100.000.000 dòng." });
  return out;
}

/** Phiên bản gói đang áp dụng tại `now`: bản đã duyệt mới nhất có hiệu lực ≤ now. */
export function effectivePlan(plans: readonly ApiQuotaPlan[], now: number = Date.now()): ApiQuotaPlan | null {
  return (
    plans
      .filter((p) => p.status === "approved" && Date.parse(p.effective_from) <= now)
      .sort((a, b) => b.version - a.version)[0] ?? null
  );
}

// --- Bảng kê (pf/metering) ---

/** Kỳ tháng gần nhất đã kết thúc theo giờ Việt Nam ("YYYY-MM"). */
export function lastEndedPeriod(now: Date = new Date()): string {
  const vn = new Date(now.getTime() + 7 * 3_600_000);
  const y = vn.getUTCFullYear();
  const m = vn.getUTCMonth(); // 0-based → tháng trước
  return m === 0 ? `${y - 1}-12` : `${y}-${String(m).padStart(2, "0")}`;
}

export const PERIOD_RE = /^\d{4}-(0[1-9]|1[0-2])$/;

/** Kỳ đã kết thúc chưa (ngày đầu tháng kế tiếp 00:00 giờ VN ≤ now). */
export function periodEnded(period: string, now: Date = new Date()): boolean {
  if (!PERIOD_RE.test(period)) return false;
  const [y, m] = period.split("-").map((x) => Number.parseInt(x, 10)) as [number, number];
  const endUtc = Date.UTC(m === 12 ? y + 1 : y, m === 12 ? 0 : m, 1) - 7 * 3_600_000;
  return now.getTime() >= endUtc;
}

export function formatPeriod(period: string): string {
  const m = /^(\d{4})-(\d{2})$/.exec(period);
  return m ? `Tháng ${m[2]}/${m[1]}` : period;
}

export function requestStatementBlock(p: Principal | null): string | null {
  return base(p, ROLES.billingOfficer, "Lập bảng kê sử dụng");
}

/** billing_approver ≠ người yêu cầu lập; bảng kê đã duyệt/đã bị thay thì đóng băng. */
export function approveStatementBlock(st: Pick<MeteringStatement, "status" | "requested_by" | "superseded_by">, p: Principal | null): string | null {
  if (st.superseded_by) return "Bảng kê đã bị thay bởi bảng kê bù.";
  if (st.status !== "draft") return "Bảng kê đã duyệt — đóng băng.";
  const b = base(p, ROLES.billingApprover, "Duyệt bảng kê sử dụng");
  if (b) return b;
  if (st.requested_by === p?.subject) return "Người duyệt bảng kê phải khác người yêu cầu lập (R-56).";
  return null;
}

export function canDownloadStatement(p: Principal | null): boolean {
  return hasRole(p, ROLES.billingOfficer, ROLES.billingApprover);
}

/** Tên tệp CSV bảng kê (tất định theo kỳ + mã bảng kê). */
export function statementFileName(st: Pick<MeteringStatement, "id" | "period">): string {
  return `bang-ke-${st.period}-${st.id.slice(0, 8)}.csv`;
}

/** Rút gọn mã tham chiếu để hiển thị (không phải định danh người). */
export const shortRef = (id: string | null | undefined): string => (id ? `${id.slice(0, 8)}…` : "—");
