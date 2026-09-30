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
  /** 429: số giây phải chờ (thân `retry_after_s` hoặc header Retry-After). */
  retry_after_s?: number;
}

/** Thông báo tiếng Việt theo mã lỗi (docs/errors.md). */
export const PROBLEM_MESSAGES: Readonly<Record<string, string>> = {
  bad_request: "Yêu cầu không hợp lệ. Vui lòng kiểm tra lại dữ liệu gửi đi.",
  idempotency_key_required: "Thiếu khóa chống gửi trùng (Idempotency-Key). Vui lòng tải lại trang và thử lại.",
  unauthenticated: "Phiên đăng nhập đã hết hạn hoặc không hợp lệ. Vui lòng đăng nhập lại.",
  forbidden: "Bạn không có quyền thực hiện thao tác này.",
  mfa_required:
    "Bề mặt quản trị của API nội bộ MKG bắt buộc xác thực hai yếu tố. Phiên hiện tại mới đăng nhập bằng mật khẩu — vui lòng đăng xuất rồi đăng nhập lại kèm mã OTP.",
  self_approval_forbidden: "Không được tự phê duyệt đề xuất do chính mình lập (R-56). Cần một người phê duyệt khác.",
  not_found: "Không tìm thấy dữ liệu yêu cầu.",
  method_not_allowed: "Thao tác không được hỗ trợ.",
  idempotency_in_progress: "Yêu cầu trước đó đang được xử lý. Vui lòng đợi giây lát rồi tải lại.",
  conflict: "Dữ liệu đã bị thay đổi bởi người khác. Vui lòng tải lại và thử lại.",
  payload_too_large: "Dữ liệu gửi lên vượt quá dung lượng cho phép.",
  validation_failed: "Dữ liệu chưa hợp lệ. Vui lòng kiểm tra các trường được đánh dấu.",
  idempotency_key_reused: "Khóa chống gửi trùng đã được dùng cho một yêu cầu khác. Vui lòng thử lại.",
  internal_error: "Hệ thống gặp lỗi. Vui lòng thử lại sau hoặc gửi mã tra cứu cho bộ phận hỗ trợ.",
  too_many_requests: "Quá nhiều yêu cầu. Vui lòng đợi rồi thử lại.",
  network_error: "Không kết nối được máy chủ. Vui lòng kiểm tra mạng và thử lại.",
  invalid_response: "Máy chủ trả về dữ liệu không đúng định dạng.",
  // mkg/hub, mkg/measurement, mkg/fieldops — docs/errors.md
  "fieldops.already_corrected": "Bản ghi đã bị bù; bù bản ghi hiệu lực mới nhất.",
  "fieldops.cursor_invalid": "Con trỏ pull bị sửa, cắt, hoặc của thiết bị/tài khoản khác — pull lại không có cursor.",
  "fieldops.device_mismatch": "Thiếu header X-Device-Id, hoặc device_id của lô đồng bộ khác header.",
  "fieldops.directory_unavailable": "Không gọi được Application API (chưa cấu hình tài khoản dịch vụ MKG, lỗi mạng, token).",
  "fieldops.discrepancy_reason_required": "Số cuộn ít hơn công đoạn trước — bắt buộc ghi lý do chênh lệch (ít nhất 10 ký tự).",
  "fieldops.htx_not_found": "Application API không có (hoặc chưa chia sẻ — câu hỏi RB-2) HTX, hoặc HTX đã vô hiệu hóa.",
  "fieldops.id_reused": "id do máy sinh đã dùng cho bản ghi khác (khác idem_key).",
  "fieldops.next_stage_conflict": "Bản bù làm công đoạn sau vi phạm FM-04 — bù công đoạn sau trước.",
  "fieldops.nothing_to_confirm": "Thửa chưa có số xếp ghe/xe nên chưa có bảng kê để HTX xác nhận.",
  "fieldops.order_not_found": "Không có lệnh gom trong tenant của token.",
  "fieldops.order_not_open": "Ghi số / mở ca trên lệnh chưa duyệt, đã đóng hoặc đã hủy.",
  "fieldops.order_transition": "Chuyển trạng thái không hợp lệ (sửa kế hoạch sau khi duyệt, duyệt lệnh không còn planned, đóng lệnh chưa giao tổ, hủy lệnh đã đóng/hủy).",
  "fieldops.plot_not_found": "Application API không có (hoặc chưa chia sẻ) thửa.",
  "fieldops.plot_not_in_order": "Thửa không thuộc lệnh gom.",
  "fieldops.plot_other_htx": "Thửa không thuộc HTX của lệnh gom, là thửa nháp hoặc đã ngừng dùng.",
  "fieldops.previous_stage_missing": "Công đoạn trước chưa có số cuộn (FM-03).",
  "fieldops.roll_count_not_found": "Không có bản ghi đếm cuộn cần bù.",
  "fieldops.stage_already_counted": "Công đoạn này của thửa đã có số — sửa bằng bản ghi bù.",
  "fieldops.stage_exceeds_previous": "Số cuộn công đoạn này nhiều hơn công đoạn trước (FM-04) — kiểm tra lại số đếm.",
  "fieldops.statement_stale": "Xác nhận theo statement_revision cũ — MKG đã sửa số sau đó.",
  "fieldops.unit_mismatch": "Công đoạn trước đếm cuộn mà công đoạn sau ghi rơm rời (hoặc ngược lại).",
  "fieldops.valve_denied": "Van công bố từ chối payload — lỗi lập trình, không có gì được ghi.",
  "hub.allocation_mismatch": "Phân bổ tay theo lô có tổng khác lượng xuất.",
  "hub.already_corrected": "Dịch chuyển đã có dòng bù; sai tiếp thì ghi nghiệp vụ mới.",
  "hub.boundary_invalid": "Ranh giới không phải Polygon/MultiPolygon WGS84 hợp lệ.",
  "hub.code_taken": "Mã cơ sở đã dùng bởi cơ sở khác (hiện hành hoặc đang chờ duyệt).",
  "hub.correction_unsupported": "Bù trực tiếp chỉ cho receive và dispatch; dòng của mẻ biến đổi sửa bằng mẻ bù (câu hỏi mở Q-MKG-05).",
  "hub.duplicate_lot_input": "Một lô xuất hiện hai lần trong cùng mẻ / phiếu xuất.",
  "hub.facility_inactive": "Nhận lô hoặc chạy mẻ ở cơ sở đã ngừng dùng.",
  "hub.facility_not_found": "Cơ sở không có (hoặc chưa được duyệt lần nào) trong tenant của token.",
  "hub.id_reused": "id lô do thiết bị sinh đã dùng cho bản ghi khác.",
  "hub.insufficient_stock": "Tồn theo lô không đủ cho lượng xuất (không có lô thì không có hàng).",
  "hub.lot_not_found": "Không có lô trong tenant; lô vào của mẻ không tồn tại.",
  "hub.lot_other_facility": "Lô vào mẻ / lô chỉ định khi xuất không thuộc cơ sở của thao tác.",
  "hub.movement_not_found": "Không có dịch chuyển tồn cần bù.",
  "hub.negative_loss": "Mẻ có khối lượng ra lớn hơn khối lượng vào.",
  "hub.param_pending_exists": "G3: bộ tham số đang có phiên bản chờ duyệt.",
  "hub.param_set_not_found": "G3: bộ tham số không phải risk / weigh.",
  "hub.param_version_not_found": "G3: không có phiên bản tham số số {version}.",
  "hub.parent_invalid": "Cơ sở cha không phải hub hiện hành khác chính nó.",
  "hub.pending_version_exists": "Cơ sở đang có một phiên bản chờ duyệt — duyệt/từ chối trước khi đề xuất tiếp.",
  "hub.ratio_invalid": "Tỷ lệ nguồn của lô không hợp lệ: Σ khác 1 chính xác, > 6 chữ số, ≤ 0, trộn mass_kg với ratio, phần đóng góp < 0.000001.",
  "hub.stocktake_lines_invalid": "G3: dòng kiểm kê lặp dạng hàng hoặc thiếu một dạng hàng đang có tồn.",
  "hub.stocktake_not_found": "G3 (Q-MKG-06): không có phiên kiểm kê trong tenant.",
  "hub.stocktake_not_pending": "G3: phiên kiểm kê đã duyệt / từ chối / bị thay.",
  "hub.stocktake_pending_exists": "G3: cơ sở đang có phiên kiểm kê chờ duyệt — duyệt/từ chối, hoặc đếm lại với supersedes_id + reason.",
  "hub.stocktake_stale": "Sổ tồn đã thay đổi kể từ lúc đếm — cần đếm lại (phiên kiểm kê mới thay phiên cũ).",
  "hub.transfer_not_found": "G3 (Q-MKG-10): không có phiếu chuyển kho.",
  "hub.transfer_not_in_transit": "G3: phiếu chuyển đã nhận.",
  "hub.transfer_same_facility": "G3: cơ sở đích trùng cơ sở đi.",
  "hub.transfer_ticket_invalid": "G3: phiếu cân không phải phiếu HUB_IN (đến) của một lô nguồn thuộc phiếu chuyển, hoặc lặp.",
  "hub.transfer_wrong_facility": "G3: nhận ở cơ sở không phải đích của phiếu chuyển.",
  "hub.valve_denied": "Van công bố từ chối payload (hợp đồng thiếu/chưa duyệt, trường ngoài hợp đồng) — lỗi lập trình, không có gì được ghi.",
  "hub.version_not_found": "Không có phiên bản dữ liệu chủ số {version} của cơ sở.",
  "hub.version_not_pending": "Phiên bản đã được duyệt hoặc từ chối.",
  "hub.weigh_mass_mismatch": "G3: mass_kg khác khối lượng phiếu cân (để trống để lấy theo phiếu); nhận chuyển kho: khác Σ phiếu.",
  "hub.weigh_ticket_lot_mismatch": "G3: id lô khác lot_ref của phiếu cân.",
  "hub.weigh_ticket_required": "G3: tham số hub.weigh.v1 bật ticket_required — cần weigh_ticket_ref, hoặc manual_reason (10..500 ký tự) khi cân hỏng.",
  "hub.weigh_ticket_unknown": "G3 (Q-MKG-04): nhận lô với weigh_ticket_ref mà hub chưa nhận sự kiện phiếu cân HUB_IN — thử lại sau.",
  "hub.weigh_ticket_used": "G3: phiếu cân đã dùng cho lần nhận khác (hoặc đã áp vào lô nhập tay).",
  "measurement.device_not_active": "Thiết bị / kỳ đăng ký không hiệu lực (chưa duyệt, đã thu hồi, đã có kỳ mới).",
  "measurement.device_not_found": "Không có thiết bị cân / kỳ đăng ký.",
  "measurement.device_taken": "device_id đã đăng ký ở tài khoản khác (một thiết bị gắn một tenant mãi mãi).",
  "measurement.dispute_not_found": "Không có hồ sơ tranh chấp.",
  "measurement.dispute_transition": "Thao tác không hợp lệ ở trạng thái hồ sơ (vd kết luận khi chưa xem xét; thêm bằng chứng khi đã đóng).",
  "measurement.import_not_found": "Không có lần nhập số cân của khách.",
  "measurement.invalid_ticket": "Phiếu vi phạm hợp đồng TicketWriter: đã có phiếu hiệu lực ở điểm cân (cần supersedes_id), phiếu bù trỏ phiếu khác lô/điểm hoặc đã bị bù, HUB_IN/HUB_OUT trên lô DIRECT, tuyến khác tuyến của lô, nhập tay thiếu bằng chứng/lý do, id đã dùng.",
  "measurement.lot_not_found": "Lô chưa có phiếu cân (hoặc chưa được đối soát) trong tenant của token.",
  "measurement.no_hub_in": "Lập bằng chứng MRV khi lô chưa có phiếu HUB_IN.",
  "measurement.no_plot_sources": "Lô không có nguồn thửa (hoặc chưa nối nguồn gốc lô mkg/hub).",
  "measurement.not_pending": "Đề xuất không còn chờ duyệt.",
  "measurement.pending_exists": "Thiết bị đang có đề xuất đăng ký chờ duyệt.",
  "measurement.reconciliation_incomplete": "Lô chưa đủ phiếu cân (HUB: HUB_IN, HUB_OUT, CUSTOMER_IN; DIRECT: CUSTOMER_IN) để đối soát.",
  "measurement.session_closed": "Phiên cân đã đóng.",
  "measurement.session_conflict": "Thiết bị (kỳ) đang có phiên mở, hoặc counter_from không lớn hơn counter_to của phiên đã đóng trước.",
  "measurement.session_not_found": "Không có phiên cân.",
  "measurement.ticket_not_found": "Không có phiếu cân.",
  "measurement.valve_denied": "Van công bố từ chối payload mrv_collection_evidence (hợp đồng thiếu, trường ngoài hợp đồng) — không có gì được ghi.",
  "measurement.version_not_found": "Không có phiên bản dung sai / định mức hao hụt số {seq}.",
  "measurement.window_incomplete": "Cửa sổ số đo có bộ đếm thiếu (không bao giờ nội suy).",
};

