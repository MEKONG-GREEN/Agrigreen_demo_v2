// Backend giả lập mkg/hub — đường dẫn và hình dạng theo contracts/openapi/mkg-internal.yaml (+ DTO backend).
import type { Allocation, Movement, Facility, FacilityVersion, Product, RankedLot, Stocktake, Transfer } from "../types";
import { addKg, cmpKg, grams, isKg, kgOf, normKg, subKg } from "@/lib/kg";
import { DEFAULT_RISK_V2, riskScore, validateRisk, type RiskParamsV2 } from "@/lib/rules";
import { remember } from "@/lib/recent";
import {
  DEFAULT_WEIGH_V1,
  facilities,
  findLot,
  hubParams,
  hubPublications,
  lots,
  newId,
  reconciliations,
  remainingOf,
  stocktakes,
  tickets,
  transfers,
  effectiveTicket,
  lotRolls,
  type FacilityRec,
  type HubParamRec,
  type LotRec,
} from "./db";
import { created, forbidden, hasRole, json, nowIso, opaque, pageSlice, problem, route, selfApproval, str, validation, type Ctx, type Route } from "./http";

const OP = "hub_operator";

/** Tỷ lệ "0.333334" → phần triệu (BigInt), -1 nếu sai dạng. */
function micro(r: string): bigint {
  const m = /^(\d+)(?:\.(\d{1,6}))?$/.exec(r.trim());
  return m ? BigInt(m[1]!) * 1_000_000n + BigInt((m[2] ?? "").padEnd(6, "0")) : -1n;
}
const APPR = "hub_approver";

function facilityOr404(id: string): FacilityRec | Response {
  const f = facilities.find((x) => x.id === id);
  return f?.current ? f : problem(404, "hub.facility_not_found", "Facility not found");
}

function activeParams(set: "risk" | "weigh"): { version: string; params: Record<string, unknown> } {
  const rule = set === "risk" ? "hub.risk.v2" : "hub.weigh.v1";
  const v = hubParams.find((p) => p.param_set === set && p.state === "approved");
  if (v) return { version: `${rule}#${v.version}`, params: v.params };
  return { version: `${rule}#0`, params: set === "risk" ? { ...DEFAULT_RISK_V2 } : { ...DEFAULT_WEIGH_V1 } };
}

export function rankLots(facilityId: string): { rule: string; ranked: RankedLot[] } {
  const a = activeParams("risk");
  const p = a.params as unknown as RiskParamsV2;
  const ranked = lots
    .filter((l) => l.lot.facility_id === facilityId && grams(remainingOf(l)) > 0n)
    .map<RankedLot>((l) => {
      const age = Math.floor((Date.now() - Date.parse(l.lot.event_time ?? "")) / 86_400_000);
      return {
        lot_id: l.lot.id,
        code: l.lot.code,
        product: l.lot.product,
        remaining_kg: remainingOf(l),
        moisture_pct: l.lot.moisture_pct ?? null,
        received_at: l.lot.event_time,
        age_days: age,
        risk_score: riskScore(p, age, l.lot.moisture_pct ? Number(l.lot.moisture_pct) : null),
      };
    })
    .sort((x, y) => Number(y.risk_score) - Number(x.risk_score) || Date.parse(x.received_at ?? "") - Date.parse(y.received_at ?? ""));
  return { rule: a.version, ranked };
}

function allocate(ranked: RankedLot[], qty: string): { allocations: Allocation[]; shortfall: string | null } {
  let left = grams(qty);
  const allocations: Allocation[] = [];
  for (const l of ranked) {
    if (left <= 0n) break;
    const rem = grams(l.remaining_kg ?? "0");
    const take = rem < left ? rem : left;
    allocations.push({ lot_id: l.lot_id, mass_kg: kgOf(take), risk_score: l.risk_score });
    left -= take;
  }
  return { allocations, shortfall: left > 0n ? kgOf(left) : null };
}

function capacity(f: Facility) {
  const used = lots.filter((l) => l.lot.facility_id === f.id).reduce((s, l) => s + grams(remainingOf(l)), 0n);
  const cap = grams(f.capacity_kg ?? "0");
  const t = (g: bigint) => (Number(g) / 1_000_000).toFixed(3);
  return { capacity_kg: kgOf(cap), used_kg: kgOf(used), available_kg: kgOf(cap - used), capacity_tons: t(cap), used_tons: t(used), available_tons: t(cap - used), as_of: nowIso() };
}

