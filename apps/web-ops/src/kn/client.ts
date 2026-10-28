// Client hợp đồng tạm KN v7.7 (contract.ts). Dùng fetch(Request) thuần để chạy được với backend giả lập
// (mockFetch nhận Request) và backend thật như nhau; Bearer, Idempotency-Key và X-AG-Context đặt ở đây.
// Kiểu trên dây là kiểu sinh từ OpenAPI (KNAssignment, KNSessionContext, KNMenu); menu dựng sang cây của khung ở đây.
import { beginAttempt, currentIdempotentRun, requestFingerprint, type IdempotencyAttempt } from "@/api/idempotency";
import { ApiProblem, networkProblem, toProblem } from "@/api/problem";
import { CONTEXT_HEADER, type KnApi, type KnMenu, type MenuBadges, type MeContexts, type SessionContext } from "./contract";
import { fromKnMenu } from "./menuAdapter";

export interface KnApiOptions {
  baseUrl: string;
  getAccessToken: () => string | null;
  getContextId: () => string | null;
  fetch?: (input: Request) => Promise<Response>;
}

export function createKnApi(opts: KnApiOptions): KnApi {
  const base = opts.baseUrl.replace(/\/+$/, "");
  const doFetch = opts.fetch ?? ((r: Request) => fetch(r));

  // P0-WEB-IDEMKEY (#129): khóa tường minh > khóa của phiên thao tác hiện hành (bấm lại cùng dữ liệu → cùng khóa) > khóa mới.
  async function call<T>(method: "GET" | "POST", path: string, body?: unknown, idem?: string): Promise<T> {
    const op = currentIdempotentRun(); // đọc đồng bộ, trước mọi await
    const headers = new Headers({ Accept: "application/json, application/problem+json" });
    const token = opts.getAccessToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
    const ctx = opts.getContextId();
    if (ctx) headers.set(CONTEXT_HEADER, ctx);
    const payload = body === undefined ? undefined : JSON.stringify(body);
    let attempt: IdempotencyAttempt | null = null;
    if (method === "POST") {
      headers.set("Content-Type", "application/json");
      attempt = beginAttempt(op, requestFingerprint(method, `${base}${path}`, ctx, payload ?? ""), idem);
      headers.set("Idempotency-Key", attempt.key);
    }
    const req = new Request(`${base}${path}`, { method, headers, body: payload });
    let res: Response;
    try {
      res = await doFetch(req);
    } catch {
      attempt?.settle("network");
      throw networkProblem();
    }
    const text = await res.text();
    let data: unknown = undefined;
    try {
      data = text ? JSON.parse(text) : undefined;
    } catch {
      data = undefined;
    }
    attempt?.settle({ status: res.status, code: (data as { code?: string } | undefined)?.code ?? null });
    if (!res.ok) throw new ApiProblem(toProblem(data, res));
    return data as T;
  }

  return {
    listContexts: () => call<MeContexts>("GET", "/v1/me/contexts"),
    selectContext: (req) => call<SessionContext>("POST", "/v1/me/context", req),
    // Vai + phòng ban lấy từ ngữ cảnh phiên (X-AG-Context) ở server — không gửi tham số (P2 resolveAudience).
    menu: async () => fromKnMenu(await call<KnMenu>("GET", "/v1/menu")),
    badges: () => call<MenuBadges>("GET", "/v1/me/menu-badges"),
  };
}
