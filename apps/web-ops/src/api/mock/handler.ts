import { uuidv7 } from "../idempotency";
import { problem, type Route } from "./http";
import { personaFromAuthHeader, type MockPersona } from "./personas";
import { contextProblem, personaForContext } from "./kn";
import { supportGuard, supportRecord } from "./kn-admin";

// BEGIN GENERATED MOCK ROUTES
import { orderedRoutes, type RouteRegistration } from "./registry";
import { registeredKnRoutes } from "./kn-admin";
import { adminOpsRoutes as route0 } from "./admin-ops";
import { areaSyncRoutes as route1 } from "./areasync";
import { collectionRoutes as route2 } from "./collection";
import { cropcycleAppRoutes as route3 } from "./cropcycle";
import { cropcycleAdminRoutes as route4 } from "./cropcycle";
import { formsAppRoutes as route5 } from "./forms/routes";
import { formsAdminRoutes as route6 } from "./forms/routes";
import { apiClientAdminRoutes as route7 } from "./g5";
import { meteringAdminRoutes as route8 } from "./g5";
import { gisAppRoutes as route9 } from "./gis";
import { gisAdminRoutes as route10 } from "./gis";
import { htxAppRoutes as route11 } from "./htx";
import { htxAdminRoutes as route12 } from "./htx";
import { htxAnchorRoutes as route13 } from "./htx-anchors";
import { htxGapAppRoutes as route14 } from "./htx-gaps";
import { htxGapAdminRoutes as route15 } from "./htx-gaps";
import { appRoutes41 as route16 } from "./legacy-document";
import { appRoutes38 as route17 } from "./legacy-geo";
import { appRoutes39 as route18 } from "./legacy-geo";
import { appRoutes40 as route19 } from "./legacy-geo";
import { appRoutes42 as route20 } from "./legacy-geo";
import { appRoutes30 as route21 } from "./legacy-masterdata";
import { appRoutes31 as route22 } from "./legacy-masterdata";
import { appRoutes32 as route23 } from "./legacy-masterdata";
import { appRoutes33 as route24 } from "./legacy-masterdata";
import { adminRoutes4 as route25 } from "./legacy-masterdata";
import { adminRoutes5 as route26 } from "./legacy-masterdata";
import { adminRoutes6 as route27 } from "./legacy-masterdata";
import { adminRoutes7 as route28 } from "./legacy-masterdata";
import { adminRoutes8 as route29 } from "./legacy-masterdata";
import { adminRoutes9 as route30 } from "./legacy-masterdata";
import { appRoutes43 as route31 } from "./legacy-mechanization";
import { appRoutes44 as route32 } from "./legacy-mechanization";
import { appRoutes45 as route33 } from "./legacy-mechanization";
import { appRoutes46 as route34 } from "./legacy-mechanization";
import { appRoutes1 as route35 } from "./legacy-party";
import { appRoutes34 as route36 } from "./legacy-party";
import { appRoutes35 as route37 } from "./legacy-party";
import { appRoutes36 as route38 } from "./legacy-party";
import { appRoutes37 as route39 } from "./legacy-party";
import { adminRoutes10 as route40 } from "./legacy-party";
import { adminRoutes11 as route41 } from "./legacy-party";
import { adminRoutes12 as route42 } from "./legacy-party";
import { metricsRoutes as route43 } from "./metrics";
import { mrvAppRoutes as route44 } from "./mrv";
import { mrvAdminRoutes as route45 } from "./mrv";
import { opsRoutes as route46 } from "./ops";
import { orgCreateRoutes as route47 } from "./orgs-create";
import { rosterRoutes as route48 } from "./roster";
import { simulationRoutes as route49 } from "./simulation";
import { syncMonitorRoutes as route50 } from "./syncmonitor";
import { waterwayAppRoutes as route51 } from "./waterway";
import { waterwayAdminRoutes as route52 } from "./waterway";