function pushMove(l: LotRec, kind: "receive" | "dispatch" | "correction" | "transform_in" | "transform_out", qty: string, at: string, ref: [string, string] | null) {
  const bal = addKg(remainingOf(l), qty);
  const m: Movement = { id: newId(4), lot_id: l.lot.id, kind, qty_kg: normKg(qty), balance_after_kg: bal, ref_type: ref?.[0] ?? null, ref_id: ref?.[1] ?? null, supersedes_id: null, correction_reason: null, event_time: at, recorded_at: nowIso() };
  l.movements.push(m);
  return m;
}

function dispatchFrom(fid: string, massKg: string, manual: { lot_id: string; mass_kg: string }[] | undefined, refType: string, refId: string, at: string) {
  const { rule, ranked } = rankLots(fid);
  let allocations: Allocation[];
  if (manual && manual.length) {
    const total = addKg(...manual.map((a) => a.mass_kg));
    if (cmpKg(total, massKg) !== 0) return problem(422, "hub.allocation_mismatch", "Allocations must sum to mass_kg");
    for (const a of manual) {
      const l = findLot(a.lot_id);
      if (!l) return problem(404, "hub.lot_not_found", "Lot not found");
      if (l.lot.facility_id !== fid) return problem(409, "hub.lot_other_facility", "Lot at other facility");
      if (cmpKg(remainingOf(l), a.mass_kg) < 0) return problem(409, "hub.insufficient_stock", "Insufficient stock");
    }
    allocations = manual.map((a) => ({ lot_id: a.lot_id, mass_kg: normKg(a.mass_kg) }));
  } else {
    const r = allocate(ranked, massKg);
    if (r.shortfall) return problem(409, "hub.insufficient_stock", "Insufficient stock", { shortfall_kg: r.shortfall });
    allocations = r.allocations;
  }
  const movements = allocations.map((a) => pushMove(findLot(a.lot_id!)!, "dispatch", `-${a.mass_kg}`, at, [refType, refId]));
  return { rule, allocations, movements, strategy: manual && manual.length ? "manual" : "risk" };
}

function versionOf(fid: string, spec: Record<string, unknown>, version: number, actor: string, reason: string | null, supersedes: string | null): FacilityVersion {
  const loc = spec.location as { lat: number; lng: number } | undefined;
  return {
    id: newId(2),
    facility_id: fid,
    version,
    state: "pending",
    supersedes_id: supersedes,
    change_reason: reason,
    proposed_by: actor,
    decided_by: null,
    decided_at: null,
    code: str(spec, "code"),
    name: str(spec, "name"),
    kind: str(spec, "kind"),
    capacity_kg: normKg(str(spec, "capacity_kg")),
    status: str(spec, "status") || "active",
    province_code: str(spec, "province_code"),
    effective_from: str(spec, "effective_from"),
    location: loc,
    event_time: nowIso(),
  };
}

function checkSpec(b: Record<string, unknown>): Response | null {
  const errs: { field: string; reason: string }[] = [];
  if (!/^[A-Z0-9][A-Z0-9_-]{1,31}$/.test(str(b, "code"))) errs.push({ field: "code", reason: "Mã in hoa, số, gạch; 2–32 ký tự" });
  if (!str(b, "name")) errs.push({ field: "name", reason: "Bắt buộc" });
  if (!["hub", "warehouse", "yard"].includes(str(b, "kind"))) errs.push({ field: "kind", reason: "hub | warehouse | yard" });
  if (!/^[0-9]{2}$/.test(str(b, "province_code"))) errs.push({ field: "province_code", reason: "Mã tỉnh 2 chữ số" });
  if (!isKg(str(b, "capacity_kg")) || grams(str(b, "capacity_kg")) <= 0n) errs.push({ field: "capacity_kg", reason: "Khối lượng kg > 0" });
  if (!str(b, "effective_from")) errs.push({ field: "effective_from", reason: "Bắt buộc" });
  const loc = b.location as { lat?: unknown; lng?: unknown } | undefined;
  if (!loc || typeof loc.lat !== "number" || typeof loc.lng !== "number") errs.push({ field: "location", reason: "Tọa độ WGS84 lat/lng" });
  return errs.length ? validation(errs) : null;
}

