// ============================================================================
// BACKEND GIẢ LẬP (NEXT_PUBLIC_API_MOCK=1). Thay `fetch` của openapi-fetch nên toàn bộ
// middleware thật (Bearer, Idempotency-Key, parse problem+json) vẫn chạy.
// Mô phỏng các quy tắc backend để UI thể hiện đúng: lọc theo phạm vi (ADR-0009),
// POST thiếu Idempotency-Key → 400, phát lại cùng khóa → Idempotent-Replayed,
// tự phê duyệt → 403 self_approval_forbidden (R-56), Admin API cần MFA.
// ============================================================================
import type { Page } from "../pending/common";
import type { DashboardKpis } from "../pending/extension";
import type { Cooperative } from "../pending/party";
import type { ParamDecisionInput, ParamProposal, ParamProposalInput } from "../pending/masterdata";
import { uuidv7 } from "../idempotency";
import {
  ADMIN_UNITS,
  boundaries,
  catalogSummaries,
  CATALOGS,
  cghDashboard,
  COMMUNES,
  COOPERATIVES,
  coverageItems,
  DOCUMENTS,
  householdsOf,
  ORGS,
  PARAMS,
  plotsOf,
  priorityCooperatives,
  PROVINCES,
  TARGET_AREA_BY_PROVINCE,
  TASKS,
  UNREPORTED_UNITS,
  type MockCooperative,
} from "./data";
import { personaFromAuthHeader, type MockPersona } from "./personas";

const CURRENT_SEASON = "HT2026";

function traceId(): string {
  return uuidv7().replace(/-/g, "");
}

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "X-Trace-Id": traceId(), ...headers },
  });
}

function problem(status: number, code: string, title: string, extra: Record<string, unknown> = {}): Response {
  const tid = traceId();
  return new Response(
    JSON.stringify({ type: `https://errors.agrigreen.vn/${code}`, title, status, code, trace_id: tid, ...extra }),
    { status, headers: { "Content-Type": "application/problem+json", "X-Trace-Id": tid } },
  );
}

function paginate<T>(items: T[], url: URL, defaultLimit = 20): Page<T> {
  const limit = Math.min(Math.max(Number(url.searchParams.get("limit") ?? defaultLimit) || defaultLimit, 1), 200);
  const cursor = url.searchParams.get("cursor");
  const offset = cursor ? Number.parseInt(atob(cursor), 10) || 0 : 0;
  const slice = items.slice(offset, offset + limit);
  const next = offset + limit < items.length ? btoa(String(offset + limit)) : null;
  return { items: slice, next_cursor: next };
}

/** Phạm vi của persona: mã tỉnh/xã được thấy (All → null). */
function scopeUnits(p: MockPersona): Set<string> | null {
  return p.me.scope.all ? null : new Set(p.me.scope.units);
}

function inScope(p: MockPersona, provinceCode: string, communeCode?: string): boolean {
  const s = scopeUnits(p);
  if (!s) return true;
  if (communeCode) return s.has(communeCode);
  return s.has(provinceCode);
}

function norm(s: string): string {
  return s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/đ/g, "d").replace(/Đ/g, "D").toLowerCase();
}

function stripLocation(c: MockCooperative): Cooperative {
  const out: Partial<MockCooperative> = { ...c };
  delete out.location;
  return out as Cooperative;
}

const replay = new Map<string, { status: number; body: string; fingerprint: string }>();

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

