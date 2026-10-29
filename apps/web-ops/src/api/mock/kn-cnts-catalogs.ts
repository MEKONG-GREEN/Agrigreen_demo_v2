// GIẢ LẬP danh mục Chăn nuôi – Thủy sản (W11.41, BRD CNTS v3.7 FN-06) theo hợp đồng /v1/masterdata/catalogs/cnts_*:
// Admin thêm/sửa/ngừng/dùng lại kèm ngày áp dụng (valid_from), phiên bản hiệu lực (…/versions), đọc theo ngày (?at=).
// Cơ sở giết mổ: facility_ref do máy chủ cấp, mã mộc dấu không trùng giữa cơ sở. Dữ liệu tổng hợp, không phải thật.
import { uuidv7 } from "../idempotency";
import type { CatalogItem, CatalogName } from "../types";
import { ADMIN_UNITS, CATALOGS, PARAM_VERSIONS } from "./data";
import { hasRole, json, nowIso, problem, reasonOf, validation, type Ctx, type Route } from "./http";

/** Mã xã của mục mẫu cũ CS_BINH_DUC (mã ĐVHC thật, không có trong danh sách xã giả lập) vẫn hợp lệ. */
const LEGACY_COMMUNES = new Set(["30292"]);
const COMMUNE_OK = (code: string) => LEGACY_COMMUNES.has(code) || ADMIN_UNITS.some((u) => u.code === code && u.level === "commune");

interface MockVersion {
  id: string;
  valid_from: string;
  after: { name: string; attrs: Record<string, string>; active: boolean };
  change_id: string;
  event_time: string;
  actor_ref: string;
  seq: number;
}

const VERSIONS = new Map<string, MockVersion[]>();
let seq = 0;

const isCnts = (c: string) => c.startsWith("cnts_") && c in CATALOGS;
const vnToday = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const keyOf = (c: string, code: string) => `${c}/${code}`;
const items = (c: string) => CATALOGS[c as CatalogName];

/** Xóa phiên bản đã ghi (kiểm thử). */
export function resetCntsVersions() {
  VERSIONS.clear();
  seq = 0;
}

function record(ctx: Ctx, it: CatalogItem, validFrom: string) {
  const k = keyOf(it.catalog, it.code);
  const list = VERSIONS.get(k) ?? [];
  list.push({
    id: uuidv7(),
    valid_from: validFrom,
    after: { name: it.name, attrs: { ...it.attrs }, active: it.active },
    change_id: uuidv7(),
    event_time: nowIso(),
    actor_ref: ctx.persona.me.actor_ref,
    seq: ++seq,
  });
  VERSIONS.set(k, list);
}

function sorted(c: string, code: string): MockVersion[] {
  return [...(VERSIONS.get(keyOf(c, code)) ?? [])].sort((a, b) => (a.valid_from === b.valid_from ? a.seq - b.seq : a.valid_from < b.valid_from ? -1 : 1));
}

/** Giá trị có hiệu lực tại ngày at (null = chưa hiệu lực); mục chưa có phiên bản (nạp sẵn) → hiện trạng. */
function at(it: CatalogItem, day: string): CatalogItem | null {
  const vs = sorted(it.catalog, it.code);
  if (vs.length === 0) return it;
  const v = vs.filter((x) => x.valid_from <= day).pop();
  return v ? { ...it, name: v.after.name, attrs: v.after.attrs, active: v.after.active, valid_from: v.valid_from } : null;
}

const canEdit = (ctx: Ctx) => hasRole(ctx.persona, "masterdata_admin", "platform_admin");

