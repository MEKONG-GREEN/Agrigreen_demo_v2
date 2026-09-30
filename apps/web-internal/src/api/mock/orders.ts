// Backend giả lập mkg/orders — theo contracts/openapi/mkg-internal.yaml (tag orders) và orders/domain:
// proto CT-01..03 (hợp đồng mua rơm có phiên bản, chồng lấn → 409), createPurchaseOrder, createGoodsReceipt (lệch > 3 % →
// cần biên bản, KHÔNG chặn), đơn bán chỉ tiến + ngưỡng giá/chiết khấu (R-56), giao nhận theo phiếu CUSTOMER_IN.
// Tiền tính bằng BigInt (src/lib/money.ts) như shared/money của backend.
import type { OrdersContract, OrdersDelivery, OrdersPayable, OrdersPurchaseOrder, OrdersReceipt, OrdersSalesOrder } from "../types";
import { grams, kgOf, pctDiff, absPctGreater } from "@/lib/kg";
import { amountForCount, amountForKg, isPrice, netPrice, percentHundredths, priceDeltaPct, priceOf, priceUnits } from "@/lib/money";
import { HTX, L, newId, sid } from "./db";
import { fileRoutes, findFile } from "./files";
import { decimalCheck, paramStore, reasonOf, todayVn } from "./g4common";
import { created, daysAgo, forbidden, hasRole, json, nowIso, pageSlice, problem, route, selfApproval, str, validation, UUID_RE, type Route } from "./http";
import { ACTORS } from "./personas";

const OFFICER = "sales_officer";
const APPROVER = "sales_approver";

export const OR = {
  contractTanHiep: sid(0x25, 1),
  contractVinhBinh: sid(0x25, 2),
  contractPending: sid(0x25, 3),
  poStraw: sid(0x26, 1),
  poOther: sid(0x26, 2),
  poBale: sid(0x26, 3),
  receiptVariance: sid(0x27, 1),
  soDelivering: sid(0x28, 1),
  soDraftDiscount: sid(0x28, 2),
  soDraftSelf: sid(0x28, 3),
  deliveryWithTicket: sid(0x2a, 1),
  deliveryPlanned: sid(0x2a, 2),
  customerPlant: sid(0x29, 20),
  customerPaper: sid(0x29, 21),
  supplierTwine: sid(0x29, 30),
} as const;

export const params = paramStore({
  prefix: "/v1/orders",
  module: "orders",
  shape: "orders",
  proposerRoles: [OFFICER, APPROVER],
  approverRoles: [APPROVER],
  readRoles: [OFFICER, APPROVER],
  defaults: {
    "orders.receipt.po_variance_warn_pct": { value: "3", note: "proto RECEIPT_VARIANCE_TOLERANCE_PCT — tạm, chờ khách", check: decimalCheck(2, 100) },
    "orders.sales.price_approval_delta_pct": { value: "0", note: "chờ khách (không có trong prototype) — mặc định 0: mọi lệch giá bảng cần sales_approver", check: decimalCheck(2, 100) },
    "orders.sales.discount_approval_pct": { value: "0", note: "chờ khách (không có trong prototype) — mặc định 0: mọi chiết khấu cần sales_approver", check: decimalCheck(2, 100) },
    "orders.payment_term_days": { value: "15", note: "proto PAYMENT_TERM_DAYS — tạm, chờ khách", check: decimalCheck(0, 365, true) },
    "orders.sales.stock_shortage_blocking": { value: "0", note: "0 = thiếu tồn chỉ cảnh báo (proto stockWarning)", check: decimalCheck(0, 1, true) },
  },
});

// ---------------------------------------------------------------------------
// Phiếu cân từ measurement (consumer mkg.orders.weigh_ticket) — giả lập: HUB_IN / CUSTOMER_IN theo lô
// ---------------------------------------------------------------------------
export const HUB_IN: Record<string, { ticket: string; kg: string }> = {
  [L.proto]: { ticket: sid(0x0b, 1), kg: "96800.000" },
  [L.ct]: { ticket: sid(0x0b, 2), kg: "45000.000" },
  [L.perBale]: { ticket: sid(0x0b, 3), kg: "50000.000" },
};
export const CUSTOMER_IN: Record<string, { ticket: string; kg: string }> = {
  [L.compressed]: { ticket: sid(0x0b, 90), kg: "14800.000" },
  [L.dried]: { ticket: sid(0x0b, 91), kg: "30120.000" },
};

// ---------------------------------------------------------------------------
// Hợp đồng mua rơm (CT-01..03)
// ---------------------------------------------------------------------------
const contracts: { contract_id: string; versions: OrdersContract[] }[] = [];
let contractSeq = 0;

function contract(id: string, htx: string, basis: "per_tonne" | "per_bale", price: string, committed: string, from: number, to: number, status: OrdersContract["status"], approved: boolean) {
  contractSeq += 1;
  contracts.push({
    contract_id: id,
    versions: [
      {
        contract_id: id,
        version: 1,
        status,
        code: `HDR${String(contractSeq).padStart(5, "0")}`,
        htx_ref: htx,
        basis,
        unit_price: price,
        committed_kg: committed,
        max_moisture_pct: "20.00",
        valid_from: todayVn(from),
        valid_to: todayVn(to),
        note: null,
        proposed_by: ACTORS.kinhDoanh,
        approved_by: approved ? ACTORS.truongKinhDoanh : null,
        reason: null,
        event_time: daysAgo(-from),
      },
    ],
  });
}
contract(OR.contractTanHiep, HTX.tanHiep, "per_tonne", "650000.0000", "500000.000", -90, 90, "active", true);
contract(OR.contractVinhBinh, HTX.vinhBinh, "per_bale", "15000.0000", "300000.000", -60, 120, "active", true);
contract(OR.contractPending, HTX.giongRieng, "per_tonne", "620000.0000", "250000.000", 0, 180, "pending", false);

