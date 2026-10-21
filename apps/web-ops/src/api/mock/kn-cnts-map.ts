// GIẢ LẬP bản đồ vùng nuôi, vùng dịch CNTS (W11.52, BRD CNTS v3.7 FN-18, UR-TINH-17) theo hợp đồng /v1/extension/cnts/map/*.
// Mỗi thay đổi là một phiên bản có ngày áp dụng (khóa lạc quan base_version, lý do); bản đồ ngày D lấy phiên bản mới nhất có
// ngày áp dụng ≤ D. Vùng theo bán kính dựng bằng đa giác 64 cạnh (máy chủ dùng PostGIS ST_Buffer). Ổ dịch, điểm, hộ là dữ
// liệu tổng hợp riêng của giả lập này, không phải thật.
import { addDays, centerOf, circlePolygon, containsPoint, distanceM, KIND_LABEL } from "@/kn/cnts/map/api";
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext, personaForContext } from "./kn";
import { AG_COMMUNES } from "./kn-reports/store";

type S = components["schemas"];
type ZoneCore = Omit<S["CNTSMapZoneVersion"], "actor_display">;

const today = () => new Date(Date.now() + 7 * 3600_000).toISOString().slice(0, 10);
const ZONES = new Map<string, ZoneCore[]>();
const OFFICER = "0190f1c4-7d3e-7000-8000-00000000a0c1";
const C1 = AG_COMMUNES[0]!.code;
const C2 = AG_COMMUNES[1]!.code;
const OUTBREAKS: S["CNTSMapOutbreak"][] = [
  { ref: "0190f1c4-7d3e-7000-8000-0000000c8001", version: 2, status: "monitoring", commune_code: C1, disease_code: "DTHCP", disease_name: "Dịch tả heo châu Phi",
    species_code: "HEO", species_name: "Heo", lat: 10.385, lng: 105.425, as_of: addDays(today(), -3), herd_size: 120, dead: 15 },
];
const PLACES: S["CNTSMapPlace"][] = [
  { kind: "site", ref: "0190f1c4-7d3e-7000-8000-0000000c7201", site_ref: "0190f1c4-7d3e-7000-8000-0000000c7201", project_ref: "0190f1c4-7d3e-7000-8000-0000000c7101",
    project_name: "Mô hình chăn nuôi gà an toàn sinh học", commune_code: C1, name: "Điểm ấp Bình Hòa", model_type_code: "GA_THUONG_PHAM", model_type_name: "Gà thương phẩm",
    scale: "1500", scale_unit: "con", lat: 10.38, lng: 105.43 },
  { kind: "household", ref: "0190f1c4-7d3e-7000-8000-0000000c7301", site_ref: "0190f1c4-7d3e-7000-8000-0000000c7201", project_ref: "0190f1c4-7d3e-7000-8000-0000000c7101",
    project_name: "Mô hình chăn nuôi gà an toàn sinh học", commune_code: C1, name: "Điểm ấp Bình Hòa", model_type_code: "GA_THUONG_PHAM", model_type_name: "Gà thương phẩm",
    scale: "500", scale_unit: "con", lat: 10.41, lng: 105.44 },
  { kind: "household", ref: "0190f1c4-7d3e-7000-8000-0000000c7302", site_ref: "0190f1c4-7d3e-7000-8000-0000000c7201", project_ref: "0190f1c4-7d3e-7000-8000-0000000c7101",
    project_name: "Mô hình chăn nuôi gà an toàn sinh học", commune_code: C2, name: "Điểm ấp Bình Hòa", model_type_code: "GA_THUONG_PHAM", model_type_name: "Gà thương phẩm",
    scale: "300", scale_unit: "con", lat: 10.5, lng: 105.45 },
];
let seeded = false;

/** Xóa trạng thái (kiểm thử). */
export function resetCntsMap() {
  ZONES.clear();
  seeded = false;
}

function who(ctx: Ctx) {
  const id = ctx.request.headers.get("X-AG-Context");
  const kc = knContext(id ?? "");
  const p = personaForContext(ctx.persona, id);
  const role = kc?.role ?? (p.me.roles.includes("platform_admin") ? "admin" : p.me.scope.level === "commune" ? "kn_xa" : "kn_tinh");
  const communes = role === "kn_xa" ? (kc?.communes.map((c) => c.code) ?? p.me.scope.units) : null;
  return { role, communes, actor: p.me.actor_ref, readOnly: !!kc?.read_only, group: kc?.province_group ?? "" };
}
type Who = ReturnType<typeof who>;