function check(c: string, code: string, body: Record<string, unknown>): Response | null {
  const attrs = (body.attrs ?? {}) as Record<string, string>;
  if (typeof body.name !== "string" || !body.name.trim()) return validation([{ field: "name", reason: "bắt buộc" }]);
  if (c === "cnts_slaughter_facility") {
    if (!attrs.commune_code) return validation([{ field: "attrs.commune_code", reason: "bắt buộc" }]);
    // FN-01 AC-02 (BF-CN-SLAUGHTER #169): xã phải có trong danh mục ĐVHC như máy chủ (pf/masterdata AdminUnit constraint).
    if (!COMMUNE_OK(attrs.commune_code))
      return problem(422, "masterdata.cnts_commune_invalid", "Cơ sở giết mổ phải thuộc một xã/phường/đặc khu có hiệu lực tại ngày áp dụng", {
        errors: [{ field: "attrs.commune_code", reason: "Không có xã/phường/đặc khu này trong danh mục ĐVHC." }],
      });
    const mine = [...(attrs.stamp_codes_gs ?? "").split(","), ...(attrs.stamp_codes_gc ?? "").split(",")].map((s) => s.trim()).filter(Boolean);
    for (const o of items(c)) {
      if (o.code === code) continue;
      const theirs = [...(o.attrs.stamp_codes_gs ?? "").split(","), ...(o.attrs.stamp_codes_gc ?? "").split(",")];
      const dup = mine.find((s) => theirs.includes(s));
      if (dup) return problem(409, "masterdata.cnts_stamp_taken", "Mã số khắc mộc dấu đã thuộc cơ sở giết mổ khác", { field: `${dup} → ${o.code}` });
    }
  }
  if (c === "cnts_vaccine" && !(attrs.species ?? "").trim()) return validation([{ field: "attrs.species", reason: "bắt buộc ít nhất một mã" }]);
  return null;
}

function clean(attrs: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(attrs)) if (String(v).trim() !== "") out[k] = String(v).trim();
  return out;
}

export const cntsCatalogAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)\/items$/,
    handler: (ctx) => {
      const c = ctx.params[0]!;
      if (!canEdit(ctx)) return problem(403, "forbidden", "Forbidden");
      if (!isCnts(c)) return problem(404, "masterdata.catalog_not_found", "Không có danh mục này");
      const day = ctx.url.searchParams.get("at") || vnToday();
      const incl = ctx.url.searchParams.get("include_inactive") !== "false";
      const list = items(c)
        .map((it) => at(it, day))
        .filter((it): it is CatalogItem => !!it && (incl || it.active));
      return json({ items: list, next_cursor: null });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)\/items$/,
    handler: (ctx) => {
      const c = ctx.params[0]!;
      if (!canEdit(ctx)) return problem(403, "forbidden", "Forbidden");
      if (!isCnts(c)) return problem(404, "masterdata.catalog_not_found", "Không có danh mục này");
      const body = (ctx.body ?? {}) as Record<string, unknown>;
      const code = String(body.code ?? "").trim();
      if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$/.test(code)) return validation([{ field: "code", reason: "mã mục 1–40 ký tự [A-Za-z0-9_.-]" }]);
      if (items(c).some((i) => i.code === code)) return problem(409, "masterdata.catalog_item_exists", "Mã đã có trong danh mục");
      const bad = check(c, code, body);
      if (bad) return bad;
      const attrs = clean((body.attrs ?? {}) as Record<string, string>);
      delete attrs.facility_ref;
      if (c === "cnts_slaughter_facility") attrs.facility_ref = uuidv7();
      const vf = typeof body.valid_from === "string" && body.valid_from ? body.valid_from : vnToday();
      const it: CatalogItem = { catalog: c, code, name: String(body.name).trim(), attrs, active: true, deactivation_reason: null, revision: 1, updated_at: nowIso() };
      items(c).push(it);
      record(ctx, it, vf);
      return json({ ...it, valid_from: vf }, 201);
    },
  },
  {
    method: "PUT",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)\/items\/([A-Za-z0-9_.-]+)$/,
    handler: (ctx) => {
      const [c, code] = [ctx.params[0]!, ctx.params[1]!];
      if (!canEdit(ctx)) return problem(403, "forbidden", "Forbidden");
      const it = isCnts(c) ? items(c).find((i) => i.code === code) : undefined;
      if (!it) return problem(404, "masterdata.catalog_item_not_found", "Không có mục danh mục");
      const body = (ctx.body ?? {}) as Record<string, unknown>;
      const bad = check(c, code, body);
      if (bad) return bad;
      const attrs = clean((body.attrs ?? {}) as Record<string, string>);
      if (it.attrs.facility_ref) attrs.facility_ref = it.attrs.facility_ref;
      const vf = typeof body.valid_from === "string" && body.valid_from ? body.valid_from : vnToday();
      if (!VERSIONS.has(keyOf(c, code))) record(ctx, it, "2000-01-01"); // giá trị nạp sẵn áp cho mọi kỳ trước
      Object.assign(it, { name: String(body.name).trim(), attrs, revision: it.revision + 1, updated_at: nowIso() });
      record(ctx, it, vf);
      return json({ ...it, valid_from: vf });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)\/items\/([A-Za-z0-9_.-]+)\/(deactivate|reactivate)$/,
    handler: (ctx) => {
      const [c, code, op] = [ctx.params[0]!, ctx.params[1]!, ctx.params[2]!];
      if (!canEdit(ctx)) return problem(403, "forbidden", "Forbidden");
      const it = isCnts(c) ? items(c).find((i) => i.code === code) : undefined;
      if (!it) return problem(404, "masterdata.catalog_item_not_found", "Không có mục danh mục");
      const active = op === "reactivate";
      if (it.active === active) return problem(409, "masterdata.catalog_item_state", "Mục đã ở trạng thái yêu cầu");
      const reason = reasonOf(ctx.body);
      if (!active && reason.length < 10) return validation([{ field: "reason", reason: "lý do bắt buộc, 10–500 ký tự" }]);
      if (!VERSIONS.has(keyOf(c, code))) record(ctx, it, "2000-01-01");
      Object.assign(it, { active, deactivation_reason: active ? null : reason, revision: it.revision + 1, updated_at: nowIso() });
      record(ctx, it, vnToday());
      return json(it);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)\/items\/([A-Za-z0-9_.-]+)\/versions$/,
    handler: (ctx) => {
      const [c, code] = [ctx.params[0]!, ctx.params[1]!];
      if (!canEdit(ctx)) return problem(403, "forbidden", "Forbidden");
      if (!isCnts(c) || !items(c).some((i) => i.code === code)) return problem(404, "masterdata.catalog_item_not_found", "Không có mục danh mục");
      const vs = sorted(c, code);
      return json({ items: vs.map((v, i) => ({ ...v, valid_to: vs[i + 1]?.valid_from ?? null })) });
    },
  },
];