function contractOr404(id: string) {
  return contracts.find((c) => c.contract_id === id) ?? problem(404, "orders.contract_not_found", "Contract not found");
}

/** Phiên bản hiệu lực của HTX tại ngày (Contracts.ContractFor). */
export function contractFor(htx: string, on: string): OrdersContract | undefined {
  for (const c of contracts) {
    const v = c.versions.find((x) => x.status === "active" && x.htx_ref === htx && x.valid_from! <= on && x.valid_to! >= on);
    if (v) return v;
  }
  return undefined;
}

function overlaps(htx: string, from: string, to: string, except?: string): boolean {
  return contracts.some((c) => c.contract_id !== except && c.versions.some((v) => v.status === "active" && v.htx_ref === htx && v.valid_from! <= to && from <= v.valid_to!));
}

function contractInput(body: Record<string, unknown>, revising: boolean): Response | Omit<OrdersContract, "contract_id" | "version" | "status" | "code" | "proposed_by" | "approved_by" | "event_time"> {
  const errs: { field: string; reason: string }[] = [];
  const htx = str(body, "htx_ref");
  if (!revising && !UUID_RE.test(htx)) errs.push({ field: "htx_ref", reason: "UUID HTX" });
  const basis = str(body, "basis");
  if (basis !== "per_tonne" && basis !== "per_bale") errs.push({ field: "basis", reason: "per_tonne | per_bale (CT-03)" });
  const price = str(body, "unit_price");
  if (!isPrice(price)) errs.push({ field: "unit_price", reason: "Giá > 0, tối đa 4 số lẻ (CT-02)" });
  const kg = str(body, "committed_kg");
  if (!/^\d+(\.\d{1,3})?$/.test(kg) || grams(kg) <= 0n) errs.push({ field: "committed_kg", reason: "Khối lượng cam kết > 0 (CT-02)" });
  const from = str(body, "valid_from");
  const to = str(body, "valid_to");
  if (!/^\d{4}-\d{2}-\d{2}$/.test(from)) errs.push({ field: "valid_from", reason: "YYYY-MM-DD" });
  if (!/^\d{4}-\d{2}-\d{2}$/.test(to) || to < from) errs.push({ field: "valid_to", reason: "Kết thúc ≥ bắt đầu (CT-02)" });
  const moist = str(body, "max_moisture_pct");
  if (moist && !/^\d{1,3}(\.\d{1,2})?$/.test(moist)) errs.push({ field: "max_moisture_pct", reason: "Phần trăm, 2 số lẻ" });
  const reason = str(body, "reason");
  if (revising && (reason.length < 10 || reason.length > 500)) errs.push({ field: "reason", reason: "Sửa hợp đồng cần lý do 10–500 ký tự" });
  if (errs.length) return validation(errs);
  return { htx_ref: htx, basis: basis as "per_tonne", unit_price: priceOf(priceUnits(price)), committed_kg: kgOf(grams(kg)), max_moisture_pct: moist || null, valid_from: from, valid_to: to, note: str(body, "note") || null, reason: revising ? reason : null };
}

// ---------------------------------------------------------------------------
// Đơn mua, phiếu nhận, phải trả
// ---------------------------------------------------------------------------
const pos: OrdersPurchaseOrder[] = [];
const receipts: OrdersReceipt[] = [];
const payables: OrdersPayable[] = [];
let poSeq = 0;
let grnSeq = 0;

function po(id: string, category: "straw" | "other", supplier: string, unit: "kg" | "bale", qty: string, price: string, status: OrdersPurchaseOrder["status"], contractRef: string | null, by: string, desc: string | null) {
  poSeq += 1;
  pos.push({
    po_id: id,
    status,
    code: `PO${String(poSeq).padStart(6, "0")}`,
    category,
    supplier_ref: supplier,
    contract_ref: contractRef,
    contract_version: contractRef ? 1 : null,
    facility_ref: null,
    unit,
    ordered_qty: qty,
    unit_price: price,
    price_source: contractRef ? "contract" : "entered",
    expected_date: todayVn(3),
    description: desc,
    created_by: by,
    approved_by: status === "approved" ? ACTORS.truongKinhDoanh : null,
    reason: null,
    event_time: daysAgo(10),
    receipts: [],
  });
}
po(OR.poStraw, "straw", HTX.tanHiep, "kg", "90000.000", "650000.0000", "approved", OR.contractTanHiep, ACTORS.kinhDoanh, "Rơm cuộn vụ Thu Đông — ghe Tân Hiệp");
po(OR.poOther, "other", OR.supplierTwine, "kg", "500.000", "45000000.0000", "draft", null, ACTORS.kinhDoanh, "Dây buộc cuộn rơm");
po(OR.poBale, "straw", HTX.vinhBinh, "bale", "4000", "15000.0000", "approved", OR.contractVinhBinh, ACTORS.kinhDoanh, "Rơm theo cuộn — Vĩnh Bình");

