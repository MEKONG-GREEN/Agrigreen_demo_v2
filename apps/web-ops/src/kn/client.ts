// Client hợp đồng tạm KN v7.7 (contract.ts). Dùng fetch(Request) thuần để chạy được với backend giả lập
// (mockFetch nhận Request) và backend thật như nhau; Bearer, Idempotency-Key và X-AG-Context đặt ở đây.
// Khi P1/P2 có OpenAPI: chuyển sang `api.GET("/v1/me/contexts")` của client sinh, giữ interface KnApi.
import { uuidv7 } from "@/api/idempotency";
import { ApiProblem, networkProblem, toProblem } from "@/api/problem";
import { CONTEXT_HEADER, type KnApi, type MenuBadges, type MenuTree, type MeContexts, type SessionContext } from "./contract";

export interface KnApiOptions {
  baseUrl: string;
  getAccessToken: () => string | null;
  getContextId: () => string | null;
  fetch?: (input: Request) => Promise<Response>;
}

/** Admin không chọn phòng ban: menu lấy qua mã phòng ban đặc biệt "_all" (giả định cho P2). */
export const ADMIN_MENU_CODE = "_all";

export function createKnApi(opts: KnApiOptions): KnApi {
  const base = opts.baseUrl.replace(/\/+$/, "");
  const doFetch = opts.fetch ?? ((r: Request) => fetch(r));

  async function call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
    const headers = new Headers({ Accept: "application/json, application/problem+json" });
    const token = opts.getAccessToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
    const ctx = opts.getContextId();
    if (ctx) headers.set(CONTEXT_HEADER, ctx);
    if (method === "POST") {
      headers.set("Content-Type", "application/json");
      headers.set("Idempotency-Key", uuidv7());
    }
    const req = new Request(`${base}${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
    let res: Response;
    try {
      res = await doFetch(req);
    } catch {
      throw networkProblem();
    }
    const text = await res.text();
    let data: unknown = undefined;
    try {
      data = text ? JSON.parse(text) : undefined;
    } catch {
      data = undefined;
    }
    if (!res.ok) throw new ApiProblem(toProblem(data, res));
    return data as T;
  }

  return {
    listContexts: () => call<MeContexts>("GET", "/v1/me/contexts"),
    selectContext: (req) => call<SessionContext>("POST", "/v1/me/context", req),
    menu: (ctx) =>
      call<MenuTree>(
        "GET",
        `/v1/departments/${encodeURIComponent(ctx.role === "admin" ? ADMIN_MENU_CODE : (ctx.department_code ?? ""))}/modules?role=${encodeURIComponent(ctx.role)}`,
      ),
    badges: () => call<MenuBadges>("GET", "/v1/me/menu-badges"),
  };
}
