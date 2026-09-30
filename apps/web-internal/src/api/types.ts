// Tên gọn cho kiểu SINH TỰ ĐỘNG từ contracts/openapi/mkg-internal.yaml (không viết tay).
// Với route mà hợp đồng chưa khai lược đồ phản hồi, interface bên dưới mô tả ĐÚNG DTO của backend
// (internal/mkg/*/handler/dto.go) — xem ghi chú "khoảng trống hợp đồng" trong README.
import type { components, paths } from "./generated/mkg-internal";

type S = components["schemas"];

/** Body 200 của một GET. */
export type Get<P extends keyof paths> = paths[P] extends {
  get: { responses: { 200: { content: { "application/json": infer R } } } };
}
  ? R
  : never;

/** Body JSON thành công (201/200) của một POST. */
type PostOk<R> = R extends { 201: { content: { "application/json": infer X } } }
  ? X
  : R extends { 200: { content: { "application/json": infer X } } }
    ? X
    : never;
export type Post<P extends keyof paths> = paths[P] extends { post: { responses: infer R } } ? PostOk<R> : never;

/** Khối lượng kg dạng chuỗi thập phân tối đa 3 chữ số (R-10). */
export type Kg = S["Kg"];
export type Pct = S["Pct"];
export type Product = S["Product"];

// mkg/hub
export type Facility = S["Facility"];
/** Phiên bản cơ sở — backend trả kèm thông số của phiên bản (specDTO) và thời điểm. */
export type FacilityVersion = S["FacilityVersion"] &
  Partial<Pick<S["Facility"], "code" | "name" | "kind" | "capacity_kg" | "status" | "province_code" | "effective_from" | "location">> & {
    event_time?: string;
    recorded_at?: string;
  };
export type FacilitySpecInput = S["FacilitySpecInput"];
export type Lot = S["Lot"];
export type Movement = S["Movement"];
export type RankedLot = S["RankedLot"];
export type Allocation = S["Allocation"];
export type Capacity = S["Capacity"];
export type ValveExport = S["ValveExport"];
export type Crew = S["Crew"];
export type CrewSuggestion = S["CrewSuggestion"];
export type CrewSuggestions = Get<"/v1/fieldops/orders/{id}/crew-suggestions">;
export type Stocktake = S["Stocktake"];
export interface TransferLot {
  source_lot_id?: string | null;
  lot_id: string;
  mass_kg: Kg;
}
export type Transfer = Omit<S["Transfer"], "source_lots" | "received_lots"> & { source_lots?: TransferLot[]; received_lots?: TransferLot[] };
/** Phiên bản tham số: `params`/`values` là đối tượng tự do trong hợp đồng — đọc dạng bản ghi. */
export type ParamVersion = Omit<S["ParamVersion"], "params" | "values"> & { params?: Record<string, unknown>; values?: Record<string, unknown> };
export type HubParams = Omit<Get<"/v1/hub/params/{set}">, "active" | "items"> & { active?: Record<string, unknown>; items?: ParamVersion[] };
export type HubInventory = Get<"/v1/hub/facilities/{id}/inventory">;
export type HubLotDetail = Get<"/v1/hub/lots/{id}">;
export type HubLosses = Get<"/v1/hub/facilities/{id}/losses">;
export type HubLotMeasurements = Get<"/v1/hub/lots/{id}/measurements">;
export type DispatchSuggestion = Get<"/v1/hub/facilities/{id}/dispatch-suggestion">;
export type ReceiptResult = Post<"/v1/hub/facilities/{id}/receipts">;
export type FacilityDecisionResult = Post<"/v1/hub/facilities/{id}/versions/{version}/decision">;

/** Phiếu xuất (POST /dispatches — hợp đồng chưa khai lược đồ, DTO backend). */
export interface DispatchResult {
  dispatch: {
    id: string;
    facility_id: string;
    mass_kg: Kg;
    destination_ref: string | null;
    strategy: string;
    risk_rule: string;
    allocations: Allocation[];
    event_time: string;
  };
  movements: Movement[];
}

// mkg/measurement
export type ScaleDevice = S["ScaleDevice"] & {
  proposed_at?: string;
  decided_at?: string | null;
  revoke_reason?: string | null;
  health?: S["ScaleDevice"]["health"] & { updated_at?: string | null };
};
export type WeighingSession = S["WeighingSession"] & { facility_ref?: string; opened_at?: string; closed_at?: string | null };
export interface TicketWindow {
  device_id: string;
  epoch: number;
  counter_from: number;
  counter_to: number;
  readings_used?: number;
  readings_total?: number;
  batch_ids?: string[];
  readings_sha256?: string | null;
}
export type WeighTicket = Omit<S["WeighTicket"], "window"> & {
  window?: TicketWindow | null;
  customer_ref?: string | null;
  reason?: string | null;
  correction_reason?: string | null;
  event_time?: string;
  recorded_at?: string;
  seq?: number;
  session_ref?: string | null;
};
export type Reconciliation = Omit<S["Reconciliation"], "internal" | "commercial" | "roll_check"> & {
  hub_in_kg?: Kg | null;
  hub_out_kg?: Kg | null;
  customer_in_kg?: Kg | null;
  hub_in_moisture_pct?: Pct | null;
  rule_version?: string;
  event_time?: string;
  recorded_at?: string;
  internal?: ReconInternal;
  commercial?: ReconCommercial;
  roll_check?: ReconRollCheck;
};
export interface ReconInternal {
  status: "normal" | "abnormal" | "gain" | "not_applicable";
  loss_kg: Kg | null;
  loss_norm_pct: Pct | null;
  expected_loss_kg: Kg | null;
  abnormal_loss_kg: Kg | null;
}
export interface ReconCommercial {
  status: "within" | "flag" | "critical" | "not_applicable";
  diff_kg: Kg | null;
  diff_pct: Pct | null;
}
export interface ReconRollCheck {
  status: "pending_rolls" | "within" | "flag" | "critical" | "informational";
  rolls: number | null;
  kg_per_roll_estimate: Kg | null;
  estimate_kg: Kg | null;
  variance_pct: string | null;
}