// --------------------------------------------------------------------------
// Bề mặt ứng dụng
// --------------------------------------------------------------------------
export const hubAppRoutes: Route[] = [
  route("GET", "/v1/hub/facilities", ({ url }) => {
    const kind = url.searchParams.get("kind");
    const list = facilities.map((f) => f.current).filter((f): f is Facility => !!f && (!kind || f.kind === kind));
    return json(pageSlice(list, url));
  }),
  route("GET", "/v1/hub/facilities/:id", ({ params }) => {
    const f = facilityOr404(params[0]!);
    return f instanceof Response ? f : json(f.current);
  }),
  route("GET", "/v1/hub/facilities/:id/inventory", ({ params }) => {
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const r = rankLots(f.id);
    return json({ facility: f.current, lots: r.ranked, capacity: capacity(f.current!), risk_rule: r.rule });
  }),
  route("GET", "/v1/hub/facilities/:id/dispatch-suggestion", ({ params, url }) => {
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const r = rankLots(f.id);
    const mass = url.searchParams.get("mass_kg");
    if (mass && !isKg(mass)) return validation([{ field: "mass_kg", reason: "Khối lượng kg" }]);
    const a = mass ? allocate(r.ranked, mass) : { allocations: [], shortfall: null };
    return json({ risk_rule: r.rule, lots: r.ranked, allocations: a.allocations, shortfall_kg: a.shortfall });
  }),
  route("POST", "/v1/hub/facilities/:id/receipts", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const ticketRef = str(body, "weigh_ticket_ref");
    const manualReason = str(body, "manual_reason");
    let mass = str(body, "mass_kg");
    let moisture = str(body, "moisture_pct") || null;
    let lotId = str(body, "id") || newId(3);
    const weigh = activeParams("weigh").params as typeof DEFAULT_WEIGH_V1;
    if (ticketRef) {
      const t = tickets.find((x) => x.id === ticketRef);
      if (!t || t.point !== "HUB_IN") return problem(422, "hub.weigh_ticket_unknown", "Weigh ticket unknown");
      if (findLot(t.lot_ref ?? "")) return problem(409, "hub.weigh_ticket_used", "Weigh ticket already used");
      lotId = t.lot_ref!;
      mass = t.mass_kg ?? mass;
      moisture = t.moisture_pct ?? moisture;
    } else {
      if (!isKg(mass) || grams(mass) <= 0n) return validation([{ field: "mass_kg", reason: "Bắt buộc khi không có weigh_ticket_ref" }]);
      if (weigh.ticket_required && manualReason.length < 10) return problem(422, "hub.weigh_ticket_required", "Weigh ticket required");
    }
    const sources = Array.isArray(body.sources) ? (body.sources as { kind: string; ref: string; mass_kg?: string; ratio?: string }[]) : [];
    if (!sources.length) return validation([{ field: "sources", reason: "Ít nhất một nguồn" }]);
    const byMass = sources.every((s) => s.mass_kg);
    const byRatio = sources.every((s) => s.ratio);
    if (!byMass && !byRatio) return problem(422, "hub.ratio_invalid", "Sources must all declare mass_kg or ratio");
    if (byRatio && sources.reduce((s, x) => s + micro(x.ratio!), 0n) !== 1_000_000n) return problem(422, "hub.ratio_invalid", "Ratios must sum to exactly 1");
    const total = byMass ? addKg(...sources.map((s) => s.mass_kg!)) : mass;
    const outSources = sources.map((s) => ({ kind: s.kind, ref: s.ref, mass_kg: s.mass_kg, ratio: byMass ? (Number(s.mass_kg) / Number(total)).toFixed(6) : Number(s.ratio).toFixed(6) }));
    const at = str(body, "event_time") || nowIso();
    const code = str(body, "code") || `LO-${(f.current!.code ?? "HUB").split("-").pop()}-${lotId.slice(-4).toUpperCase()}`;
    const rec: LotRec = {
      lot: { id: lotId, facility_id: f.id, code, product: str(body, "product") as LotRec["lot"]["product"], origin: "receipt", batch_id: null, initial_kg: normKg(mass), moisture_pct: moisture, sources: outSources, event_time: at, recorded_at: nowIso() },
      movements: [],
    };
    lots.push(rec);
    const m = pushMove(rec, "receive", mass, at, ticketRef ? ["weigh_ticket", ticketRef] : null);
    remember("lot", lotId, code);
    const cap = capacity(f.current!);
    return created({ lot: rec.lot, movement: m, over_capacity: grams(cap.available_kg) < 0n });
  }),
  route("POST", "/v1/hub/facilities/:id/dispatches", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const mass = str(body, "mass_kg");
    if (!isKg(mass) || grams(mass) <= 0n) return validation([{ field: "mass_kg", reason: "phải > 0" }]);
    const id = newId(5);
    const at = str(body, "event_time") || nowIso();
    const r = dispatchFrom(f.id, mass, body.allocations as { lot_id: string; mass_kg: string }[] | undefined, "dispatch", id, at);
    if (r instanceof Response) return r;
    return created({
      dispatch: { id, facility_id: f.id, mass_kg: normKg(mass), destination_ref: str(body, "destination_ref") || null, strategy: r.strategy, risk_rule: r.rule, allocations: r.allocations, event_time: at },
      movements: r.movements,
    });
  }),
  route("POST", "/v1/hub/facilities/:id/capacity-snapshots", ({ params, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const c = capacity(f.current!);
    const x = {
      id: newId(13),
      contract: "hub_capacity" as const,
      contract_version: 1,
      event_type: "mkg.hub.capacity.published",
      schema_version: 1,
      subject_ref: f.id,
      seq: hubPublications.filter((p) => p.subject_ref === f.id).length + 1,
      payload: opaque({ facility_ref: f.id, capacity_tons: c.capacity_tons, used_tons: c.used_tons, available_tons: c.available_tons, as_of: c.as_of }),
      event_time: c.as_of,
      recorded_at: c.as_of,
    };
    hubPublications.unshift(x);
    return created(x);
  }),
  route("GET", "/v1/hub/facilities/:id/publications", ({ params, url }) => {
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const contract = url.searchParams.get("contract") ?? "hub_capacity";
    return json({ items: hubPublications.filter((p) => p.subject_ref === f.id && p.contract === contract) });
  }),
  route("GET", "/v1/hub/lots/:id", ({ params }) => {
    const l = findLot(params[0]!);
    if (!l) return problem(404, "hub.lot_not_found", "Lot not found");
    return json({ lot: l.lot, remaining_kg: remainingOf(l), movements: l.movements });
  }),
  route("POST", "/v1/hub/movements/:id/corrections", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const l = lots.find((x) => x.movements.some((m) => m.id === params[0]));
    const m = l?.movements.find((x) => x.id === params[0]);
    if (!l || !m) return problem(404, "hub.movement_not_found", "Movement not found");
    if (m.kind !== "receive" && m.kind !== "dispatch") return problem(409, "hub.correction_unsupported", "Correction unsupported");
    if (l.movements.some((x) => x.supersedes_id === m.id)) return problem(409, "hub.already_corrected", "Already corrected");
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Ít nhất 10 ký tự" }]);
    const qty = m.qty_kg!.startsWith("-") ? m.qty_kg!.slice(1) : `-${m.qty_kg}`;
    if (grams(addKg(remainingOf(l), qty)) < 0n) return problem(409, "hub.insufficient_stock", "Negative balance");
    const c = pushMove(l, "correction", qty, str(body, "event_time") || nowIso(), m.ref_type ? [m.ref_type, m.ref_id!] : null);
    c.supersedes_id = m.id!;
    c.correction_reason = str(body, "reason");
    return created(c);
  }),
  route("POST", "/v1/hub/facilities/:id/stocktakes", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const lines = Array.isArray(body.lines) ? (body.lines as { product: string; counted_kg: string }[]) : [];
    const products = Array.from(new Set(lots.filter((l) => l.lot.facility_id === f.id && grams(remainingOf(l)) > 0n).map((l) => l.lot.product!)));
    if (!lines.length || lines.some((x) => !isKg(x.counted_kg)) || products.some((p) => !lines.find((x) => x.product === p)))
      return problem(422, "hub.stocktake_lines_invalid", "Lines must cover every product in stock");
    const supersedes = str(body, "supersedes_id") || null;
    const pending = stocktakes.find((s) => s.facility_id === f.id && s.status === "pending");
    if (pending && pending.id !== supersedes) return problem(409, "hub.stocktake_pending_exists", "Pending stocktake exists");
    if (supersedes && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Đếm lại cần lý do ≥ 10 ký tự" }]);
    if (pending) pending.status = "superseded";
    const outLines = lines.map((x) => {
      const book = kgOf(lots.filter((l) => l.lot.facility_id === f.id && l.lot.product === x.product).reduce((s, l) => s + grams(remainingOf(l)), 0n));
      return { product: x.product as Product, book_kg: book, counted_kg: normKg(x.counted_kg), variance_kg: subKg(x.counted_kg, book) };
    });
    const st: Stocktake = {
      id: newId(11),
      facility_id: f.id,
      status: "pending",
      lines: outLines,
      book_kg: addKg(...outLines.map((x) => x.book_kg)),
      counted_kg: addKg(...outLines.map((x) => x.counted_kg)),
      variance_kg: addKg(...outLines.map((x) => x.variance_kg)),
      supersedes_id: supersedes,
      counted_by: persona.actor,
      decided_by: null,
      adjustments: [],
    };
    stocktakes.unshift(st);
    return created(st);
  }),
  route("GET", "/v1/hub/facilities/:id/stocktakes", ({ params }) => {
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    return json({ items: stocktakes.filter((s) => s.facility_id === f.id) });
  }),
  route("GET", "/v1/hub/stocktakes/:id", ({ params }) => {
    const s = stocktakes.find((x) => x.id === params[0]);
    return s ? json(s) : problem(404, "hub.stocktake_not_found", "Stocktake not found");
  }),
  route("POST", "/v1/hub/stocktakes/:id/decision", ({ params, body, persona }) => {
    if (!hasRole(persona, APPR)) return forbidden();
    const s = stocktakes.find((x) => x.id === params[0]);
    if (!s) return problem(404, "hub.stocktake_not_found", "Stocktake not found");
    if (s.status !== "pending") return problem(409, "hub.stocktake_not_pending", "Stocktake not pending");
    if (s.counted_by === persona.actor) return selfApproval();
    const decision = str(body, "decision");
    if (decision === "rejected" && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ chối cần lý do ≥ 10 ký tự" }]);
    if (decision === "approved") {
      for (const line of s.lines ?? []) {
        const v = grams(line.variance_kg ?? "0");
        if (v === 0n) continue;
        const { ranked } = rankLots(s.facility_id!);
        const target = ranked.find((r) => r.product === line.product);
        const l = target ? findLot(target.lot_id!) : lots.find((x) => x.lot.facility_id === s.facility_id && x.lot.product === line.product);
        if (!l) continue;
        const m = pushMove(l, "correction", kgOf(v), nowIso(), ["stocktake", s.id!]);
        s.adjustments = [...(s.adjustments ?? []), { lot_id: l.lot.id, movement_id: m.id, qty_kg: m.qty_kg }];
      }
    }
    s.status = decision === "approved" ? "approved" : "rejected";
    s.decided_by = persona.actor;
    return created(s);
  }),
  route("POST", "/v1/hub/facilities/:id/transfers", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const to = str(body, "to_facility_id");
    if (to === f.id) return problem(422, "hub.transfer_same_facility", "Same facility");
    const dest = facilityOr404(to);
    if (dest instanceof Response) return dest;
    const mass = str(body, "mass_kg");
    if (!isKg(mass) || grams(mass) <= 0n) return validation([{ field: "mass_kg", reason: "phải > 0" }]);
    const id = newId(12);
    const at = str(body, "event_time") || nowIso();
    const r = dispatchFrom(f.id, mass, body.allocations as { lot_id: string; mass_kg: string }[] | undefined, "transfer", id, at);
    if (r instanceof Response) return r;
    const t: Transfer = {
      id,
      from_facility_id: f.id,
      to_facility_id: dest.id,
      dispatch_id: newId(5),
      status: "in_transit",
      dispatched_kg: normKg(mass),
      received_kg: null,
      loss_kg: null,
      source_lots: r.allocations.map((a) => ({ lot_id: a.lot_id!, mass_kg: a.mass_kg! })),
      received_lots: [],
      dispatched_at: at,
      received_at: null,
    };
    transfers.unshift(t);
    remember("transfer", id, `${f.current!.name} → ${dest.current!.name}`);
    return created(t);
  }),
  route("POST", "/v1/hub/facilities/:id/transfers/:transfer/receipt", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const t = transfers.find((x) => x.id === params[1]);
    if (!t) return problem(404, "hub.transfer_not_found", "Transfer not found");
    if (t.to_facility_id !== params[0]) return problem(409, "hub.transfer_wrong_facility", "Wrong facility");
    if (t.status !== "in_transit") return problem(409, "hub.transfer_not_in_transit", "Not in transit");
    let received = str(body, "mass_kg");
    const refs = Array.isArray(body.weigh_ticket_refs) ? (body.weigh_ticket_refs as string[]) : [];
    if (refs.length) {
      const ts = refs.map((id) => tickets.find((x) => x.id === id && x.point === "HUB_IN"));
      if (ts.some((x) => !x)) return problem(422, "hub.transfer_ticket_invalid", "Invalid weigh ticket");
      received = addKg(...ts.map((x) => x!.mass_kg!));
    }
    if (!isKg(received) || grams(received) <= 0n) return validation([{ field: "mass_kg", reason: "Nhập khối lượng nhận hoặc phiếu cân HUB_IN" }]);
    if (cmpKg(received, t.dispatched_kg!) > 0) return problem(422, "hub.negative_loss", "Received more than dispatched");
    const src = t.source_lots ?? [];
    let left = grams(received);
    const out = src.map((s, i) => {
      const share = i === src.length - 1 ? left : (grams(received) * grams(s.mass_kg)) / grams(t.dispatched_kg!);
      left -= share;
      const orig = findLot(s.lot_id);
      const id = newId(3);
      const rec: LotRec = {
        lot: { id, facility_id: t.to_facility_id, code: `${orig?.lot.code ?? "LO"}-CK`, product: orig?.lot.product ?? "straw_bale", origin: "receipt", batch_id: null, initial_kg: kgOf(share), moisture_pct: orig?.lot.moisture_pct ?? null, sources: [{ kind: "lot", ref: s.lot_id, ratio: "1.000000" }], event_time: nowIso(), recorded_at: nowIso() },
        movements: [],
      };
      lots.push(rec);
      pushMove(rec, "receive", kgOf(share), nowIso(), ["transfer", t.id!]);
      remember("lot", id, rec.lot.code);
      return { source_lot_id: s.lot_id, lot_id: id, mass_kg: kgOf(share) };
    });
    t.status = "received";
    t.received_kg = normKg(received);
    t.loss_kg = subKg(t.dispatched_kg!, received);
    t.received_lots = out;
    t.received_at = nowIso();
    return created(t);
  }),
  route("GET", "/v1/hub/transfers/:id", ({ params }) => {
    const t = transfers.find((x) => x.id === params[0]);
    return t ? json(t) : problem(404, "hub.transfer_not_found", "Transfer not found");
  }),
  route("GET", "/v1/hub/lots/:id/measurements", ({ params }) => {
    const id = params[0]!;
    const l = findLot(id);
    const hin = effectiveTicket(id, "HUB_IN");
    const hout = effectiveTicket(id, "HUB_OUT");
    if (!l && !hin && !lotRolls[id]) return problem(404, "hub.lot_not_found", "Lot not found");
    const weigh = activeParams("weigh");
    const recon = reconciliations.find((r) => r.lot_ref === id);
    const link = (t: typeof hin, point: string) =>
      t ? { id: newId(28), point, ticket_id: t.id, ticket_source: t.source, measured_kg: t.mass_kg, book_kg: point === "HUB_IN" ? l?.lot.initial_kg ?? null : null, diff_kg: point === "HUB_IN" && l ? subKg(t.mass_kg!, l.lot.initial_kg!) : null, outcome: point === "HUB_IN" ? (l ? "receipt" : "pending") : "dispatch_linked", param_version: weigh.version, event_time: t.event_time } : null;
    const rolls = lotRolls[id];
    return json({
      lot_ref: id,
      hub_in: hin ? { ticket_id: hin.id, measured_kg: hin.mass_kg, moisture_pct: hin.moisture_pct, status: l ? "receipt" : "pending", basis: "ticket", book_kg: l?.lot.initial_kg ?? null, dispatch_id: null } : null,
      hub_out: hout ? { ticket_id: hout.id, measured_kg: hout.mass_kg, moisture_pct: null, status: "dispatch_linked", basis: "ticket", book_kg: null, dispatch_id: null } : null,
      links: [link(hin, "HUB_IN"), link(hout, "HUB_OUT")].filter(Boolean),
      expected_rolls: rolls ? { boat_load: rolls.rolls } : {},
      expected_kg: rolls ? kgOf(grams((weigh.params as typeof DEFAULT_WEIGH_V1).kg_per_roll_default) * BigInt(rolls.rolls)) : null,
      weigh_param_version: weigh.version,
      loss: recon?.route === "HUB" && recon.internal ? { reconciliation_id: recon.id, route: "HUB", hub_in_kg: recon.hub_in_kg, hub_out_kg: recon.hub_out_kg, expected_loss_kg: recon.internal.expected_loss_kg, abnormal_loss_kg: recon.internal.abnormal_loss_kg, outcome: recon.outcome, event_time: recon.event_time } : null,
    });
  }),
  route("GET", "/v1/hub/facilities/:id/losses", ({ params }) => {
    const f = facilityOr404(params[0]!);
    if (f instanceof Response) return f;
    const ids = new Set(lots.filter((l) => l.lot.facility_id === f.id).map((l) => l.lot.id));
    const recs = reconciliations.filter((r, i, all) => ids.has(r.lot_ref!) && r.route === "HUB" && all.findIndex((x) => x.lot_ref === r.lot_ref) === i);
    const sum = (k: (r: (typeof recs)[number]) => string | null | undefined) => addKg("0", ...recs.map((r) => k(r) ?? "0"));
    return json({
      facility_id: f.id,
      lots: recs.length,
      loss_alerts: recs.filter((r) => r.internal?.status === "abnormal" || r.internal?.status === "gain").length,
      hub_in_kg: sum((r) => r.hub_in_kg),
      hub_out_kg: sum((r) => r.hub_out_kg),
      expected_loss_kg: sum((r) => r.internal?.expected_loss_kg),
      abnormal_loss_kg: sum((r) => r.internal?.abnormal_loss_kg),
    });
  }),
];

