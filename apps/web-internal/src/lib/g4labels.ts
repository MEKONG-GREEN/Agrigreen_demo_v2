// Nhãn tiếng Việt cho trạng thái, cờ cảnh báo và mã tham số của bốn module G4
// (mkg/logistics, mkg/orders, mkg/costing, mkg/acctbridge — docs/dev/g4-conventions.md, docs/errors.md).

// ---------------------------------------------------------------------------
// Vận tải (logistics)
// ---------------------------------------------------------------------------
export const TRIP_STATUS_LABEL: Record<string, string> = {
  planned: "Kế hoạch",
  departed: "Đã xuất phát",
  arrived: "Đã đến",
  completed: "Hoàn tất",
  cancelled: "Đã huỷ",
};

/**
 * Trạng thái chuyến CHỈ TIẾN (L-02, như logistics/domain.CanTransition): mốc departed/arrived từng bậc;
 * hoàn tất khi đã xuất phát (phiếu cân có thể về trước mốc đến); huỷ từ planned/departed; completed/cancelled là cuối.
 */
export const TRIP_FLOW = ["planned", "departed", "arrived", "completed"] as const;

export function canTripAdvance(from: string, to: string): boolean {
  if (from === "completed" || from === "cancelled") return false;
  const i = TRIP_FLOW.indexOf(from as (typeof TRIP_FLOW)[number]);
  if (i < 0) return false;
  if (to === "cancelled") return from === "planned" || from === "departed";
  if (to === "completed") return i >= 1;
  const j = TRIP_FLOW.indexOf(to as (typeof TRIP_FLOW)[number]);
  return j === i + 1;
}

/** Mốc kế tiếp tài xế ghi được (departed / arrived) hoặc null. */
export function nextMilestone(status: string): "departed" | "arrived" | null {
  if (status === "planned") return "departed";
  if (status === "departed") return "arrived";
  return null;
}

export const MODE_LABEL: Record<string, string> = { road: "Đường bộ", waterway: "Đường thuỷ" };

export const ROUTE_SOURCE_LABEL: Record<string, string> = {
  routed: "Định tuyến mạng đường thuỷ",
  digitised: "Tuyến số hoá",
  haversine: "Haversine × hệ số #10 (dự phòng)",
  manual: "Nhập tay có lý do",
};

export const COST_SOURCE_LABEL: Record<string, string> = {
  param_rate: "Đơn giá tham số (#22/#23)",
  vessel_rate: "Đơn giá thuê ghe (GH-03)",
  manual: "Nhập tay có chứng từ",
};

export const ENDPOINT_KIND_LABEL: Record<string, string> = {
  plot: "Thửa",
  collection_point: "Điểm tập kết",
  facility: "Cơ sở (hub/kho)",
  customer_site: "Nhà máy khách",
  point: "Điểm toạ độ",
};

/** Cờ cảnh báo KHÔNG CHẶN của chuyến / chặng (docs/errors.md mkg/logistics). */
export const TRIP_FLAG_LABEL: Record<string, string> = {
  no_vehicle: "Chưa gán phương tiện",
  no_route: "Chưa xác định đích",
  vessel_unregistered: "Ghe ngoài danh mục",
  vessel_inactive: "Ghe đã ngừng",
  registration_expired: "Hết hạn đăng kiểm",
  registration_expiring: "Sắp hết hạn đăng kiểm",
  payload_exceeded: "Tấn ước > 1,1 × tải thực chở",
  weigh_variance: "Lệch cân > 5 % so với tấn ước",
  routing_unavailable: "Nền tảng định tuyến lỗi — dùng Haversine",
  no_waterway_route: "Không có đường thuỷ — dùng Haversine",
  waterway_suggested: "Gợi ý đường thuỷ (xa hơn #27, có kênh)",
  rolls_corrected: "Số cuộn đã bù",
};

export function flagLabel(f: string): string {
  return TRIP_FLAG_LABEL[f] ?? f;
}

export const TRIP_EVENT_LABEL: Record<string, string> = {
  planned: "Lập",
  revised: "Lập lại chặng",
  vehicle_assigned: "Gán phương tiện",
  departed: "Xuất phát",
  arrived: "Đến nơi",
  completed: "Hoàn tất",
  cancelled: "Huỷ",
};

export const VESSEL_KIND_LABEL: Record<string, string> = { ghe: "Ghe", sa_lan: "Sà lan", truck: "Xe tải" };
export const REGISTRATION_LABEL: Record<string, string> = {
  unknown: "Chưa rõ hạn",
  valid: "Còn hạn",
  expiring: "Sắp hết hạn (≤ 30 ngày)",
  expired: "Hết hạn",
};
export const RATE_TYPE_LABEL: Record<string, string> = {
  "": "Không có (dùng đơn giá tham số)",
  per_ton: "Theo tấn",
  per_trip: "Theo chuyến",
  per_ton_km: "Theo tấn·km",
};

