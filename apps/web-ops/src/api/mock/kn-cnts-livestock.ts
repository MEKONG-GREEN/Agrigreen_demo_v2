// GIẢ LẬP danh sách hộ chăn nuôi CNTS (CNTS-V4-HOUSEHOLD-LIST #251, BRD CNTS v4.0 FN-10) theo hợp đồng
// /v1/extension/cnts/livestock-households*. Bám quy tắc máy chủ (extension/cntslivestock): Cấp Xã chỉ xã mình, Cấp Tỉnh chỉ
// cán bộ tổng hợp ghi, thêm hộ bắt buộc check-in, trùng họ tên + ấp + xã → 409, vô hiệu hóa có lý do, lịch sử cũ → mới
// (họ tên/SĐT che bớt). Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext, personaForContext } from "./kn";
import { communeName } from "./kn-reports/store";

type S = components["schemas"];
type Detail = S["CNTSLivestockHouseholdDetail"];
type Version = Omit<Detail, "can_edit" | "can_deactivate" | "history" | "warnings" | "owner_name" | "updated_by_name"> & {
  owner_full: string;
  phone_full: string;
  actor: string;
  changes: S["CNTSLivestockChange"][];
};

const HH = new Map<string, Version[]>();
let counter = 0;

/** Xóa trạng thái (kiểm thử). */
export function resetCntsLivestock() {
  HH.clear();
  counter = 0;
}

function who(ctx: Ctx) {
  const id = ctx.request.headers.get("X-AG-Context");
  const kc = knContext(id ?? "");
  const p = personaForContext(ctx.persona, id);
  const role = kc?.role ?? (p.me.scope.level === "commune" ? "kn_xa" : "kn_tinh");
  const communes = role === "kn_xa" ? (kc?.communes.map((c) => c.code) ?? p.me.scope.units) : null;
  return { role, communes, actor: p.me.actor_ref, readOnly: !!kc?.read_only, group: kc?.province_group ?? "" };
}
type Who = ReturnType<typeof who>;

const sees = (w: Who, commune: string) => w.communes === null || w.communes.includes(commune);
const canWrite = (w: Who, commune: string) =>
  !w.readOnly && (w.role === "admin" || (w.role === "kn_xa" && sees(w, commune)) || (w.role === "kn_tinh" && w.group === "tong_hop"));
const canDeact = (w: Who) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && w.group === "tong_hop"));

const label = (name: string) => {
  const ws = name.trim().split(/\s+/).filter(Boolean);
  return ws.map((x, i) => (i === 0 && ws.length > 1 ? x : `${x[0]!.toUpperCase()}.`)).join(" ");
};
const mask = (p: string) => (p.length >= 7 ? `${p.slice(0, 4)}•••${p.slice(-3)}` : "");
const speciesName = (code: string) => CATALOGS.cnts_species?.find((i) => i.code === code)?.name ?? null;

function detail(w: Who, vs: Version[], warnings: Detail["warnings"] = []): Detail {
  const v = vs.at(-1)!;
  const { owner_full, phone_full, actor, changes, ...rest } = v;
  void phone_full;
  void changes;
  const edit = v.status !== "deactivated" && canWrite(w, v.commune_code);
  return {
    ...rest,
    owner_name: edit ? owner_full : null,
    updated_by_name: actor === w.actor ? "Bạn" : "Cán bộ (giả lập)",
    can_edit: edit,
    can_deactivate: v.status !== "deactivated" && canDeact(w),
    warnings,
    history: [...vs].reverse().map((x) => ({
      version: x.version,
      event: x.event,
      actor_ref: x.actor,
      actor_name: "Cán bộ (giả lập)",
      at: x.updated_at,
      reason: x.reason,
      changes: x.changes,
    })),
  };
}

function item(w: Who, vs: Version[]): S["CNTSLivestockHousehold"] {
  const d = detail(w, vs);
  return {
    id: d.id, code: d.code, version: d.version, status: d.status, event: d.event, commune_code: d.commune_code, commune_name: d.commune_name,
    hamlet: d.hamlet, livestock_type: d.livestock_type, animals: d.animals, location: d.location, owner_label: d.owner_label,
    owner_masked_phone: d.owner_masked_phone, can_edit: d.can_edit, can_deactivate: d.can_deactivate, updated_at: d.updated_at, reason: d.reason,
  };
}