function variance(r: OrdersReceipt): void {
  if (!r.received_qty) {
    r.variance_pct = null;
    r.needs_variance_report = false;
    r.audit_note = null;
    return;
  }
  const th = params.effective("orders.receipt.po_variance_warn_pct");
  const pct = pctDiff(r.received_qty, r.ordered_qty!);
  r.variance_pct = pct;
  r.threshold_pct = th.value;
  r.param_version = th.version;
  r.needs_variance_report = absPctGreater(pct, th.value);
  const u = r.unit === "bale" ? "cuộn" : "kg";
  r.audit_note = r.needs_variance_report
    ? `Chênh lệch ${pct}% so với đơn mua (nhận ${r.received_qty} ${u}, đặt ${r.ordered_qty} ${u}) — vượt ngưỡng ${th.value}% (${th.version}), cần lập biên bản chênh lệch.`
    : null;
}

function newReceipt(p: OrdersPurchaseOrder, by: string, lot: string | null, qty: string | null, source: OrdersReceipt["qty_source"]): OrdersReceipt {
  grnSeq += 1;
  const hub = lot ? HUB_IN[lot] : undefined;
  const r: OrdersReceipt = {
    receipt_id: newId(0x27),
    po_id: p.po_id,
    status: source === "ticket" && !hub ? "awaiting_weigh" : "pending_approval",
    code: `GRN${String(grnSeq).padStart(6, "0")}`,
    category: p.category,
    supplier_ref: p.supplier_ref,
    lot_ref: lot,
    unit: p.unit,
    ordered_qty: p.ordered_qty,
    ticket_ref: hub?.ticket ?? null,
    ticket_kg: hub?.kg ?? null,
    received_qty: source === "ticket" ? (hub?.kg ?? null) : qty,
    qty_source: source === "ticket" && !hub ? null : source,
    variance_pct: null,
    threshold_pct: null,
    param_version: null,
    needs_variance_report: false,
    audit_note: null,
    report_note: null,
    report_file_ref: null,
    created_by: by,
    approved_by: null,
    reason: null,
    event_time: nowIso(),
  };
  variance(r);
  receipts.push(r);
  p.receipts = receipts.filter((x) => x.po_id === p.po_id);
  return r;
}
{
  const r = newReceipt(pos[0]!, ACTORS.kinhDoanh, L.proto, null, "ticket");
  r.receipt_id = OR.receiptVariance;
  r.event_time = daysAgo(2);
  pos[0]!.receipts = [r];
}

function poOr404(id: string) {
  return pos.find((p) => p.po_id === id) ?? problem(404, "orders.po_not_found", "PO not found");
}
function receiptOr404(id: string) {
  return receipts.find((r) => r.receipt_id === id) ?? problem(404, "orders.receipt_not_found", "Receipt not found");
}
function withReceipts(p: OrdersPurchaseOrder): OrdersPurchaseOrder {
  return { ...p, receipts: receipts.filter((r) => r.po_id === p.po_id).map((r) => ({ ...r, ...(payables.find((x) => x.receipt_id === r.receipt_id) ? { payable: payables.find((x) => x.receipt_id === r.receipt_id) } : {}) })) };
}

function decision(body: Record<string, unknown>): Response | { d: "approved" | "rejected"; reason: string | null } {
  const d = str(body, "decision");
  if (d !== "approved" && d !== "rejected") return validation([{ field: "decision", reason: "approved | rejected" }]);
  if (d === "rejected") {
    const r = reasonOf(body);
    if (r instanceof Response) return r;
    return { d, reason: r };
  }
  return { d, reason: null };
}

// ---------------------------------------------------------------------------
// Đơn bán, giao nhận
// ---------------------------------------------------------------------------
const sos: OrdersSalesOrder[] = [];
const deliveries: OrdersDelivery[] = [];
let soSeq = 0;
let gnSeq = 0;

function priceRule(list: string | null, unit: string, discount: string): { needs: boolean; rule: string } {
  const delta = params.effective("orders.sales.price_approval_delta_pct");
  const disc = params.effective("orders.sales.discount_approval_pct");
  let needs = percentHundredths(discount) > percentHundredths(disc.value);
  if (list && priceUnits(list) > 0n) {
    const d = priceDeltaPct(unit, list);
    if (d !== null && percentHundredths(d) > percentHundredths(delta.value)) needs = true;
  }
  return { needs, rule: `${delta.version};${disc.version}` };
}

function so(id: string, customer: string, product: OrdersSalesOrder["product"], kg: string, list: string | null, unit: string, discount: string, status: OrdersSalesOrder["status"], by: string) {
  soSeq += 1;
  const net = netPrice(unit, discount);
  const pr = priceRule(list, unit, discount);
  sos.push({
    order_id: id,
    status,
    code: `SO${String(soSeq).padStart(6, "0")}`,
    customer_ref: customer,
    product,
    ordered_kg: kg,
    list_price: list,
    unit_price: unit,
    discount_pct: discount,
    net_price: net,
    amount: amountForKg(kg, net),
    needs_price_approval: pr.needs,
    price_rule: pr.rule,
    payment_term_days: 15,
    delivery_from: todayVn(-5),
    delivery_to: todayVn(20),
    created_by: by,
    approved_by: status !== "draft" && pr.needs ? ACTORS.truongKinhDoanh : null,
    reason: null,
    event_time: daysAgo(6),
    deliveries: [],
  });
}
so(OR.soDelivering, OR.customerPlant, "compressed_bale", "30000.000", "1800000.0000", "1800000.0000", "0", "delivering", ACTORS.kinhDoanh);
so(OR.soDraftDiscount, OR.customerPaper, "dried_straw", "60000.000", "1500000.0000", "1500000.0000", "5", "draft", ACTORS.banHang);
so(OR.soDraftSelf, OR.customerPlant, "straw_bale", "40000.000", "1200000.0000", "1140000.0000", "0", "draft", ACTORS.kinhDoanh);