/** Chuẩn hoá số hiệu ghe (proto GH-01): bỏ khoảng trắng, chữ hoa. */
export function normalizeVesselCode(code: string): string {
  return code.replace(/\s+/g, "").toUpperCase();
}

export const LOGISTICS_PARAM_LABEL: Record<string, string> = {
  "logistics.rate.road_vnd_per_t_km": "Đơn giá đường bộ (đ/t·km, #22)",
  "logistics.rate.waterway_vnd_per_t_km": "Đơn giá đường thuỷ (đ/t·km, #23)",
  "logistics.mode.road_feasible_km": "Ngưỡng chọn đường thuỷ (km, #27)",
  "logistics.vessel.straw_payload_tons": "Tải rơm thực chở của ghe (t, #51)",
  "logistics.vessel.payload_warn_factor": "Hệ số cảnh báo quá tải",
  "logistics.vessel.expiry_warn_days": "Cảnh báo sắp hết hạn đăng kiểm (ngày)",
  "logistics.trip.unweighed_alert_hours": "Cảnh báo ghe chưa cân (giờ)",
  "logistics.route.road_haversine_factor": "Hệ số Haversine đường bộ (#10)",
};

// ---------------------------------------------------------------------------
// Đơn hàng (orders)
// ---------------------------------------------------------------------------
export const CONTRACT_STATUS_LABEL: Record<string, string> = {
  pending: "Chờ duyệt",
  active: "Hiệu lực",
  rejected: "Từ chối",
  superseded: "Bị thay",
  terminated: "Đã chấm dứt",
};
export const BASIS_LABEL: Record<string, string> = { per_tonne: "Theo tấn (nhà máy cân)", per_bale: "Theo cuộn" };
export const PO_STATUS_LABEL: Record<string, string> = { draft: "Nháp", approved: "Đã duyệt", rejected: "Từ chối", cancelled: "Đã huỷ" };
export const RECEIPT_STATUS_LABEL: Record<string, string> = {
  awaiting_weigh: "Chờ phiếu cân HUB_IN",
  pending_approval: "Chờ duyệt",
  approved: "Đã duyệt",
  rejected: "Từ chối",
};
export const QTY_SOURCE_LABEL: Record<string, string> = { ticket: "Phiếu cân HUB_IN", count: "Số cuộn đếm", entered: "Nhập tay" };
export const SO_STATUS_LABEL: Record<string, string> = {
  draft: "Nháp",
  confirmed: "Đã xác nhận",
  delivering: "Đang giao",
  delivered: "Đã giao",
  completed: "Hoàn tất",
  cancelled: "Đã huỷ",
};
export const SO_FLOW = ["draft", "confirmed", "delivering", "delivered", "completed"] as const;

/** Đơn bán chỉ tiến (proto advanceSalesOrder); huỷ chỉ từ draft/confirmed — như orders/domain.CanAdvance. */
export function canSalesAdvance(from: string, to: string): boolean {
  if (to === "cancelled") return from === "draft" || from === "confirmed";
  const i = SO_FLOW.indexOf(from as (typeof SO_FLOW)[number]);
  const j = SO_FLOW.indexOf(to as (typeof SO_FLOW)[number]);
  return i >= 0 && j > i;
}

export const DELIVERY_STATUS_LABEL: Record<string, string> = {
  planned: "Đã lên lịch",
  dispatched: "Đã xuất giao",
  accepted: "Đã chấp nhận",
  rejected: "Khách từ chối",
  cancelled: "Đã huỷ",
};
export const EVIDENCE_KIND_LABEL: Record<string, string> = { epod: "ePOD", minutes: "Biên bản giao nhận", ebill: "e-bill" };
export const PO_CATEGORY_LABEL: Record<string, string> = { straw: "Rơm (HTX)", other: "Hàng khác" };
export const ORDERS_PARAM_LABEL: Record<string, string> = {
  "orders.receipt.po_variance_warn_pct": "Ngưỡng lệch phiếu nhận ↔ đơn mua (%)",
  "orders.sales.price_approval_delta_pct": "Ngưỡng lệch giá bảng cần duyệt (%)",
  "orders.sales.discount_approval_pct": "Ngưỡng chiết khấu cần duyệt (%)",
  "orders.payment_term_days": "Thời hạn thanh toán (ngày)",
  "orders.sales.stock_shortage_blocking": "Chặn khi thiếu tồn (0 = chỉ cảnh báo)",
};

// ---------------------------------------------------------------------------
// Giá thành (costing)
// ---------------------------------------------------------------------------
export const PERIOD_STATUS_LABEL: Record<string, string> = {
  open: "Đang mở",
  closing: "Đang đóng",
  closed: "Đã đóng",
  posted: "Đã ghi sổ kế toán",
};
export const CLOSE_JOB_STATUS_LABEL: Record<string, string> = { queued: "Đang xử lý", completed: "Hoàn tất", failed: "Lỗi" };

