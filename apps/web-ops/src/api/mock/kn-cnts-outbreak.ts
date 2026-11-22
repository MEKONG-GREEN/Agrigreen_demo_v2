// GIẢ LẬP Báo cáo dịch bệnh BM-10 và báo nghi dịch (W11.49, BRD CNTS v3.7 FN-11, UR-XA-10) theo hợp đồng
// /v1/extension/cnts/outbreaks*, /outbreak-points, /suspects*: ổ dịch là chuỗi phiên bản đủ trường (gửi → cập nhật diễn biến
// → Cấp Tỉnh kết thúc / mở lại), khóa lạc quan base_version, quy tắc chặn (thiếu vị trí, chết/tiêu hủy > quy mô đàn), tin báo
// nghi dịch xác nhận thành ổ dịch hoặc bác bỏ. Dữ liệu tổng hợp, không phải thật.
// CNTS-V4-OUTBREAK-MULTI (#252): DB-04 chọn hộ trong danh sách hộ chăn nuôi (giả lập kn-cnts-livestock) hoặc nhập hộ mới
// (tạo hộ "Chờ bổ sung vị trí"), DB-14 chụp loại theo hộ, DB-15 tự đếm, DB-16 bắt buộc khi bệnh thuộc nhóm thủy sản.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { createPendingHousehold, livestockHouseholdRef } from "./kn-cnts-livestock";

type S = components["schemas"];
type Outbreak = S["CNTSOutbreak"];
type Suspect = S["CNTSSuspect"];
type OutbreakInput = S["CNTSOutbreakInput"];

const OUTBREAKS = new Map<string, Outbreak[]>(); // ref → phiên bản, mới nhất cuối
const SUSPECTS = new Map<string, Suspect[]>();

/** Xóa trạng thái (kiểm thử). */
export function resetCntsOutbreaks() {
  OUTBREAKS.clear();
  SUSPECTS.clear();
}

interface Who { role: string; communes: string[] | null; actor: string; readOnly: boolean; dept: string | null }
function who(ctx: Ctx): Who {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const role = kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh");
  return {
    role,
    communes: role === "kn_xa" ? (kc?.communes ?? []).map((c) => c.code) : null,
    actor: ctx.persona.me.actor_ref,
    readOnly: !!kc?.read_only,
    dept: kc?.department_code ?? null,
  };
}

const sees = (w: Who, commune: string) => w.communes === null || w.communes.includes(commune);
const body = <T>(ctx: Ctx) => (ctx.body ?? {}) as T;
const status = (event: string): Outbreak["status"] => (event === "reported" ? "reported" : event === "closed" ? "closed" : "monitoring");
const name = (catalog: "cnts_disease" | "cnts_species", code: string) => CATALOGS[catalog].find((x) => x.code === code)?.name ?? code;
const today = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const dayOf = (iso: string) => new Date(Date.parse(iso) + 7 * 3_600_000).toISOString().slice(0, 10);

function reader(w: Who): Response | null {
  if (w.role === "admin") return null;
  if ((w.role === "kn_tinh" || w.role === "kn_xa") && (w.dept === null || w.dept === "chan_nuoi_thuy_san")) return null;
  return problem(403, "forbidden", "Báo cáo dịch bệnh thuộc phòng Chăn nuôi – Thủy sản (Admin, Cấp Tỉnh, Cấp Xã)");
}
function writer(w: Who, role: "kn_xa" | "kn_tinh"): Response | null {
  const r = reader(w);
  if (r) return r;
  if (w.role !== "admin" && w.role !== role) {
    return problem(403, "forbidden", role === "kn_xa" ? "chỉ Cấp Xã lập, cập nhật báo cáo dịch bệnh" : "chỉ Cấp Tỉnh xác nhận kết thúc, mở lại ổ dịch");
  }
  return w.readOnly ? problem(403, "extension.read_only", "Ngữ cảnh chỉ xem") : null;
}

const HA = /^\d{1,10}(\.\d{1,2})?$/;

