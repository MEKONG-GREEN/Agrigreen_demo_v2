// GIẢ LẬP tổng hợp CNTS Cấp Tỉnh (W11.44, BRD CNTS v3.7 UR-TINH-14, UR-TINH-15) theo hợp đồng /v1/extension/cnts/bm02|bm18|
// aggregates|shares: BM-02 = tổng BM-01 đã gửi theo cơ sở × tuần KSGM (1–7, 8–14, 15–21, 22–cuối tháng) + dòng điều chỉnh;
// BM-18 = khai thác BM-17 theo xã + nuôi trồng, cột Tuần trước / Trong kỳ / Lũy kế quý / Lũy kế năm (theo ngày cuối tuần);
// chốt khi mọi kỳ nguồn của tuần đã chốt; phiên bản tệp chia sẻ (tệp tải về là tệp giả). Đọc kỳ/bài của kn-cnts-periods.
// Số dùng Number (giả lập) — máy chủ dùng decimal.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { cntsPeriodFigures } from "./kn-cnts-periods";
import { calendarAt, DEPT_CNTS } from "./kn-reports/domain";

type S = components["schemas"];
const D = 86_400_000;
const ymd = (t: number) => new Date(t).toISOString().slice(0, 10);
const parse = (d: string) => Date.parse(d + "T00:00:00Z");
const s = (n: number) => String(Math.round(n * 1e6) / 1e6);

const SPECIES = [{ key: "trau_bo", label: "Trâu bò" }, { key: "heo", label: "Heo" }, { key: "gia_cam", label: "Gia cầm" }, { key: "de", label: "Dê" }];
const GROUPS = [{ key: "ca", label: "Cá các loại" }, { key: "tom", label: "Tôm các loại" }, { key: "muc", label: "Mực" }, { key: "khac", label: "Thủy sản khác" },
  { key: "tong", label: "Tổng khai thác" }];
const AQUA_GROUPS = [{ key: "nuoi_tom", label: "Nuôi tôm" }, { key: "nhuyen_the", label: "Nhuyễn thể" }, { key: "ca_ao_ruong_veo", label: "Cá ao, ruộng, vèo" }];
const AQUA: [string, string, string, string, string][] = [
  ["tom_tc_btc", "Tôm thâm canh – bán thâm canh", "nuoi_tom", "ha", "tấn"], ["tom_cnc", "Tôm công nghệ cao", "nuoi_tom", "ha", "tấn"],
  ["tom_lua", "Tôm – lúa", "nuoi_tom", "ha", "tấn"], ["tom_cang_xanh", "Tôm càng xanh", "nuoi_tom", "ha", "tấn"],
  ["tom_qccc", "Tôm quảng canh cải tiến", "nuoi_tom", "ha", "tấn"], ["tom_rung", "Tôm rừng", "nuoi_tom", "ha", "tấn"],
  ["ca_long_bien", "Cá lồng bè trên biển", "", "lồng", "tấn"], ["ngoc_trai", "Ngọc trai", "", "ha", "1.000 viên"],
  ["long_be_nuoc_ngot", "Lồng bè nước ngọt", "", "lồng", "tấn"], ["ca_tra", "Cá tra", "", "ha", "tấn"], ["ca_ro_phi", "Cá rô phi", "", "ha", "tấn"],
  ["hen", "Hến", "nhuyen_the", "ha", "tấn"], ["so_huyet", "Sò huyết", "nhuyen_the", "ha", "tấn"], ["so_long", "Sò lông", "nhuyen_the", "ha", "tấn"],
  ["vem", "Vẹm", "nhuyen_the", "ha", "tấn"], ["ngheu", "Nghêu", "nhuyen_the", "ha", "tấn"], ["cua_bien", "Cua biển", "", "ha", "tấn"],
  ["ca_loc", "Cá lóc", "ca_ao_ruong_veo", "ha", "tấn"], ["ech", "Ếch", "ca_ao_ruong_veo", "ha", "tấn"], ["luon", "Lươn", "", "ha", "tấn"],
];