/** Application API: mọi người dùng đã xác thực đọc danh mục CNTS (Cấp Tỉnh chỉ xem, FN-06). */
export const cntsCatalogAppRoutes: Route[] = [
  {
    // Tham số đã duyệt có hiệu lực tại ?at= (mặc định hiện tại) — ngưỡng mặc định CNTS.
    method: "GET",
    pattern: /^\/v1\/masterdata\/params\/value$/,
    handler: ({ url }) => {
      const key = url.searchParams.get("key") ?? "";
      const t = Date.parse(url.searchParams.get("at") || nowIso());
      const v = PARAM_VERSIONS.filter((x) => x.key === key && x.status === "approved" && Date.parse(x.effective_from) <= t)
        .sort((a, b) => Date.parse(a.effective_from) - Date.parse(b.effective_from) || a.version - b.version)
        .pop();
      if (!v) return problem(404, "masterdata.param_not_found", "Không có tham số đã phê duyệt có hiệu lực tại thời điểm tra");
      return json({
        key: v.key,
        value: v.value,
        text: v.text,
        unit: v.unit,
        version: v.version,
        effective_from: v.effective_from,
        source_doc: v.source_doc,
        approved_at: v.decided_at,
        param_version_id: v.id,
        supersedes_id: null,
        seq: v.version,
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)$/,
    handler: (ctx) => {
      const c = ctx.params[0]!;
      if (!isCnts(c)) return problem(404, "masterdata.catalog_not_found", "Không có danh mục này");
      const day = ctx.url.searchParams.get("at") || vnToday();
      const incl = ctx.url.searchParams.get("include_inactive") === "true";
      const list = items(c)
        .map((it) => at(it, day))
        .filter((it): it is CatalogItem => !!it && (incl || it.active));
      return json({ items: list, next_cursor: null });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/(cnts_[a-z_]+)\/([A-Za-z0-9_.-]+)$/,
    handler: (ctx) => {
      const [c, code] = [ctx.params[0]!, ctx.params[1]!];
      const it = isCnts(c) ? items(c).find((i) => i.code === code) : undefined;
      const v = it ? at(it, ctx.url.searchParams.get("at") || vnToday()) : null;
      return v ? json(v) : problem(404, "masterdata.catalog_item_not_found", "Không có mục danh mục");
    },
  },
];