/** Mã 409 của pf/geo mà người dùng được phép "Xác nhận vẫn lưu" (gửi lại với confirm_overlap: true). */
export const CONFIRMABLE_CODES: ReadonlySet<string> = new Set<string>();

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
  429: "too_many_requests",
};

export class ApiProblem extends Error {
  readonly status: number;
  readonly code: string;
  readonly traceId: string | undefined;
  readonly fieldErrors: ProblemFieldError[];
  readonly problem: ProblemDetails;
  /** Giây phải chờ trước khi thử lại (429), nếu máy chủ cho biết. */
  readonly retryAfterS: number | undefined;

  constructor(problem: ProblemDetails) {
    super(problemMessage(problem));
    this.name = "ApiProblem";
    this.status = problem.status;
    this.code = problem.code;
    this.traceId = problem.trace_id;
    this.fieldErrors = problem.errors ?? [];
    this.problem = problem;
    this.retryAfterS = problem.retry_after_s;
  }
}

/** Header Retry-After: số giây hoặc HTTP-date (RFC 9110 §10.2.3). Sai dạng → undefined. */
export function parseRetryAfter(value: string | null | undefined, now: number = Date.now()): number | undefined {
  if (!value) return undefined;
  const v = value.trim();
  if (/^\d+$/.test(v)) return Number(v);
  const t = Date.parse(v);
  if (!Number.isFinite(t)) return undefined;
  return Math.max(0, Math.ceil((t - now) / 1000));
}