export const COST_CATEGORY_LABEL: Record<string, string> = {
  straw_purchase: "Mua rơm (quyết toán HTX)",
  collection_labour: "Nhân công gom",
  consumables: "Vật tư",
  transport_road: "Vận chuyển đường bộ",
  transport_waterway: "Vận chuyển đường thuỷ",
  hub_processing: "Sấy / nén ở hub",
  storage: "Lưu kho",
  handling: "Bốc xếp",
  loss: "Hao hụt",
  sales_revenue: "Doanh thu bán hàng",
  carbon_revenue: "Doanh thu carbon",
  carbon_share: "Chia doanh thu carbon",
};
/** Nhóm chi phí được ghi tay (POST /v1/costing/entries). */
export const MANUAL_CATEGORIES = ["collection_labour", "consumables", "transport_road", "transport_waterway", "hub_processing", "storage", "handling", "loss"] as const;

export const DIMENSION_LABEL: Record<string, string> = { facility: "Cơ sở", htx: "HTX", customer: "Khách", mode: "Phương thức", none: "Không" };
export const ALLOCATION_KIND_LABEL: Record<string, string> = {
  hub_processing: "Chi phí hub theo khối lượng",
  trip_cost: "Cước chuyến theo khối lượng lô",
  delivery_revenue: "Doanh thu giao nhận",
  loss_attribution: "Quy kết hao hụt theo tỷ lệ nguồn",
};
export const SETTLEMENT_STATUS_LABEL: Record<string, string> = {
  awaiting_weighing: "Chờ cân đủ ghe",
  pending_confirmation: "Chờ duyệt",
  approved: "Đã duyệt — phải trả",
  paid: "Đã trả",
  cancelled: "Đã huỷ",
};
export const BLOCKED_REASON_LABEL: Record<string, string> = {
  no_contract: "Không có hợp đồng — cần nhập giá thoả thuận (PM-03)",
  awaiting_weighing: "Chờ cân đủ mọi ghe (PM-02)",
  no_rolls: "Chưa có số cuộn",
};
export const COSTING_PARAM_LABEL: Record<string, string> = {
  "costing.allocation.key": "Khoá phân bổ chi phí",
  "costing.moisture.base_pct": "Quy đổi ẩm độ (C-4)",
  "costing.loss.expected_pct": "Hao hụt dự kiến (%, #19)",
  "costing.loss.hub_processing_pct": "Hao hụt sơ chế hub (%, #56)",
  "costing.rate.hub_processing_vnd_per_t": "Đơn giá sơ chế hub (đ/t, #55)",
  "costing.settlement.payment_term_days": "Hạn thanh toán quyết toán (ngày)",
  "costing.carbon.retained_pct": "Carbon — MKG giữ lại (%)",
  "costing.carbon.split.farmers_pct": "Carbon — nông hộ/HTX (%)",
  "costing.carbon.split.authority_pct": "Carbon — cơ quan (%)",
  "costing.carbon.split.verifier_pct": "Carbon — VVB (%, nhận phần dư)",
};

// ---------------------------------------------------------------------------
// Cầu nối kế toán (acctbridge)
// ---------------------------------------------------------------------------
export const JOURNAL_STATUS_LABEL: Record<string, string> = {
  built: "Đã dựng — chờ duyệt",
  approved: "Đã duyệt — đang đẩy",
  pushed: "Đã đẩy sang kế toán",
  push_failed: "Đẩy lỗi (DLQ)",
  superseded: "Bị thay",
};
export const AB_JOB_STATUS_LABEL: Record<string, string> = { queued: "Đang xử lý", done: "Xong", failed: "Lỗi" };
export const SIDE_LABEL: Record<string, string> = { debit: "Nợ", credit: "Có" };
export const DIMENSION_ON_LABEL: Record<string, string> = { none: "Không ghi chiều", debit: "Chiều ở bên Nợ", credit: "Chiều ở bên Có" };
export const PUSH_OUTCOME_LABEL: Record<string, string> = { succeeded: "Thành công", unavailable: "Đích tạm không nhận — hẹn thử lại", rejected: "Đích từ chối — DLQ" };

/** Mã kết quả của job / lần đẩy (không phải lỗi HTTP — docs/errors.md mkg/acctbridge). */
export const AB_FAILURE_LABEL: Record<string, string> = {
  "acctbridge.mapping_missing": "Chưa có bảng ánh xạ được duyệt hoặc thiếu quy tắc cho một nhóm × chiều",
  "acctbridge.period_not_closed": "Kỳ giá thành chưa đóng",
  "acctbridge.source_mismatch": "Băm kết quả kỳ khác sự kiện đóng kỳ",
  "acctbridge.empty_period": "Kỳ không có số phát sinh",
  "acctbridge.journal_pushed_immutable": "Kỳ đã đẩy — không dựng lại",
  "acctbridge.journal_not_pushable": "Bút toán bị thay trước khi đẩy",
  "acctbridge.adapter_unavailable": "Phần mềm kế toán tạm không nhận — thử lại giãn cách",
  "acctbridge.adapter_rejected": "Phần mềm kế toán từ chối — vào DLQ",
  "acctbridge.retry_exhausted": "Hết số lần thử — vào DLQ",
};