/** Điểm cân hiệu lực của lô (GET /lots/{id}/tickets → mass). */
export interface MassPoint {
  mass_kg: Kg;
  ticket_id: string;
  event_time: string;
}
export interface LotMass {
  lot_ref: string;
  route: "HUB" | "DIRECT";
  hub_in: MassPoint | null;
  hub_out: MassPoint | null;
  customer_in: MassPoint | null;
  as_of: string;
}
export interface LotTickets {
  mass: LotMass;
  tickets: WeighTicket[];
}
export interface ManualTicketResult {
  ticket: WeighTicket;
  reconciliation: Reconciliation | null;
}
export interface ReconciliationHistory {
  latest?: Reconciliation;
  items: Reconciliation[];
}
export interface LotYield {
  lot_ref: string;
  route: string;
  basis_point: string;
  basis_kg: Kg;
  rolls: number | null;
  yield_kg_per_roll: Kg | null;
  aggregated: boolean;
  reconciliation_id: string | null;
}

export type DisputeStatus = "open" | "under_review" | "resolved" | "cancelled";
export interface DisputeEvent {
  seq: number;
  action: "opened" | "add_evidence" | "start_review" | "resolve" | "cancel";
  status_after: DisputeStatus;
  evidence_refs: string[] | null;
  resolution: string | null;
  agreed_kg: Kg | null;
  note: string | null;
  event_time: string;
  actor_ref: string;
}
export interface Dispute {
  id: string;
  lot_ref: string;
  status: DisputeStatus;
  severity: "flag" | "critical";
  reconciliation_id: string;
  hub_out_kg: Kg | null;
  customer_in_kg: Kg | null;
  diff_kg: Kg;
  diff_pct: Pct | null;
  evidence_refs: string[];
  resolution: "customer_weight" | "mkg_weight" | "agreed_weight" | null;
  agreed_kg: Kg | null;
  opened_at: string;
  opened_by: string;
  events?: DisputeEvent[];
}
export interface DisputePage {
  items: Dispute[];
  next_cursor: string | null;
}

export interface CustomerImport {
  id: string;
  channel: "file" | "api" | "manual";
  customer_ref: string | null;
  evidence_ref: string | null;
  file_sha256: string | null;
  rows: number;
  ticket_ids: string[];
  provisional: boolean;
  reason: string | null;
  recorded_at: string;
}
export interface CustomerImportResult {
  import: CustomerImport;
  reconciliations: Reconciliation[];
}

/** Phiên bản dung sai / định mức (GET tolerance-versions, loss-norm-versions). */
export type MeasurementParam = Omit<ParamVersion, "values"> & { values?: ToleranceValues | LossNormValues };
export interface ToleranceValues {
  commercial_flag_pct: Pct;
  commercial_critical_pct: Pct;
  roll_flag_pct: Pct;
  roll_critical_pct: Pct;
}
export interface LossBand {
  moisture_from: Pct;
  moisture_to: Pct;
  loss_pct: Pct;
}
export interface LossNormValues {
  bands: LossBand[];
  default_loss_pct: Pct;
  abnormal_alert_pct: Pct;
}
export interface ToleranceList {
  items: MeasurementParam[];
  provisional_default?: { label: string; note: string; commercial_flag_pct: Pct; commercial_critical_pct: Pct };
}