interface Adj { id: string; week_key: string; facility_ref: string; commune: string; deltas: Record<string, string>; reason: string; base: Record<string, number>; at: string; by: string }
interface Aqua { week: string; to: string; object: string; area: string | null; output: string | null }
interface AggEv { action: "closed" | "reopened"; at: string; hash: string }
interface Share { id: string; version: number; at: string; basis: "submitted" | "closed"; note: string; hash: string; by: string }

const ADJ: Adj[] = [];
const AQUA_ROWS: Aqua[] = [];
const AGG = new Map<string, AggEv[]>();
const SHARES = new Map<string, Share[]>();

/** Xóa trạng thái (kiểm thử). */
export function resetCntsAggregate() {
  ADJ.length = 0;
  AQUA_ROWS.length = 0;
  AGG.clear();
  SHARES.clear();
}

function role(ctx: Ctx) {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  return kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh");
}
const writer = (ctx: Ctx) => (role(ctx) === "kn_tinh" ? null : problem(403, "forbidden", "chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản ghi"));
const body = <T>(ctx: Ctx) => (ctx.body ?? {}) as T;
const facilityName = (ref: string) => CATALOGS.cnts_slaughter_facility.find((x) => x.attrs.facility_ref === ref)?.name ?? ref;

function aggState(key: string, evs: AggEv[] = []): S["CntsAggregateState"] {
  const last = evs.at(-1);
  const closes = evs.filter((e) => e.action === "closed");
  return { key, state: last?.action ?? "none", close_count: closes.length, last_at: last?.at ?? null, figures_hash: closes.at(-1)?.hash ?? null, changed: false };
}
const closedNow = (k: string) => AGG.get(k)?.at(-1)?.action === "closed";

// --- tuần KSGM ---
function ksgmWeeks(y: number, m: number): S["CntsKeySpan"][] {
  const last = new Date(Date.UTC(y, m, 0)).getUTCDate();
  const starts = [1, 8, 15, 22];
  return starts.map((d, i) => {
    const from = Date.UTC(y, m - 1, d);
    const to = Date.UTC(y, m - 1, i < 3 ? starts[i + 1]! - 1 : last);
    return { key: `${y}-${String(m).padStart(2, "0")}-K${i + 1}`, label: `Tuần KSGM ${i + 1} tháng ${m}/${y}`, from: ymd(from), to: ymd(to) };
  });
}

/** Bỏ trường nội bộ của dòng giả lập. */
function strip<T extends { _auto: unknown; _adj: unknown }>(r: T): Omit<T, "_auto" | "_adj"> {
  const out = { ...r } as Partial<T>;
  delete out._auto;
  delete out._adj;
  return out as Omit<T, "_auto" | "_adj">;
}

