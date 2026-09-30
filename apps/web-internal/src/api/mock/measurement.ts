// Backend giả lập mkg/measurement — theo contracts/openapi/mkg-internal.yaml và DTO backend (handler/dto.go).
import type { Dispute, MeasurementParam, ScaleDevice, WeighingSession, WeighTicket } from "../types";
import { grams, isKg, isPct, kgOf, normKg } from "@/lib/kg";
import { remember } from "@/lib/recent";
import {
  devices,
  disputes,
  effectiveTicket,
  findLot,
  imports,
  lossNorms,
  lotRolls,
  measurementPublications,
  newId,
  publish,
  reconciliations,
  remainingOf,
  runReconcile,
  sessions,
  terms,
  termsOf,
  tickets,
  tolerances,
} from "./db";
import { created, forbidden, hasRole, json, nowIso, pageSlice, problem, route, selfApproval, str, validation, UUID_RE, type Ctx, type Route } from "./http";

const SUP = "scale_supervisor";
const DEV_APPR = "device_approver";
const RECON = "reconciliation_officer";
const TOL_APPR = "tolerance_approver";
const DISPUTE = "dispute_officer";

function lotTickets(lotRef: string) {
  const list = tickets.filter((t) => t.lot_ref === lotRef);
  const eff = new Set((["HUB_IN", "HUB_OUT", "CUSTOMER_IN"] as const).map((p) => effectiveTicket(lotRef, p)?.id).filter(Boolean));
  const point = (p: WeighTicket["point"]) => {
    const t = effectiveTicket(lotRef, p);
    return t ? { mass_kg: t.mass_kg, ticket_id: t.id, event_time: t.event_time } : null;
  };
  const cin = effectiveTicket(lotRef, "CUSTOMER_IN");
  return {
    mass: { lot_ref: lotRef, route: cin?.route ?? "HUB", hub_in: point("HUB_IN"), hub_out: point("HUB_OUT"), customer_in: point("CUSTOMER_IN"), as_of: nowIso() },
    tickets: list.map((t) => ({ ...t, effective: eff.has(t.id) })),
  };
}

function autoReconcile(lotRef: string, actor: string) {
  const r = runReconcile(lotRef, actor, nowIso());
  return r;
}

function paramList(list: MeasurementParam[], withDefault: boolean) {
  return json({
    items: list,
    ...(withDefault
      ? { provisional_default: { label: "provisional-default", note: "Chưa có phiên bản duyệt có hiệu lực → dùng mặc định tạm (prototype, chờ khách C-4)", commercial_flag_pct: "5.00", commercial_critical_pct: "15.00" } }
      : {}),
  });
}