/** Bệnh nhóm thủy sản (như máy chủ): một loài của bệnh có nhóm thuy_san; bệnh chưa khai loài → xét vật nuôi ổ dịch. */
function aquatic(o: OutbreakInput): boolean {
  const d = CATALOGS.cnts_disease.find((x) => x.code === o.disease_code);
  const list = (d?.attrs.species ?? "").split(",").map((x) => x.trim()).filter(Boolean);
  const codes = list.length ? list : o.species_code ? [o.species_code] : [];
  return codes.some((c) => CATALOGS.cnts_species.find((x) => x.code === c)?.attrs.group === "thuy_san");
}

function check(o: OutbreakInput, commune: string): { field: string; reason: string }[] {
  const e: { field: string; reason: string }[] = [];
  const refs = o.household_refs ?? [], news = o.new_households ?? [];
  if ((o as { household_ref?: string | null }).household_ref) e.push({ field: "household_ref", reason: "dữ liệu cũ — chọn hộ trong danh sách hộ chăn nuôi (DB-04)" });
  if (refs.length + news.length === 0) e.push({ field: "households", reason: "chọn ít nhất một hộ chăn nuôi hoặc nhập hộ chưa có (DB-04, DB-15 ≥ 1)" });
  if (new Set(refs).size !== refs.length) e.push({ field: "households", reason: "mã hộ bị lặp" });
  refs.forEach((r, i) => {
    const h = livestockHouseholdRef(r);
    if (!h) e.push({ field: `households[${i}]`, reason: "không có trong danh sách hộ chăn nuôi" });
    else if (h.status === "deactivated") e.push({ field: `households[${i}]`, reason: `hộ ${h.code} đã vô hiệu hóa (FN-10 BR-07)` });
    else if (h.commune_code !== commune) e.push({ field: `households[${i}]`, reason: `hộ ${h.code} thuộc xã khác xã của ổ dịch` });
  });
  news.forEach((h, i) => {
    if (!h.owner_name?.trim()) e.push({ field: `new_households[${i}].owner_name`, reason: "bắt buộc, tối đa 200 ký tự (HCN-02)" });
    if (!h.livestock_type) e.push({ field: `new_households[${i}].livestock_type`, reason: "nông hộ, trang trại nhỏ, vừa hoặc công nghiệp" });
    if (!h.animals?.length) e.push({ field: `new_households[${i}].animals`, reason: "nhập ít nhất một vật nuôi" });
  });
  for (const k of ["aqua_farmed_ha", "aqua_wild_ha"] as const) {
    const v = o[k];
    if (v !== null && v !== undefined && !HA.test(v)) e.push({ field: k, reason: "số ha ≥ 0, tối đa 2 chữ số thập phân (DB-16)" });
    else if (aquatic(o) && (v === null || v === undefined)) e.push({ field: k, reason: "bắt buộc khi bệnh thuộc nhóm thủy sản (DB-16)" });
    else if (!aquatic(o) && v !== null && v !== undefined) e.push({ field: k, reason: "chỉ ghi khi bệnh thuộc nhóm thủy sản (DB-16)" });
  }
  if (!o.disease_code || !CATALOGS.cnts_disease.some((x) => x.code === o.disease_code)) e.push({ field: "disease_code", reason: "chọn bệnh trong danh mục (DB-01)" });
  if (!o.species_code || !CATALOGS.cnts_species.some((x) => x.code === o.species_code)) e.push({ field: "species_code", reason: "chọn vật nuôi trong danh mục" });
  if (!o.received_at) e.push({ field: "received_at", reason: "bắt buộc (DB-02)" });
  if (!o.source) e.push({ field: "source", reason: "UBND xã, nông hộ báo hoặc cán bộ phát hiện (DB-03)" });
  if (!o.location) e.push({ field: "location", reason: "chưa có vị trí ổ dịch — lấy GPS hoặc chọn trên bản đồ (DB-05)" });
  else {
    const lat = Number(o.location.lat), lon = Number(o.location.lon);
    if (!(lat >= 8 && lat <= 24 && lon >= 102 && lon <= 118)) e.push({ field: "location", reason: "tọa độ ngoài lãnh thổ Việt Nam" });
  }
  const herd = o.herd_size ?? 0;
  if (herd < 1) e.push({ field: "herd_size", reason: "quy mô đàn phải ≥ 1 con (DB-06)" });
  if ((o.dead ?? 0) > herd) e.push({ field: "dead", reason: "số con chết lớn hơn quy mô đàn (DB-07 > DB-06)" });
  if ((o.culled_head ?? 0) > herd) e.push({ field: "culled_head", reason: "số con tiêu hủy lớn hơn quy mô đàn (DB-10 > DB-06)" });
  if (!o.symptoms?.trim()) e.push({ field: "symptoms", reason: "bắt buộc" });
  if (o.as_of && o.as_of > today()) e.push({ field: "as_of", reason: "ngày diễn biến ở tương lai" });
  return e;
}