export const commonRouteRegistrations: RouteRegistration[] = [
  { surface: "admin", order: 13, routes: route0 },
  { surface: "app", order: 47, routes: route1 },
  { surface: "app", order: 57, routes: route2 },
  { surface: "app", order: 53, routes: route3 },
  { surface: "admin", order: 19, routes: route4 },
  { surface: "app", order: 3, routes: route5 },
  { surface: "admin", order: 3, routes: route6 },
  { surface: "admin", order: 22, routes: route7 },
  { surface: "admin", order: 23, routes: route8 },
  { surface: "app", order: 50, routes: route9 },
  { surface: "admin", order: 17, routes: route10 },
  { surface: "app", order: 54, routes: route11 },
  { surface: "admin", order: 20, routes: route12 },
  { surface: "admin", order: 16, routes: route13 },
  { surface: "app", order: 0, routes: route14 },
  { surface: "admin", order: 1, routes: route15 },
  { surface: "app", order: 41, routes: route16 },
  { surface: "app", order: 38, routes: route17 },
  { surface: "app", order: 39, routes: route18 },
  { surface: "app", order: 40, routes: route19 },
  { surface: "app", order: 42, routes: route20 },
  { surface: "app", order: 30, routes: route21 },
  { surface: "app", order: 31, routes: route22 },
  { surface: "app", order: 32, routes: route23 },
  { surface: "app", order: 33, routes: route24 },
  { surface: "admin", order: 4, routes: route25 },
  { surface: "admin", order: 5, routes: route26 },
  { surface: "admin", order: 6, routes: route27 },
  { surface: "admin", order: 7, routes: route28 },
  { surface: "admin", order: 8, routes: route29 },
  { surface: "admin", order: 9, routes: route30 },
  { surface: "app", order: 43, routes: route31 },
  { surface: "app", order: 44, routes: route32 },
  { surface: "app", order: 45, routes: route33 },
  { surface: "app", order: 46, routes: route34 },
  { surface: "app", order: 1, routes: route35 },
  { surface: "app", order: 34, routes: route36 },
  { surface: "app", order: 35, routes: route37 },
  { surface: "app", order: 36, routes: route38 },
  { surface: "app", order: 37, routes: route39 },
  { surface: "admin", order: 10, routes: route40 },
  { surface: "admin", order: 11, routes: route41 },
  { surface: "admin", order: 12, routes: route42 },
  { surface: "app", order: 55, routes: route43 },
  { surface: "app", order: 56, routes: route44 },
  { surface: "admin", order: 21, routes: route45 },
  { surface: "app", order: 48, routes: route46 },
  { surface: "admin", order: 14, routes: route47 },
  { surface: "app", order: 49, routes: route48 },
  { surface: "app", order: 52, routes: route49 },
  { surface: "admin", order: 15, routes: route50 },
  { surface: "app", order: 51, routes: route51 },
  { surface: "admin", order: 18, routes: route52 },
];

const registrations = [...commonRouteRegistrations, ...registeredKnRoutes()];
const appRoutes = orderedRoutes(registrations, "app");
const adminRoutes = orderedRoutes(registrations, "admin");
// END GENERATED MOCK ROUTES

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

const replay = new Map<string, { status: number; body: string; headers: Record<string, string>; fingerprint: string }>();

function binaryFingerprint(bytes: Uint8Array): string {
  let h = 2166136261;
  for (let i = 0; i < bytes.length; i++) h = Math.imul(h ^ (bytes[i] ?? 0), 16777619) >>> 0;
  return `bin:${bytes.length}:${h.toString(16)}`;
}

function replayBody(res: Response, text: string): string {
  if (res.headers.get("Cache-Control") !== "no-store") return text;
  try {
    const parsed = JSON.parse(text) as Record<string, unknown>;
    delete parsed.api_key;
    return JSON.stringify(parsed);
  } catch {
    return "{}";
  }
}