function bm02(ctx: Ctx): Response {
  const q = ctx.url.searchParams;
  const pt = q.get("period_type") === "month" ? "month" : "ksgm_week";
  let key = q.get("key") ?? "";
  const now = new Date(Date.now() + 7 * 3_600_000);
  if (!key) key = pt === "month" ? now.toISOString().slice(0, 7) : ksgmWeeks(now.getUTCFullYear(), now.getUTCMonth() + 1).find((w) => w.from <= ymd(now.getTime()) && ymd(now.getTime()) <= w.to)!.key;
  const m = /^(\d{4})-(\d{2})(?:-K([1-4]))?$/.exec(key);
  if (!m) return validation([{ field: "key", reason: "tuần KSGM YYYY-MM-Kn hoặc tháng YYYY-MM" }]);
  const y = Number(m[1]);
  const mo = Number(m[2]);
  const all = ksgmWeeks(y, mo);
  const weeks = pt === "month" ? all : all.filter((w) => w.key === key);
  if (!weeks.length) return validation([{ field: "key", reason: "không có tuần" }]);
  const from = weeks[0]!.from;
  const to = weeks.at(-1)!.to;
  const closedBasis = q.get("basis") === "closed";
  const days = cntsPeriodFigures("CNTS-BM01").filter((p) => p.from >= from && p.from <= to);
  type Row = S["CntsBM02Row"] & { _auto: Record<string, number>; _adj: Record<string, number> };
  const rows = new Map<string, Row>();
  const get = (ref: string, commune: string): Row => {
    let r = rows.get(ref);
    if (!r) {
      r = { facility_ref: ref, facility_code: "", facility_name: facilityName(ref), facility_kind: "", stamps_gs: [], stamps_gc: [], commune_code: commune,
        commune_name: commune, auto: {}, adjust: {}, total: {}, days_expected: 0, days_reported: 0, missing_days: 0, stale: false, _auto: {}, _adj: {} };
      rows.set(ref, r);
    }
    return r;
  };
  const autoByWeek = new Map<string, Record<string, number>>();
  for (const p of days) {
    const wk = all.find((w) => w.from <= p.from && p.from <= w.to)!.key;
    p.units.forEach((u, i) => {
      const r = get(u, p.parents[i] ?? "");
      r.days_expected++;
      const last = p.last.get(u);
      if (!last || (closedBasis && p.status !== "closed")) return;
      r.days_reported++;
      const ab = autoByWeek.get(u + wk) ?? {};
      for (const sp of SPECIES) {
        const v = Number(last.values[sp.key] ?? 0);
        r._auto[sp.key] = (r._auto[sp.key] ?? 0) + v;
        ab[sp.key] = (ab[sp.key] ?? 0) + v;
      }
      autoByWeek.set(u + wk, ab);
    });
  }
  const adjs = ADJ.filter((a) => weeks.some((w) => w.key === a.week_key));
  for (const a of adjs) {
    const r = get(a.facility_ref, a.commune);
    for (const [k, v] of Object.entries(a.deltas)) r._adj[k] = (r._adj[k] ?? 0) + Number(v);
    if (SPECIES.some((sp) => (a.base[sp.key] ?? 0) !== (autoByWeek.get(a.facility_ref + a.week_key)?.[sp.key] ?? 0))) r.stale = true;
  }
  const out = [...rows.values()].sort((a, b) => (a.commune_code + a.facility_name).localeCompare(b.commune_code + b.facility_name));
  const tot = { auto: {} as Record<string, number>, adjust: {} as Record<string, number> };
  const communes: S["CntsBM02Commune"][] = [];
  for (const r of out) {
    for (const sp of SPECIES) {
      const a = r._auto[sp.key] ?? 0;
      const d = r._adj[sp.key] ?? 0;
      r.auto[sp.key] = s(a);
      r.adjust[sp.key] = s(d);
      r.total[sp.key] = s(a + d);
      tot.auto[sp.key] = (tot.auto[sp.key] ?? 0) + a;
      tot.adjust[sp.key] = (tot.adjust[sp.key] ?? 0) + d;
    }
    r.missing_days = Math.max(0, r.days_expected - r.days_reported);
    let c = communes.find((x) => x.commune_code === r.commune_code);
    if (!c) communes.push((c = { commune_code: r.commune_code, commune_name: r.commune_name, total: {}, facilities: 0 }));
    c.facilities++;
    for (const sp of SPECIES) c.total[sp.key] = s(Number(c.total[sp.key] ?? 0) + Number(r.total[sp.key]));
  }
  const map = (f: (k: string) => number) => Object.fromEntries(SPECIES.map((sp) => [sp.key, s(f(sp.key))]));
  const prevMonth = new Date(Date.UTC(y, mo - 2, 1)).toISOString().slice(0, 7);
  const nextMonth = new Date(Date.UTC(y, mo, 1)).toISOString().slice(0, 7);
  const idx = all.findIndex((w) => w.key === weeks[0]!.key);
  const v: S["CntsBM02"] = {
    province: "89", period_type: pt, period_key: pt === "month" ? key.slice(0, 7) : key,
    label: pt === "month" ? `Tháng ${mo}/${y}` : `${weeks[0]!.label} (${weeks[0]!.from.slice(8)}/${mo} – ${weeks[0]!.to.slice(8)}/${mo})`, from, to,
    prev_key: pt === "month" ? prevMonth : idx > 0 ? all[idx - 1]!.key : `${prevMonth}-K4`,
    next_key: pt === "month" ? nextMonth : idx < 3 ? all[idx + 1]!.key : `${nextMonth}-K1`,
    basis: closedBasis ? "closed" : "submitted", weeks, species: SPECIES,
    rows: out.map(strip), communes,
    auto: map((k) => tot.auto[k] ?? 0), adjust: map((k) => tot.adjust[k] ?? 0), total: map((k) => (tot.auto[k] ?? 0) + (tot.adjust[k] ?? 0)),
    sources: { count: days.length, closed: days.filter((p) => p.status === "closed").length, all_closed: days.length > 0 && days.every((p) => p.status === "closed") },
    units_total: out.length, units_reported: out.filter((r) => r.days_reported > 0).length,
    adjustments: adjs.map((a) => ({ id: a.id, week_key: a.week_key, facility_ref: a.facility_ref, facility_name: facilityName(a.facility_ref), commune_code: a.commune,
      deltas: map((k) => Number(a.deltas[k] ?? 0)), reason: a.reason, actor_name: "Cán bộ tổng hợp", at: a.at,
      stale: SPECIES.some((sp) => (a.base[sp.key] ?? 0) !== (autoByWeek.get(a.facility_ref + a.week_key)?.[sp.key] ?? 0)) })),
    aggregates: weeks.map((w) => aggState(w.key, AGG.get(`CNTS-BM02:${w.key}`))),
    shares: shareViews(`CNTS-BM02:${pt}:${pt === "month" ? key.slice(0, 7) : key}`),
  };
  return json(v);
}