/** DB-04 của phiên bản mới: hộ chọn (chụp bản hiện hành) rồi hộ nhập mới (tạo "Chờ bổ sung vị trí"). Gọi sau khi `check` sạch. */
function households(o: OutbreakInput, commune: string, w: Who): S["CNTSOutbreakHousehold"][] {
  const sel = (o.household_refs ?? []).map((r) => livestockHouseholdRef(r)!);
  const made = (o.new_households ?? []).map((h) => createPendingHousehold(w.actor, commune, h));
  return [...sel, ...made].map(({ commune_code, ...h }) => (void commune_code, h));
}

function version(o: OutbreakInput, base: Partial<Outbreak>, event: Outbreak["event"], w: Who, seq: number, ref: string, hs: S["CNTSOutbreakHousehold"][]): Outbreak {
  return {
    legacy: false, households: hs, affected_households: hs.length, aqua_farmed_ha: o.aqua_farmed_ha ? String(Number(o.aqua_farmed_ha)) : null,
    aqua_wild_ha: o.aqua_wild_ha ? String(Number(o.aqua_wild_ha)) : null,
    ref, version: seq, version_id: uuidv7(), event, status: status(event), commune_code: o.commune_code ?? base.commune_code ?? "",
    disease_code: o.disease_code!, disease_name: name("cnts_disease", o.disease_code!), species_code: o.species_code!,
    species_name: name("cnts_species", o.species_code!), received_at: o.received_at!, source: o.source!, suspect_ref: base.suspect_ref ?? null,
    household_ref: null, locality: o.locality || null,
    location: o.location ? { lat: o.location.lat, lon: o.location.lon, accuracy_m: o.location.accuracy_m ?? null, source: o.location.source ?? "map" } : null,
    herd_size: o.herd_size!, herd_age_days: o.herd_age_days ?? null, dead: o.dead ?? 0, symptoms: o.symptoms!, sample_date: o.sample_date || null,
    lab_name: o.lab_name || null, lab_result: o.lab_result || null, culled_head: o.culled_head ?? null, culled_kg: o.culled_kg || null,
    chemical_liters: o.chemical_liters || null, buffer_note: o.buffer_note || null, prevention_note: o.prevention_note || null,
    attachment_refs: o.attachment_refs ?? [], as_of: o.as_of ?? (seq === 1 ? dayOf(o.received_at!) : today()), progress_note: o.progress_note || null,
    status_reason: null, captured_at: o.captured_at ?? null, recorded_at: new Date().toISOString(), actor: { ref: w.actor, display: null },
  };
}

function current(w: Who) {
  return [...OUTBREAKS.values()].map((vs) => vs.at(-1)!).filter((o) => sees(w, o.commune_code));
}

function filtered(ctx: Ctx, w: Who) {
  const q = ctx.url.searchParams;
  const st = q.get("status"), commune = q.get("commune"), disease = q.get("disease"), from = q.get("from"), to = q.get("to");
  return current(w)
    .filter((o) => (!commune || o.commune_code === commune) && (!disease || o.disease_code === disease))
    .filter((o) => (!from || dayOf(o.received_at) >= from) && (!to || dayOf(o.received_at) <= to))
    .map((o) => ({ o, statusOk: !st || o.status === st }))
    .sort((a, b) => b.o.received_at.localeCompare(a.o.received_at));
}