const sees = (w: Who, c: string) => w.communes === null || w.communes.includes(c);
const seesZone = (w: Who, z: ZoneCore) => z.communes.some((c) => sees(w, c));
const drawer = (w: Who) => !w.readOnly && (w.role === "admin" || (w.role === "kn_tinh" && ["", "vien_chuc"].includes(w.group)));
const view = (w: Who, z: ZoneCore): S["CNTSMapZone"] => ({ ...z, can_edit: drawer(w) && z.communes.every((c) => sees(w, c)), warnings: [] });

function areaHa(g: S["CNTSMapGeometry"]): string | null {
  if (g.type === "LineString") return null;
  const ring = (g.type === "Polygon" ? g.coordinates[0] : (g.coordinates[0] as unknown[])[0]) as [number, number][];
  let s = 0;
  for (let i = 1; i < ring.length; i++) s += ring[i - 1]![0] * ring[i]![1] - ring[i]![0] * ring[i - 1]![1];
  const k = 111_320 ** 2 * Math.cos((ring[0]![1] * Math.PI) / 180);
  return ((Math.abs(s) / 2) * k / 10_000).toFixed(4);
}

function at(day: string, ref: string): ZoneCore | undefined {
  return (ZONES.get(ref) ?? []).filter((v) => v.effective_on <= day).at(-1);
}

function zonesAt(day: string, withRetired = false): ZoneCore[] {
  return [...ZONES.keys()].map((r) => at(day, r)).filter((z): z is ZoneCore => !!z && (withRetired || z.status === "active"));
}

function put(w: Who, ref: string, prev: ZoneCore | undefined, b: Partial<S["CNTSMapZoneSave"]>, event: ZoneCore["event"], reason: string | null): ZoneCore {
  const mode = b.draw_mode ?? prev?.draw_mode ?? "polygon";
  const center = mode === "radius" ? (b.center ?? prev?.center ?? null) : null;
  const radius = mode === "radius" ? (b.radius_m ?? prev?.radius_m ?? null) : null;
  const geometry = mode === "radius" && center && radius ? circlePolygon([center.lng, center.lat], radius) : (b.geometry ?? prev!.geometry);
  const kind = b.kind ?? prev!.kind;
  const z: ZoneCore = {
    ref, version: (prev?.version ?? 0) + 1, event, status: event === "retired" ? "retired" : "active", province_code: prev?.province_code ?? b.province_code ?? "91",
    communes: [...new Set(b.communes ?? prev?.communes ?? [])].sort(), kind, kind_label: KIND_LABEL[kind], name: (b.name ?? prev?.name ?? "").trim(), draw_mode: mode,
    geometry, center, radius_m: radius, area_ha: areaHa(geometry), outbreak_ref: b.outbreak_ref ?? prev?.outbreak_ref ?? null,
    parent_zone_ref: b.parent_zone_ref ?? prev?.parent_zone_ref ?? null, species_code: b.species_code ?? prev?.species_code ?? null,
    disease_code: b.disease_code ?? prev?.disease_code ?? null, effective_on: b.effective_on ?? today(), note: b.note ?? prev?.note ?? null, reason,
    updated_at: new Date().toISOString(), updated_by: w.actor,
  };
  ZONES.set(ref, [...(ZONES.get(ref) ?? []), z]);
  return z;
}