function delivery(id: string, order: OrdersSalesOrder, lot: string, route: "HUB" | "DIRECT", kg: string, status: OrdersDelivery["status"], trip: string | null) {
  gnSeq += 1;
  const t = status === "dispatched" ? CUSTOMER_IN[lot] : undefined;
  deliveries.push({ delivery_id: id, order_id: order.order_id, status, code: `GN${String(gnSeq).padStart(6, "0")}`, customer_ref: order.customer_ref, route, lot_ref: lot, trip_ref: trip, planned_date: todayVn(1), planned_kg: kg, unit_price: order.net_price, customer_in_ticket_ref: t?.ticket ?? null, ticket_kg: t?.kg ?? null, accepted_kg: null, amount: null, accepted_at: null, acceptance_id: null, dispute_ref: null, evidence_file_ref: null, evidence_kind: null, reason: null, rule_version: "orders.delivery.v1", event_time: daysAgo(1) });
}
delivery(OR.deliveryWithTicket, sos[0]!, L.compressed, "HUB", "15000.000", "dispatched", sid(0x20, 3));
delivery(OR.deliveryPlanned, sos[0]!, L.arriving, "HUB", "15000.000", "planned", null);

function soOut(o: OrdersSalesOrder): OrdersSalesOrder {
  return { ...o, deliveries: deliveries.filter((d) => d.order_id === o.order_id) };
}
function soOr404(id: string) {
  return sos.find((o) => o.order_id === id) ?? problem(404, "orders.sales_order_not_found", "Sales order not found");
}
function deliveryOr404(id: string) {
  return deliveries.find((d) => d.delivery_id === id) ?? problem(404, "orders.delivery_not_found", "Delivery not found");
}
const deliveryTransition = () => problem(409, "orders.delivery_transition", "Delivery transition");

