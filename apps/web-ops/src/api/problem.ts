// Lỗi RFC 9457 (application/problem+json) — bảng mã ở docs/errors.md.
// Mã lỗi là hợp đồng: hiển thị thông báo tiếng Việt theo `code`, luôn kèm trace_id (R-63).

export interface ProblemFieldError {
  field?: string;
  reason?: string;
}

export interface ProblemDetails {
  type: string;
  title: string;
  status: number;
  code: string;
  detail?: string;
  instance?: string;
  trace_id?: string;
  errors?: ProblemFieldError[];
}

/** Thông báo tiếng Việt theo mã lỗi (docs/errors.md). */
export const PROBLEM_MESSAGES: Readonly<Record<string, string>> = {
  bad_request: "Yêu cầu không hợp lệ. Vui lòng kiểm tra lại dữ liệu gửi đi.",
  idempotency_key_required: "Thiếu khóa chống gửi trùng (Idempotency-Key). Vui lòng tải lại trang và thử lại.",
  unauthenticated: "Phiên đăng nhập đã hết hạn hoặc không hợp lệ. Vui lòng đăng nhập lại.",
  forbidden: "Bạn không có quyền thực hiện thao tác này.",
  mfa_required:
    "Chức năng quản trị (Admin API) bắt buộc xác thực hai yếu tố. Phiên hiện tại mới đăng nhập bằng mật khẩu — vui lòng đăng xuất rồi đăng nhập lại kèm mã OTP.",
  self_approval_forbidden: "Không được tự phê duyệt thay đổi do chính mình đề xuất. Cần một người phê duyệt khác.",
  not_found: "Không tìm thấy dữ liệu yêu cầu.",
  method_not_allowed: "Thao tác không được hỗ trợ.",
  idempotency_in_progress: "Yêu cầu trước đó đang được xử lý. Vui lòng đợi giây lát rồi tải lại.",
  conflict: "Dữ liệu đã bị thay đổi bởi người khác. Vui lòng tải lại và thử lại.",
  payload_too_large: "Dữ liệu gửi lên vượt quá dung lượng cho phép.",
  validation_failed: "Dữ liệu chưa hợp lệ. Vui lòng kiểm tra các trường được đánh dấu.",
  idempotency_key_reused: "Khóa chống gửi trùng đã được dùng cho một yêu cầu khác. Vui lòng thử lại.",
  internal_error: "Hệ thống gặp lỗi. Vui lòng thử lại sau hoặc gửi mã tra cứu cho bộ phận hỗ trợ.",
  "party.consent_not_found": "Không tìm thấy bản ghi đồng ý.",
  "party.consent_id_reused": "Mã bản ghi đồng ý đã được dùng cho bản ghi khác.",
  // pf/masterdata
  "masterdata.admin_unit_not_found": "Không tìm thấy đơn vị hành chính có hiệu lực với mã này.",
  "masterdata.catalog_not_found": "Danh mục không tồn tại.",
  "masterdata.param_version_not_found": "Không tìm thấy phiên bản tham số.",
  "masterdata.param_invalid_transition": "Trạng thái phiên bản không cho phép thao tác này (có thể đã được người khác xử lý). Vui lòng tải lại.",
  "masterdata.param_not_setter": "Chỉ người đặt tham số mới được gửi duyệt phiên bản này.",
  "masterdata.param_correction_required":
    "Đã có phiên bản được duyệt cùng ngày hiệu lực. Nhập “Lý do sửa” (ít nhất 10 ký tự) để ghi bản ghi bù.",
  // pf/party
  "party.org_not_found": "Không tìm thấy tổ chức/HTX, hoặc nằm ngoài phạm vi được phân quyền.",
  "party.org_state_conflict": "Trạng thái tổ chức không cho phép thao tác này (hoặc không có gì thay đổi).",
  "party.out_of_scope": "Thao tác nằm ngoài phạm vi TW → Tỉnh → Xã được phân quyền.",
  "party.import_invalid": "Tệp nhập sai mẫu (thiếu cột bắt buộc full_name, phone, cooperative_code; sai CSV hoặc quá 200 dòng). Cả tệp bị từ chối.",
  "party.dependency_unavailable": "Dịch vụ danh bạ tổ chức/kho định danh tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/geo
  "geo.admin_boundary_not_found": "Chưa nhập ranh giới hành chính cho đơn vị này.",
  "geo.plot_not_found": "Không tìm thấy thửa, hoặc thửa nằm ngoài phạm vi.",
  "geo.out_of_scope": "Thửa/HTX nằm ngoài phạm vi xã được phân quyền.",
  "geo.plot_state_conflict": "Thửa đã ngừng dùng hoặc đã ở đúng trạng thái yêu cầu.",
  "geo.overlap_cross_coop":
    "Ranh giới chồng lấn thửa của HTX khác vượt ngưỡng cho phép — không thể lưu. Cần hiệu chỉnh ranh giới hoặc xử lý với HTX liên quan.",
  "geo.overlap_needs_confirm":
    "Ranh giới chồng lấn thửa cùng HTX hoặc thửa nháp chưa gán. Bạn có thể xác nhận vẫn lưu — hệ thống sẽ mở xung đột để xem xét.",
  "geo.area_deviation_needs_confirm":
    "Diện tích đo lệch quá 5% so với diện tích khai báo. Kiểm tra lại, hoặc xác nhận vẫn lưu nếu số đo là đúng.",
  "geo.version_conflict": "Ranh giới đã được người khác cập nhật. Vui lòng tải lại để sửa trên phiên bản mới nhất.",
  "geo.dependency_unavailable": "Dịch vụ bản đồ tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/extension, pf/content, pf/mechanization
  "extension.out_of_scope": "Dữ liệu nằm ngoài phạm vi TW → Tỉnh → Xã được phân quyền.",
  "extension.dependency_unavailable": "Dịch vụ khuyến nông tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  "content.document_not_found": "Tài liệu không tồn tại, đã gỡ hoặc nằm ngoài phạm vi.",
  "content.dependency_unavailable": "Thư viện số tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  "mechanization.run_not_found": "Chưa có kết quả cân đối đã lưu cho vụ này.",
  "mechanization.dependency_unavailable": "Dịch vụ Bản đồ CGH tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  network_error: "Không kết nối được máy chủ. Vui lòng kiểm tra mạng và thử lại.",
  invalid_response: "Máy chủ trả về dữ liệu không đúng định dạng.",
};