// --- tuần CNTS ---
function weekSpan(key: string): { key: string; from: string; to: string; no: number; y: number } | null {
  const ws = calendarAt(DEPT_CNTS, "2026-01-01").rule.week_start;
  const startOf = (t: number) => t - ((new Date(t).getUTCDay() - ws + 7) % 7) * D;
  let m = /^(\d{4})-W(\d{2})$/.exec(key);
  if (!key) {
    const t = parse(ymd(Date.now() + 7 * 3_600_000));
    const st = startOf(t);
    const y = new Date(st + 6 * D).getUTCFullYear();
    const no = Math.floor((st - startOf(Date.UTC(y, 0, 1))) / (7 * D)) + 1;
    m = [key, String(y), String(no).padStart(2, "0")] as unknown as RegExpExecArray;
  }
  if (!m) return null;
  const y = Number(m[1]);
  const no = Number(m[2]);
  const from = startOf(Date.UTC(y, 0, 1)) + (no - 1) * 7 * D;
  return { key: `${y}-W${String(no).padStart(2, "0")}`, from: ymd(from), to: ymd(from + 6 * D), no, y };
}
const keyOf = (from: string) => {
  const ws = calendarAt(DEPT_CNTS, "2026-01-01").rule.week_start;
  const startOf = (t: number) => t - ((new Date(t).getUTCDay() - ws + 7) % 7) * D;
  const st = startOf(parse(from));
  const y = new Date(st + 6 * D).getUTCFullYear();
  return `${y}-W${String(Math.floor((st - startOf(Date.UTC(y, 0, 1))) / (7 * D)) + 1).padStart(2, "0")}`;
};