type Body = S["CNTSLivestockHouseholdSave"];

function check(b: Body, create: boolean) {
  const errs: { field: string; reason: string }[] = [];
  if (create && !(b.owner_name ?? "").trim()) errs.push({ field: "owner_name", reason: "bắt buộc" });
  if (b.owner_phone && !/^[0-9]{1,15}$/.test(b.owner_phone)) errs.push({ field: "owner_phone", reason: "chỉ nhập chữ số" });
  if (!b.animals?.length) errs.push({ field: "animals", reason: "nhập ít nhất một vật nuôi" });
  b.animals?.forEach((a, i) => {
    if (!Number.isInteger(a.head_count) || a.head_count < 0) errs.push({ field: `animals[${i}].head_count`, reason: "số nguyên ≥ 0" });
  });
  return errs;
}

const animals = (b: Body) => b.animals.map((a) => ({ species_code: a.species_code, species_name: speciesName(a.species_code), head_count: a.head_count }));
const animalsText = (as: Version["animals"]) => as.map((a) => `${a.species_name ?? a.species_code}: ${a.head_count}`).join("; ");
const TYPE: Record<string, string> = { nong_ho: "Nông hộ", trang_trai_nho: "Trang trại nhỏ", trang_trai_vua: "Trang trại vừa", trang_trai_cong_nghiep: "Trang trại công nghiệp" };
const STATUS: Record<string, string> = { active: "Đang dùng", pending_location: "Chờ bổ sung vị trí", deactivated: "Vô hiệu hóa" };
const where = (c: S["CNTSLivestockCheckin"]) => `${c.lat.toFixed(6)}, ${c.lon.toFixed(6)} (±${c.accuracy_m} m)`;
const warn = (c: S["CNTSLivestockCheckin"]): Detail["warnings"] => [
  ...(Number(c.accuracy_m) > 50 ? (["gps_accuracy"] as const) : []),
  ...(c.mocked ? (["mocked_location"] as const) : []),
];

function current(ctx: Ctx, w: Who): Version[] | Response {
  const vs = HH.get(ctx.params[0]!);
  if (!vs || !sees(w, vs.at(-1)!.commune_code)) return problem(404, "extension.cnts_livestock_household_not_found", "Không có hộ chăn nuôi");
  return vs;
}

function guard(vs: Version[], base: number | undefined): Response | null {
  const v = vs.at(-1)!;
  if (v.status === "deactivated") return problem(409, "extension.cnts_livestock_household_deactivated", "Hộ đã vô hiệu hóa — không sửa được");
  if (base !== v.version) return problem(409, "extension.cnts_livestock_household_version_conflict", "Hộ đã có người cập nhật — tải lại bản mới");
  return null;
}

function next(w: Who, vs: Version[], patch: Partial<Version>): Version {
  const v = vs.at(-1)!;
  const n: Version = { ...v, reason: null, changes: [], ...patch, version: v.version + 1, updated_at: new Date().toISOString(), actor: w.actor };
  vs.push(n);
  return n;
}

/** Hộ DB-04 của ổ dịch (giả lập #252): bản hiện hành của hộ — mã, phiên bản, loại (DB-14), ấp, trạng thái, xã, nhãn chủ hộ. */
export interface OutbreakHouseholdRef {
  household_ref: string;
  household_version: number;
  code: string;
  livestock_type: Version["livestock_type"];
  hamlet: string | null;
  status: Version["status"];
  owner_label: string | null;
  commune_code: string;
}

const outbreakRef = (v: Version): OutbreakHouseholdRef => ({
  household_ref: v.id, household_version: v.version, code: v.code, livestock_type: v.livestock_type, hamlet: v.hamlet, status: v.status,
  owner_label: v.owner_label, commune_code: v.commune_code,
});

/** Bản hiện hành của hộ (ô chọn DB-04); không có → null. */
export function livestockHouseholdRef(ref: string): OutbreakHouseholdRef | null {
  const v = HH.get(ref)?.at(-1);
  return v ? outbreakRef(v) : null;
}