/** Mã 409 của pf/geo mà người dùng được phép "Xác nhận vẫn lưu" (gửi lại với confirm_overlap: true). */
export const CONFIRMABLE_CODES: ReadonlySet<string> = new Set(["geo.overlap_needs_confirm", "geo.area_deviation_needs_confirm"]);

export function isConfirmable(p: { code: string } | null | undefined): boolean {
  return !!p && CONFIRMABLE_CODES.has(p.code);
}

const STATUS_FALLBACK: Readonly<Record<number, string>> = {
  400: "bad_request",
  401: "unauthenticated",
  403: "forbidden",
  404: "not_found",
  405: "method_not_allowed",
  409: "conflict",
  413: "payload_too_large",
  422: "validation_failed",
};

export class ApiProblem extends Error {
  readonly status: number;
  readonly code: string;
  readonly traceId: string | undefined;
  readonly fieldErrors: ProblemFieldError[];
  readonly problem: ProblemDetails;

  constructor(problem: ProblemDetails) {
    super(problemMessage(problem));
    this.name = "ApiProblem";
    this.status = problem.status;
    this.code = problem.code;
    this.traceId = problem.trace_id;
    this.fieldErrors = problem.errors ?? [];
    this.problem = problem;
  }
}

/** Thông báo cho người dùng: ưu tiên bảng mã; mã lạ thì dùng detail/title của server. */
export function problemMessage(p: Pick<ProblemDetails, "code" | "status" | "detail" | "title">): string {
  const known = PROBLEM_MESSAGES[p.code];
  if (known) return known;
  if (p.detail) return p.detail;
  if (p.title) return p.title;
  const byStatus = STATUS_FALLBACK[p.status];
  if (byStatus) return PROBLEM_MESSAGES[byStatus] ?? "";
  return PROBLEM_MESSAGES.internal_error ?? "Lỗi";
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Lấy trace-id từ header W3C traceparent: 00-<trace-id>-<span-id>-<flags>. */
function traceIdFromHeaders(headers: Headers | undefined): string | undefined {
  if (!headers) return undefined;
  const explicit = headers.get("X-Trace-Id");
  if (explicit) return explicit;
  const tp = headers.get("traceparent");
  const m = tp ? /^[\da-f]{2}-([\da-f]{32})-[\da-f]{16}-[\da-f]{2}$/i.exec(tp.trim()) : null;
  return m?.[1];
}

/**
 * Chuẩn hoá body lỗi (đã parse JSON hoặc chuỗi) + response thành ProblemDetails.
 * Không bao giờ ném: body lạ vẫn ra một problem có code theo mã HTTP.
 */
export function toProblem(body: unknown, response?: Pick<Response, "status" | "headers">): ProblemDetails {
  const status = response?.status ?? (isRecord(body) && typeof body.status === "number" ? body.status : 500);
  const headerTrace = traceIdFromHeaders(response?.headers);
  if (isRecord(body) && typeof body.code === "string") {
    const errors = Array.isArray(body.errors)
      ? body.errors.filter(isRecord).map((e) => ({
          field: typeof e.field === "string" ? e.field : undefined,
          reason: typeof e.reason === "string" ? e.reason : undefined,
        }))
      : undefined;
    return {
      type: typeof body.type === "string" ? body.type : `https://errors.agrigreen.vn/${body.code}`,
      title: typeof body.title === "string" ? body.title : "",
      status: typeof body.status === "number" ? body.status : status,
      code: body.code,
      detail: typeof body.detail === "string" ? body.detail : undefined,
      instance: typeof body.instance === "string" ? body.instance : undefined,
      trace_id: typeof body.trace_id === "string" ? body.trace_id : headerTrace,
      errors,
    };
  }
  const code = STATUS_FALLBACK[status] ?? (status >= 500 ? "internal_error" : "bad_request");
  return {
    type: `https://errors.agrigreen.vn/${code}`,
    title: "",
    status,
    code,
    trace_id: headerTrace,
  };
}

/** Đọc một Response lỗi (thường là application/problem+json) thành ApiProblem. */
export async function parseProblemResponse(response: Response): Promise<ApiProblem> {
  let body: unknown = undefined;
  try {
    const text = await response.text();
    body = text ? JSON.parse(text) : undefined;
  } catch {
    body = undefined;
  }
  return new ApiProblem(toProblem(body, response));
}

export function networkProblem(): ApiProblem {
  return new ApiProblem({
    type: "https://errors.agrigreen.vn/network_error",
    title: "",
    status: 0,
    code: "network_error",
  });
}

/** Ánh xạ errors[] theo trường → thông điệp để gắn vào form. */
export function fieldErrorMap(p: ApiProblem): Record<string, string> {
  const out: Record<string, string> = {};
  for (const e of p.fieldErrors) {
    if (e.field) out[e.field] = e.reason ?? "Không hợp lệ";
  }
  return out;
}