// --------------------------------------------------------------------------
// Bề mặt quản trị (MFA)
// --------------------------------------------------------------------------
function paramOut(v: HubParamRec) {
  return { ...v };
}

function decideParam(ctx: Ctx): Response {
  const { params, body, persona } = ctx;
  if (!hasRole(persona, APPR)) return forbidden();
  const v = hubParams.find((p) => p.param_set === params[0] && p.version === Number(params[1]));
  if (!v) return problem(404, "hub.param_version_not_found", "Param version not found");
  if (v.state !== "pending") return problem(409, "hub.version_not_pending", "Version not pending");
  if (v.proposed_by === persona.actor) return selfApproval();
  const decision = str(body, "decision");
  if (decision === "rejected" && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ chối cần lý do ≥ 10 ký tự" }]);
  v.state = decision === "approved" ? "approved" : "rejected";
  v.decided_by = persona.actor;
  v.decided_at = nowIso();
  // Bộ mới nhất được duyệt đứng đầu danh sách → là bộ đang áp dụng.
  hubParams.sort((a, b) => b.version - a.version);
  return created(paramOut(v));
}

export const hubAdminRoutes: Route[] = [
  route("POST", "/v1/hub/facilities", ({ body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const bad = checkSpec(body);
    if (bad) return bad;
    const code = str(body, "code");
    if (facilities.some((f) => f.current?.code === code || f.versions.some((v) => v.code === code))) return problem(409, "hub.code_taken", "Code taken");
    const id = newId(1);
    const v = versionOf(id, body, 1, persona.actor, null, null);
    facilities.push({ id, current: null, versions: [v] });
    return created(v);
  }),
  route("POST", "/v1/hub/facilities/:id/versions", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const f = facilities.find((x) => x.id === params[0]);
    if (!f) return problem(404, "hub.facility_not_found", "Facility not found");
    const bad = checkSpec(body);
    if (bad) return bad;
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ phiên bản 2 bắt buộc lý do ≥ 10 ký tự" }]);
    if (f.versions.some((v) => v.state === "pending")) return problem(409, "hub.pending_version_exists", "Pending version exists");
    const top = Math.max(...f.versions.map((v) => v.version ?? 0));
    const v = versionOf(f.id, body, top + 1, persona.actor, str(body, "reason"), f.current?.version_id ?? null);
    f.versions.unshift(v);
    return created(v);
  }),
  route("GET", "/v1/hub/facilities/:id/versions", ({ params }) => {
    const f = facilities.find((x) => x.id === params[0]);
    return f ? json({ items: f.versions }) : problem(404, "hub.facility_not_found", "Facility not found");
  }),
  route("POST", "/v1/hub/facilities/:id/versions/:version/decision", ({ params, body, persona }) => {
    if (!hasRole(persona, APPR)) return forbidden();
    const f = facilities.find((x) => x.id === params[0]);
    const v = f?.versions.find((x) => x.version === Number(params[1]));
    if (!f || !v) return problem(404, "hub.version_not_found", "Version not found");
    if (v.state !== "pending") return problem(409, "hub.version_not_pending", "Version not pending");
    if (v.proposed_by === persona.actor) return selfApproval();
    const decision = str(body, "decision");
    if (decision === "rejected" && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ chối cần lý do ≥ 10 ký tự" }]);
    v.state = decision === "approved" ? "approved" : "rejected";
    v.decided_by = persona.actor;
    v.decided_at = nowIso();
    const publications = [];
    if (v.state === "approved") {
      const cap = v.capacity_kg ?? "0.000";
      f.current = {
        id: f.id,
        code: v.code,
        name: v.name,
        kind: v.kind,
        parent_facility_id: null,
        location: v.location,
        province_code: v.province_code,
        status: v.status,
        capacity_kg: cap,
        capacity_tons: (Number(cap) / 1000).toFixed(3),
        effective_from: v.effective_from,
        version: v.version,
        version_id: v.id,
        proposed_by: v.proposed_by,
        approved_by: persona.actor,
        approved_at: v.decided_at,
      };
      for (const contract of ["hub_facility", "hub_capacity"] as const) {
        const x = {
          id: newId(13),
          contract,
          contract_version: 1,
          event_type: `mkg.hub.${contract === "hub_facility" ? "facility" : "capacity"}.published`,
          schema_version: 1,
          subject_ref: f.id,
          seq: hubPublications.filter((p) => p.subject_ref === f.id).length + 1,
          payload: opaque(contract === "hub_facility" ? { facility_ref: f.id, code: v.code, name: v.name, kind: v.kind, province_code: v.province_code, status: v.status } : { facility_ref: f.id, capacity_tons: f.current.capacity_tons, as_of: nowIso() }),
          event_time: nowIso(),
          recorded_at: nowIso(),
        };
        hubPublications.unshift(x);
        publications.push(x);
      }
    }
    return created({ version: v, facility: f.current, publications });
  }),
  route("POST", "/v1/hub/projections/rebuild", () => json({ lot_balances: lots.length })),
  route("GET", "/v1/hub/params/:set", ({ params }) => {
    const set = params[0] as "risk" | "weigh";
    if (set !== "risk" && set !== "weigh") return problem(404, "hub.param_set_not_found", "Param set not found");
    const a = activeParams(set);
    return json({ param_set: set, active_version: a.version, active: a.params, items: hubParams.filter((p) => p.param_set === set).map(paramOut) });
  }),
  route("POST", "/v1/hub/params/:set/versions", ({ params, body, persona }) => {
    if (!hasRole(persona, OP)) return forbidden();
    const set = params[0] as "risk" | "weigh";
    if (set !== "risk" && set !== "weigh") return problem(404, "hub.param_set_not_found", "Param set not found");
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Ít nhất 10 ký tự" }]);
    const p = (body.params ?? {}) as Record<string, unknown>;
    const base = set === "risk" ? DEFAULT_RISK_V2 : DEFAULT_WEIGH_V1;
    const unknown = Object.keys(p).filter((k) => !(k in base));
    if (unknown.length) return validation(unknown.map((k) => ({ field: `params.${k}`, reason: "Khóa lạ" })));
    const merged = { ...base, ...p };
    if (set === "risk") {
      const err = validateRisk(merged as RiskParamsV2);
      if (err) return validation([{ field: "params", reason: err }]);
    }
    if (hubParams.some((x) => x.param_set === set && x.state === "pending")) return problem(409, "hub.param_pending_exists", "Pending version exists");
    const v: HubParamRec = {
      id: newId(10),
      param_set: set,
      version: Math.max(0, ...hubParams.filter((x) => x.param_set === set).map((x) => x.version)) + 1,
      rule_version: set === "risk" ? "hub.risk.v2" : "hub.weigh.v1",
      params: merged,
      reason: str(body, "reason"),
      effective_from: str(body, "effective_from") || nowIso(),
      proposed_by: persona.actor,
      state: "pending",
      decided_by: null,
      decided_at: null,
      event_time: nowIso(),
    };
    hubParams.unshift(v);
    return created(paramOut(v));
  }),
  route("POST", "/v1/hub/params/:set/versions/:version/decision", decideParam),
];