function decideParamOf(list: MeasurementParam[]) {
  return ({ params, body, persona }: Ctx): Response => {
    if (!hasRole(persona, TOL_APPR)) return forbidden();
    const v = list.find((x) => x.version === Number(params[0]));
    if (!v) return problem(404, "measurement.version_not_found", "Version not found");
    if (v.status !== "pending") return problem(409, "measurement.not_pending", "Not pending");
    if (v.proposed_by === persona.actor) return selfApproval();
    const d = str(body, "decision");
    if (d === "rejected" && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ chối cần lý do ≥ 10 ký tự" }]);
    v.status = d === "approved" ? "approved" : "rejected";
    v.decided_by = persona.actor;
    v.decided_at = nowIso();
    list.sort((a, b) => (b.version ?? 0) - (a.version ?? 0));
    return created(v);
  };
}

function checkPcts(body: Record<string, unknown>, fields: string[]): Response | null {
  const errs = fields.filter((f) => !isPct(str(body, f))).map((f) => ({ field: f, reason: "Phần trăm 0..100, tối đa 2 chữ số" }));
  return errs.length ? validation(errs) : null;
}

function newParam(list: MeasurementParam[], kind: "tolerance" | "loss_norm", values: MeasurementParam["values"], body: Record<string, unknown>, actor: string): MeasurementParam {
  const v: MeasurementParam = {
    id: newId(14),
    kind,
    version: Math.max(0, ...list.map((x) => x.version ?? 0)) + 1,
    status: "pending",
    effective_from: str(body, "effective_from"),
    provisional: body.provisional !== false,
    reason: str(body, "reason"),
    proposed_by: actor,
    decided_by: null,
    decided_at: null,
    values,
  };
  list.unshift(v);
  return v;
}

export const measurementAppRoutes: Route[] = [
  route("GET", "/v1/measurement/devices", ({ url }) => json(pageSlice(devices, url))),
  route("GET", "/v1/measurement/devices/:device_id", ({ params }) => {
    const eps = devices.filter((d) => d.device_id === decodeURIComponent(params[0]!));
    return eps.length ? json({ device_id: eps[0]!.device_id, epochs: eps }) : problem(404, "measurement.device_not_found", "Device not found");
  }),
  route("POST", "/v1/measurement/sessions", ({ body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const d = devices.find((x) => x.device_id === str(body, "device_id") && x.epoch === body.epoch);
    if (!d || d.status !== "active") return problem(409, "measurement.device_not_active", "Device epoch not active");
    const lotRef = str(body, "lot_ref");
    if (!UUID_RE.test(lotRef)) return validation([{ field: "lot_ref", reason: "UUID" }]);
    if (typeof body.counter_from !== "number" || body.counter_from < 0) return validation([{ field: "counter_from", reason: "Số nguyên ≥ 0" }]);
    if (sessions.some((s) => s.device_id === d.device_id && s.status === "open")) return problem(409, "measurement.session_conflict", "Device has an open session");
    const s: WeighingSession = {
      id: str(body, "id") || newId(16),
      lot_ref: lotRef,
      point: d.point,
      route: "HUB",
      facility_ref: d.facility_ref,
      device_id: d.device_id,
      epoch: d.epoch,
      counter_from: body.counter_from,
      counter_to: null,
      status: "open",
      moisture_pct: null,
      tare_kg: null,
      opened_at: str(body, "event_time") || nowIso(),
      closed_at: null,
    };
    sessions.unshift(s);
    remember("session", s.id!, `${findLot(lotRef)?.lot.code ?? lotRef.slice(-6)} · ${s.point}`);
    return created(s);
  }),
  route("GET", "/v1/measurement/sessions/:id", ({ params }) => {
    const s = sessions.find((x) => x.id === params[0]);
    return s ? json(s) : problem(404, "measurement.session_not_found", "Session not found");
  }),
  route("POST", "/v1/measurement/sessions/:id/close", ({ params, body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const s = sessions.find((x) => x.id === params[0]);
    if (!s) return problem(404, "measurement.session_not_found", "Session not found");
    if (s.status !== "open") return problem(409, "measurement.session_closed", "Session closed");
    const to = body.counter_to;
    if (typeof to !== "number" || to <= (s.counter_from ?? 0)) return validation([{ field: "counter_to", reason: "Phải lớn hơn counter_from" }]);
    const tare = str(body, "tare_kg");
    if (tare && !isKg(tare)) return validation([{ field: "tare_kg", reason: "Khối lượng kg" }]);
    s.status = "closed";
    s.counter_to = to;
    s.tare_kg = tare ? normKg(tare) : null;
    s.moisture_pct = str(body, "moisture_pct") || null;
    s.closed_at = str(body, "event_time") || nowIso();
    // ingest tích phân cửa sổ (weigh.integrate.v1, tạm C-6): tịnh = trung vị số đo ổn định − bì.
    const hubLot = findLot(s.lot_ref!);
    const gross = hubLot ? remainingOf(hubLot) : "25000.000";
    const net = kgOf(grams(gross) - (tare ? grams(tare) : 0n));
    const t: WeighTicket = {
      id: newId(17),
      lot_ref: s.lot_ref,
      point: s.point as WeighTicket["point"],
      route: "HUB",
      source: "edge",
      mass_kg: grams(net) > 0n ? net : gross,
      moisture_pct: s.moisture_pct ?? null,
      moisture_state: s.moisture_pct ? "as_received" : "not_measured",
      rule_version: "weigh.integrate.v1",
      window: { device_id: s.device_id!, epoch: s.epoch!, counter_from: s.counter_from!, counter_to: to, readings_used: Math.max(1, to - s.counter_from! - 3), readings_total: to - s.counter_from! + 1 },
      evidence_ref: null,
      supersedes_id: null,
      effective: true,
      session_ref: s.id,
      event_time: s.closed_at,
      recorded_at: nowIso(),
      seq: tickets.length + 1,
    };
    tickets.push(t);
    autoReconcile(s.lot_ref!, persona.actor);
    return created(s);
  }),
  route("GET", "/v1/measurement/tickets/:id", ({ params }) => {
    const t = tickets.find((x) => x.id === params[0]);
    return t ? json(t) : problem(404, "measurement.ticket_not_found", "Ticket not found");
  }),
  route("GET", "/v1/measurement/lots/:id/tickets", ({ params }) => {
    const id = params[0]!;
    if (!tickets.some((t) => t.lot_ref === id) && !findLot(id)) return problem(404, "measurement.lot_not_found", "Lot not found");
    return json(lotTickets(id));
  }),
  route("POST", "/v1/measurement/lots/:id/tickets", ({ params, body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const lotRef = params[0]!;
    const point = (str(body, "point") || "CUSTOMER_IN") as WeighTicket["point"];
    const errs: { field: string; reason: string }[] = [];
    if (!isKg(str(body, "mass_kg")) || grams(str(body, "mass_kg")) <= 0n) errs.push({ field: "mass_kg", reason: "Khối lượng kg > 0" });
    if (!str(body, "weighed_at")) errs.push({ field: "weighed_at", reason: "Bắt buộc" });
    if (!UUID_RE.test(str(body, "evidence_ref"))) errs.push({ field: "evidence_ref", reason: "Bắt buộc bằng chứng (UUID tệp)" });
    if (str(body, "reason").length < 10) errs.push({ field: "reason", reason: "Ít nhất 10 ký tự" });
    const m = str(body, "moisture_pct");
    if (m && !isPct(m)) errs.push({ field: "moisture_pct", reason: "Phần trăm" });
    if (errs.length) return validation(errs);
    const prev = effectiveTicket(lotRef, point);
    const supersedes = str(body, "supersedes_id") || null;
    if (prev && prev.id !== supersedes) return problem(422, "measurement.invalid_ticket", "Point already has an effective ticket — submit a correction (supersedes_id + correction_reason)");
    if (supersedes && str(body, "correction_reason").length < 10) return validation([{ field: "correction_reason", reason: "Ít nhất 10 ký tự" }]);
    const t: WeighTicket = {
      id: str(body, "id") || newId(17),
      lot_ref: lotRef,
      point,
      route: (str(body, "route") || "HUB") as WeighTicket["route"],
      source: "manual_evidence",
      mass_kg: normKg(str(body, "mass_kg")),
      moisture_pct: m || null,
      moisture_state: (str(body, "moisture_state") || (m ? "as_received" : "not_measured")) as WeighTicket["moisture_state"],
      rule_version: "measurement.ticket.v1",
      window: null,
      evidence_ref: str(body, "evidence_ref"),
      customer_ref: str(body, "customer_ref") || null,
      reason: str(body, "reason"),
      supersedes_id: supersedes,
      correction_reason: str(body, "correction_reason") || null,
      effective: true,
      event_time: str(body, "weighed_at"),
      recorded_at: nowIso(),
      seq: tickets.length + 1,
    };
    tickets.push(t);
    remember("lot", lotRef, findLot(lotRef)?.lot.code);
    const reconciliation = autoReconcile(lotRef, persona.actor);
    return created({ ticket: t, reconciliation });
  }),
  route("POST", "/v1/measurement/lots/:id/terms", ({ params, body, persona }) => {
    if (!hasRole(persona, RECON)) return forbidden();
    const basis = str(body, "pricing_basis");
    if (basis !== "per_tonne" && basis !== "per_bale") return validation([{ field: "pricing_basis", reason: "per_tonne | per_bale" }]);
    const cur = terms.find((t) => t.lot_ref === params[0]);
    if (cur && cur.pricing_basis !== basis && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Đổi cách tính giá cần lý do ≥ 10 ký tự" }]);
    if (cur) cur.pricing_basis = basis;
    else terms.push({ lot_ref: params[0]!, pricing_basis: basis, contract_ref: str(body, "contract_ref") || null, customer_ref: str(body, "customer_ref") || null });
    return created({ id: newId(19), ...termsOf(params[0]!), rule_version: "measurement.terms.v1" });
  }),
  route("POST", "/v1/measurement/lots/:id/reconciliations", ({ params, persona }) => {
    if (!hasRole(persona, RECON)) return forbidden();
    const prev = reconciliations.find((r) => r.lot_ref === params[0]);
    const r = runReconcile(params[0]!, persona.actor, nowIso());
    if (!r) return problem(409, "measurement.reconciliation_incomplete", "Reconciliation incomplete");
    // Đầu vào không đổi so với lần trước → trả lần trước (200).
    if (prev && JSON.stringify([prev.hub_in_kg, prev.hub_out_kg, prev.customer_in_kg, prev.pricing_basis, prev.tolerance_version, prev.roll_check]) === JSON.stringify([r.hub_in_kg, r.hub_out_kg, r.customer_in_kg, r.pricing_basis, r.tolerance_version, r.roll_check])) {
      reconciliations.shift();
      return json(prev);
    }
    return created(r);
  }),
  route("GET", "/v1/measurement/lots/:id/reconciliations", ({ params }) => {
    const items = reconciliations.filter((r) => r.lot_ref === params[0]);
    return json({ latest: items[0], items });
  }),
  route("GET", "/v1/measurement/lots/:id/yield", ({ params }) => {
    const r = reconciliations.find((x) => x.lot_ref === params[0]);
    const cin = effectiveTicket(params[0]!, "CUSTOMER_IN");
    const direct = cin?.route === "DIRECT";
    const basis = direct ? cin?.mass_kg : effectiveTicket(params[0]!, "HUB_IN")?.mass_kg;
    if (!basis) return problem(409, "measurement.no_hub_in", "No HUB_IN ticket");
    const rolls = lotRolls[params[0]!]?.rolls ?? null;
    return json({
      lot_ref: params[0],
      route: direct ? "DIRECT" : "HUB",
      basis_point: direct ? "CUSTOMER_IN" : "HUB_IN",
      basis_kg: basis,
      rolls,
      yield_kg_per_roll: rolls ? kgOf(grams(basis) / BigInt(rolls)) : null,
      aggregated: direct,
      reconciliation_id: r?.id ?? null,
    });
  }),
  route("POST", "/v1/measurement/lots/:id/mrv-evidence", ({ params, persona }) => {
    if (!hasRole(persona, RECON)) return forbidden();
    const hin = effectiveTicket(params[0]!, "HUB_IN");
    if (!hin) return problem(409, "measurement.no_hub_in", "No HUB_IN");
    const hubLot = findLot(params[0]!);
    const plots = hubLot?.lot.sources?.filter((s) => s.kind === "plot" || s.kind === "roll_count") ?? [];
    if (!plots.length) return problem(409, "measurement.no_plot_sources", "No plot sources");
    const t = Number(hin.mass_kg) / 1000;
    const bucket = t < 50 ? "0-50t" : t < 100 ? "50-100t" : "≥100t";
    const x = publish(measurementPublications, params[0]!, "hub_capacity", "mkg.measurement.mrv_collection_evidence.published", { lot_ref: params[0], mass_bucket: bucket, plots: plots.map((p) => p.ref) }, nowIso());
    (x as { contract: string }).contract = "mrv_collection_evidence";
    return created({ publications: [x] });
  }),
  route("GET", "/v1/measurement/lots/:id/publications", ({ params }) => json({ items: measurementPublications[params[0]!] ?? [] })),
  route("POST", "/v1/measurement/customer-weight-imports", ({ body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const channel = str(body, "channel");
    const rows = Array.isArray(body.rows) ? (body.rows as Record<string, unknown>[]) : [];
    const errs: { field: string; reason: string }[] = [];
    if (!["file", "api", "manual"].includes(channel)) errs.push({ field: "channel", reason: "file | api | manual" });
    if ((channel === "file" || channel === "manual") && !UUID_RE.test(str(body, "evidence_ref"))) errs.push({ field: "evidence_ref", reason: "Bắt buộc với file và manual" });
    if (channel === "manual" && str(body, "reason").length < 10) errs.push({ field: "reason", reason: "Bắt buộc với manual (≥ 10 ký tự)" });
    if (!rows.length || rows.length > 200) errs.push({ field: "rows", reason: "1..200 dòng" });
    rows.forEach((r, i) => {
      if (!UUID_RE.test(str(r, "lot_ref"))) errs.push({ field: `rows[${i}].lot_ref`, reason: "UUID lô" });
      if (!isKg(str(r, "mass_kg")) || grams(str(r, "mass_kg")) <= 0n) errs.push({ field: `rows[${i}].mass_kg`, reason: "Khối lượng kg > 0" });
      if (!str(r, "weighed_at")) errs.push({ field: `rows[${i}].weighed_at`, reason: "Bắt buộc" });
      else if (effectiveTicket(str(r, "lot_ref"), "CUSTOMER_IN")) errs.push({ field: `rows[${i}].lot_ref`, reason: "Lô đã có phiếu CUSTOMER_IN hiệu lực — sửa bằng phiếu bù" });
    });
    // Một dòng sai → cả lần bị từ chối.
    if (errs.length) return validation(errs);
    const ids: string[] = [];
    for (const r of rows) {
      const t: WeighTicket = {
        id: newId(17),
        lot_ref: str(r, "lot_ref"),
        point: "CUSTOMER_IN",
        route: (str(r, "route") || "HUB") as WeighTicket["route"],
        source: channel === "manual" ? "manual_evidence" : "customer_integration",
        mass_kg: normKg(str(r, "mass_kg")),
        moisture_pct: str(r, "moisture_pct") || null,
        moisture_state: str(r, "moisture_pct") ? "as_received" : "not_measured",
        rule_version: "measurement.customer_import.v1",
        window: null,
        evidence_ref: str(body, "evidence_ref") || null,
        customer_ref: str(body, "customer_ref") || null,
        reason: str(body, "reason") || `Phiếu khách số ${str(r, "customer_ticket_no") || "—"}`,
        supersedes_id: null,
        effective: true,
        event_time: str(r, "weighed_at"),
        recorded_at: nowIso(),
        seq: tickets.length + 1,
      };
      tickets.push(t);
      ids.push(t.id!);
      remember("lot", t.lot_ref!, findLot(t.lot_ref!)?.lot.code);
    }
    const recs = Array.from(new Set(rows.map((r) => str(r, "lot_ref"))))
      .map((l) => autoReconcile(l, persona.actor))
      .filter(Boolean);
    const im = {
      id: newId(29),
      channel: channel as "file" | "api" | "manual",
      customer_ref: str(body, "customer_ref") || null,
      evidence_ref: str(body, "evidence_ref") || null,
      file_sha256: str(body, "file_sha256") || null,
      rows: rows.length,
      ticket_ids: ids,
      provisional: true,
      reason: str(body, "reason") || null,
      recorded_at: nowIso(),
    };
    imports.unshift(im);
    return created({ import: im, reconciliations: recs });
  }),
  route("GET", "/v1/measurement/customer-weight-imports/:id", ({ params }) => {
    const im = imports.find((x) => x.id === params[0]);
    return im ? json(im) : problem(404, "measurement.import_not_found", "Import not found");
  }),
  route("GET", "/v1/measurement/disputes", ({ url }) => {
    const st = url.searchParams.get("status");
    const list = disputes.filter((d) => !st || d.status === st).map((d) => ({ ...d, events: undefined }));
    return json(pageSlice(list, url));
  }),
  route("GET", "/v1/measurement/disputes/:id", ({ params }) => {
    const d = disputes.find((x) => x.id === params[0]);
    return d ? json(d) : problem(404, "measurement.dispute_not_found", "Dispute not found");
  }),
  route("POST", "/v1/measurement/disputes/:id/actions", ({ params, body, persona }) => {
    if (!hasRole(persona, DISPUTE)) return forbidden();
    const d = disputes.find((x) => x.id === params[0]);
    if (!d) return problem(404, "measurement.dispute_not_found", "Dispute not found");
    const action = str(body, "action") as "add_evidence" | "start_review" | "resolve" | "cancel";
    const note = str(body, "note");
    const refs = Array.isArray(body.evidence_refs) ? (body.evidence_refs as string[]) : [];
    const closed = d.status === "resolved" || d.status === "cancelled";
    const transition = (): Dispute["status"] | Response => {
      if (closed) return problem(409, "measurement.dispute_transition", "Dispute closed");
      switch (action) {
        case "add_evidence":
          if (!refs.length || refs.some((r) => !UUID_RE.test(r))) return validation([{ field: "evidence_refs", reason: "Ít nhất một UUID bằng chứng" }]);
          return d.status;
        case "start_review":
          return d.status === "open" ? "under_review" : problem(409, "measurement.dispute_transition", "Only open → under_review");
        case "resolve": {
          if (d.status !== "under_review") return problem(409, "measurement.dispute_transition", "Only under_review → resolved");
          const res = str(body, "resolution");
          if (!["customer_weight", "mkg_weight", "agreed_weight"].includes(res)) return validation([{ field: "resolution", reason: "Chọn cách giải quyết" }]);
          if (res === "agreed_weight" && !isKg(str(body, "agreed_kg"))) return validation([{ field: "agreed_kg", reason: "Khối lượng thỏa thuận" }]);
          if (note.length < 10) return validation([{ field: "note", reason: "Ghi chú ≥ 10 ký tự" }]);
          d.resolution = res as Dispute["resolution"];
          d.agreed_kg = res === "agreed_weight" ? normKg(str(body, "agreed_kg")) : res === "customer_weight" ? d.customer_in_kg : d.hub_out_kg;
          return "resolved";
        }
        case "cancel":
          return note.length < 10 ? validation([{ field: "note", reason: "Lý do huỷ ≥ 10 ký tự" }]) : "cancelled";
        default:
          return validation([{ field: "action", reason: "add_evidence | start_review | resolve | cancel" }]);
      }
    };
    const next = transition();
    if (next instanceof Response) return next;
    d.status = next;
    if (refs.length) d.evidence_refs = [...d.evidence_refs, ...refs];
    d.events = [
      ...(d.events ?? []),
      { seq: (d.events?.length ?? 0) + 1, action, status_after: next, evidence_refs: refs.length ? refs : null, resolution: d.resolution, agreed_kg: action === "resolve" ? d.agreed_kg : null, note: note || null, event_time: nowIso(), actor_ref: persona.actor },
    ];
    return created(d);
  }),
  route("GET", "/v1/measurement/tolerance-versions", () => paramList(tolerances, true)),
  route("GET", "/v1/measurement/loss-norm-versions", () => paramList(lossNorms, false)),
];

export const measurementAdminRoutes: Route[] = [
  route("POST", "/v1/measurement/devices", ({ body, persona }) => {
    if (!hasRole(persona, SUP)) return forbidden();
    const errs: { field: string; reason: string }[] = [];
    const id = str(body, "device_id");
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$/.test(id)) errs.push({ field: "device_id", reason: "3–64 ký tự, trùng CN/SAN chứng thư" });
    if (!UUID_RE.test(str(body, "facility_ref"))) errs.push({ field: "facility_ref", reason: "Chọn cơ sở" });
    if (!["HUB_IN", "HUB_OUT"].includes(str(body, "point"))) errs.push({ field: "point", reason: "HUB_IN | HUB_OUT" });
    if (!/^[A-Za-z0-9+/]{43}=$|^[A-Za-z0-9+/]{59}=$/.test(str(body, "public_key"))) errs.push({ field: "public_key", reason: "Khóa Ed25519 32 byte, base64 chuẩn" });
    if (!/^[0-9a-f]{64}$/.test(str(body, "cert_sha256"))) errs.push({ field: "cert_sha256", reason: "SHA-256 hex thường, 64 ký tự" });
    if (!str(body, "scale_model")) errs.push({ field: "scale_model", reason: "Bắt buộc (C-6)" });
    if (errs.length) return validation(errs);
    const eps = devices.filter((d) => d.device_id === id);
    if (eps.some((d) => d.status === "pending")) return problem(409, "measurement.pending_exists", "Pending epoch exists");
    const epoch = typeof body.epoch === "number" ? body.epoch : Math.max(0, ...eps.map((d) => d.epoch ?? 0)) + 1;
    if (eps.some((d) => d.epoch === epoch)) return problem(409, "measurement.device_taken", "Epoch already used");
    const d: ScaleDevice = {
      device_id: id,
      epoch,
      facility_ref: str(body, "facility_ref"),
      point: str(body, "point") as ScaleDevice["point"],
      public_key: str(body, "public_key"),
      key_id: `${id}#${epoch}`,
      cert_sha256: str(body, "cert_sha256"),
      scale_model: str(body, "scale_model"),
      status: "pending",
      proposed_by: persona.actor,
      proposed_at: nowIso(),
      decided_by: null,
      decided_at: null,
      revoked_at: null,
      revoke_reason: null,
      health: { last_seen_at: null, buffer_pct: null, clock_skew_ms: null, last_counter: null },
    };
    devices.push(d);
    return created(d);
  }),
  route("GET", "/v1/measurement/devices", ({ url }) => json(pageSlice(devices, url))),
  route("POST", "/v1/measurement/devices/:device_id/epochs/:epoch/decision", ({ params, body, persona }) => {
    if (!hasRole(persona, DEV_APPR)) return forbidden();
    const d = devices.find((x) => x.device_id === decodeURIComponent(params[0]!) && x.epoch === Number(params[1]));
    if (!d) return problem(404, "measurement.device_not_found", "Device not found");
    if (d.status !== "pending") return problem(409, "measurement.not_pending", "Not pending");
    if (d.proposed_by === persona.actor) return selfApproval();
    const dec = str(body, "decision");
    if (dec === "rejected" && str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Từ chối cần lý do ≥ 10 ký tự" }]);
    if (dec === "approved") for (const o of devices) if (o.device_id === d.device_id && o.status === "active") o.status = "superseded";
    d.status = dec === "approved" ? "active" : "rejected";
    d.decided_by = persona.actor;
    d.decided_at = nowIso();
    return created(d);
  }),
  route("POST", "/v1/measurement/devices/:device_id/epochs/:epoch/revoke", ({ params, body, persona }) => {
    if (!hasRole(persona, DEV_APPR, SUP)) return forbidden();
    const d = devices.find((x) => x.device_id === decodeURIComponent(params[0]!) && x.epoch === Number(params[1]));
    if (!d) return problem(404, "measurement.device_not_found", "Device not found");
    if (d.status !== "active" && d.status !== "pending") return problem(409, "measurement.device_not_active", "Not active");
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Ít nhất 10 ký tự" }]);
    d.status = "revoked";
    d.revoked_at = nowIso();
    d.revoke_reason = str(body, "reason");
    return created(d);
  }),
  route("POST", "/v1/measurement/tolerance-versions", ({ body, persona }) => {
    if (!hasRole(persona, RECON)) return forbidden();
    const bad = checkPcts(body, ["commercial_flag_pct", "commercial_critical_pct", "roll_flag_pct", "roll_critical_pct"]);
    if (bad) return bad;
    if (Number(str(body, "commercial_critical_pct")) < Number(str(body, "commercial_flag_pct"))) return validation([{ field: "commercial_critical_pct", reason: "phải ≥ commercial_flag_pct" }]);
    if (Number(str(body, "roll_critical_pct")) < Number(str(body, "roll_flag_pct"))) return validation([{ field: "roll_critical_pct", reason: "phải ≥ roll_flag_pct" }]);
    if (!str(body, "effective_from")) return validation([{ field: "effective_from", reason: "Bắt buộc" }]);
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Ít nhất 10 ký tự" }]);
    if (tolerances.some((t) => t.status === "pending")) return problem(409, "measurement.pending_exists", "Pending version exists");
    const f = (k: string) => Number(str(body, k)).toFixed(2);
    return created(newParam(tolerances, "tolerance", { commercial_flag_pct: f("commercial_flag_pct"), commercial_critical_pct: f("commercial_critical_pct"), roll_flag_pct: f("roll_flag_pct"), roll_critical_pct: f("roll_critical_pct") }, body, persona.actor));
  }),
  route("GET", "/v1/measurement/tolerance-versions", () => paramList(tolerances, true)),
  route("POST", "/v1/measurement/tolerance-versions/:seq/decision", decideParamOf(tolerances)),
  route("POST", "/v1/measurement/loss-norm-versions", ({ body, persona }) => {
    if (!hasRole(persona, RECON)) return forbidden();
    const bands = Array.isArray(body.bands) ? (body.bands as { moisture_from: string; moisture_to: string; loss_pct: string }[]) : [];
    if (!bands.length) return validation([{ field: "bands", reason: "Cần ít nhất một khoảng" }]);
    for (let i = 0; i < bands.length; i++) {
      const b = bands[i]!;
      if (![b.moisture_from, b.moisture_to, b.loss_pct].every((x) => typeof x === "string" && isPct(x))) return validation([{ field: `bands[${i}]`, reason: "Phần trăm 0..100" }]);
      if (Number(b.moisture_to) <= Number(b.moisture_from)) return validation([{ field: `bands[${i}].moisture_to`, reason: "Phải lớn hơn moisture_from" }]);
      if (i > 0 && b.moisture_from !== bands[i - 1]!.moisture_to) return validation([{ field: `bands[${i}].moisture_from`, reason: "Các khoảng phải liền nhau" }]);
    }
    const bad = checkPcts(body, ["default_loss_pct", "abnormal_alert_pct"]);
    if (bad) return bad;
    if (str(body, "reason").length < 10) return validation([{ field: "reason", reason: "Ít nhất 10 ký tự" }]);
    if (lossNorms.some((t) => t.status === "pending")) return problem(409, "measurement.pending_exists", "Pending version exists");
    return created(newParam(lossNorms, "loss_norm", { bands, default_loss_pct: str(body, "default_loss_pct"), abnormal_alert_pct: str(body, "abnormal_alert_pct") }, body, persona.actor));
  }),
  route("GET", "/v1/measurement/loss-norm-versions", () => paramList(lossNorms, false)),
  route("POST", "/v1/measurement/loss-norm-versions/:seq/decision", decideParamOf(lossNorms)),
  route("POST", "/v1/measurement/projections/rebuild", () => json({ lot_mass_current: new Set(tickets.map((t) => t.lot_ref)).size, dispute_current: disputes.length })),
];

