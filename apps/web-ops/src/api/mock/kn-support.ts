// Q10/FN-CH-00.01 mock. Synthetic form data stays in this tab; authorization precedes every replay.
import { uuidv7 } from "../idempotency";
import type { components } from "../generated/application";
import { json, problem, validation } from "./http";
import { knContext } from "./kn";
import { TENANT, type MockPersona } from "./personas";

type S = components["schemas"];
type Record = S["KNSupportDetail"] & { tenant: string };
const requests = new Map<string, Record>();
const creates = new Map<string, { fingerprint: string }>();
const resolutions = new Map<string, number>();
const headers = { "Cache-Control": "no-store" };
export function resetKNSupport() { requests.clear(); creates.clear(); resolutions.clear(); }
const keyOK = (key: string | null) => !!key && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/iu.test(key);

async function parse(request: Request): Promise<object | Response> {
  if (request.headers.get("Content-Type")?.split(";")[0]?.trim() !== "application/json") return problem(415, "contact.support_json_required", "Chỉ nhận JSON", {}, headers);
  const text = await request.text();
  if (new TextEncoder().encode(text).length > 12 * 1024) return problem(413, "contact.support_too_large", "Nội dung quá dài", {}, headers);
  try { const body: unknown = JSON.parse(text); if (body && typeof body === "object" && !Array.isArray(body)) return body; } catch { /* safe generic error */ }
  return validation([{ field: "body", reason: "JSON không hợp lệ" }]);
}

/** Public create runs before persona/context processing, as the native public group does. */
export async function knSupportPublicMock(request: Request, url: URL): Promise<Response | null> {
  if (url.pathname !== "/v1/public/contacts/support-requests" || request.method !== "POST") return null;
  const parsed = await parse(request); if (parsed instanceof Response) return parsed;
  const body = parsed as Partial<S["KNSupportInput"]>;
  const errors = Object.keys(body).filter((k) => !["name", "contact", "message"].includes(k)).map((field) => ({ field, reason: "Trường không được hỗ trợ" }));
  for (const [field, max, min] of [["name", 100, 1], ["contact", 254, 1], ["message", 2000, 10]] as const) {
    const value = typeof body[field] === "string" ? body[field]!.trim() : "";
    if ([...value].length < min || [...value].length > max || [...value].some((ch) => /\p{Cc}/u.test(ch) && !(field === "message" && (ch === "\n" || ch === "\t")))) errors.push({ field, reason: "Bắt buộc và không vượt giới hạn" });
  }
  const contact = typeof body.contact === "string" ? body.contact.trim() : "";
  if (!/^\+?[0-9]{8,15}$/u.test(contact) && !/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/u.test(contact)) errors.push({ field: "contact", reason: "Email hoặc số điện thoại không hợp lệ" });
  if (errors.length) return validation(errors);
  const key = request.headers.get("Idempotency-Key"); if (!keyOK(key)) return validation([{ field: "Idempotency-Key", reason: "Bắt buộc là UUID" }]);
  const input = { name: body.name!.trim(), contact, message: body.message!.trim() };
  const encoded = JSON.stringify(input).replace(/[<>&\u2028\u2029]/gu, (ch) => "\\u" + ch.charCodeAt(0).toString(16).padStart(4, "0"));
  if (new TextEncoder().encode(encoded).length + 16 > 12 * 1024) return validation([{ field: "message", reason: "Tổng nội dung quá dài. Vui lòng rút gọn rồi gửi lại." }]);
  const fingerprint = JSON.stringify(input); const old = creates.get(key!.toLowerCase());
  if (old && old.fingerprint !== fingerprint) return problem(422, "idempotency_key_reused", "Khóa đã được dùng cho nội dung khác", {}, headers);
  if (!old) { const id = uuidv7(); requests.set(id, { ...input, id, tenant: TENANT, created_at: new Date().toISOString(), state: "new", version: 1 }); creates.set(key!.toLowerCase(), { fingerprint }); }
  return json({ status: "accepted" }, 202, headers);
}

/** Invoked after real mock persona/context validation, before the generic replay map. Never returns another tenant. */
export async function knSupportAdminMock(request: Request, url: URL, persona: MockPersona): Promise<Response | null> {
  const match = /^\/v1\/contacts\/support-requests(?:\/([^/]+)(\/resolution)?)?$/u.exec(url.pathname); if (!match) return null;
  if (!persona.me.roles.includes("platform_admin")) return problem(403, "forbidden", "Không có quyền", {}, headers);
  if (!persona.amr.includes("otp")) return problem(403, "mfa_required", "Cần MFA", {}, headers);
  const session = knContext(request.headers.get("X-AG-Context") ?? "");
  if (session?.role !== "admin" || session.read_only || session.support_session_id) return problem(403, "forbidden", "Cần phiên Admin KN", {}, headers);
  const summary = (r: Record): S["KNSupportSummary"] => ({ id: r.id, state: r.state, version: r.version, created_at: r.created_at });
  if (!match[1] && request.method === "GET") {
    const state = url.searchParams.get("state") ?? ""; const offset = Number(url.searchParams.get("offset") ?? 0);
    if (!["", "new", "resolved"].includes(state) || !Number.isInteger(offset) || offset < 0 || offset > 10000) return validation([{ field: "state", reason: "Bộ lọc không hợp lệ" }]);
    return json({ items: [...requests.values()].filter((r) => r.tenant === persona.me.tenant_id && (!state || r.state === state)).reverse().slice(offset, offset + 50).map(summary) }, 200, headers);
  }
  const r = requests.get(match[1] ?? ""); if (!r || r.tenant !== persona.me.tenant_id) return problem(404, "contact.support_not_found", "Không tìm thấy yêu cầu", {}, headers);
  if (!match[2] && request.method === "GET") return json({ ...summary(r), name: r.name, contact: r.contact, message: r.message }, 200, headers);
  if (match[2] && request.method === "PUT") {
    const parsed = await parse(request); if (parsed instanceof Response) return parsed;
    const body = parsed as { expected_version?: number }; const key = request.headers.get("Idempotency-Key");
    if (!keyOK(key) || Object.keys(body).some((k) => k !== "expected_version") || !Number.isInteger(body.expected_version) || (body.expected_version ?? 0) < 1) return validation([{ field: "expected_version", reason: "Phiên bản và UUID thao tác bắt buộc" }]);
    const binding = `${persona.me.tenant_id}:${persona.me.actor_ref}:${r.id}:${key?.toLowerCase()}`; const old = resolutions.get(binding);
    if (old !== undefined) return old === body.expected_version ? json(summary(r), 200, headers) : problem(422, "idempotency_key_reused", "Khóa khác nội dung", {}, headers);
    if (r.version !== body.expected_version || r.state !== "new") return problem(409, "contact.support_stale", "Yêu cầu đã thay đổi", {}, headers);
    r.state = "resolved"; r.version++; resolutions.set(binding, body.expected_version!); return json(summary(r), 200, headers);
  }
  return problem(404, "not_found", "Không có API", {}, headers);
}