function dashboard(p: MockPersona): DashboardKpis {
  const coops = COOPERATIVES.filter((c) => inScope(p, c.province_code, p.me.scope.level === "xa" ? c.commune_code : undefined));
  const level = p.me.scope.level;
  const provinceScope = level === "tinh" ? p.me.org?.admin_unit_code ?? null : null;
  const units =
    level === "tw" || level === "cuc" || p.me.scope.all
      ? PROVINCES.map((x) => ({ code: x.code, name: x.name, target: TARGET_AREA_BY_PROVINCE[x.code] ?? 30000 }))
      : COMMUNES.filter((c) => (provinceScope ? c.province_code === provinceScope : p.me.scope.units.includes(c.code))).map((c) => ({
          code: c.code,
          name: c.name,
          target: 4000,
        }));
  const breakdown = units.map((u) => {
    const unitCoops = coops.filter((c) => c.province_code === u.code || c.commune_code === u.code);
    const area = unitCoops.reduce((s, c) => s + Number(c.area_ha), 0) * (u.code.length === 2 ? 21 : 3.5);
    const reported = !UNREPORTED_UNITS.has(u.code);
    return {
      unit_code: u.code,
      unit_name: u.name,
      deployed_area_ha: reported ? area.toFixed(4) : "0",
      target_area_ha: String(u.target),
      reported,
    };
  });
  const missing = breakdown.filter((b) => !b.reported).length;
  const deployed = breakdown.reduce((s, b) => s + Number(b.deployed_area_ha), 0);
  const scopedTasks = TASKS.filter((t) => {
    const coop = COOPERATIVES.find((c) => c.id === t.cooperative_id);
    return coop ? inScope(p, coop.province_code, level === "xa" ? coop.commune_code : undefined) : false;
  });
  const now = Date.now();
  return {
    scope: { level, unit_name: p.me.scope.unit_name },
    season: CURRENT_SEASON,
    as_of: new Date(now - 40 * 60_000).toISOString(),
    complete: missing === 0,
    missing_units: missing,
    total_units: breakdown.length,
    kpis: {
      deployed_area_ha: deployed.toFixed(4),
      cooperatives: coops.length,
      digitized_plots: coops.reduce((s, c) => s + c.digitized_plots, 0),
      overdue_tasks: scopedTasks.filter((t) => !t.resolved_at && Date.parse(t.due_at) < now).length,
    },
    breakdown,
  };
}

type Handler = (ctx: { url: URL; persona: MockPersona; params: string[]; body: unknown; request: Request }) => Response;

function adminOnly(persona: MockPersona): Response | null {
  if (!persona.amr.includes("otp")) return problem(403, "mfa_required", "MFA required");
  return null;
}