/** Hộ nhập mới tại DB-04 của BM-10: tạo ở trạng thái "Chờ bổ sung vị trí", không check-in (FN-10 BR-08). */
export function createPendingHousehold(actor: string, commune: string, b: S["CNTSOutbreakNewHousehold"]): OutbreakHouseholdRef {
  const id = uuidv7();
  const name = b.owner_name.trim().replace(/\s+/g, " ");
  const v: Version = {
    id, code: `HCN${String(++counter).padStart(7, "0")}`, version: 1, status: "pending_location", event: "created_pending", commune_code: commune,
    commune_name: communeName(commune), hamlet: b.hamlet?.trim() || null, livestock_type: b.livestock_type,
    animals: b.animals.map((a) => ({ species_code: a.species_code, species_name: speciesName(a.species_code), head_count: a.head_count })),
    location: null, owner_label: label(name), owner_masked_phone: b.owner_phone ? mask(b.owner_phone) : null, updated_at: new Date().toISOString(),
    reason: null, owner_full: name, phone_full: b.owner_phone ?? "", actor, changes: [],
  };
  HH.set(id, [v]);
  return outbreakRef(v);
}

export const cntsLivestockRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/livestock-households$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.role === "kn_tw") return problem(403, "forbidden", "Danh sách hộ chăn nuôi thuộc Cấp Tỉnh, Cấp Xã");
      const q = ctx.url.searchParams;
      const limit = Number(q.get("limit") ?? 50), offset = Number(q.get("offset") ?? 0);
      const rows = [...HH.values()]
        .filter((vs) => {
          const v = vs.at(-1)!;
          return sees(w, v.commune_code) && (!q.get("commune") || v.commune_code === q.get("commune")) && (!q.get("status") || v.status === q.get("status")) &&
            (!q.get("livestock_type") || v.livestock_type === q.get("livestock_type"));
        })
        .sort((a, b) => a[0]!.code.localeCompare(b[0]!.code));
      return json({ items: rows.slice(offset, offset + limit).map((vs) => item(w, vs)), total: rows.length, limit, offset });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/livestock-households$/,
    handler: (ctx) => {
      const w = who(ctx);
      const b = ctx.body as Body;
      const errs = check(b, true);
      if (errs.length) return validation(errs);
      if (!canWrite(w, b.commune_code)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      if (!b.checkin) return problem(422, "extension.cnts_livestock_household_checkin_required", "Thêm hộ phải check-in tại nhà hộ trước khi lưu", { errors: [{ field: "checkin", reason: "chưa check-in tại nhà hộ (AC-05)" }] });
      const norm = (s: string | null | undefined) => (s ?? "").trim().replace(/\s+/g, " ").toLowerCase();
      const dups = [...HH.values()].map((vs) => vs.at(-1)!).filter((v) => v.status === "active" && v.commune_code === b.commune_code && norm(v.hamlet) === norm(b.hamlet) && norm(v.owner_full) === norm(b.owner_name));
      if (dups.length && !b.confirm_distinct)
        return problem(409, "extension.cnts_livestock_household_duplicate", "Trùng họ tên chủ hộ, ấp, xã với hộ đang dùng", {
          errors: [{ field: "confirm_distinct", reason: "trùng hộ " + dups.map((d) => d.code).join(", ") }],
          duplicates: dups.map((d) => d.code),
        });
      const id = uuidv7();
      const name = (b.owner_name ?? "").trim().replace(/\s+/g, " ");
      const v: Version = {
        id, code: `HCN${String(++counter).padStart(7, "0")}`, version: 1, status: "active", event: "created", commune_code: b.commune_code,
        commune_name: communeName(b.commune_code), hamlet: b.hamlet?.trim() || null, livestock_type: b.livestock_type, animals: animals(b),
        location: { ...b.checkin }, owner_label: label(name), owner_masked_phone: b.owner_phone ? mask(b.owner_phone) : null,
        updated_at: new Date().toISOString(), reason: null, owner_full: name, phone_full: b.owner_phone ?? "", actor: w.actor, changes: [],
      };
      HH.set(id, [v]);
      return json(detail(w, [v], warn(b.checkin)), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/livestock-households\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const vs = current(ctx, w);
      return vs instanceof Response ? vs : json(detail(w, vs));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/livestock-households\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const vs = current(ctx, w);
      if (vs instanceof Response) return vs;
      const b = ctx.body as Body;
      const g = guard(vs, b.base_version);
      if (g) return g;
      if (!canWrite(w, vs.at(-1)!.commune_code) || !canWrite(w, b.commune_code)) return problem(403, "forbidden", "Không có quyền sửa hộ");
      const errs = check(b, false);
      if (errs.length) return validation(errs);
      const v = vs.at(-1)!;
      const name = b.owner_name?.trim().replace(/\s+/g, " ") || v.owner_full;
      const phone = b.owner_phone ?? v.phone_full;
      const na = animals(b);
      const changes: S["CNTSLivestockChange"][] = [];
      if (name !== v.owner_full) changes.push({ field: "owner_name", old: v.owner_label ?? "", new: label(name) });
      if (phone !== v.phone_full) changes.push({ field: "owner_phone", old: mask(v.phone_full), new: mask(phone) });
      if (b.commune_code !== v.commune_code) changes.push({ field: "commune_code", old: communeName(v.commune_code), new: communeName(b.commune_code) });
      if ((b.hamlet?.trim() || null) !== v.hamlet) changes.push({ field: "hamlet", old: v.hamlet ?? "", new: b.hamlet?.trim() ?? "" });
      if (animalsText(na) !== animalsText(v.animals)) changes.push({ field: "animals", old: animalsText(v.animals), new: animalsText(na) });
      if (b.livestock_type !== v.livestock_type) changes.push({ field: "livestock_type", old: TYPE[v.livestock_type]!, new: TYPE[b.livestock_type]! });
      if (!changes.length) return problem(422, "validation_failed", "Không có thay đổi");
      next(w, vs, {
        event: "updated", commune_code: b.commune_code, commune_name: communeName(b.commune_code), hamlet: b.hamlet?.trim() || null,
        livestock_type: b.livestock_type, animals: na, owner_full: name, phone_full: phone, owner_label: label(name),
        owner_masked_phone: phone ? mask(phone) : null, changes,
      });
      return json(detail(w, vs));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/livestock-households\/([^/]+)\/checkin$/,
    handler: (ctx) => {
      const w = who(ctx);
      const vs = current(ctx, w);
      if (vs instanceof Response) return vs;
      const b = ctx.body as S["CNTSLivestockHouseholdCheckinRequest"];
      const g = guard(vs, b.base_version);
      if (g) return g;
      const v = vs.at(-1)!;
      if (!canWrite(w, v.commune_code)) return problem(403, "forbidden", "Không có quyền check-in hộ");
      if (v.status === "active" && [...(b.reason ?? "").trim()].length < 5) return validation([{ field: "reason", reason: "đổi vị trí phải nhập lý do" }]);
      const changes: S["CNTSLivestockChange"][] = [{ field: "location", old: v.location ? where(v.location) : "Chưa có vị trí", new: where(b.checkin) }];
      if (v.status === "pending_location") changes.push({ field: "status", old: STATUS.pending_location!, new: STATUS.active! });
      next(w, vs, { event: v.status === "pending_location" ? "checked_in" : "relocated", status: "active", location: { ...b.checkin }, reason: b.reason?.trim() || null, changes });
      return json(detail(w, vs, warn(b.checkin)));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/livestock-households\/([^/]+)\/deactivate$/,
    handler: (ctx) => {
      const w = who(ctx);
      const vs = current(ctx, w);
      if (vs instanceof Response) return vs;
      const b = ctx.body as S["CNTSStatusChange"];
      const g = guard(vs, b.base_version);
      if (g) return g;
      if (!canDeact(w)) return problem(403, "forbidden", "Chỉ cán bộ tổng hợp Cấp Tỉnh (hoặc Admin) vô hiệu hóa hộ");
      if ([...(b.reason ?? "").trim()].length < 5) return validation([{ field: "reason", reason: "bắt buộc 5–1000 ký tự" }]);
      next(w, vs, { event: "deactivated", status: "deactivated", reason: b.reason.trim(), changes: [{ field: "status", old: STATUS[vs.at(-1)!.status]!, new: STATUS.deactivated! }] });
      return json(detail(w, vs));
    },
  },
];