export const ordersRoutes: Route[] = [
  // --- tệp chứng từ (biên bản chênh lệch, ePOD / biên bản / e-bill) ---
  ...fileRoutes("orders", [OFFICER, APPROVER], ["receipt_variance_report", "delivery_evidence"]),
  // --- hợp đồng ---
  route("GET", "/v1/orders/contracts", ({ url, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const htx = url.searchParams.get("htx_ref");
    const st = url.searchParams.get("status");
    const items = contracts.map((c) => c.versions[0]!).filter((v) => (!htx || v.htx_ref === htx) && (!st || v.status === st));
    return json({ items });
  }),
  route("POST", "/v1/orders/contracts", ({ body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const inp = contractInput(body, false);
    if (inp instanceof Response) return inp;
    if (overlaps(inp.htx_ref!, inp.valid_from!, inp.valid_to!)) return problem(409, "orders.contract_overlap", "Contract overlap (CT-01)");
    contractSeq += 1;
    const id = newId(0x25);
    const v: OrdersContract = { ...inp, contract_id: id, version: 1, status: "pending", code: `HDR${String(contractSeq).padStart(5, "0")}`, proposed_by: persona.actor, approved_by: null, event_time: nowIso() };
    contracts.unshift({ contract_id: id, versions: [v] });
    return created({ contract_id: id, versions: [v] });
  }),
  route("GET", "/v1/orders/contract-terms", ({ url, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const v = contractFor(url.searchParams.get("htx_ref") ?? "", url.searchParams.get("on") ?? todayVn());
    if (!v) return problem(404, "orders.no_contract", "No contract");
    return json({ contract_id: v.contract_id, code: v.code, htx_ref: v.htx_ref, basis: v.basis, unit_price: v.unit_price, committed_kg: v.committed_kg, max_moisture_pct: v.max_moisture_pct, valid_from: v.valid_from, valid_to: v.valid_to, version: v.version });
  }),
  route("GET", "/v1/orders/contracts/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const c = contractOr404(p[0]!);
    return c instanceof Response ? c : json(c);
  }),
  route("POST", "/v1/orders/contracts/:id/revisions", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const c = contractOr404(p[0]!);
    if (c instanceof Response) return c;
    const cur = c.versions.find((v) => v.status === "active");
    if (!cur || c.versions.some((v) => v.status === "pending")) return problem(409, "orders.contract_transition", "Contract not revisable");
    const inp = contractInput(body, true);
    if (inp instanceof Response) return inp;
    const v: OrdersContract = { ...inp, htx_ref: cur.htx_ref, contract_id: c.contract_id, version: c.versions.length + 1, status: "pending", code: cur.code, proposed_by: persona.actor, approved_by: null, event_time: nowIso() };
    c.versions.unshift(v);
    return created(c);
  }),
  route("POST", "/v1/orders/contracts/:id/versions/:version/decision", ({ params: p, body, persona }) => {
    if (!hasRole(persona, APPROVER)) return forbidden();
    const c = contractOr404(p[0]!);
    if (c instanceof Response) return c;
    const v = c.versions.find((x) => x.version === Number(p[1]));
    if (!v) return problem(404, "orders.contract_not_found", "Version not found");
    if (v.status !== "pending") return problem(409, "orders.contract_transition", "Version not pending");
    if (v.proposed_by === persona.actor) return selfApproval();
    const d = decision(body);
    if (d instanceof Response) return d;
    if (d.d === "approved") {
      if (overlaps(v.htx_ref!, v.valid_from!, v.valid_to!, c.contract_id)) return problem(409, "orders.contract_overlap", "Contract overlap (CT-01)");
      for (const o of c.versions) if (o.status === "active") o.status = "superseded";
      v.status = "active";
      v.approved_by = persona.actor;
    } else {
      v.status = "rejected";
      v.reason = d.reason;
    }
    return created(c);
  }),
  route("POST", "/v1/orders/contracts/:id/terminate", ({ params: p, body, persona }) => {
    if (!hasRole(persona, APPROVER)) return forbidden();
    const c = contractOr404(p[0]!);
    if (c instanceof Response) return c;
    const r = reasonOf(body);
    if (r instanceof Response) return r;
    const cur = c.versions.find((v) => v.status === "active");
    if (!cur) return problem(409, "orders.contract_transition", "No active version");
    cur.status = "terminated";
    cur.reason = r;
    return created(c);
  }),
  // --- đơn mua ---
  route("GET", "/v1/orders/purchase-orders", ({ url, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const st = url.searchParams.get("status");
    const sup = url.searchParams.get("supplier_ref");
    return json({ items: pos.filter((x) => (!st || x.status === st) && (!sup || x.supplier_ref === sup)).map(withReceipts).reverse() });
  }),
  route("POST", "/v1/orders/purchase-orders", ({ body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const errs: { field: string; reason: string }[] = [];
    const category = str(body, "category");
    if (category !== "straw" && category !== "other") errs.push({ field: "category", reason: "straw | other" });
    const supplier = str(body, "supplier_ref");
    if (!UUID_RE.test(supplier)) errs.push({ field: "supplier_ref", reason: "UUID HTX / nhà cung cấp" });
    const expected = str(body, "expected_date") || todayVn();
    let unit = str(body, "unit") as "kg" | "bale" | "";
    const price = str(body, "unit_price");
    if (price && !isPrice(price)) errs.push({ field: "unit_price", reason: "Đơn giá > 0, tối đa 4 số lẻ" });
    if (errs.length) return validation(errs);
    let contractRef: string | null = null;
    let finalPrice = price;
    if (category === "straw") {
      const c = contractFor(supplier, expected);
      if (c) {
        const want = c.basis === "per_bale" ? "bale" : "kg";
        if (unit && unit !== want) return problem(422, "orders.basis_mismatch", "Unit does not match contract basis");
        unit = want;
        if (price && priceUnits(price) !== priceUnits(c.unit_price!)) return validation([{ field: "unit_price", reason: "Khác đơn giá hợp đồng hiệu lực" }]);
        finalPrice = c.unit_price!;
        contractRef = c.contract_id!;
      } else if (!price) return problem(422, "orders.price_required", "Price required without contract");
    } else if (!price) return validation([{ field: "unit_price", reason: "Bắt buộc với đơn mua khác" }]);
    if (!unit) unit = "kg";
    const qty = str(body, "ordered_qty");
    const okQty = unit === "bale" ? /^[1-9]\d*$/.test(qty) : /^\d+(\.\d{1,3})?$/.test(qty) && grams(qty) > 0n;
    if (!okQty) return validation([{ field: "ordered_qty", reason: unit === "bale" ? "Số cuộn nguyên > 0" : "kg > 0, tối đa 3 số lẻ" }]);
    poSeq += 1;
    const p: OrdersPurchaseOrder = { po_id: newId(0x26), status: "draft", code: `PO${String(poSeq).padStart(6, "0")}`, category: category as "straw", supplier_ref: supplier, contract_ref: contractRef, contract_version: contractRef ? 1 : null, facility_ref: str(body, "facility_ref") || null, unit, ordered_qty: unit === "kg" ? kgOf(grams(qty)) : qty, unit_price: priceOf(priceUnits(finalPrice)), price_source: contractRef ? "contract" : "entered", expected_date: expected, description: str(body, "description") || null, created_by: persona.actor, approved_by: null, reason: null, event_time: nowIso(), receipts: [] };
    pos.push(p);
    return created({ purchase_order: p });
  }),
  route("GET", "/v1/orders/purchase-orders/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const x = poOr404(p[0]!);
    return x instanceof Response ? x : json({ purchase_order: withReceipts(x) });
  }),
  route("POST", "/v1/orders/purchase-orders/:id/decision", ({ params: p, body, persona }) => {
    if (!hasRole(persona, APPROVER)) return forbidden();
    const x = poOr404(p[0]!);
    if (x instanceof Response) return x;
    if (x.status !== "draft") return problem(409, "orders.po_transition", "PO not draft");
    if (x.created_by === persona.actor) return selfApproval();
    const d = decision(body);
    if (d instanceof Response) return d;
    x.status = d.d;
    x.approved_by = persona.actor;
    x.reason = d.reason;
    return created({ purchase_order: withReceipts(x) });
  }),
  route("POST", "/v1/orders/purchase-orders/:id/cancel", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const x = poOr404(p[0]!);
    if (x instanceof Response) return x;
    const r = reasonOf(body);
    if (r instanceof Response) return r;
    if ((x.status !== "draft" && x.status !== "approved") || receipts.some((rc) => rc.po_id === x.po_id && rc.status !== "rejected")) return problem(409, "orders.po_transition", "PO cannot be cancelled");
    x.status = "cancelled";
    x.reason = r;
    return created({ purchase_order: withReceipts(x) });
  }),
  route("POST", "/v1/orders/purchase-orders/:id/receipts", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const x = poOr404(p[0]!);
    if (x instanceof Response) return x;
    if (x.status !== "approved") return problem(409, "orders.po_not_approved", "PO not approved");
    const lot = str(body, "lot_ref") || null;
    const qty = str(body, "received_qty") || null;
    if (x.category === "straw" && x.unit === "kg") {
      if (!lot || !UUID_RE.test(lot)) return validation([{ field: "lot_ref", reason: "Rơm theo kg: bắt buộc lô (số nhận = tịnh phiếu cân HUB_IN)" }]);
      if (qty) return validation([{ field: "received_qty", reason: "Rơm theo kg không nhập tay số nhận — lấy từ phiếu cân" }]);
      return created({ receipt: newReceipt(x, persona.actor, lot, null, "ticket") });
    }
    const ok = x.unit === "bale" ? /^\d+$/.test(qty ?? "") : /^\d+(\.\d{1,3})?$/.test(qty ?? "");
    if (!ok) return validation([{ field: "received_qty", reason: x.unit === "bale" ? "Số cuộn đếm (nguyên)" : "kg, tối đa 3 số lẻ" }]);
    return created({ receipt: newReceipt(x, persona.actor, lot, x.unit === "kg" ? kgOf(grams(qty!)) : qty, x.unit === "bale" ? "count" : "entered") });
  }),
  route("GET", "/v1/orders/receipts/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const r = receiptOr404(p[0]!);
    if (r instanceof Response) return r;
    const pay = payables.find((x) => x.receipt_id === r.receipt_id);
    return json({ receipt: { ...r, ...(pay ? { payable: pay } : {}) } });
  }),
  route("POST", "/v1/orders/receipts/:id/variance-report", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const r = receiptOr404(p[0]!);
    if (r instanceof Response) return r;
    if (r.status === "rejected") return problem(409, "orders.receipt_transition", "Receipt rejected");
    const note = str(body, "note");
    if (note.length < 10 || note.length > 2000) return validation([{ field: "note", reason: "10–2000 ký tự" }]);
    const fileRef = str(body, "file_ref") || null;
    if (fileRef && !UUID_RE.test(fileRef)) return validation([{ field: "file_ref", reason: "UUID tệp chứng từ" }]);
    // Tệp đã tải qua POST /v1/orders/files phải đúng mục đích; mã chưa đăng ký vẫn nhận (tương thích ngược).
    const f = fileRef ? findFile("orders", fileRef) : undefined;
    if (f && f.purpose !== "receipt_variance_report") return problem(422, "orders.file_purpose", "File uploaded for another purpose");
    r.report_note = note;
    r.report_file_ref = fileRef;
    return created({ receipt: r });
  }),
  route("POST", "/v1/orders/receipts/:id/corrections", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const r = receiptOr404(p[0]!);
    if (r instanceof Response) return r;
    if (r.status === "approved" || r.status === "rejected") return problem(409, "orders.receipt_transition", "Receipt decided");
    if (r.qty_source === "ticket") return validation([{ field: "received_qty", reason: "Số nhận theo phiếu cân — không bù tay" }]);
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    const qty = str(body, "received_qty");
    const ok = r.unit === "bale" ? /^\d+$/.test(qty) : /^\d+(\.\d{1,3})?$/.test(qty);
    if (!ok) return validation([{ field: "received_qty", reason: "Số nhận không hợp lệ" }]);
    r.received_qty = r.unit === "kg" ? kgOf(grams(qty)) : qty;
    r.reason = reason;
    variance(r);
    return created({ receipt: r });
  }),
  route("POST", "/v1/orders/receipts/:id/decision", ({ params: p, body, persona }) => {
    if (!hasRole(persona, APPROVER)) return forbidden();
    const r = receiptOr404(p[0]!);
    if (r instanceof Response) return r;
    if (r.status === "awaiting_weigh") return problem(409, "orders.receipt_not_weighed", "Receipt awaiting weigh ticket");
    if (r.status !== "pending_approval") return problem(409, "orders.receipt_transition", "Receipt decided");
    if (r.created_by === persona.actor) return selfApproval();
    const d = decision(body);
    if (d instanceof Response) return d;
    r.status = d.d;
    r.approved_by = persona.actor;
    r.reason = d.reason ?? r.reason ?? null;
    if (d.d === "approved" && r.category === "other") {
      const x = pos.find((o) => o.po_id === r.po_id)!;
      const term = params.effective("orders.payment_term_days");
      payables.push({ payable_id: newId(0x2b), receipt_id: r.receipt_id, po_id: r.po_id, supplier_ref: r.supplier_ref, unit: r.unit, qty: r.received_qty!, unit_price: x.unit_price, amount: r.unit === "bale" ? amountForCount(BigInt(r.received_qty!), x.unit_price!) : amountForKg(r.received_qty!, x.unit_price!), due_date: todayVn(Number(term.value)), payment_term_days: Number(term.value), param_version: term.version, recorded_at: nowIso() });
    }
    const pay = payables.find((x) => x.receipt_id === r.receipt_id);
    return created({ receipt: { ...r, ...(pay ? { payable: pay } : {}) } });
  }),
  route("GET", "/v1/orders/payables", ({ url, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const poId = url.searchParams.get("po_id");
    const pg = pageSlice(payables.filter((x) => !poId || x.po_id === poId), url);
    return json({ items: pg.items, ...(pg.next_cursor ? { next_cursor: pg.next_cursor } : {}) });
  }),
  // --- đơn bán ---
  route("GET", "/v1/orders/sales-orders", ({ url, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const st = url.searchParams.get("status");
    return json({ items: sos.filter((o) => !st || o.status === st).map(soOut).reverse() });
  }),
  route("POST", "/v1/orders/sales-orders", ({ body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const errs: { field: string; reason: string }[] = [];
    const customer = str(body, "customer_ref");
    if (!UUID_RE.test(customer)) errs.push({ field: "customer_ref", reason: "UUID khách (vault)" });
    const product = str(body, "product");
    if (!["straw_bale", "straw_loose", "dried_straw", "compressed_bale"].includes(product)) errs.push({ field: "product", reason: "Dạng hàng không hợp lệ" });
    const kg = str(body, "ordered_kg");
    if (!/^\d+(\.\d{1,3})?$/.test(kg) || grams(kg) <= 0n) errs.push({ field: "ordered_kg", reason: "kg > 0" });
    const unit = str(body, "unit_price");
    if (!isPrice(unit)) errs.push({ field: "unit_price", reason: "Đơn giá > 0" });
    const list = str(body, "list_price");
    if (list && !isPrice(list)) errs.push({ field: "list_price", reason: "Giá bảng > 0" });
    const disc = str(body, "discount_pct") || "0";
    if (!/^\d{1,2}(\.\d{1,2})?$/.test(disc)) errs.push({ field: "discount_pct", reason: "0 ≤ chiết khấu < 100, tối đa 2 chữ số thập phân" });
    if (errs.length) return validation(errs);
    soSeq += 1;
    const net = netPrice(unit, disc);
    const pr = priceRule(list || null, unit, disc);
    const term = params.effective("orders.payment_term_days");
    const o: OrdersSalesOrder = { order_id: newId(0x28), status: "draft", code: `SO${String(soSeq).padStart(6, "0")}`, customer_ref: customer, product: product as "straw_bale", ordered_kg: kgOf(grams(kg)), list_price: list ? priceOf(priceUnits(list)) : null, unit_price: priceOf(priceUnits(unit)), discount_pct: disc, net_price: net, amount: amountForKg(kg, net), needs_price_approval: pr.needs, price_rule: pr.rule, payment_term_days: Number(term.value), delivery_from: str(body, "delivery_from") || null, delivery_to: str(body, "delivery_to") || null, created_by: persona.actor, approved_by: null, reason: null, event_time: nowIso(), deliveries: [] };
    sos.push(o);
    return created({ sales_order: o });
  }),
  route("GET", "/v1/orders/sales-orders/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const o = soOr404(p[0]!);
    return o instanceof Response ? o : json({ sales_order: soOut(o) });
  }),
  route("POST", "/v1/orders/sales-orders/:id/confirm", ({ params: p, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const o = soOr404(p[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "draft") return problem(409, "orders.order_transition", "Order not draft");
    if (o.needs_price_approval) {
      if (!hasRole(persona, APPROVER)) return problem(403, "orders.price_approval_required", "Price approval required");
      if (o.created_by === persona.actor) return selfApproval();
      o.approved_by = persona.actor;
    }
    o.status = "confirmed";
    return created({ sales_order: soOut(o) });
  }),
  route("POST", "/v1/orders/sales-orders/:id/advance", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const o = soOr404(p[0]!);
    if (o instanceof Response) return o;
    const to = str(body, "status");
    const flow = ["draft", "confirmed", "delivering", "delivered", "completed"];
    if (to === "cancelled") {
      const r = reasonOf(body);
      if (r instanceof Response) return r;
      if (o.status !== "draft" && o.status !== "confirmed") return problem(409, "orders.order_transition", "Cannot cancel after delivering");
      o.status = "cancelled";
      o.reason = r;
      return created({ sales_order: soOut(o) });
    }
    if (!flow.includes(to) || flow.indexOf(to) <= flow.indexOf(o.status ?? "")) return problem(409, "orders.order_transition", "Forward only");
    o.status = to as OrdersSalesOrder["status"];
    return created({ sales_order: soOut(o) });
  }),
  route("POST", "/v1/orders/sales-orders/:id/deliveries", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const o = soOr404(p[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "confirmed" && o.status !== "delivering") return problem(409, "orders.order_transition", "Order not confirmed");
    const errs: { field: string; reason: string }[] = [];
    const lot = str(body, "lot_ref");
    if (!UUID_RE.test(lot)) errs.push({ field: "lot_ref", reason: "UUID lô" });
    const rt = str(body, "route");
    if (rt !== "HUB" && rt !== "DIRECT") errs.push({ field: "route", reason: "HUB | DIRECT" });
    const date = str(body, "planned_date");
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) errs.push({ field: "planned_date", reason: "YYYY-MM-DD" });
    const kg = str(body, "planned_kg");
    if (!/^\d+(\.\d{1,3})?$/.test(kg) || grams(kg) <= 0n) errs.push({ field: "planned_kg", reason: "kg > 0" });
    if (errs.length) return validation(errs);
    gnSeq += 1;
    const d: OrdersDelivery = { delivery_id: newId(0x2a), order_id: o.order_id, status: "planned", code: `GN${String(gnSeq).padStart(6, "0")}`, customer_ref: o.customer_ref, route: rt as "HUB", lot_ref: lot, trip_ref: str(body, "trip_ref") || null, planned_date: date, planned_kg: kgOf(grams(kg)), unit_price: o.net_price, customer_in_ticket_ref: null, ticket_kg: null, accepted_kg: null, amount: null, accepted_at: null, acceptance_id: null, dispute_ref: null, evidence_file_ref: null, evidence_kind: null, reason: null, rule_version: "orders.delivery.v1", event_time: nowIso() };
    deliveries.push(d);
    return created({ delivery: d });
  }),
  route("GET", "/v1/orders/deliveries/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, OFFICER, APPROVER)) return forbidden();
    const d = deliveryOr404(p[0]!);
    return d instanceof Response ? d : json({ delivery: d });
  }),
  route("POST", "/v1/orders/deliveries/:id/dispatch", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const d = deliveryOr404(p[0]!);
    if (d instanceof Response) return d;
    if (d.status !== "planned") return deliveryTransition();
    d.status = "dispatched";
    d.trip_ref = str(body, "trip_ref") || d.trip_ref || null;
    // consumer mkg.orders.weigh_ticket: phiếu CUSTOMER_IN đã về → gắn ngay.
    const t = CUSTOMER_IN[d.lot_ref!];
    if (t) {
      d.customer_in_ticket_ref = t.ticket;
      d.ticket_kg = t.kg;
    }
    const o = sos.find((x) => x.order_id === d.order_id);
    if (o?.status === "confirmed") o.status = "delivering";
    return created({ delivery: d });
  }),
  route("POST", "/v1/orders/deliveries/:id/accept", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const d = deliveryOr404(p[0]!);
    if (d instanceof Response) return d;
    if (d.status !== "dispatched") return deliveryTransition();
    const ev = (typeof body.evidence === "object" && body.evidence !== null ? body.evidence : {}) as Record<string, unknown>;
    const file = typeof ev.file_ref === "string" ? ev.file_ref : "";
    const kind = typeof ev.kind === "string" ? ev.kind : "";
    const errs: { field: string; reason: string }[] = [];
    if (!UUID_RE.test(file)) errs.push({ field: "evidence.file_ref", reason: "UUID tệp chứng từ" });
    if (!["epod", "minutes", "ebill"].includes(kind)) errs.push({ field: "evidence.kind", reason: "epod | minutes | ebill" });
    if (errs.length) return validation(errs);
    const f = findFile("orders", file);
    if (f && f.purpose !== "delivery_evidence") return problem(422, "orders.file_purpose", "File uploaded for another purpose");
    if (!d.customer_in_ticket_ref || !d.ticket_kg) return problem(409, "orders.customer_in_missing", "CUSTOMER_IN ticket missing");
    d.status = "accepted";
    d.accepted_kg = d.ticket_kg;
    d.amount = amountForKg(d.ticket_kg, d.unit_price!);
    d.accepted_at = str(body, "accepted_at") || nowIso();
    d.acceptance_id = newId(0x2c);
    d.evidence_file_ref = file;
    d.evidence_kind = kind as "epod";
    d.dispute_ref = str(body, "dispute_ref") || null;
    return created({ delivery: d });
  }),
  route("POST", "/v1/orders/deliveries/:id/reject", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const d = deliveryOr404(p[0]!);
    if (d instanceof Response) return d;
    const r = reasonOf(body);
    if (r instanceof Response) return r;
    if (d.status !== "dispatched") return deliveryTransition();
    d.status = "rejected";
    d.reason = r;
    return created({ delivery: d });
  }),
  route("POST", "/v1/orders/deliveries/:id/cancel", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const d = deliveryOr404(p[0]!);
    if (d instanceof Response) return d;
    const r = reasonOf(body);
    if (r instanceof Response) return r;
    if (d.status !== "planned") return deliveryTransition();
    d.status = "cancelled";
    d.reason = r;
    return created({ delivery: d });
  }),
  route("POST", "/v1/orders/deliveries/:id/corrections", ({ params: p, body, persona }) => {
    if (!hasRole(persona, OFFICER)) return forbidden();
    const d = deliveryOr404(p[0]!);
    if (d instanceof Response) return d;
    const r = reasonOf(body);
    if (r instanceof Response) return r;
    if (d.status !== "accepted") return deliveryTransition();
    const ticket = str(body, "customer_in_ticket_ref");
    if (ticket && ticket !== CUSTOMER_IN[d.lot_ref!]?.ticket) return problem(422, "orders.ticket_unknown", "Ticket unknown");
    d.acceptance_id = newId(0x2c);
    d.reason = r;
    d.dispute_ref = str(body, "dispute_ref") || d.dispute_ref || null;
    return created({ delivery: d });
  }),
  ...params.routes,
];

export const ordersAdminRoutes: Route[] = [
  route("POST", "/v1/orders/projections/rebuild", ({ persona }) => (hasRole(persona, APPROVER) ? json({ contract_current: contracts.length, po_current: pos.length, receipt_current: receipts.length, sales_order_current: sos.length, delivery_current: deliveries.length }) : forbidden())),
];
