// Tiện ích dùng chung của backend giả lập (NEXT_PUBLIC_API_MOCK=1): phản hồi JSON/problem+json,
// phân trang cursor, kiểm vai theo persona. Chỉ chạy trong trình duyệt/kiểm thử.
import { uuidv7 } from "../idempotency";
import type { MockPersona } from "./personas";

export function traceId(): string {
  return uuidv7().replace(/-/g, "");
}

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", "X-Trace-Id": traceId() } });
}

export function created(body: unknown): Response {
  return json(body, 201);
}

export function problem(status: number, code: string, title: string, extra: Record<string, unknown> = {}): Response {
  const tid = traceId();
  return new Response(JSON.stringify({ type: `https://errors.agrigreen.vn/${code}`, title, status, code, trace_id: tid, ...extra }), {
    status,
    headers: { "Content-Type": "application/problem+json", "X-Trace-Id": tid },
  });
}

export function validation(errors: { field: string; reason: string }[]): Response {
  return problem(422, "validation_failed", "Validation failed", { errors });
}

export function forbidden(): Response {
  return problem(403, "forbidden", "Forbidden");
}

export function selfApproval(): Response {
  return problem(403, "self_approval_forbidden", "Self approval forbidden");
}

export function pageSlice<T>(items: T[], url: URL, defaultLimit = 50, maxLimit = 200): { items: T[]; next_cursor: string | null } {
  const limit = Math.min(Math.max(Number(url.searchParams.get("limit") ?? defaultLimit) || defaultLimit, 1), maxLimit);
  const cursor = url.searchParams.get("cursor");
  const offset = cursor ? Number.parseInt(atob(cursor), 10) || 0 : 0;
  const slice = items.slice(offset, offset + limit);
  return { items: slice, next_cursor: offset + limit < items.length ? btoa(String(offset + limit)) : null };
}

export function hasRole(p: MockPersona, ...roles: string[]): boolean {
  return roles.some((r) => p.roles.includes(r));
}

export interface Ctx {
  url: URL;
  persona: MockPersona;
  params: string[];
  body: Record<string, unknown>;
}
export type Handler = (ctx: Ctx) => Response;
export interface Route {
  method: "GET" | "POST";
  pattern: RegExp;
  handler: Handler;
}

/** Khai route: "/v1/hub/facilities/:id" → RegExp có nhóm bắt cho mỗi tham số. */
export function route(method: Route["method"], path: string, handler: Handler): Route {
  const src = path.replace(/:[a-z_]+/g, "([^/]+)");
  return { method, pattern: new RegExp(`^${src}$`), handler };
}

export const nowIso = () => new Date().toISOString();
export const daysAgo = (d: number, h = 0) => new Date(Date.now() - d * 86_400_000 - h * 3_600_000).toISOString();
export const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

export function str(body: Record<string, unknown>, field: string): string {
  const v = body[field];
  return typeof v === "string" ? v.trim() : "";
}

export function num(body: Record<string, unknown>, field: string): number | undefined {
  const v = body[field];
  return typeof v === "number" && Number.isFinite(v) ? v : undefined;
}

/** Đối tượng tự do của hợp đồng (`{type: object}`): openapi-typescript sinh Record<string, never>. */
export const opaque = <T>(v: T): Record<string, never> => v as unknown as Record<string, never>;

export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
