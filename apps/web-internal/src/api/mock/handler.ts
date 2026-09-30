// ============================================================================
// BACKEND GIẢ LẬP của api-internal (NEXT_PUBLIC_API_MOCK=1). Thay `fetch` của openapi-fetch nên toàn bộ
// middleware thật (Bearer, Idempotency-Key, parse problem+json) vẫn chạy.
// Mô phỏng quy tắc backend để UI thể hiện đúng: POST thiếu Idempotency-Key → 400, phát lại cùng khóa →
// Idempotent-Replayed, tự phê duyệt → 403 self_approval_forbidden (R-56), bề mặt quản trị cần MFA →
// 403 mfa_required, route của bề mặt quản trị gọi nhầm bề mặt ứng dụng → 404 (như gin).
// ============================================================================
import { acctbridgeAdminRoutes, acctbridgeRoutes } from "./acctbridge";
import { arrivalRoutes } from "./arrivals";
import { costingAdminRoutes, costingRoutes } from "./costing";
import { fieldopsRoutes } from "./fieldops";
import { hubAdminRoutes, hubAppRoutes } from "./hub";
import { sha256Hex } from "@/lib/sha256";
import { problem, type MultipartFile, type Route } from "./http";
import { logisticsAdminRoutes, logisticsRoutes } from "./logistics";
import { measurementAdminRoutes, measurementAppRoutes } from "./measurement";
import { ordersAdminRoutes, ordersRoutes } from "./orders";
import { personaFromAuthHeader, type MockPersona } from "./personas";

const appRoutes: Route[] = [...hubAppRoutes, ...arrivalRoutes, ...measurementAppRoutes, ...fieldopsRoutes, ...logisticsRoutes, ...ordersRoutes, ...costingRoutes, ...acctbridgeRoutes];
const adminRoutes: Route[] = [...hubAdminRoutes, ...measurementAdminRoutes, ...logisticsAdminRoutes, ...ordersAdminRoutes, ...costingAdminRoutes, ...acctbridgeAdminRoutes];

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

const replay = new Map<string, { status: number; body: string; fingerprint: string }>();

function dispatch(routes: Route[], method: string, url: URL, persona: MockPersona, body: Record<string, unknown>): Response {
  for (const r of routes) {
    if (r.method !== method) continue;
    const m = r.pattern.exec(url.pathname);
    if (!m) continue;
    try {
      return r.handler({ url, persona, params: m.slice(1).map(decodeURIComponent), body });
    } catch (e) {
      return problem(500, "internal_error", "Mock handler failed", { detail: e instanceof Error ? e.message : String(e) });
    }
  }
  return problem(404, "not_found", "Not found");
}

function createMockFetch(routes: Route[], opts: { surface: "app" | "admin"; latency?: boolean }) {
  return async function mockFetch(request: Request): Promise<Response> {
    if (opts.latency !== false) await sleep(80 + Math.random() * 160);
    const url = new URL(request.url);
    const persona = personaFromAuthHeader(request.headers.get("Authorization"));
    if (!persona) return problem(401, "unauthenticated", "Unauthenticated");
    // Bề mặt quản trị: mọi route bắt buộc token đã qua yếu tố thứ hai (httpx.RequireMFA).
    if (opts.surface === "admin" && !persona.amr.includes("otp")) return problem(403, "mfa_required", "MFA required");
    if (request.method !== "POST") return dispatch(routes, request.method, url, persona, {});

    const key = request.headers.get("Idempotency-Key");
    if (!key) return problem(400, "idempotency_key_required", "Idempotency-Key required");
    let text: string;
    let body: Record<string, unknown> = {};
    if ((request.headers.get("Content-Type") ?? "").toLowerCase().startsWith("multipart/form-data")) {
      // Tải tệp: phần tệp → {name, type, bytes}; dấu vân tay idempotency theo băm nội dung thay cho thân thô.
      let form: FormData;
      try {
        form = await request.formData();
      } catch {
        return problem(400, "bad_request", "Invalid multipart body");
      }
      const parts: string[] = [];
      for (const [k, v] of form.entries()) {
        if (typeof v === "string") {
          body[k] = v;
          parts.push(`${k}=${v}`);
        } else {
          const bytes = new Uint8Array(await v.arrayBuffer());
          body[k] = { name: v.name, type: v.type, bytes } satisfies MultipartFile;
          parts.push(`${k}=${v.name}:${bytes.length}:${sha256Hex(bytes)}`);
        }
      }
      text = parts.join("&");
    } else {
      text = await request.text();
      try {
        body = text ? (JSON.parse(text) as Record<string, unknown>) : {};
      } catch {
        return problem(400, "bad_request", "Invalid JSON");
      }
    }
    const fingerprint = `${opts.surface}|${persona.id}|${url.pathname}${url.search}|${text}`;
    const prev = replay.get(key);
    if (prev) {
      if (prev.fingerprint !== fingerprint) return problem(422, "idempotency_key_reused", "Idempotency key reused");
      return new Response(prev.body, { status: prev.status === 201 ? 200 : prev.status, headers: { "Content-Type": "application/json", "Idempotent-Replayed": "true" } });
    }
    const res = dispatch(routes, "POST", url, persona, body);
    if (res.ok) replay.set(key, { status: res.status, body: await res.clone().text(), fingerprint });
    return res;
  };
}

/** fetch giả lập cho bề mặt ứng dụng. */
export const mockFetch = createMockFetch(appRoutes, { surface: "app" });
/** fetch giả lập cho bề mặt quản trị (MFA bắt buộc). */
export const mockAdminFetch = createMockFetch(adminRoutes, { surface: "admin" });

/** Cho kiểm thử: không chờ độ trễ giả lập. */
export const mockFetchNoDelay = createMockFetch(appRoutes, { surface: "app", latency: false });
export const mockAdminFetchNoDelay = createMockFetch(adminRoutes, { surface: "admin", latency: false });