function bm18(ctx: Ctx, weekKey?: string): Response {
  const q = ctx.url.searchParams;
  const w = weekSpan(weekKey ?? q.get("week") ?? "");
  if (!w) return validation([{ field: "week", reason: "dạng YYYY-Www" }]);
  const closedBasis = q.get("basis") === "closed";
  const prevTo = ymd(parse(w.from) - D);
  const year = w.to.slice(0, 4);
  const qOf = (d: string) => Math.floor((Number(d.slice(5, 7)) - 1) / 3);
  const inYTD = (to: string) => to.slice(0, 4) === year && to <= w.to;
  const inQTD = (to: string) => inYTD(to) && qOf(to) === qOf(w.to);
  const periods = cntsPeriodFigures("CNTS-BM17").filter((p) => p.to === prevTo || inYTD(p.to));
  type C = { prev: number; cur: number; qtd: number; ytd: number };
  const zero = (): C => ({ prev: 0, cur: 0, qtd: 0, ytd: 0 });
  const communes = new Map<string, { cells: Record<string, C>; expected: boolean; reported: boolean; missing: string[]; by: Record<string, Record<string, string>> }>();
  const cur = periods.filter((p) => p.key === w.key);
  for (const p of periods) {
    p.units.forEach((u) => {
      let c = communes.get(u);
      if (!c) communes.set(u, (c = { cells: Object.fromEntries(GROUPS.map((g) => [g.key, zero()])), expected: false, reported: false, missing: [], by: {} }));
      const last = p.last.get(u);
      const ok = last && (!closedBasis || p.status === "closed");
      if (p.key === w.key) {
        c.expected = true;
        c.reported = !!ok;
      }
      if (!ok) {
        if (inYTD(p.to)) c.missing.push(p.key);
        return;
      }
      let tong = 0;
      const by: Record<string, string> = {};
      for (const g of GROUPS.slice(0, 4)) {
        const v = Number(last!.values[g.key] ?? 0);
        tong += v;
        by[g.key] = s(v);
        add(c.cells[g.key]!, v, p.key === w.key, p.to === prevTo, inQTD(p.to), inYTD(p.to));
      }
      by.tong = s(tong);
      add(c.cells.tong!, tong, p.key === w.key, p.to === prevTo, inQTD(p.to), inYTD(p.to));
      if (inYTD(p.to)) c.by[p.key] = by;
    });
  }
  function add(c: C, v: number, isCur: boolean, isPrev: boolean, qtd: boolean, ytd: boolean) {
    if (isCur) c.cur += v;
    if (isPrev) c.prev += v;
    if (qtd) c.qtd += v;
    if (ytd) c.ytd += v;
  }
  const cols = (c: C): S["CntsCols"] => ({ prev: s(c.prev), cur: s(c.cur), qtd: s(c.qtd), ytd: s(c.ytd) });
  const catchT = Object.fromEntries(GROUPS.map((g) => [g.key, zero()]));
  for (const c of communes.values()) for (const g of GROUPS) {
    const x = c.cells[g.key]!;
    const t = catchT[g.key]!;
    t.prev += x.prev; t.cur += x.cur; t.qtd += x.qtd; t.ytd += x.ytd;
  }
  const aquaArea = zero();
  const aquaOut = zero();
  const aqua = AQUA.map(([code, label, group, au, ou]) => {
    const a = zero();
    const o = zero();
    for (const e of AQUA_ROWS.filter((x) => x.object === code)) {
      if (e.area) add(a, Number(e.area), e.week === w.key, e.to === prevTo, inQTD(e.to), inYTD(e.to));
      if (e.output) add(o, Number(e.output), e.week === w.key, e.to === prevTo, inQTD(e.to), inYTD(e.to));
    }
    if (au === "ha") for (const k of ["prev", "cur", "qtd", "ytd"] as const) aquaArea[k] += a[k];
    if (ou === "tấn") for (const k of ["prev", "cur", "qtd", "ytd"] as const) aquaOut[k] += o[k];
    const entered = AQUA_ROWS.find((x) => x.object === code && x.week === w.key);
    return { code, label, group, area_unit: au, output_unit: ou, in_total: ou === "tấn", area: cols(a), output: cols(o), entered_area: entered?.area ?? null,
      entered_output: entered?.output ?? null, note: "" };
  });
  const total = zero();
  for (const k of ["prev", "cur", "qtd", "ytd"] as const) total[k] = catchT.tong![k] + aquaOut[k];
  const yearWeeks = [...new Set(periods.filter((p) => inYTD(p.to)).map((p) => `${p.key}|${p.from}|${p.to}`))].sort((a, b) => (a.split("|")[2]! < b.split("|")[2]! ? -1 : 1))
    .map((x) => ({ key: x.split("|")[0]!, label: x.split("|")[0]!, from: x.split("|")[1]!, to: x.split("|")[2]! }));
  const list = [...communes.entries()].sort(([a], [b]) => a.localeCompare(b));
  const prevKey = keyOf(ymd(parse(w.from) - 7 * D));
  const v: S["CntsBM18"] = {
    province: "89", week: { key: w.key, label: `Tuần ${w.no}/${w.y} (${w.from.slice(8)}/${w.from.slice(5, 7)} – ${w.to.split("-").reverse().join("/")})`, from: w.from, to: w.to },
    prev: { key: prevKey, label: prevKey, from: ymd(parse(w.from) - 7 * D), to: prevTo }, prev_key: prevKey, next_key: keyOf(ymd(parse(w.from) + 7 * D)),
    calendar: "chan_nuoi_thuy_san:v3", basis: closedBasis ? "closed" : "submitted", groups: GROUPS, aqua_groups: AQUA_GROUPS, weeks: yearWeeks,
    communes: list.map(([code, c]) => ({ commune_code: code, commune_name: code, region_code: "", region_name: "Chưa gán khu vực",
      cells: Object.fromEntries(GROUPS.map((g) => [g.key, cols(c.cells[g.key]!)])), expected: c.expected, reported: c.reported, nil_report: false,
      missing_weeks: c.missing, by_week: c.by })),
    regions: [{ region_code: "", region_name: "Chưa gán khu vực", sort: 0, cells: Object.fromEntries(GROUPS.map((g) => [g.key, cols(catchT[g.key]!)])) }],
    catch: Object.fromEntries(GROUPS.map((g) => [g.key, cols(catchT[g.key]!)])), aqua, aqua_area: cols(aquaArea), aqua_output: cols(aquaOut), total: cols(total),
    sources: { count: cur.length, closed: cur.filter((p) => p.status === "closed").length, all_closed: cur.length > 0 && cur.every((p) => p.status === "closed") },
    units_total: list.filter(([, c]) => c.expected).length, units_reported: list.filter(([, c]) => c.reported).length,
    missing_units: list.filter(([, c]) => c.expected && !c.reported).map(([k]) => k),
    aggregate: aggState(w.key, AGG.get(`CNTS-BM18:${w.key}`)), shares: shareViews(`CNTS-BM18:week:${w.key}`),
  };
  return json(v);
}

