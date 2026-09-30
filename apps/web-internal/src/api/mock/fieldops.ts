// Backend giả lập mkg/fieldops — theo contracts/openapi/mkg-internal.yaml và DTO backend (handler/dto.go).
import type { CollectionOrder, Crew, HtxConfirmation, RollCount, Stage } from "../types";
import { isKg, normKg } from "@/lib/kg";
import { checkStage, STAGES } from "@/lib/rules";
import { remember } from "@/lib/recent";
import {
  buySide,
  buySideFor,
  confirmations,
  CREW,
  fieldPublications,
  HTX_COMMUNE,
  kgPerRoll,
  lotRolls,
  newId,
  orderEvent,
  orderEvents,
  orders,
  PLOT_HTX,
  publish,
  recordCount,
  rollCounts,
  shifts,
  SYSTEM_KG_PER_ROLL,
} from "./db";
import { created, forbidden, hasRole, json, nowIso, pageSlice, problem, route, selfApproval, str, validation, UUID_RE, type Route } from "./http";

const SUP = "field_supervisor";

// Sổ tổ gom giả lập (FM-06/07/08): hai tổ đang hoạt động theo cuộn/ngày, một tổ đã ngừng.
const crews: Crew[] = [
  { crew_id: CREW.t1, code: "TO-01", name: "Tổ gom Vĩnh Bình", member_refs: [], capacity_unit: "bales", capacity_per_day: "1500", service_communes: [], status: "active", version: 1 },
  { crew_id: CREW.t2, code: "TO-02", name: "Tổ gom Tân Hiệp", member_refs: [], capacity_unit: "bales", capacity_per_day: "3000", service_communes: [], status: "active", version: 1 },
  { crew_id: newId(), code: "TO-99", name: "Tổ gom đã giải thể", member_refs: [], capacity_unit: "bales", capacity_per_day: "9000", service_communes: [], status: "inactive", version: 2 },
];
const COL = "field_collector";

function orderOr404(id: string): CollectionOrder | Response {
  return orders.find((o) => o.order_id === id) ?? problem(404, "fieldops.order_not_found", "Order not found");
}

function result(o: CollectionOrder, ev: ReturnType<typeof orderEvent>, pubs: unknown[] = []) {
  return created({ order: o, event: ev, publications: pubs });
}

function checkPlan(body: Record<string, unknown>): Response | { htx: string; plots: string[]; from: string; to: string; commune: string } {
  const htx = str(body, "htx_ref");
  const plots = Array.isArray(body.plot_refs) ? (body.plot_refs as string[]).map((p) => p.trim()).filter(Boolean) : [];
  const from = str(body, "window_from");
  const to = str(body, "window_to");
  const errs: { field: string; reason: string }[] = [];
  if (!UUID_RE.test(htx)) errs.push({ field: "htx_ref", reason: "UUID HTX" });
  if (!plots.length || plots.length > 200) errs.push({ field: "plot_refs", reason: "1..200 thửa" });
  if (!/^\d{4}-\d{2}-\d{2}$/.test(from)) errs.push({ field: "window_from", reason: "Ngày YYYY-MM-DD" });
  if (!/^\d{4}-\d{2}-\d{2}$/.test(to) || to < from) errs.push({ field: "window_to", reason: "≥ window_from" });
  else if ((Date.parse(to) - Date.parse(from)) / 86_400_000 > 90) errs.push({ field: "window_to", reason: "Khung tối đa 90 ngày" });
  if (errs.length) return validation(errs);
  if (!HTX_COMMUNE[htx]) return problem(422, "fieldops.htx_not_found", "HTX not found");
  for (const p of plots) {
    if (!PLOT_HTX[p]) return problem(422, "fieldops.plot_not_found", "Plot not found", { errors: [{ field: "plot_refs", reason: p }] });
    if (PLOT_HTX[p] !== htx) return problem(422, "fieldops.plot_other_htx", "Plot belongs to another HTX", { errors: [{ field: "plot_refs", reason: p }] });
  }
  return { htx, plots, from, to, commune: str(body, "commune_code") || HTX_COMMUNE[htx]! };
}