function createMockFetch(routes: Route[], opts: { surface: "app" | "admin"; latency?: boolean }) {
  return async function mockFetch(request: Request): Promise<Response> {
    if (opts.latency !== false) await sleep(120 + Math.random() * 220);
    const url = new URL(request.url);
    const tokenPersona = personaFromAuthHeader(request.headers.get("Authorization"));
    if (!tokenPersona) return problem(401, "unauthenticated", "Unauthenticated");
    // KN v7.7: header X-AG-Context chọn vai trò + phạm vi của lần gán (middleware P1 giả lập): lạ/của người khác → 403.
    const badContext = contextProblem(tokenPersona, request.headers.get("X-AG-Context"));
    if (badContext) return badContext;
    const persona = personaForContext(tokenPersona, request.headers.get("X-AG-Context"));
    // Bề mặt Admin: mọi route bắt buộc token đã qua yếu tố thứ hai (mục 11).
    if (opts.surface === "admin" && !persona.amr.includes("otp")) return problem(403, "mfa_required", "MFA required");

    let body: unknown = undefined;
    // POST bắt buộc Idempotency-Key; PUT/PATCH (KN v7.7 P1/P7) đọc thân như POST, khóa tùy chọn.
    if (request.method === "POST" || ((request.method === "PUT" || request.method === "PATCH") && request.headers.get("Content-Type")?.includes("json"))) {
      const key = request.headers.get("Idempotency-Key") ?? (request.method === "POST" ? null : uuidv7());
      if (!key) return problem(400, "idempotency_key_required", "Idempotency-Key required");
      const type = request.headers.get("Content-Type") ?? "application/json";
      let fingerprintBody: string;
      if (type.includes("json")) {
        const text = await request.text();
        fingerprintBody = text;
        try {
          body = text ? JSON.parse(text) : undefined;
        } catch {
          return problem(400, "bad_request", "Invalid JSON");
        }
      } else if (type.startsWith("text/") || /[+/]xml\b/.test(type)) {
        body = await request.text();
        fingerprintBody = body as string;
      } else {
        const bytes = new Uint8Array(await request.arrayBuffer());
        body = bytes;
        fingerprintBody = binaryFingerprint(bytes);
      }
      const fingerprint = `${opts.surface}|${persona.id}|${url.pathname}${url.search}|${fingerprintBody}`;
      const prev = replay.get(key);
      if (prev) {
        if (prev.fingerprint !== fingerprint) return problem(422, "idempotency_key_reused", "Idempotency key reused");
        return new Response(prev.body, { status: prev.status, headers: { ...prev.headers, "Idempotent-Replayed": "true" } });
      }
      // FN-AD-04.07: chế độ hỗ trợ — lý do bắt buộc, nhật ký attempted + completed.
      const guard = supportGuard(request, url);
      if (guard) return guard;
      supportRecord(request, url, null, "attempted", key);
      const res = dispatch(routes, request, url, persona, body);
      supportRecord(request, url, res.status, "completed", key);
      if (res.ok) {
        replay.set(key, { status: res.status, body: replayBody(res, await res.clone().text()), headers: { "Content-Type": res.headers.get("Content-Type") ?? "application/json" }, fingerprint });
      }
      return res;
    }
    return dispatch(routes, request, url, persona, body);
  };
}

function dispatch(routes: Route[], request: Request, url: URL, persona: MockPersona, body: unknown): Response {
  for (const r of routes) {
    if (r.method !== request.method) continue;
    const m = r.pattern.exec(url.pathname);
    if (!m || (r.match && !r.match(url, request))) continue;
    try {
      return r.handler({ url, persona, params: m.slice(1), body, request });
    } catch (e) {
      // Lỗi của chính backend giả lập → 500 internal_error (không thành "mất kết nối").
      return problem(500, "internal_error", "Mock handler failed", { detail: e instanceof Error ? e.message : String(e) });
    }
  }
  return problem(404, "not_found", "Not found");
}

export const mockFetch = createMockFetch(appRoutes, { surface: "app" });

export const mockAdminFetch = createMockFetch(adminRoutes, { surface: "admin" });

export const mockFetchNoDelay = createMockFetch(appRoutes, { surface: "app", latency: false });

export const mockAdminFetchNoDelay = createMockFetch(adminRoutes, { surface: "admin", latency: false });