function latest(ctx: Ctx, w: Who, ref: string, base: number | undefined): Outbreak | Response {
  const vs = OUTBREAKS.get(ref);
  const cur = vs?.at(-1);
  if (!cur || !sees(w, cur.commune_code)) return problem(404, "extension.cnts_outbreak_not_found", "Không có ổ dịch");
  if (base === undefined || base === null) return validation([{ field: "base_version", reason: "bắt buộc (khóa lạc quan)" }]);
  if (base !== cur.version) return problem(409, "extension.cnts_outbreak_version_conflict", "Ổ dịch đã có người cập nhật — tải lại bản mới",
    { detail: `Đã có người cập nhật lúc ${new Date(cur.recorded_at).toLocaleString("vi-VN")} (phiên bản ${cur.version})` });
  void ctx;
  return cur;
}

const invalid = (code: string, errors: { field: string; reason: string }[]) =>
  problem(422, code, code.includes("suspect") ? "Tin báo nghi dịch không hợp lệ" : "Báo cáo dịch bệnh không hợp lệ", { errors });

const WORD_NUMBERS = new Map<string, string>();
const WORD_RUNS = new Map<string, S["CNTSOutbreakWordRun"][]>();

export const cntsOutbreakRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/outbreaks$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      const all = filtered(ctx, w);
      const byStatus = (["reported", "monitoring", "closed"] as const).map((k) => ({
        key: k, label: { reported: "Chờ xác nhận", monitoring: "Đang theo dõi", closed: "Đã kết thúc" }[k], count: all.filter((x) => x.o.status === k).length,
      }));
      const diseases = [...new Set(all.map((x) => x.o.disease_code))].sort();
      const byDisease = diseases.map((d) => ({ key: d, label: name("cnts_disease", d), count: all.filter((x) => x.o.disease_code === d).length }));
      return json({ items: all.filter((x) => x.statusOk).map((x) => x.o), by_status: byStatus, by_disease: byDisease });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/outbreaks$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = writer(w, "kn_xa");
      if (deny) return deny;
      const b = body<OutbreakInput>(ctx);
      if (!b.commune_code || !sees(w, b.commune_code)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      const errs = check(b, b.commune_code);
      if (errs.length) return invalid("extension.cnts_outbreak_invalid", errs);
      const o = version(b, {}, "reported", w, 1, uuidv7(), households(b, b.commune_code, w));
      OUTBREAKS.set(o.ref, [o]);
      return json(o, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/outbreak-points$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      const closed = ctx.url.searchParams.get("include_closed") === "true";
      const features = filtered(ctx, w)
        .filter((x) => x.statusOk && x.o.location && (closed || x.o.status !== "closed"))
        .map(({ o }) => ({
          type: "Feature" as const, id: o.ref, geometry: { type: "Point" as const, coordinates: [Number(o.location!.lon), Number(o.location!.lat)] },
          properties: { ref: o.ref, status: o.status, disease_name: o.disease_name, commune_code: o.commune_code, as_of: o.as_of },
        }));
      return json({ type: "FeatureCollection", features });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/outbreaks\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      const vs = OUTBREAKS.get(ctx.params[0]!);
      if (!vs || !sees(w, vs[0]!.commune_code)) return problem(404, "extension.cnts_outbreak_not_found", "Không có ổ dịch");
      const versions = [...vs].reverse();
      return json({ current: versions[0], versions });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/outbreaks\/([^/]+)\/progress$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = writer(w, "kn_xa");
      if (deny) return deny;
      const b = body<OutbreakInput>(ctx);
      const cur = latest(ctx, w, ctx.params[0]!, b.base_version);
      if (cur instanceof Response) return cur;
      if (cur.status === "closed") return problem(409, "extension.cnts_outbreak_closed", "Ổ dịch đã kết thúc");
      const errs = check(b, cur.commune_code);
      const asOf = b.as_of ?? today();
      if (asOf < cur.as_of) errs.push({ field: "as_of", reason: `ngày diễn biến trước ngày của phiên bản hiện hành (${cur.as_of})` });
      if (b.commune_code && b.commune_code !== cur.commune_code) errs.push({ field: "commune_code", reason: "không đổi xã của ổ dịch" });
      if (errs.length) return invalid("extension.cnts_outbreak_invalid", errs);
      const o = version({ ...b, as_of: asOf, commune_code: cur.commune_code }, cur, "updated", w, cur.version + 1, cur.ref, households(b, cur.commune_code, w));
      OUTBREAKS.get(cur.ref)!.push(o);
      return json(o, 201);
    },
  },
  ...(["close", "reopen"] as const).map(
    (kind): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/extension/cnts/outbreaks/([^/]+)/${kind}$`),
      handler: (ctx) => {
        const w = who(ctx);
        const deny = writer(w, "kn_tinh");
        if (deny) return deny;
        const b = body<S["CNTSStatusInput"]>(ctx);
        const cur = latest(ctx, w, ctx.params[0]!, b.base_version);
        if (cur instanceof Response) return cur;
        if (!b.reason?.trim()) return invalid("extension.cnts_outbreak_invalid", [{ field: "reason", reason: "bắt buộc" }]);
        if (kind === "close" && cur.status === "closed") return problem(409, "extension.cnts_outbreak_closed", "Ổ dịch đã kết thúc");
        if (kind === "reopen" && cur.status !== "closed") return problem(409, "extension.cnts_outbreak_not_closed", "Ổ dịch chưa kết thúc");
        const event = kind === "close" ? "closed" : "reopened";
        const o: Outbreak = { ...cur, version: cur.version + 1, version_id: uuidv7(), event, status: status(event), status_reason: b.reason.trim(),
          progress_note: null, as_of: today() > cur.as_of ? today() : cur.as_of, recorded_at: new Date().toISOString(), actor: { ref: w.actor, display: null } };
        OUTBREAKS.get(cur.ref)!.push(o);
        return json(o, 201);
      },
    }),
  ),
  // BF-CN-OUTBREAK (UP-08): Word BM-10 (giả lập pf/export — số {n}/BC-TTKN một số mỗi ổ dịch, tệp .docx giả).
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/outbreaks\/([^/]+)\/word$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      const ref = ctx.params[0]!;
      const cur = OUTBREAKS.get(ref)?.at(-1);
      if (!cur || !sees(w, cur.commune_code)) return problem(404, "extension.cnts_outbreak_not_found", "Không có ổ dịch");
      let no = WORD_NUMBERS.get(ref);
      if (!no) {
        no = `${WORD_NUMBERS.size + 1}/BC-TTKN`;
        WORD_NUMBERS.set(ref, no);
      }
      const runs = WORD_RUNS.get(ref) ?? [];
      const run = { id: uuidv7(), seq: runs.length + 1, template: "cnts.bm10", template_version: 2, doc_number: no, doc_number_auto: true,
        file_name: `bao-cao-dich-benh-${ref}-v${cur.version}.docx`, body_hash: "0".repeat(64), size: 4, actor_ref: w.actor, at: new Date().toISOString() };
      WORD_RUNS.set(ref, [run, ...runs]);
      return json(run, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/outbreaks\/([^/]+)\/words$/,
    handler: (ctx) => json({ items: WORD_RUNS.get(ctx.params[0]!) ?? [] }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/outbreaks\/([^/]+)\/words\/([^/]+)\/download$/,
    handler: (ctx) => {
      const run = (WORD_RUNS.get(ctx.params[0]!) ?? []).find((r) => r.id === ctx.params[1]);
      if (!run) return problem(404, "export.run_not_found", "Không có lần xuất này");
      return new Response(new Blob(["PK\u0003\u0004"]), {
        status: 200,
        headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Content-Disposition": `attachment; filename="${run.file_name}"` },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/suspects$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      const st = ctx.url.searchParams.get("status");
      const items = [...SUSPECTS.values()].map((vs) => vs.at(-1)!).filter((s) => sees(w, s.commune_code) && (!st || s.status === st))
        .sort((a, b) => b.observed_at.localeCompare(a.observed_at));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/suspects$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = writer(w, "kn_xa");
      if (deny) return deny;
      const b = body<S["CNTSSuspectInput"]>(ctx);
      if (!b.commune_code || !sees(w, b.commune_code)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      const errs: { field: string; reason: string }[] = [];
      if (!b.description?.trim()) errs.push({ field: "description", reason: "bắt buộc" });
      if (!b.household_ref && !b.locality?.trim()) errs.push({ field: "locality", reason: "bắt buộc" });
      if (!b.observed_at) errs.push({ field: "observed_at", reason: "bắt buộc" });
      if (errs.length) return invalid("extension.cnts_suspect_invalid", errs);
      const s: Suspect = {
        ref: uuidv7(), version: 1, version_id: uuidv7(), status: "reported", commune_code: b.commune_code, reporter_kind: b.reporter_kind,
        observed_at: b.observed_at, species_code: b.species_code || null, disease_code: b.disease_code || null, household_ref: b.household_ref ?? null,
        locality: b.locality || null, location: b.location ? { ...b.location, accuracy_m: b.location.accuracy_m ?? null, source: null } : null,
        affected: b.affected ?? null, dead: b.dead ?? null, description: b.description.trim(), attachment_refs: b.attachment_refs ?? [], outbreak_ref: null,
        status_reason: null, captured_at: b.captured_at ?? null, recorded_at: new Date().toISOString(), actor: { ref: w.actor, display: null },
      };
      SUSPECTS.set(s.ref, [s]);
      return json(s, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/suspects\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const vs = SUSPECTS.get(ctx.params[0]!);
      if (reader(w) || !vs || !sees(w, vs[0]!.commune_code)) return problem(404, "extension.cnts_suspect_not_found", "Không có tin báo nghi dịch");
      const versions = [...vs].reverse();
      return json({ current: versions[0], versions });
    },
  },
  ...(["confirm", "dismiss"] as const).map(
    (kind): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/extension/cnts/suspects/([^/]+)/${kind}$`),
      handler: (ctx) => {
        const w = who(ctx);
        const deny = writer(w, "kn_xa");
        if (deny) return deny;
        const vs = SUSPECTS.get(ctx.params[0]!);
        const cur = vs?.at(-1);
        if (!cur || !sees(w, cur.commune_code)) return problem(404, "extension.cnts_suspect_not_found", "Không có tin báo nghi dịch");
        if (cur.status !== "reported") return problem(409, "extension.cnts_suspect_resolved", "Tin báo nghi dịch đã được xác nhận hoặc bác bỏ");
        const b = body<OutbreakInput & { reason?: string }>(ctx);
        if (b.base_version !== cur.version) return problem(409, "extension.cnts_suspect_version_conflict", "Tin báo đã có người cập nhật — tải lại bản mới");
        const next = { ...cur, version: cur.version + 1, version_id: uuidv7(), recorded_at: new Date().toISOString(), actor: { ref: w.actor, display: null } };
        if (kind === "dismiss") {
          if (!b.reason?.trim()) return invalid("extension.cnts_suspect_invalid", [{ field: "reason", reason: "bắt buộc" }]);
          const s: Suspect = { ...next, status: "dismissed", status_reason: b.reason.trim() };
          vs!.push(s);
          return json(s, 201);
        }
        const input: OutbreakInput = {
          ...b, commune_code: cur.commune_code, source: b.source ?? cur.reporter_kind, received_at: b.received_at ?? cur.observed_at,
          location: b.location ?? (cur.location ? { ...cur.location, source: "gps" } : null),
        };
        const errs = check(input, cur.commune_code);
        if (errs.length) return invalid("extension.cnts_outbreak_invalid", errs);
        const o = version(input, { suspect_ref: cur.ref }, "reported", w, 1, uuidv7(), households(input, cur.commune_code, w));
        OUTBREAKS.set(o.ref, [o]);
        vs!.push({ ...next, status: "confirmed", outbreak_ref: o.ref });
        return json(o, 201);
      },
    }),
  ),
];