function seed() {
  if (seeded) return;
  seeded = true;
  const w = { role: "kn_tinh", communes: null, actor: OFFICER, readOnly: false, group: "" } as Who;
  put(w, "0190f1c4-7d3e-7000-8000-0000000c9001", undefined, { kind: "vung_thuy_san", name: "Vùng nuôi cá tra Mỹ Hòa Hưng", communes: [C1], draw_mode: "polygon",
    geometry: { type: "Polygon", coordinates: [[[105.40, 10.36], [105.46, 10.36], [105.46, 10.40], [105.40, 10.40], [105.40, 10.36]]] }, effective_on: addDays(today(), -300) }, "created", null);
  const o = OUTBREAKS[0]!;
  const dich = "0190f1c4-7d3e-7000-8000-0000000c9101";
  put(w, dich, undefined, { kind: "vung_dich", name: "Vùng dịch DTHCP – " + o.commune_code, communes: [o.commune_code], draw_mode: "radius",
    center: { lat: o.lat, lng: o.lng }, radius_m: 500, outbreak_ref: o.ref, disease_code: o.disease_code, species_code: o.species_code, effective_on: addDays(today(), -3) }, "created", null);
  put(w, "0190f1c4-7d3e-7000-8000-0000000c9102", undefined, { kind: "vung_uy_hiep", name: "Vùng bị uy hiếp DTHCP – " + o.commune_code, communes: [o.commune_code],
    draw_mode: "radius", center: { lat: o.lat, lng: o.lng }, radius_m: 3000, outbreak_ref: o.ref, parent_zone_ref: dich, effective_on: addDays(today(), -3) }, "created", null);
}

function checkSave(w: Who, b: Partial<S["CNTSMapZoneSave"]>): Response | null {
  if (w.readOnly) return problem(403, "extension.read_only", "Ngữ cảnh chỉ xem");
  if (!drawer(w)) return problem(403, "forbidden", "Chỉ cán bộ phụ trách dịch Cấp Tỉnh (viên chức chuyên môn) hoặc Admin vẽ vùng (FN-18)");
  const errs = [
    !b.name?.trim() && { field: "name", reason: "1–300 ký tự" },
    !b.communes?.length && { field: "communes", reason: "chọn ít nhất một xã" },
    b.draw_mode === "radius" && (!b.center || !b.radius_m || b.radius_m < 10 || b.radius_m > 50000) && { field: "radius_m", reason: "tâm và bán kính 10–50.000 m" },
    b.draw_mode !== "radius" && !b.geometry && { field: "geometry", reason: "bắt buộc" },
    b.effective_on && b.effective_on > today() && { field: "effective_on", reason: "không sau hôm nay" },
  ].filter(Boolean) as { field: string; reason: string }[];
  if (errs.length) return validation(errs);
  if (b.communes?.some((c) => !sees(w, c))) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
  return null;
}

const notFound = () => problem(404, "extension.cnts_zone_not_found", "Không có vùng trên bản đồ");

function contents(w: Who, z: ZoneCore, day: string) {
  const c = centerOf(z.geometry);
  const places = PLACES.filter((p) => sees(w, p.commune_code) && containsPoint(z.geometry, [p.lng, p.lat])).map((p) => ({ ...p, distance_m: Math.round(distanceM(c, [p.lng, p.lat])) }));
  const obs = OUTBREAKS.filter((o) => o.as_of <= day && sees(w, o.commune_code) && containsPoint(z.geometry, [o.lng, o.lat])).map((o) => ({ ...o, distance_m: Math.round(distanceM(c, [o.lng, o.lat])) }));
  return { date: day, zone: view(w, z), places, outbreaks: obs, site_count: places.filter((p) => p.kind === "site").length,
    household_count: places.filter((p) => p.kind === "household").length, outbreak_count: obs.length };
}