/** Thông báo cho người dùng: ưu tiên bảng mã; mã lạ thì dùng detail/title của server. */
const PREFER_DETAIL: ReadonlySet<string> = new Set(["party.out_of_scope", "party.import_invalid"]);

export function problemMessage(p: Pick<ProblemDetails, "code" | "status" | "detail" | "title">): string {
  // Thông báo nghiệp vụ của 422 (vd "Vui lòng nhập đầy đủ Tên tuyến và Phân loại." — docs/errors.md) nằm ở `detail`.
  if (p.code === "validation_failed" && p.detail) return p.detail;
  // Thông điệp AC nằm ở detail: "Bạn không có quyền truy cập nông hộ này." (HTX US-HH-01 AC-3), "Sai định dạng file: …" (US-HTX-02).
  if (PREFER_DETAIL.has(p.code) && p.detail) return p.detail;
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
  const retryAfter =
    isRecord(body) && typeof body.retry_after_s === "number" ? body.retry_after_s : parseRetryAfter(response?.headers?.get("Retry-After"));
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
      retry_after_s: retryAfter,
    };
  }
  const code = STATUS_FALLBACK[status] ?? (status >= 500 ? "internal_error" : "bad_request");
  return {
    type: `https://errors.agrigreen.vn/${code}`,
    title: "",
    status,
    code,
    trace_id: headerTrace,
    retry_after_s: retryAfter,
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