// mkg/fieldops
export type CollectionOrder = S["CollectionOrder"];
export type CollectionPlanInput = S["CollectionPlanInput"];
export type CollectionOrderResult = S["CollectionOrderResult"];
export type OrderStatus = NonNullable<CollectionOrder["status"]>;
export type RollCount = S["RollCount"] & { actor_ref?: string; htx_confirmation_ref?: string | null };
export type RollCountInput = S["RollCountInput"];
export type RollCountResult = S["RollCountResult"];
export type BuySideReconciliation = S["BuySideReconciliation"];
export type Stage = NonNullable<RollCount["stage"]>;
export interface OrderEvent {
  id: string;
  seq: number;
  kind: "planned" | "revised" | "approved" | "rejected" | "assigned" | "closed" | "cancelled";
  revision: number;
  crew_ref: string | null;
  reason: string | null;
  actor_ref: string;
  event_time: string;
  recorded_at: string;
}
export interface OrderDetail {
  order: CollectionOrder;
  history: OrderEvent[];
}
export interface StageTotal {
  plot_ref: string;
  stage: Stage;
  rolls: number;
  loose_kg: Kg | null;
  roll_count_id: string;
  discrepancy_reason: string | null;
  event_time: string;
}
export interface RollCountBook {
  counts: RollCount[];
  totals: StageTotal[];
  order_totals: Record<string, number>;
}
export interface HtxConfirmation {
  id: string;
  seq: number;
  order_id: string;
  plot_ref: string;
  htx_ref: string;
  statement_ref: string;
  statement_revision: number;
  roll_count_id: string;
  mkg_rolls: number;
  outcome: "confirmed" | "disputed";
  htx_rolls: number;
  reason: string | null;
  channel: string;
  evidence_ref: string | null;
  actor_ref: string;
  event_time: string;
  recorded_at: string;
}
export interface CrewShift {
  id: string;
  crew_ref: string;
  order_id: string;
  kind: "opened" | "closed";
  headcount: number | null;
  equipment_refs: string[];
  actor_ref: string;
  device_id: string | null;
  event_time: string;
  recorded_at: string;
}
export type BuySidePage = Get<"/v1/fieldops/reconciliation">;
export type KgPerRoll = Get<"/v1/fieldops/htx/{id}/kg-per-roll">;

// ---------------------------------------------------------------------------
// G4: mkg/logistics, mkg/orders, mkg/costing, mkg/acctbridge — kiểu sinh từ hợp đồng
// ---------------------------------------------------------------------------
export type LogisticsTrip = S["LogisticsTrip"];
export type LogisticsLeg = S["LogisticsLeg"];
export type LogisticsTripEvent = S["LogisticsTripEvent"];
export type LogisticsVessel = S["LogisticsVessel"];
export type LogisticsVesselInput = S["LogisticsVesselInput"];
export type LogisticsParam = S["LogisticsParam"];
export type LogisticsEndpointInput = S["LogisticsEndpointInput"];
export type LogisticsParamView = Get<"/v1/logistics/params/{key}">;
export type LogisticsParamKey = paths["/v1/logistics/params/{key}"]["parameters"]["path"]["key"];
export type LogisticsAlerts = Get<"/v1/logistics/alerts">;
export type RouteSnapshot = Get<"/v1/logistics/trips/{id}/legs/{leg}/route-snapshot">;
export type TripResult = Post<"/v1/logistics/trips">;

export type OrdersContract = S["OrdersContract"];
export type OrdersContractInput = S["OrdersContractInput"];
export type OrdersContractVersions = S["OrdersContractVersions"];
export type OrdersPurchaseOrder = S["OrdersPurchaseOrder"];
export type OrdersReceipt = S["OrdersReceipt"];
export type OrdersPayable = S["OrdersPayable"];
export type OrdersSalesOrder = S["OrdersSalesOrder"];
export type OrdersDelivery = S["OrdersDelivery"];
export type OrdersParam = S["OrdersParam"];
export type OrdersParamView = Get<"/v1/orders/params/{key}">;
export type OrdersParamKey = paths["/v1/orders/params/{key}"]["parameters"]["path"]["key"];

export type CostCategory = S["CostCategory"];
export type CostEntry = S["CostEntry"];
export type CostingAllocation = S["CostingAllocation"];
export type CostingCloseJob = S["CostingCloseJob"];
export type CostingLotCost = S["CostingLotCost"];
export type CostingParam = S["CostingParam"];
export type CostingParamVersion = S["CostingParamVersion"];
export type CostingPeriod = S["CostingPeriod"];
export type CostingSettlement = S["CostingSettlement"];
export type CarbonRevenueResult = Post<"/v1/costing/carbon-revenue">;
export type CostingConversion = Get<"/v1/costing/collections/{id}/conversion">;

export type AcctbridgeJournal = S["AcctbridgeJournal"];
export type AcctbridgeJournalLine = S["AcctbridgeJournalLine"];
export type AcctbridgeJob = S["AcctbridgeJob"];
export type AcctbridgeMapping = S["AcctbridgeMapping"];
export type AcctbridgeMappingRule = S["AcctbridgeMappingRule"];
export type AcctbridgePushAttempt = S["AcctbridgePushAttempt"];
export type AcctbridgePushJob = S["AcctbridgePushJob"];
export type AcctbridgeJournalDetail = Get<"/v1/acctbridge/journals/{id}">;
export type AcctbridgeMappings = Get<"/v1/acctbridge/mappings">;

// G4 — danh sách có con trỏ, tệp chứng từ, thông báo hàng đến
export type MkgFile = S["MkgFile"];
export type MkgFilePurpose = MkgFile["purpose"];
export type HubArrival = S["HubArrival"];
export type LogisticsTripDocument = S["LogisticsTripDocument"];
export type LogisticsVesselVersion = S["LogisticsVesselVersion"];
export type CostingCarbonRevenue = S["CostingCarbonRevenue"];