export const cntsMapRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/map\/layers$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const day = ctx.url.searchParams.get("date") || today();
      return json({ date: day, can_draw: drawer(w), zones: zonesAt(day).filter((z) => seesZone(w, z)).map((z) => view(w, z)),
        outbreaks: OUTBREAKS.filter((o) => o.as_of <= day && sees(w, o.commune_code)), places: PLACES.filter((p) => sees(w, p.commune_code)),
        plot_stats: [{ commune_code: C1, plots: 42, area_ha: "63.5000" }].filter((p) => sees(w, p.commune_code)), plots_ready: true });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/map\/zones$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const q = ctx.url.searchParams;
      const items = zonesAt(q.get("date") || today(), q.get("with_retired") === "true")
        .filter((z) => seesZone(w, z) && (!q.get("kind") || z.kind === q.get("kind")) && (!q.get("outbreak_ref") || z.outbreak_ref === q.get("outbreak_ref")))
        .map((z) => view(w, z));
      return json({ items, can_draw: drawer(w) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/map\/zones$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const b = (ctx.body ?? {}) as S["CNTSMapZoneSave"];
      const bad = checkSave(w, b);
      if (bad) return bad;
      return json(view(w, put(w, b.id ?? uuidv7(), undefined, b, "created", null)), 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/map\/zones\/from-outbreak$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const b = (ctx.body ?? {}) as S["CNTSMapFromOutbreak"];
      const o = OUTBREAKS.find((x) => x.ref === b.outbreak_ref && sees(w, x.commune_code));
      if (!o) return validation([{ field: "outbreak_ref", reason: "không có ổ dịch này trong phạm vi" }]);
      const radii = { vung_dich: 500, vung_uy_hiep: 3000, vung_dem: 10000, ...(b.radii ?? {}) };
      if (!(radii.vung_dich < radii.vung_uy_hiep && radii.vung_uy_hiep < radii.vung_dem)) return validation([{ field: "radii", reason: "bán kính tăng dần" }]);
      const denied = checkSave(w, { name: "x", communes: [o.commune_code], draw_mode: "radius", center: { lat: o.lat, lng: o.lng }, radius_m: 500 });
      if (denied) return denied;
      let parent: string | null = null;
      const items = (["vung_dich", "vung_uy_hiep", "vung_dem"] as const).map((k) => {
        const body = { kind: k, name: `${KIND_LABEL[k]} ${o.disease_code} – ${o.commune_code}`, communes: [o.commune_code], draw_mode: "radius" as const,
          center: { lat: o.lat, lng: o.lng }, radius_m: radii[k], outbreak_ref: o.ref, parent_zone_ref: parent, disease_code: o.disease_code,
          species_code: o.species_code, effective_on: b.effective_on ?? today() };
        const z = put(w, uuidv7(), undefined, body, "created", null);
        if (k === "vung_dich") parent = z.ref;
        return view(w, z);
      });
      return json({ items }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/map\/zones\/([0-9a-f-]+)$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const vs = ZONES.get(ctx.params[0]!);
      const cur = vs?.at(-1);
      if (!cur || !seesZone(w, cur)) return notFound();
      return json({ ...view(w, cur), versions: [...vs!].reverse().map((v) => ({ ...v, actor_display: v.updated_by === OFFICER ? "Cán bộ phụ trách dịch (giả lập)" : null })) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/map\/zones\/([0-9a-f-]+)$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const cur = ZONES.get(ctx.params[0]!)?.at(-1);
      if (!cur || !seesZone(w, cur)) return notFound();
      const b = (ctx.body ?? {}) as S["CNTSMapZoneSave"];
      const bad = checkSave(w, b);
      if (bad) return bad;
      if (cur.status === "retired") return problem(409, "extension.cnts_zone_retired", "Vùng đã ngưng hiển thị — khôi phục trước khi sửa");
      if (b.base_version !== cur.version) return problem(409, "extension.cnts_zone_version_conflict", "Vùng đã có người cập nhật — tải lại bản mới");
      if (!b.reason || b.reason.trim().length < 5) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
      if ((b.effective_on ?? today()) < cur.effective_on) return validation([{ field: "effective_on", reason: "không sớm hơn ngày áp dụng của phiên bản trước" }]);
      return json(view(w, put(w, cur.ref, cur, b, "updated", b.reason.trim())));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/map\/zones\/([0-9a-f-]+)\/(retire|restore)$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const cur = ZONES.get(ctx.params[0]!)?.at(-1);
      if (!cur || !seesZone(w, cur)) return notFound();
      if (!drawer(w)) return problem(403, w.readOnly ? "extension.read_only" : "forbidden", "Không có quyền");
      const b = (ctx.body ?? {}) as S["CNTSMapZoneStatus"];
      const retire = ctx.params[1] === "retire";
      if (b.base_version !== cur.version || (cur.status === "retired") === retire) return problem(409, "extension.cnts_zone_version_conflict", "Vùng đã có người cập nhật — tải lại bản mới");
      if (!b.reason || b.reason.trim().length < 5) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
      return json(view(w, put(w, cur.ref, cur, { effective_on: b.effective_on ?? today() }, retire ? "retired" : "restored", b.reason.trim())));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/map\/zones\/([0-9a-f-]+)\/contents$/,
    handler: (ctx) => {
      seed();
      const w = who(ctx);
      const day = ctx.url.searchParams.get("date") || today();
      const z = at(day, ctx.params[0]!);
      if (!z || !seesZone(w, z)) return notFound();
      return json(contents(w, z, day));
    },
  },
];
