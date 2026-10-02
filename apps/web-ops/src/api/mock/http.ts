// Tiện ích dùng chung của backend giả lập (NEXT_PUBLIC_API_MOCK=1): phản hồi JSON/problem+json,
// phân trang cursor, kiểm phạm vi theo persona. Chỉ chạy trong trình duyệt/kiểm thử.
import { uuidv7 } from "../idempotency";
import type { Page } from "../types";
import type { MockPersona } from "./personas";

export function traceId(): string {
  return uuidv7().replace(/-/g, "");
}

export function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "X-Trace-Id": traceId(), ...headers },
  });
}

const PROBLEMS = new WeakMap<Response, { code: string; title: string }>();

export function problem(status: number, code: string, title: string, extra: Record<string, unknown> = {}, headers: Record<string, string> = {}): Response {
  const tid = traceId();
  const res = new Response(
    JSON.stringify({ type: `https://errors.agrigreen.vn/${code}`, title, status, code, trace_id: tid, ...extra }),
    { status, headers: { "Content-Type": "application/problem+json", "X-Trace-Id": tid, ...headers } },
  );
  PROBLEMS.set(res, { code, title });
  return res;
}

/** Mã + tiêu đề của một phản hồi problem do mock dựng (đọc đồng bộ, không tiêu thụ thân) — dùng cho thao tác loạt. */
export function problemOf(res: Response): { code: string; title: string } | null {
  return PROBLEMS.get(res) ?? null;
}

export function validation(errors: { field: string; reason: string }[]): Response {
  return problem(422, "validation_failed", "Validation failed", { errors });
}

export function pageSlice<T>(items: T[], url: URL, defaultLimit = 50, maxLimit = 200): { slice: T[]; next: string | null } {
  const limit = Math.min(Math.max(Number(url.searchParams.get("limit") ?? defaultLimit) || defaultLimit, 1), maxLimit);
  const cursor = url.searchParams.get("cursor");
  const offset = cursor ? Number.parseInt(atob(cursor), 10) || 0 : 0;
  const slice = items.slice(offset, offset + limit);
  const next = offset + limit < items.length ? btoa(String(offset + limit)) : null;
  return { slice, next };
}

export function paginate<T>(items: T[], url: URL, defaultLimit = 50, maxLimit = 200): Page<T> {
  const { slice, next } = pageSlice(items, url, defaultLimit, maxLimit);
  return { items: slice, next_cursor: next };
}

/** Phạm vi của persona: mã tỉnh/xã được thấy (All → null). */
export function scopeUnits(p: MockPersona): Set<string> | null {
  return p.me.scope.all ? null : new Set(p.me.scope.units);
}

export function inScope(p: MockPersona, provinceCode: string | null | undefined, communeCode?: string | null): boolean {
  const s = scopeUnits(p);
  if (!s) return true;
  if (p.me.scope.level === "commune") return !!communeCode && s.has(communeCode);
  return (!!provinceCode && s.has(provinceCode)) || (!!communeCode && s.has(communeCode));
}

export function norm(s: string): string {
  return s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/đ/g, "d").replace(/Đ/g, "D").toLowerCase();
}

export function hasRole(p: MockPersona, ...roles: string[]): boolean {
  return roles.some((r) => p.me.roles.includes(r));
}

export interface Ctx {
  url: URL;
  persona: MockPersona;
  params: string[];
  /** JSON đã parse, chuỗi (text/*) hoặc Uint8Array (nhị phân: .xlsx, .zip). */
  body: unknown;
  request: Request;
}
export type Handler = (ctx: Ctx) => Response;
export interface Route {
  method: string;
  pattern: RegExp;
  /** Điều kiện thêm ngoài đường dẫn (vd tham số truy vấn); false → xét route kế tiếp. */
  match?: (url: URL, request: Request) => boolean;
  handler: Handler;
}

export const nowIso = () => new Date().toISOString();
export const iso = (daysAgo: number) => new Date(Date.now() - daysAgo * 86_400_000).toISOString();
export const isoHours = (hoursFromNow: number) => new Date(Date.now() + hoursFromNow * 3_600_000).toISOString();

/** Tệp nhị phân có chữ ký ZIP (PK\x03\x04) — .xlsx và Shapefile .zip. */
export function isZip(body: unknown): body is Uint8Array {
  return body instanceof Uint8Array && body[0] === 0x50 && body[1] === 0x4b;
}

/**
 * Đối tượng tự do của hợp đồng (`{type: object}` không khai thuộc tính — vd details, data, params, norm):
 * openapi-typescript sinh `Record<string, never>`, nên mock ép kiểu ở một chỗ này.
 */
export const opaque = (v: Record<string, unknown>): Record<string, never> => v as Record<string, never>;

/** Bản sao bỏ các khóa chỉ mock dùng (không có trong hợp đồng). */
export function omit<T extends object, K extends keyof T>(obj: T, ...keys: K[]): Omit<T, K> {
  const copy = { ...obj };
  for (const k of keys) delete copy[k];
  return copy;
}

/** Chuỗi lý do tối thiểu n ký tự (sau khi bỏ khoảng trắng hai đầu). */
export function reasonOf(body: unknown, field = "reason"): string {
  const v = (body as Record<string, unknown> | undefined)?.[field];
  return typeof v === "string" ? v.trim() : "";
}