function effective(orderId: string): RollCount[] {
  const list = rollCounts.filter((r) => r.order_id === orderId);
  const superseded = new Set(list.map((r) => r.supersedes_id).filter(Boolean));
  return list.filter((r) => !superseded.has(r.id));
}

function effectiveOf(orderId: string, plot: string, stage: Stage): RollCount | undefined {
  return effective(orderId).find((r) => r.plot_ref === plot && r.stage === stage);
}

function totals(orderId: string) {
  const eff = effective(orderId);
  const order_totals: Record<string, number> = {};
  for (const s of STAGES) order_totals[s] = eff.filter((r) => r.stage === s).reduce((n, r) => n + (r.rolls ?? 0), 0);
  return {
    counts: rollCounts.filter((r) => r.order_id === orderId),
    totals: eff.map((r) => ({ plot_ref: r.plot_ref, stage: r.stage, rolls: r.rolls, loose_kg: r.loose_kg ?? null, roll_count_id: r.id, discrepancy_reason: r.discrepancy_reason ?? null, event_time: r.event_time })),
    order_totals,
  };
}

/** boat_load → đối soát chiều mua + bảng kê HTX (van htx_collection_statement) + số cuộn cho measurement (RB-6). */
function afterBoatLoad(o: CollectionOrder, rc: RollCount) {
  let rec = buySideFor(o.order_id!, rc.plot_ref!);
  if (!rec) {
    rec = { order_id: o.order_id, plot_ref: rc.plot_ref, htx_ref: rc.htx_ref, statement_ref: newId(25), statement_revision: 0, mkg_rolls: 0, mkg_roll_count_id: rc.id, htx_rolls: null, outcome: null, confirmation_id: null, confirmed_revision: null, status: "pending", updated_at: nowIso() };
    buySide.push(rec);
  }
  rec.statement_revision = (rec.statement_revision ?? 0) + 1;
  rec.mkg_rolls = rc.rolls;
  rec.mkg_roll_count_id = rc.id;
  rec.status = rec.outcome === null ? "pending" : rec.outcome === "confirmed" && rec.htx_rolls === rc.rolls ? "match" : "mismatch";
  rec.updated_at = nowIso();
  const boat = effective(o.order_id!).filter((r) => r.stage === "boat_load");
  lotRolls[o.order_id!] = { rolls: boat.reduce((n, r) => n + (r.rolls ?? 0), 0), kgPerRoll: kgPerRoll[o.htx_ref!]?.kg_per_roll ?? "20" };
  const pub = publish(fieldPublications, o.order_id!, "htx_collection_statement", "mkg.fieldops.htx_collection_statement.published", {
    schedule_ref: o.order_id,
    htx_ref: o.htx_ref,
    revision: rec.statement_revision,
    plots: boat.map((r) => ({ plot_ref: r.plot_ref, rolls: r.rolls })),
  }, nowIso());
  return { rec, pubs: [pub] };
}

function countInput(body: Record<string, unknown>): Response | { rolls: number; loose: string | null } {
  const rolls = body.rolls;
  if (typeof rolls !== "number" || !Number.isInteger(rolls) || rolls < 0) return validation([{ field: "rolls", reason: "Bắt buộc số cuộn nguyên ≥ 0 (FM-09)" }]);
  const loose = str(body, "loose_kg");
  if (loose && rolls !== 0) return validation([{ field: "loose_kg", reason: "Chỉ nhập kg rơm rời khi số cuộn = 0 (FM-09)" }]);
  if (loose && !isKg(loose)) return validation([{ field: "loose_kg", reason: "Khối lượng kg" }]);
  if (rolls === 0 && !loose) return validation([{ field: "loose_kg", reason: "Số cuộn = 0 thì nhập kg rơm rời" }]);
  if (!str(body, "event_time")) return validation([{ field: "event_time", reason: "Bắt buộc" }]);
  return { rolls, loose: loose ? normKg(loose) : null };
}