function shareViews(k: string): S["CntsShare"][] {
  return (SHARES.get(k) ?? []).map((x) => ({ id: x.id, version: x.version, at: x.at, basis: x.basis, recipient: "Chi cục Chăn nuôi và Thú y", channel: "download",
    note: x.note, content_hash: x.hash, actor_name: "Cán bộ tổng hợp" }));
}

export const cntsAggregateRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/extension\/cnts\/bm02$/, handler: bm02 },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/bm02\/adjustments$/,
    handler: (ctx) => {
      const deny = writer(ctx);
      if (deny) return deny;
      const b = body<{ week_key: string; facility_ref: string; deltas: Record<string, string>; reason: string }>(ctx);
      if (!/^\d{4}-\d{2}-K[1-4]$/.test(b.week_key ?? "")) return validation([{ field: "week_key", reason: "tuần KSGM YYYY-MM-Kn" }]);
      if ((b.reason ?? "").trim().length < 10) return validation([{ field: "reason", reason: "lý do 10–500 ký tự" }]);
      if (!Object.values(b.deltas ?? {}).some((x) => Number(x) !== 0)) return validation([{ field: "deltas", reason: "nhập ít nhất một số khác 0" }]);
      if (closedNow(`CNTS-BM02:${b.week_key}`)) return problem(409, "extension.cnts_aggregate_closed", "Bảng tổng hợp của tuần đã chốt — mở lại trước khi điều chỉnh hoặc nhập nuôi trồng");
      const [y, m] = b.week_key.split("-").map(Number);
      const w = ksgmWeeks(y!, m!).find((x) => x.key === b.week_key)!;
      const base: Record<string, number> = {};
      let commune = "";
      for (const p of cntsPeriodFigures("CNTS-BM01").filter((p) => p.from >= w.from && p.from <= w.to)) {
        const i = p.units.indexOf(b.facility_ref);
        if (i >= 0) commune = p.parents[i] ?? commune;
        const last = p.last.get(b.facility_ref);
        if (last) for (const sp of SPECIES) base[sp.key] = (base[sp.key] ?? 0) + Number(last.values[sp.key] ?? 0);
      }
      const a: Adj = { id: uuidv7(), week_key: b.week_key, facility_ref: b.facility_ref, commune, deltas: b.deltas, reason: b.reason.trim(), base, at: new Date().toISOString(), by: ctx.persona.me.actor_ref };
      ADJ.push(a);
      return json({ id: a.id, week_key: a.week_key, facility_ref: a.facility_ref, facility_name: facilityName(a.facility_ref), commune_code: commune, deltas: a.deltas,
        reason: a.reason, actor_name: "", at: a.at, stale: false }, 201);
    },
  },
  { method: "GET", pattern: /^\/v1\/extension\/cnts\/bm18$/, handler: (ctx) => bm18(ctx) },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/bm18\/aquaculture$/,
    handler: (ctx) => {
      const deny = writer(ctx);
      if (deny) return deny;
      const b = body<{ week: string; entries: { object: string; area?: string | null; output?: string | null }[] }>(ctx);
      const w = weekSpan(b.week ?? "");
      if (!w || !b.week) return validation([{ field: "week", reason: "dạng YYYY-Www" }]);
      if (closedNow(`CNTS-BM18:${w.key}`)) return problem(409, "extension.cnts_aggregate_closed", "Bảng tổng hợp của tuần đã chốt — mở lại trước khi điều chỉnh hoặc nhập nuôi trồng");
      for (const [i, e] of (b.entries ?? []).entries()) {
        if (!AQUA.some((x) => x[0] === e.object)) return validation([{ field: `entries[${i}].object`, reason: "đối tượng không có hoặc trùng" }]);
        for (const f of ["area", "output"] as const) if (e[f] && !/^\d{1,12}(\.\d{1,4})?$/.test(e[f]!)) return validation([{ field: `entries[${i}].${f}`, reason: "số không âm" }]);
      }
      for (const e of b.entries) {
        const i = AQUA_ROWS.findIndex((x) => x.week === w.key && x.object === e.object);
        const row = { week: w.key, to: w.to, object: e.object, area: e.area ?? null, output: e.output ?? null };
        if (i >= 0) AQUA_ROWS[i] = row;
        else AQUA_ROWS.push(row);
      }
      return bm18(ctx, w.key);
    },
  },
  ...(["close", "reopen"] as const).map(
    (action): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/extension/cnts/aggregates/${action}$`),
      handler: (ctx) => {
        const deny = writer(ctx);
        if (deny) return deny;
        const b = body<{ form: string; period_key: string; reason?: string }>(ctx);
        const k = `${b.form}:${b.period_key}`;
        const evs = AGG.get(k) ?? [];
        if (action === "reopen") {
          if ((b.reason ?? "").trim().length < 10) return validation([{ field: "reason", reason: "lý do 10–500 ký tự" }]);
          if (!closedNow(k)) return problem(409, "extension.cnts_aggregate_state", "Bảng tổng hợp đã ở trạng thái này (đã chốt / chưa chốt)");
          evs.push({ action: "reopened", at: new Date().toISOString(), hash: "" });
        } else {
          if (closedNow(k)) return problem(409, "extension.cnts_aggregate_state", "Bảng tổng hợp đã ở trạng thái này (đã chốt / chưa chốt)");
          let src: { status: string }[];
          if (b.form === "CNTS-BM18") src = cntsPeriodFigures("CNTS-BM17").filter((p) => p.key === b.period_key);
          else {
            const [y, m] = b.period_key.split("-").map(Number);
            const w = ksgmWeeks(y!, m!).find((x) => x.key === b.period_key);
            src = w ? cntsPeriodFigures("CNTS-BM01").filter((p) => p.from >= w.from && p.from <= w.to) : [];
          }
          if (!src.length || src.some((p) => p.status !== "closed")) {
            return problem(409, "extension.cnts_aggregate_sources_open", "Còn kỳ nguồn của tuần chưa chốt — chốt mọi biểu nguồn của tuần trước khi chốt bảng tổng hợp",
              { detail: `${src.filter((p) => p.status === "closed").length}/${src.length} kỳ nguồn đã chốt` });
          }
          evs.push({ action: "closed", at: new Date().toISOString(), hash: uuidv7().replace(/-/g, "").padEnd(64, "0") });
        }
        AGG.set(k, evs);
        return json(aggState(b.period_key, evs));
      },
    }),
  ),
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/shares$/,
    handler: (ctx) => {
      const deny = writer(ctx);
      if (deny) return deny;
      const b = body<{ form: string; period_type: string; period_key: string }>(ctx);
      const k = `${b.form}:${b.period_type}:${b.period_key}`;
      const list = SHARES.get(k) ?? [];
      const basis: Share["basis"] = closedNow(`${b.form}:${b.period_key}`) ? "closed" : "submitted";
      const hash = `${basis}:${ADJ.length}:${AQUA_ROWS.length}:${cntsPeriodFigures(b.form === "CNTS-BM18" ? "CNTS-BM17" : "CNTS-BM01").reduce((n, p) => n + p.last.size, 0)}`;
      const last = list.at(-1);
      if (last && last.hash === hash) return json({ share: shareViews(k).at(-1), replayed: true });
      const sh: Share = { id: uuidv7(), version: list.length + 1, at: new Date().toISOString(), basis, hash, by: ctx.persona.me.actor_ref,
        note: last ? `Thay đổi so với bản đã gửi ngày ${new Date(last.at).toLocaleString("vi-VN")} (phiên bản ${last.version})` : "" };
      list.push(sh);
      SHARES.set(k, list);
      return json({ share: shareViews(k).at(-1), replayed: false }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/shares$/,
    handler: (ctx) => {
      const q = ctx.url.searchParams;
      return json({ items: shareViews(`${q.get("form")}:${q.get("period_type")}:${q.get("key")}`) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/shares\/([^/]+)\/xlsx$/,
    handler: (ctx) => {
      const id = ctx.params[0]!;
      const found = [...SHARES.values()].flat().find((x) => x.id === id);
      if (!found) return problem(404, "extension.cnts_share_not_found", "Không tìm thấy phiên bản tệp chia sẻ");
      return new Response(new Blob([`GIẢ LẬP — phiên bản ${found.version}`]), {
        status: 200,
        headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
          "Content-Disposition": `attachment; filename="cnts-v${found.version}.xlsx"` },
      });
    },
  },
];