const routes: { method: string; pattern: RegExp; handler: Handler }[] = [
  { method: "GET", pattern: /^\/v1\/me$/, handler: ({ persona }) => json(persona.me) },
  { method: "GET", pattern: /^\/v1\/dashboard\/kpis$/, handler: ({ persona }) => json(dashboard(persona)) },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives$/,
    handler: ({ url, persona }) => {
      const q = norm(url.searchParams.get("q") ?? "");
      const unit = url.searchParams.get("admin_unit");
      const status = url.searchParams.get("status");
      const list = COOPERATIVES.filter(
        (c) =>
          inScope(persona, c.province_code, persona.me.scope.level === "xa" ? c.commune_code : undefined) &&
          (!q || norm(`${c.name} ${c.code} ${c.commune_name}`).includes(q)) &&
          (!unit || c.province_code === unit || c.commune_code === unit) &&
          (!status || c.status === status),
      ).map(stripLocation);
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/locations$/,
    handler: ({ persona }) =>
      json({
        type: "FeatureCollection",
        features: COOPERATIVES.filter((c) => inScope(persona, c.province_code, persona.me.scope.level === "xa" ? c.commune_code : undefined)).map((c) => ({
          type: "Feature",
          id: c.id,
          geometry: { type: "Point", coordinates: c.location },
          properties: { id: c.id, code: c.code, name: c.name, province_code: c.province_code, commune_code: c.commune_code, area_ha: c.area_ha },
        })),
      }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      const c = COOPERATIVES.find((x) => x.id === params[0]);
      // Ngoài phạm vi trả 404 như backend (không tiết lộ sự tồn tại).
      if (!c || !inScope(persona, c.province_code, persona.me.scope.level === "xa" ? c.commune_code : undefined)) {
        return problem(404, "not_found", "Not found");
      }
      return json(stripLocation(c));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/households$/,
    handler: ({ params, url, persona }) => {
      const c = COOPERATIVES.find((x) => x.id === params[0]);
      if (!c || !inScope(persona, c.province_code, persona.me.scope.level === "xa" ? c.commune_code : undefined)) {
        return problem(404, "not_found", "Not found");
      }
      return json(paginate(householdsOf(c), url, 15));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/plots$/,
    handler: ({ url, persona }) => {
      const cid = url.searchParams.get("cooperative_id");
      const c = COOPERATIVES.find((x) => x.id === cid);
      if (!c || !inScope(persona, c.province_code)) return json({ items: [], next_cursor: null });
      return json(paginate(plotsOf(c), url, 15));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/tasks$/,
    handler: ({ url, persona }) => {
      const status = url.searchParams.get("status");
      const kind = url.searchParams.get("kind");
      const overdue = url.searchParams.get("overdue") === "true";
      const now = Date.now();
      const list = TASKS.filter((t) => {
        const coop = COOPERATIVES.find((c) => c.id === t.cooperative_id);
        if (!coop || !inScope(persona, coop.province_code, persona.me.scope.level === "xa" ? coop.commune_code : undefined)) return false;
        if (status && t.status !== status) return false;
        if (kind && t.kind !== kind) return false;
        if (overdue && (t.resolved_at || Date.parse(t.due_at) >= now)) return false;
        return true;
      }).sort((a, b) => Date.parse(a.due_at) - Date.parse(b.due_at));
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/documents$/,
    handler: ({ url }) => {
      const q = norm(url.searchParams.get("q") ?? "");
      const kind = url.searchParams.get("kind");
      const topic = url.searchParams.get("topic");
      const list = DOCUMENTS.filter(
        (d) => (!q || norm(`${d.title} ${d.summary}`).includes(q)) && (!kind || d.kind === kind) && (!topic || d.topic === topic),
      );
      return json(paginate(list, url, 12));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/admin-units$/,
    handler: ({ url }) => {
      const level = url.searchParams.get("level");
      const parent = url.searchParams.get("parent");
      return json({ items: ADMIN_UNITS.filter((u) => (!level || u.level === level) && (!parent || u.parent_code === parent)) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/admin-units\/boundaries$/,
    handler: ({ url, persona }) => {
      const level = url.searchParams.get("level") === "commune" ? "commune" : "province";
      const fc = boundaries(level);
      const s = scopeUnits(persona);
      if (s) fc.features = fc.features.filter((f) => s.has(f.properties.code) || (f.properties.parent_code !== null && s.has(f.properties.parent_code)));
      return new Response(JSON.stringify(fc), { status: 200, headers: { "Content-Type": "application/geo+json" } });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/orgs$/,
    handler: ({ url, persona }) => {
      if (!persona.me.roles.includes("platform_admin")) return problem(403, "forbidden", "Forbidden");
      return json(paginate(ORGS, url, 200));
    },
  },
  { method: "GET", pattern: /^\/v1\/catalogs$/, handler: () => json({ items: catalogSummaries() }) },
  {
    method: "GET",
    pattern: /^\/v1\/catalogs\/([\w-]+)$/,
    handler: ({ params, url }) => {
      const cat = CATALOGS[params[0] ?? ""];
      if (!cat) return problem(404, "not_found", "Not found");
      const all = url.searchParams.get("include_inactive") === "true";
      return json({ items: cat.items.filter((i) => all || i.active) });
    },
  },
  { method: "GET", pattern: /^\/v1\/params$/, handler: () => json({ items: PARAMS }) },
  {
    method: "POST",
    pattern: /^\/v1\/params\/([^/]+)\/proposals$/,
    handler: ({ params, persona, body }) => {
      const denied = adminOnly(persona);
      if (denied) return denied;
      if (!persona.me.roles.includes("param_setter")) return problem(403, "forbidden", "Forbidden");
      const param = PARAMS.find((p) => p.key === decodeURIComponent(params[0] ?? ""));
      if (!param) return problem(404, "not_found", "Not found");
      const input = body as Partial<ParamProposalInput>;
      const errors: { field: string; reason: string }[] = [];
      if (param.kind === "decimal" && !/^\d+(\.\d{1,4})?$/.test(input.value ?? "")) errors.push({ field: "value", reason: "Giá trị phải là số thập phân không âm, tối đa 4 chữ số lẻ" });
      if (!input.effective_from || !/^\d{4}-\d{2}-\d{2}$/.test(input.effective_from)) errors.push({ field: "effective_from", reason: "Ngày hiệu lực không hợp lệ" });
      if (!input.source_doc?.trim()) errors.push({ field: "source_doc", reason: "Cần số văn bản làm căn cứ" });
      if (!input.reason?.trim()) errors.push({ field: "reason", reason: "Cần ghi lý do thay đổi" });
      if (errors.length) return problem(422, "validation_failed", "Validation failed", { errors });
      const p: ParamProposal = {
        id: uuidv7(),
        key: param.key,
        value: input.value ?? null,
        text: input.text ?? null,
        unit: param.unit,
        effective_from: input.effective_from!,
        source_doc: input.source_doc!,
        reason: input.reason!,
        status: "pending",
        proposed_by: persona.me.subject,
        proposed_by_name: persona.me.display_name,
        proposed_at: new Date().toISOString(),
        decided_by: null,
        decided_by_name: null,
        decided_at: null,
        decision_note: null,
      };
      param.pending.push(p);
      return json(p, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/params\/proposals\/([\w-]+)\/(approve|reject)$/,
    handler: ({ params, persona, body }) => {
      const denied = adminOnly(persona);
      if (denied) return denied;
      if (!persona.me.roles.includes("param_approver")) return problem(403, "forbidden", "Forbidden");
      const param = PARAMS.find((p) => p.pending.some((x) => x.id === params[0]));
      const prop = param?.pending.find((x) => x.id === params[0]);
      if (!param || !prop) return problem(404, "not_found", "Not found");
      if (prop.proposed_by === persona.me.subject) return problem(403, "self_approval_forbidden", "Self approval forbidden");
      if (prop.status !== "pending") return problem(409, "conflict", "Already decided");
      const note = (body as ParamDecisionInput | undefined)?.note ?? null;
      const decided: ParamProposal = {
        ...prop,
        status: params[1] === "approve" ? "approved" : "rejected",
        decided_by: persona.me.subject,
        decided_by_name: persona.me.display_name,
        decided_at: new Date().toISOString(),
        decision_note: note,
      };
      param.pending = param.pending.filter((x) => x.id !== prop.id);
      if (decided.status === "approved") {
        param.current = {
          version: (param.current?.version ?? 0) + 1,
          value: decided.value,
          text: decided.text,
          unit: decided.unit,
          effective_from: decided.effective_from,
          source_doc: decided.source_doc,
          approved_by: persona.me.display_name,
          approved_at: decided.decided_at!,
        };
      }
      return json(decided);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cgh\/coverage$/,
    handler: ({ url, persona }) => {
      if (!(persona.me.scope.level === "cuc" || persona.me.roles.includes("platform_admin"))) return problem(403, "forbidden", "Forbidden");
      const stage = url.searchParams.get("stage") ?? "thu_hoach";
      const factor = stage === "thu_hoach" ? 1 : stage === "cham_soc" ? 0.8 : stage === "lam_dat" ? 1.12 : 0.95;
      return json({
        season: url.searchParams.get("season") ?? CURRENT_SEASON,
        stage,
        machine_type: url.searchParams.get("machine_type"),
        thresholds: { sufficient_pct: "85", attention_pct: "60", surplus_pct: "120" },
        as_of: new Date(Date.now() - 25 * 60_000).toISOString(),
        items: coverageItems(factor),
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cgh\/dashboard$/,
    handler: ({ url, persona }) => {
      if (!(persona.me.scope.level === "cuc" || persona.me.roles.includes("platform_admin"))) return problem(403, "forbidden", "Forbidden");
      return json(cghDashboard(url.searchParams.get("season") ?? CURRENT_SEASON));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cgh\/priority-cooperatives$/,
    handler: ({ url, persona }) => {
      if (!(persona.me.scope.level === "cuc" || persona.me.roles.includes("platform_admin"))) return problem(403, "forbidden", "Forbidden");
      return json(paginate(priorityCooperatives(url.searchParams.get("stage") ?? "thu_hoach"), url, 10));
    },
  },
];

/** fetch giả lập cho openapi-fetch. */
export async function mockFetch(request: Request): Promise<Response> {
  await sleep(120 + Math.random() * 220);
  const url = new URL(request.url);
  const persona = personaFromAuthHeader(request.headers.get("Authorization"));
  if (!persona) return problem(401, "unauthenticated", "Unauthenticated");

  let body: unknown = undefined;
  if (request.method === "POST") {
    const key = request.headers.get("Idempotency-Key");
    if (!key) return problem(400, "idempotency_key_required", "Idempotency-Key required");
    const text = await request.text();
    const fingerprint = `${persona.id}|${url.pathname}|${text}`;
    const prev = replay.get(key);
    if (prev) {
      if (prev.fingerprint !== fingerprint) return problem(422, "idempotency_key_reused", "Idempotency key reused");
      return new Response(prev.body, { status: prev.status, headers: { "Content-Type": "application/json", "Idempotent-Replayed": "true" } });
    }
    try {
      body = text ? JSON.parse(text) : undefined;
    } catch {
      return problem(400, "bad_request", "Invalid JSON");
    }
    const res = dispatch(request, url, persona, body);
    if (res.ok) replay.set(key, { status: res.status, body: await res.clone().text(), fingerprint });
    return res;
  }
  return dispatch(request, url, persona, body);
}

function dispatch(request: Request, url: URL, persona: MockPersona, body: unknown): Response {
  for (const r of routes) {
    if (r.method !== request.method) continue;
    const m = r.pattern.exec(url.pathname);
    if (m) return r.handler({ url, persona, params: m.slice(1), body, request });
  }
  return problem(404, "not_found", "Not found");
}