export const fieldopsRoutes: Route[] = [
  route("GET", "/v1/fieldops/orders", ({ url }) => {
    const st = url.searchParams.get("status");
    const htx = url.searchParams.get("htx_ref");
    const list = orders.filter((o) => (!st || o.status === st) && (!htx || o.htx_ref === htx)).sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? ""));
    return json(pageSlice(list, url));
  }),
  route("POST", "/v1/fieldops/orders", ({ body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const p = checkPlan(body);
    if (p instanceof Response) return p;
    const id = newId(6);
    const at = nowIso();
    const o: CollectionOrder = { order_id: id, status: "planned", revision: 1, htx_ref: p.htx, commune_code: p.commune, window_from: p.from, window_to: p.to, plot_refs: p.plots, crew_ref: null, reason: null, created_by: persona.actor, approved_by: null, approved_at: null, published: false, created_at: at, updated_at: at };
    orders.push(o);
    remember("htx", p.htx);
    return result(o, orderEvent(id, "planned", persona.actor, at));
  }),
  route("GET", "/v1/fieldops/orders/:id", ({ params }) => {
    const o = orderOr404(params[0]!);
    return o instanceof Response ? o : json({ order: o, history: orderEvents[o.order_id!] ?? [] });
  }),
  route("POST", "/v1/fieldops/orders/:id/revisions", ({ params, body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "planned") return problem(409, "fieldops.order_transition", "Only planned orders can be revised");
    const p = checkPlan(body);
    if (p instanceof Response) return p;
    Object.assign(o, { htx_ref: p.htx, commune_code: p.commune, window_from: p.from, window_to: p.to, plot_refs: p.plots, revision: (o.revision ?? 1) + 1, created_by: persona.actor, updated_at: nowIso() });
    return result(o, orderEvent(o.order_id!, "revised", persona.actor, nowIso()));
  }),
  route("POST", "/v1/fieldops/orders/:id/decision", ({ params, body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "planned") return problem(409, "fieldops.order_transition", "Order not planned");
    if (o.created_by === persona.actor) return selfApproval();
    const d = str(body, "decision");
    if (d === "rejected") {
      if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ chối cần lý do ≥ 10 ký tự" }]);
      o.status = "cancelled";
      o.reason = str(body, "reason");
      o.updated_at = nowIso();
      return result(o, orderEvent(o.order_id!, "rejected", persona.actor, nowIso(), { reason: o.reason }));
    }
    o.status = "approved";
    o.approved_by = persona.actor;
    o.approved_at = nowIso();
    o.published = true;
    o.updated_at = nowIso();
    const pub = publish(fieldPublications, o.order_id!, "collection_schedule", "mkg.fieldops.collection_schedule.published", { schedule_ref: o.order_id, htx_ref: o.htx_ref, commune_code: o.commune_code, window_from: o.window_from, window_to: o.window_to, plot_refs: o.plot_refs, status: "approved" }, nowIso());
    return result(o, orderEvent(o.order_id!, "approved", persona.actor, nowIso()), [pub]);
  }),
  route("GET", "/v1/fieldops/crews", ({ url }) => {
    const status = url.searchParams.get("status");
    return json({ items: crews.filter((c) => !status || c.status === status), next_cursor: null });
  }),
  // Gợi ý tổ (proto FM-08) — giả lập tối giản: tổ đang hoạt động, đơn vị cuộn, đủ năng lực/ngày theo khung lệnh; xếp theo tỷ lệ tải rồi mã.
  route("GET", "/v1/fieldops/orders/:id/crew-suggestions", ({ params, url, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    const bales = Number(url.searchParams.get("expected_bales") ?? "");
    if (!(bales > 0)) return problem(422, "fieldops.workload_required", "Workload required");
    const days = Math.max(1, Math.round((Date.parse(o.window_to!) - Date.parse(o.window_from!)) / 86_400_000) + 1);
    const daily = bales / days;
    const view = crews
      .filter((c) => c.status === "active")
      .map((c) => ({ c, ratio: daily / Number(c.capacity_per_day) }))
      .sort((a, b) => a.ratio - b.ratio || a.c.code!.localeCompare(b.c.code!));
    const fit = view.filter((v) => v.ratio <= 1);
    const row = (v: (typeof view)[number], rank: number | null) => ({
      rank, crew_ref: v.c.crew_id, code: v.c.code, name: v.c.name, capacity_unit: v.c.capacity_unit, capacity_per_day: v.c.capacity_per_day,
      local: true, daily_load: daily.toFixed(3), peak_load: daily.toFixed(3), load_ratio: v.ratio.toFixed(4), over_day: rank ? null : o.window_from, unfit: rank ? null : "over_capacity",
    });
    const suggestions = fit.map((v, i) => row(v, i + 1));
    return json({ order_id: o.order_id, commune_code: o.commune_code, planned_from: o.window_from, planned_to: o.window_to, suggested: suggestions[0] ?? null, suggestions,
      unfit: view.filter((v) => v.ratio > 1).map((v) => row(v, null)), inactive_crews: crews.filter((c) => c.status !== "active").length, rule_version: "fieldops.crew_assignment.v1" });
  }),
  route("POST", "/v1/fieldops/orders/:id/assign", ({ params, body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "approved" && o.status !== "assigned") return problem(409, "fieldops.order_transition", "Order must be approved");
    const crew = str(body, "crew_ref");
    if (!UUID_RE.test(crew)) return validation([{ field: "crew_ref", reason: "UUID tổ gom" }]);
    const c = crews.find((x) => x.crew_id === crew);
    if (!c) return problem(422, "fieldops.crew_unknown", "Crew unknown");
    if (c.status !== "active") return problem(409, "fieldops.crew_inactive", "Crew inactive (FM-07)");
    if (!(Number(body.expected_bales) > 0)) return problem(422, "fieldops.workload_required", "Workload required");
    o.status = "assigned";
    o.crew_ref = crew;
    o.updated_at = nowIso();
    return result(o, orderEvent(o.order_id!, "assigned", persona.actor, nowIso(), { crew_ref: crew }));
  }),
  route("POST", "/v1/fieldops/orders/:id/close", ({ params, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "assigned") return problem(409, "fieldops.order_transition", "Order must be assigned");
    o.status = "closed";
    o.updated_at = nowIso();
    return result(o, orderEvent(o.order_id!, "closed", persona.actor, nowIso()));
  }),
  route("POST", "/v1/fieldops/orders/:id/cancel", ({ params, body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    if (o.status === "closed" || o.status === "cancelled") return problem(409, "fieldops.order_transition", "Order already finished");
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Ít nhất 10 ký tự" }]);
    o.status = "cancelled";
    o.reason = str(body, "reason");
    o.updated_at = nowIso();
    return result(o, orderEvent(o.order_id!, "cancelled", persona.actor, nowIso(), { reason: o.reason }));
  }),
  route("GET", "/v1/fieldops/orders/:id/roll-counts", ({ params }) => {
    const o = orderOr404(params[0]!);
    return o instanceof Response ? o : json(totals(o.order_id!));
  }),
  route("POST", "/v1/fieldops/orders/:id/roll-counts", ({ params, body, persona }) => {
    if (!hasRole(persona, COL, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    if (o.status !== "approved" && o.status !== "assigned") return problem(409, "fieldops.order_not_open", "Order not open");
    const plot = str(body, "plot_ref");
    const stage = str(body, "stage") as Stage;
    if (!STAGES.includes(stage)) return validation([{ field: "stage", reason: "bale | gather | boat_load" }]);
    if (!o.plot_refs?.includes(plot)) return problem(422, "fieldops.plot_not_in_order", "Plot not in order");
    const inp = countInput(body);
    if (inp instanceof Response) return inp;
    if (effectiveOf(o.order_id!, plot, stage)) return problem(409, "fieldops.stage_already_counted", "Stage already counted — submit a correction");
    const prevStage = STAGES[STAGES.indexOf(stage) - 1];
    const prev = prevStage ? effectiveOf(o.order_id!, plot, prevStage) : undefined;
    const reason = str(body, "discrepancy_reason");
    const chk = checkStage(stage, inp.rolls, prev?.rolls, reason);
    if (!chk.ok) return problem(chk.code === "fieldops.previous_stage_missing" ? 409 : 422, chk.code, chk.code);
    const rc = recordCount({
      id: str(body, "id") || undefined,
      order_id: o.order_id,
      plot_ref: plot,
      htx_ref: o.htx_ref,
      stage,
      rolls: inp.rolls,
      loose_kg: inp.loose,
      discrepancy_reason: chk.needsReason ? reason : null,
      supersedes_id: null,
      correction_reason: null,
      device_id: str(body, "device_id") || null,
      event_time: str(body, "event_time"),
      actor_ref: persona.actor,
    });
    const extra = stage === "boat_load" ? afterBoatLoad(o, rc) : { rec: null, pubs: [] };
    const t = totals(o.order_id!).totals.find((x) => x.roll_count_id === rc.id);
    return created({ roll_count: rc, total: t, reconciliation: extra.rec, publications: extra.pubs });
  }),
  route("POST", "/v1/fieldops/roll-counts/:id/corrections", ({ params, body, persona }) => {
    if (!hasRole(persona, COL, SUP)) return forbidden();
    const orig = rollCounts.find((r) => r.id === params[0]);
    if (!orig) return problem(404, "fieldops.roll_count_not_found", "Roll count not found");
    if (rollCounts.some((r) => r.supersedes_id === orig.id)) return problem(409, "fieldops.already_corrected", "Already corrected");
    const o = orders.find((x) => x.order_id === orig.order_id)!;
    if (str(body, "correction_reason").length < 10) return validation([{ field: "correction_reason", reason: "Bắt buộc với bản bù (≥ 10 ký tự)" }]);
    const inp = countInput(body);
    if (inp instanceof Response) return inp;
    const stage = orig.stage!;
    const idx = STAGES.indexOf(stage);
    const prev = idx > 0 ? effectiveOf(o.order_id!, orig.plot_ref!, STAGES[idx - 1]!) : undefined;
    const reason = str(body, "discrepancy_reason");
    const chk = checkStage(stage, inp.rolls, prev?.rolls, reason);
    if (!chk.ok) return problem(422, chk.code, chk.code);
    const next = STAGES[idx + 1] ? effectiveOf(o.order_id!, orig.plot_ref!, STAGES[idx + 1]!) : undefined;
    if (next && (next.rolls ?? 0) > inp.rolls) return problem(409, "fieldops.next_stage_conflict", "Next stage would exceed corrected count (FM-04)");
    const rc = recordCount({
      order_id: orig.order_id,
      plot_ref: orig.plot_ref,
      htx_ref: orig.htx_ref,
      stage,
      rolls: inp.rolls,
      loose_kg: inp.loose,
      discrepancy_reason: chk.needsReason ? reason : null,
      supersedes_id: orig.id,
      correction_reason: str(body, "correction_reason"),
      device_id: str(body, "device_id") || null,
      event_time: str(body, "event_time"),
      actor_ref: persona.actor,
    });
    const extra = stage === "boat_load" ? afterBoatLoad(o, rc) : { rec: null, pubs: [] };
    return created({ roll_count: rc, total: totals(o.order_id!).totals.find((x) => x.roll_count_id === rc.id), reconciliation: extra.rec, publications: extra.pubs });
  }),
  route("GET", "/v1/fieldops/orders/:id/htx-confirmations", ({ params }) => json({ items: confirmations.filter((c) => c.order_id === params[0]) })),
  route("POST", "/v1/fieldops/orders/:id/htx-confirmations", ({ params, body, persona }) => {
    if (!hasRole(persona, COL, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    const plot = str(body, "plot_ref");
    const rec = buySideFor(o.order_id!, plot);
    const boat = effectiveOf(o.order_id!, plot, "boat_load");
    if (!rec || !boat) return problem(409, "fieldops.nothing_to_confirm", "No boat_load count to confirm");
    const outcome = str(body, "outcome") as "confirmed" | "disputed";
    if (outcome !== "confirmed" && outcome !== "disputed") return validation([{ field: "outcome", reason: "confirmed | disputed" }]);
    const rev = body.statement_revision;
    if (typeof rev === "number" && rev !== rec.statement_revision) return problem(409, "fieldops.statement_stale", "Statement revision stale");
    const htxRolls = typeof body.htx_rolls === "number" ? body.htx_rolls : undefined;
    if (outcome === "disputed" && (htxRolls === undefined || str(body, "reason").length < 10))
      return validation([{ field: htxRolls === undefined ? "htx_rolls" : "reason", reason: "Tranh chấp cần số cuộn HTX đếm và lý do ≥ 10 ký tự" }]);
    const c: HtxConfirmation = {
      id: newId(24),
      seq: confirmations.length + 1,
      order_id: o.order_id!,
      plot_ref: plot,
      htx_ref: o.htx_ref!,
      statement_ref: rec.statement_ref!,
      statement_revision: rec.statement_revision!,
      roll_count_id: boat.id!,
      mkg_rolls: boat.rolls!,
      outcome,
      htx_rolls: outcome === "confirmed" ? boat.rolls! : htxRolls!,
      reason: str(body, "reason") || null,
      channel: "mkg_staff",
      evidence_ref: str(body, "evidence_ref") || null,
      actor_ref: persona.actor,
      event_time: str(body, "event_time") || nowIso(),
      recorded_at: nowIso(),
    };
    confirmations.push(c);
    rec.outcome = outcome;
    rec.htx_rolls = c.htx_rolls;
    rec.confirmation_id = c.id;
    rec.confirmed_revision = c.statement_revision;
    rec.status = outcome === "confirmed" ? "match" : c.htx_rolls === boat.rolls ? "match" : "mismatch";
    rec.updated_at = nowIso();
    return created({ confirmation: c, reconciliation: rec });
  }),
  route("GET", "/v1/fieldops/orders/:id/crew-shifts", ({ params }) => json({ items: shifts.filter((s) => s.order_id === params[0]) })),
  route("POST", "/v1/fieldops/orders/:id/crew-shifts", ({ params, body, persona }) => {
    if (!hasRole(persona, COL, SUP)) return forbidden();
    const o = orderOr404(params[0]!);
    if (o instanceof Response) return o;
    const kind = str(body, "kind") as "opened" | "closed";
    if (kind === "opened" && typeof body.headcount !== "number") return validation([{ field: "headcount", reason: "Bắt buộc khi mở ca" }]);
    const s = { id: newId(26), crew_ref: str(body, "crew_ref"), order_id: o.order_id!, kind, headcount: typeof body.headcount === "number" ? body.headcount : null, equipment_refs: (body.equipment_refs as string[]) ?? [], actor_ref: persona.actor, device_id: str(body, "device_id") || null, event_time: str(body, "event_time") || nowIso(), recorded_at: nowIso() };
    shifts.push(s);
    return created(s);
  }),
  route("GET", "/v1/fieldops/orders/:id/publications", ({ params }) => json({ items: fieldPublications[params[0]!] ?? [] })),
  route("GET", "/v1/fieldops/reconciliation", ({ url }) => {
    const oid = url.searchParams.get("order_id");
    const st = url.searchParams.get("status");
    const scope = buySide.filter((b) => !oid || b.order_id === oid);
    const page_counts = { pending: 0, match: 0, mismatch: 0 } as Record<string, number>;
    for (const b of scope) page_counts[b.status!] = (page_counts[b.status!] ?? 0) + 1;
    const p = pageSlice(scope.filter((b) => !st || b.status === st), url, 100, 500);
    return json({ ...p, page_counts });
  }),
  route("GET", "/v1/fieldops/htx/:id/kg-per-roll", ({ params }) => {
    const k = kgPerRoll[params[0]!];
    const v = k ?? (Object.keys(HTX_COMMUNE).includes(params[0]!) ? SYSTEM_KG_PER_ROLL : { kg_per_roll: "20.0", source: "default" as const, samples: 0 });
    return json({ htx_ref: params[0], ...v, rule_version: "fieldops.kg_per_roll.v1" });
  }),
  route("POST", "/v1/fieldops/kg-per-roll/refresh", ({ persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const k = kgPerRoll[Object.keys(HTX_COMMUNE)[2]!];
    if (!k) {
      kgPerRoll[Object.keys(HTX_COMMUNE)[2]!] = { kg_per_roll: "20.1", source: "htx", samples: 1 };
      return json({ weighed_lots_added: 1 });
    }
    return json({ weighed_lots_added: 0 });
  }),
];
