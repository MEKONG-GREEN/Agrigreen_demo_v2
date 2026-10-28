// ============================================================================
// BACKEND GIẢ LẬP — Điều tra giá thành sản xuất theo vụ (W11.78, cap/price, BRD Báo giá nông sản v1.3 FN-TI-02.10…02.12,
// FN-TW-01.04). Đường dẫn, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/price/surveys*,
// /v1/price/survey-report*). Quy tắc mô phỏng (rút gọn từ internal/kn/price/app/survey*.go):
//   - Hai đợt: "Hè Thu 2026" Đang rà soát (11 phiếu, một phiếu Mít Thái 25.000 cao bất thường so trung vị 10.000) và
//     "Đông Xuân 2025–2026" Đã chốt (3 phiếu Lúa OM 18 giá thành 5.274 / 5.580 / 5.384 → bình quân 5.413, trung vị 5.384).
//   - Nhập tệp: chỉ đọc CSV UTF-8 (tệp .xlsx → 422); gợi ý cột theo từ khóa, khớp tên xã / mặt hàng không dấu, trùng SĐT +
//     mặt hàng → bỏ dòng sau. Cờ CT-06: lệch trung vị ≥ ±40 % khi có ≥ 5 phiếu.
//   - Quyền: đợt, nhập tệp, rà soát chỉ Cấp Tỉnh (Admin xem); báo cáo Cấp TW / Tỉnh / Admin, xuất Cấp Tỉnh / Admin.
// Dữ liệu GIẢ LẬP, trạng thái trong bộ nhớ tab. Không float (Decimal BigInt).
// ============================================================================
import type { components } from "../generated/application";
import { Decimal } from "@/kn/grid/decimal";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Role = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";
type Form = S["PriceSurveyForm"] & { round: string; phone: string };
type Round = S["PriceSurveyRound"] & {
  note: string;
  communes_: { code: string; targets: Record<string, number> }[];
  officers_: { id: string; commune: string; full_name: string; phone: string; unit: string }[];
  log_: S["PriceSurveyRoundDetail"]["log"];
};

const XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
const COMMUNES: Record<string, { name: string; region: string }> = {
  "30316": { name: "An Châu", region: "an_giang" },
  "30280": { name: "Núi Cấm", region: "an_giang" },
  "30544": { name: "Hà Tiên", region: "kien_giang" },
};
const REGIONS: Record<string, string> = { an_giang: "An Giang", kien_giang: "Kiên Giang" };
const ITEMS: Record<string, { name: string; crop: string }> = {
  MH00001: { name: "Dưa leo Loại 1", crop: "rau_mau" },
  MH00003: { name: "Mít Thái", crop: "cay_an_trai" },
  MH00004: { name: "Lúa OM 18", crop: "lua" },
  MH00005: { name: "Xoài keo", crop: "cay_an_trai" },
};
const ACTOR = "0190c0de-0000-7000-8000-0000000000aa";

function roleOf(ctx: Ctx): Role | null {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (kc) return kc.role as Role;
  const r = ctx.persona.me.roles;
  if (r.includes("platform_admin")) return "admin";
  if (r.includes("kn_province_admin")) return "kn_tinh";
  return null;
}

const forbid = () => problem(403, "forbidden", "vai của bạn không thao tác điều tra giá thành");
const D = (s: string) => Decimal.parse(s)!;
const fold = (s: string) =>
  s
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/đ/gi, "d")
    .toLowerCase()
    .replace(/^(xa|phuong|thi tran)\s+/, "")
    .trim()
    .replace(/\s+/g, " ");

let seq = 100;
const uid = () => `0190c0de-5000-7000-8000-${String(++seq).padStart(12, "0")}`;
const now = () => new Date().toISOString();

function costOf(seed: string, rest = "0"): Record<string, string> {
  return { seed, fertilizer: rest, pesticide: rest, labor: rest, machine: rest, other: "0" };
}

function makeForm(round: string, i: number, commune: string, item: string, farmer: string, price: string, area: string, output: string,
  costs: Record<string, string>, status: string): Form {
  return {
    id: uid(),
    round,
    phone: `09010000${String(i).padStart(2, "0")}`,
    seq: 1,
    row_no: i + 1,
    commune,
    commune_name: COMMUNES[commune]!.name,
    crop: ITEMS[item]!.crop,
    item,
    item_name: ITEMS[item]!.name,
    officer_id: null,
    officer_name: "Lê Văn Hải",
    farmer_name: farmer,
    price,
    orig_price: price,
    corrected: false,
    median: null,
    pct: null,
    flag: "",
    cost_per_kg: "0",
    status: status as Form["status"],
    area_ha: area,
    output_kg: output,
    other_income: "0",
    costs,
    calc: { total_cost: "0", cost_per_kg: "0", yield_ha: "0", revenue: "0", profit: "0", profit_per_ha: "0", margin_pct: null },
    note: "",
    unverifiable: false,
    farmer_phone: "",
    officer_phone: "0912000111",
    officer_unit: "TTKN An Giang",
    import_id: "0190c0de-5000-7000-8000-000000000001",
    log: [{ seq: 1, op: "import", status: "imported", note: "", price: null, at: "2026-08-20T03:00:00Z", actor_ref: ACTOR }],
  };
}

let ROUNDS: Round[] = [];
const FORMS: Form[] = [];
let seeded = false;

function seed() {
  if (seeded) return;
  seeded = true;
  const base = (id: string, name: string, season: Round["season"], year: number, from: string, to: string, status: Round["status"],
    crops: Round["crops"]): Round => ({
    id, name, season, year, collect_from: from, collect_to: to, crops, status, communes: 3, forms: 0, target: 0, pending: 0, seq: 2,
    note: "",
    communes_: Object.keys(COMMUNES).map((code) => ({ code, targets: { lua: 4, rau_mau: 4, nam: 0, cay_an_trai: 4 } })),
    officers_: [
      { id: uid(), commune: "30316", full_name: "Lê Văn Hải", phone: "0912000111", unit: "TTKN An Giang" },
      { id: uid(), commune: "30316", full_name: "Trần Thị Lan", phone: "0912000222", unit: "TTKN An Giang" },
      { id: uid(), commune: "30280", full_name: "Phạm Minh Tâm", phone: "0912000333", unit: "Trạm KN Tịnh Biên" },
    ],
    log_: [
      { seq: 1, op: "create", status: "draft", reason: "", at: "2026-07-10T02:00:00Z", actor_ref: ACTOR },
      { seq: 2, op: "open", status: "collecting", reason: "", at: "2026-07-10T02:00:00Z", actor_ref: ACTOR },
    ],
  });
  const ht = base("0190c0de-5000-7000-8000-0000000000a1", "Hè Thu 2026", "he_thu", 2026, "2026-07-14", "2026-08-25", "reviewing",
    ["lua", "rau_mau", "cay_an_trai"]);
  const dx = base("0190c0de-5000-7000-8000-0000000000a0", "Đông Xuân 2025–2026", "dong_xuan", 2026, "2026-02-01", "2026-03-15", "closed",
    ["lua"]);
  ROUNDS = [ht, dx];
  const mit = ["10000", "10000", "9800", "10200", "25000"];
  mit.forEach((p, i) => FORMS.push(makeForm(ht.id, i, "30280", "MH00003", `Nông hộ Mít ${i + 1}`, p, "1", "8000", costOf("2000000", "1000000"), "imported")));
  ["9000", "9500", "9200"].forEach((p, i) => FORMS.push(makeForm(ht.id, 5 + i, "30316", "MH00001", `Nông hộ Dưa ${i + 1}`, p, "0.5", "10000", costOf("500000", "300000"), "imported")));
  ["7000", "7200", "6800"].forEach((p, i) => FORMS.push(makeForm(ht.id, 8 + i, i < 2 ? "30280" : "30544", "MH00004", `Nông hộ Lúa ${i + 1}`, p, "1", "6000", costOf("1000000", "2000000"), "approved")));
  [["5274000", "7000", "30280"], ["5580000", "7200", "30280"], ["5384000", "6800", "30544"]].forEach(([c, p, cm], i) =>
    FORMS.push(makeForm(dx.id, 20 + i, cm!, "MH00004", `Nông hộ ĐX ${i + 1}`, p!, "1", "1000", costOf(c!), "approved")),
  );
}

/** Giá trị tự tính CT-05 + cờ CT-06 (trung vị giá bán cùng mặt hàng trong đợt, ≥ 5 phiếu, ngưỡng 40 %). */
function refresh(round: string) {
  const live = FORMS.filter((f) => f.round === round && f.status !== "deleted");
  const by: Record<string, Decimal[]> = {};
  for (const f of live) (by[f.item] ??= []).push(D(f.price));
  for (const f of live) {
    let total = Decimal.fromInt(0);
    for (const k of Object.keys(f.costs)) total = total.add(D(f.costs[k]!));
    const out = D(f.output_kg);
    const area = D(f.area_ha);
    const rev = out.mul(D(f.price)).add(D(f.other_income));
    const profit = rev.sub(total);
    f.calc = {
      total_cost: total.toString(),
      cost_per_kg: total.divRound(out, 0).toString(),
      yield_ha: out.divRound(area, 2).toString(),
      revenue: rev.toString(),
      profit: profit.toString(),
      profit_per_ha: profit.divRound(area, 0).toString(),
      margin_pct: total.sign() > 0 ? profit.mul(Decimal.fromInt(100)).divRound(total, 2).toString() : null,
    };
    f.cost_per_kg = f.calc.cost_per_kg;
    const ps = [...(by[f.item] ?? [])].sort((a, b) => a.cmp(b));
    if (ps.length >= 5) {
      const n = ps.length;
      const med = n % 2 ? ps[(n - 1) / 2]! : ps[n / 2 - 1]!.add(ps[n / 2]!).divRound(Decimal.fromInt(2), 0);
      const pct = D(f.price).sub(med).mul(Decimal.fromInt(100)).divRound(med, 2);
      f.median = med.toString();
      f.pct = pct.toString();
      f.flag = pct.cmp(Decimal.fromInt(40)) >= 0 ? "high" : pct.cmp(Decimal.fromInt(-40)) <= 0 ? "low" : "";
    } else {
      f.median = null;
      f.pct = null;
      f.flag = "";
    }
  }
}

const pending = (s: string) => s === "imported" || s === "verify" || s === "verified";

function summary(r: Round): S["PriceSurveyRound"] {
  const fs = FORMS.filter((f) => f.round === r.id && f.status !== "deleted");
  const target = r.communes_.reduce((t, c) => t + r.crops.reduce((a, cr) => a + (c.targets[cr] ?? 0), 0), 0);
  return {
    id: r.id, name: r.name, season: r.season, year: r.year, collect_from: r.collect_from, collect_to: r.collect_to, crops: r.crops,
    status: r.status, communes: r.communes_.length, forms: fs.length, target, pending: fs.filter((f) => pending(f.status)).length, seq: r.seq,
  };
}

function detail(r: Round, role: Role): S["PriceSurveyRoundDetail"] {
  const fs = FORMS.filter((f) => f.round === r.id && f.status !== "deleted");
  const crops = ["lua", "rau_mau", "nam", "cay_an_trai"] as const;
  return {
    round: summary(r),
    note: r.note,
    communes: r.communes_.map((c) => {
      const imported = Object.fromEntries(crops.map((cr) => [cr, fs.filter((f) => f.commune === c.code && f.crop === cr).length])) as S["PriceSurveyCropCounts"];
      const targets = Object.fromEntries(crops.map((cr) => [cr, c.targets[cr] ?? 0])) as S["PriceSurveyCropCounts"];
      return {
        code: c.code, name: COMMUNES[c.code]?.name ?? c.code, targets, imported,
        officers: r.officers_.filter((o) => o.commune === c.code).map((o) => ({ ...o, forms: 0 })),
      };
    }),
    log: r.log_,
    warnings: r.communes_.filter((c) => !r.officers_.some((o) => o.commune === c.code)).map((c) => `Xã ${COMMUNES[c.code]?.name ?? c.code} chưa giao cán bộ điều tra`),
    can_edit: role === "kn_tinh" && r.status !== "closed",
    can_reopen: role === "kn_tinh" && r.status === "closed",
  };
}

function bodyOf<T>(ctx: Ctx): T {
  return (ctx.body ?? {}) as T;
}

function findRound(id: string): Round | Response {
  return ROUNDS.find((r) => r.id === id) ?? problem(404, "price.survey_round_not_found", "Không có đợt điều tra này");
}

function readCsv(ctx: Ctx): { name: string; text: string; parts: Record<string, string> } | Response {
  const ct = ctx.request.headers.get("Content-Type") ?? "";
  const b = /boundary=([^;]+)/.exec(ct)?.[1]?.replace(/^"|"$/g, "");
  if (!b || !(ctx.body instanceof Uint8Array)) return problem(422, "validation_failed", "thân phải là multipart/form-data có phần file");
  let raw = "";
  for (const x of ctx.body) raw += String.fromCharCode(x);
  const dec = new TextDecoder();
  const bytes = (s: string) => Uint8Array.from(s, (ch) => ch.charCodeAt(0));
  const parts: Record<string, string> = {};
  let name = "tep.csv";
  let text = "";
  for (const part of raw.split(`--${b}`)) {
    const i = part.indexOf("\r\n\r\n");
    if (i < 0) continue;
    const head = part.slice(0, i);
    const content = part.slice(i + 4).replace(/\r\n$/, "");
    const n = /name="([^"]*)"/.exec(head)?.[1];
    if (!n) continue;
    if (n === "file") {
      name = dec.decode(bytes(/filename="([^"]*)"/.exec(head)?.[1] ?? name));
      if (content.startsWith("PK")) return problem(422, "price.survey_import_invalid", "Bản giả lập chỉ đọc tệp CSV UTF-8");
      text = dec.decode(bytes(content));
    } else parts[n] = dec.decode(bytes(content));
  }
  return { name, text, parts };
}

function splitCsv(line: string): string[] {
  const out: string[] = [];
  let cur = "";
  let q = false;
  for (const ch of line) {
    if (ch === '"') q = !q;
    else if (ch === "," && !q) {
      out.push(cur.trim());
      cur = "";
    } else cur += ch;
  }
  out.push(cur.trim());
  return out;
}

const FIELDS: S["PriceSurveyImportPreview"]["fields"] = [
  { key: "officer", label: "Tên cán bộ điều tra", required: false },
  { key: "commune", label: "Xã", required: true },
  { key: "crop", label: "Nhóm cây", required: true },
  { key: "item", label: "Mặt hàng", required: true },
  { key: "farmer", label: "Họ tên nông hộ", required: true },
  { key: "phone", label: "Số điện thoại nông hộ", required: false },
  { key: "area", label: "Diện tích (ha)", required: true },
  { key: "output", label: "Sản lượng (kg)", required: false },
  { key: "price", label: "Giá bán (đ/kg)", required: true },
  { key: "seed", label: "Chi phí giống", required: false },
];
const KEYWORDS: [string, string][] = [
  ["officer", "can bo"], ["phone", "dien thoai"], ["farmer", "nong ho"], ["seed", "giong"], ["crop", "nhom cay"],
  ["item", "mat hang"], ["commune", "xa"], ["area", "dien tich"], ["output", "san luong"], ["price", "gia ban"],
];

function num(s: string | undefined): Decimal | null {
  if (!s) return null;
  const t = s.replace(/\s|đ/g, "");
  const norm = /^\d{1,3}(\.\d{3})+$/.test(t) ? t.replace(/\./g, "") : t.replace(",", ".");
  return Decimal.parse(norm);
}

interface Parsed {
  preview: S["PriceSurveyImportPreview"];
  rows: { line: S["PriceSurveyImportLine"]; cells: string[]; ok: boolean; phone: string }[];
  map: Record<string, number>;
}

function parse(r: Round, name: string, text: string, parts: Record<string, string>): Parsed {
  const lines = text.split(/\r?\n/).filter((l) => l.trim());
  const header = splitCsv(lines[0] ?? "");
  let map: Record<string, number> = {};
  if (parts.mapping) map = JSON.parse(parts.mapping) as Record<string, number>;
  else
    header.forEach((h, i) => {
      const f = fold(h);
      const hit = KEYWORDS.find(([k, w]) => map[k] === undefined && ` ${f} `.includes(` ${w} `));
      if (hit) map[hit[0]] = i;
    });
  const names = (parts.names ? JSON.parse(parts.names) : {}) as { item?: Record<string, string>; commune?: Record<string, string> };
  const missing = FIELDS.filter((f) => f.required && (map[f.key] ?? -1) < 0).map((f) => f.key);
  const unknown: Record<string, string[]> = { item: [], commune: [] };
  const seen = new Set<string>();
  const rows: Parsed["rows"] = [];
  if (!missing.length)
    lines.slice(1).forEach((l, i) => {
      const cells = splitCsv(l);
      const g = (k: string) => (map[k] !== undefined && map[k]! >= 0 ? cells[map[k]!] ?? "" : "");
      const msgs: string[] = [];
      const st = { level: "ok" as "ok" | "warning" | "error" };
      const bad = (m: string) => { msgs.push(m); st.level = "error"; };
      const cm = names.commune?.[g("commune")] ?? r.communes_.find((c) => fold(COMMUNES[c.code]?.name ?? "") === fold(g("commune")))?.code ?? "";
      if (!cm) { bad("Tên xã chưa khớp danh mục ĐVHC — chọn ánh xạ thủ công"); if (g("commune")) unknown.commune!.push(g("commune")); }
      const it = names.item?.[g("item")] ?? Object.keys(ITEMS).find((k) => fold(ITEMS[k]!.name).startsWith(fold(g("item"))) && g("item")) ?? "";
      if (!it) { bad("Mặt hàng chưa khớp danh mục — chọn ánh xạ thủ công"); if (g("item")) unknown.item!.push(g("item")); }
      if (!g("farmer")) bad("Thiếu họ tên nông hộ");
      const area = num(g("area"));
      const out = num(g("output"));
      if (!area || area.sign() <= 0 || !out || out.sign() <= 0) bad("Diện tích, năng suất hoặc sản lượng bằng 0 / trống");
      if (!num(g("price"))) bad("Giá bán trống");
      const phone = g("phone").replace(/\D/g, "");
      let skip = false;
      if (st.level !== "error") {
        const key = `${phone}|${it}`;
        if (phone && (seen.has(key) || FORMS.some((f) => f.round === r.id && f.status !== "deleted" && f.phone === phone && f.item === it))) {
          msgs.push("Trùng phiếu (cùng đợt, SĐT, mặt hàng) — bỏ qua dòng này");
          st.level = "warning";
          skip = true;
        }
        seen.add(key);
      }
      rows.push({
        cells, phone, ok: st.level !== "error" && !skip,
        line: {
          row: i + 2, level: st.level, messages: msgs, skip, commune: cm, commune_name: COMMUNES[cm]?.name ?? "", crop: it ? ITEMS[it]!.crop : "",
          item: it, item_name: it ? ITEMS[it]!.name : "", officer_id: null, cost_per_kg: null, flag: "",
        },
      });
    });
  const ls = rows.map((x) => x.line);
  return {
    map,
    rows,
    preview: {
      sha256: String(text.length).padStart(64, "0"), file_name: name, columns: header, mapping: map, fields: FIELDS, missing,
      unknown, lines: ls, ok: ls.filter((l) => l.level === "ok").length, warnings: ls.filter((l) => l.level === "warning").length,
      errors: ls.filter((l) => l.level === "error").length, skipped: ls.filter((l) => l.skip).length, already_imported_at: null,
    },
  };
}

function formList(r: Round, url: URL, role: Role): S["PriceSurveyFormList"] {
  refresh(r.id);
  const all = FORMS.filter((f) => f.round === r.id && f.status !== "deleted");
  const q = (k: string) => url.searchParams.get(k) ?? "";
  const rows = all.filter(
    (f) =>
      (!q("crop") || f.crop === q("crop")) && (!q("item") || f.item === q("item")) && (!q("status") || f.status === q("status")) &&
      (q("flag") !== "flag" || f.flag) && (q("flag") !== "noflag" || !f.flag),
  );
  const pend = all.filter((f) => pending(f.status)).length;
  const live = r.status === "collecting" || r.status === "reviewing";
  const dist: S["PriceSurveyFormList"]["distribution"] = [];
  if (q("item"))
    for (const code of new Set(rows.map((f) => f.commune))) {
      const ps = rows.filter((f) => f.commune === code).map((f) => D(f.price)).sort((a, b) => a.cmp(b));
      dist.push({ commune: code, commune_name: COMMUNES[code]!.name, n: ps.length, min: ps[0]!.toString(), max: ps[ps.length - 1]!.toString(), median: ps[Math.floor((ps.length - 1) / 2)]!.toString() });
    }
  return {
    round: summary(r),
    kpis: {
      sent: all.length, flagged: all.filter((f) => f.flag && f.status !== "approved" && f.status !== "rejected").length,
      verify: all.filter((f) => f.status === "verify").length, approved: all.filter((f) => f.status === "approved").length,
      rejected: all.filter((f) => f.status === "rejected").length, pending: pend,
    },
    total: rows.length,
    rows: rows.map(formRow),
    distribution: dist,
    items: [...new Set(all.map((f) => f.item))].map((code) => ({ code, name: ITEMS[code]!.name })),
    communes: [...new Set(all.map((f) => f.commune))].map((code) => ({ code, name: COMMUNES[code]!.name })),
    threshold_pct: "40.00",
    can_review: role === "kn_tinh" && live,
    can_close: role === "kn_tinh" && live && pend === 0,
  };
}

function formOut(f: Form): S["PriceSurveyForm"] {
  const out: Partial<Form> = { ...f, farmer_phone: f.phone };
  delete out.round;
  delete out.phone;
  return out as S["PriceSurveyForm"];
}

function formRow(f: Form): S["PriceSurveyFormRow"] {
  return formOut(f);
}

function cost(round: Round, url: URL): S["PriceSurveyCostBlock"] {
  const q = (k: string) => url.searchParams.get(k) ?? "";
  refresh(round.id);
  const fs = FORMS.filter(
    (f) => f.round === round.id && f.status === "approved" && (!q("crop") || f.crop === q("crop")) && (!q("item") || f.item === q("item")) &&
      (!q("region") || COMMUNES[f.commune]!.region === q("region")),
  );
  const avg = (vs: Decimal[]) => (vs.length ? vs.reduce((a, b) => a.add(b), Decimal.fromInt(0)).divRound(Decimal.fromInt(vs.length), 0).toString() : "0");
  const med = (vs: Decimal[]) => {
    const s = [...vs].sort((a, b) => a.cmp(b));
    const n = s.length;
    return !n ? "0" : n % 2 ? s[(n - 1) / 2]!.toString() : s[n / 2 - 1]!.add(s[n / 2]!).divRound(Decimal.fromInt(2), 0).toString();
  };
  const rows = [...new Set(fs.map((f) => f.item))].map((item) => {
    const g = fs.filter((f) => f.item === item);
    return {
      item, item_name: ITEMS[item]!.name, crop: ITEMS[item]!.crop, n: g.length, small_sample: g.length < 3,
      avg_costs: Object.fromEntries(Object.keys(g[0]!.costs).map((k) => [k, avg(g.map((f) => D(f.costs[k]!)))])),
      avg_total_cost: avg(g.map((f) => D(f.calc.total_cost))), avg_yield_ha: avg(g.map((f) => D(f.calc.yield_ha))),
      avg_price: avg(g.map((f) => D(f.price))), median_price: med(g.map((f) => D(f.price))),
      avg_cost_per_kg: avg(g.map((f) => D(f.calc.cost_per_kg))), median_cost_per_kg: med(g.map((f) => D(f.calc.cost_per_kg))),
      avg_profit: avg(g.map((f) => D(f.calc.profit))), avg_profit_per_ha: avg(g.map((f) => D(f.calc.profit_per_ha))), avg_margin_pct: null,
    };
  });
  const regions = rows.flatMap((r) =>
    [...new Set(fs.filter((f) => f.item === r.item).map((f) => COMMUNES[f.commune]!.region))].map((rg) => {
      const g = fs.filter((f) => f.item === r.item && COMMUNES[f.commune]!.region === rg);
      return { item: r.item, item_name: r.item_name, region: rg, region_name: REGIONS[rg]!, n: g.length, avg_cost_per_kg: avg(g.map((f) => D(f.calc.cost_per_kg))), avg_price: avg(g.map((f) => D(f.price))) };
    }),
  );
  const communes = q("item")
    ? [...new Set(fs.map((f) => f.commune))].map((c) => {
        const g = fs.filter((f) => f.commune === c);
        const ps = g.map((f) => D(f.price)).sort((a, b) => a.cmp(b));
        return { commune: c, commune_name: COMMUNES[c]!.name, n: g.length, min_price: ps[0]!.toString(), max_price: ps[ps.length - 1]!.toString(), median_price: med(ps), avg_cost_per_kg: avg(g.map((f) => D(f.calc.cost_per_kg))), median_cost_per_kg: med(g.map((f) => D(f.calc.cost_per_kg))) };
      })
    : [];
  return {
    round: { ...summary(round), forms: fs.length },
    kpis: {
      forms: fs.length,
      avg_cost_per_kg: fs.length ? avg(fs.map((f) => D(f.calc.cost_per_kg))) : null,
      avg_price: fs.length ? avg(fs.map((f) => D(f.price))) : null,
      avg_profit_per_ha: fs.length ? avg(fs.map((f) => D(f.calc.profit_per_ha))) : null,
    },
    rows, regions, communes,
  };
}

const xlsx = (name: string) => new Response(new Uint8Array([0x50, 0x4b, 0x03, 0x04]), { status: 200, headers: { "Content-Type": XLSX, "Content-Disposition": `attachment; filename*=UTF-8''${name}` } });

export const priceSurveyRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      return json({ items: ROUNDS.map(summary) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys$/,
    handler: (ctx) => {
      seed();
      if (roleOf(ctx) !== "kn_tinh") return forbid();
      const b = bodyOf<S["PriceSurveyRoundInput"]>(ctx);
      if (!b.name?.trim()) return problem(422, "validation_failed", "Tên đợt bắt buộc");
      if (b.open && (!b.crops.length || !b.communes.length)) return problem(422, "price.survey_round_incomplete", "Chưa chọn nhóm cây hoặc xã điều tra — không mở được đợt");
      const r: Round = {
        id: uid(), name: b.name, season: b.season, year: b.year, collect_from: b.collect_from, collect_to: b.collect_to, crops: b.crops,
        status: b.open ? "collecting" : "draft", communes: 0, forms: 0, target: 0, pending: 0, seq: 1, note: b.note ?? "",
        communes_: b.communes.map((c) => ({ code: c.code, targets: c.targets })),
        officers_: b.officers.map((o) => ({ id: uid(), commune: o.commune, full_name: o.full_name ?? "", phone: o.phone ?? "", unit: o.unit })),
        log_: [{ seq: 1, op: "create", status: b.open ? "collecting" : "draft", reason: "", at: now(), actor_ref: ACTOR }],
      };
      ROUNDS.unshift(r);
      return json(detail(r, "kn_tinh"), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys\/([^/]+)$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      const r = findRound(ctx.params[0]!);
      return r instanceof Response ? r : json(detail(r, role));
    },
  },
  {
    method: "PUT",
    pattern: /^\/v1\/price\/surveys\/([^/]+)$/,
    handler: (ctx) => {
      seed();
      if (roleOf(ctx) !== "kn_tinh") return forbid();
      const r = findRound(ctx.params[0]!);
      if (r instanceof Response) return r;
      const b = bodyOf<S["PriceSurveyRoundInput"]>(ctx);
      Object.assign(r, { name: b.name, season: b.season, year: b.year, collect_from: b.collect_from, collect_to: b.collect_to, crops: b.crops, note: b.note ?? "" });
      r.communes_ = b.communes.map((c) => ({ code: c.code, targets: c.targets }));
      r.officers_ = b.officers.map((o) => {
        const old = r.officers_.find((x) => x.id === o.id);
        return old ? { ...old, commune: o.commune, unit: o.unit } : { id: uid(), commune: o.commune, full_name: o.full_name ?? "", phone: o.phone ?? "", unit: o.unit };
      });
      r.seq++;
      return json(detail(r, "kn_tinh"));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/actions$/,
    handler: (ctx) => {
      seed();
      if (roleOf(ctx) !== "kn_tinh") return forbid();
      const r = findRound(ctx.params[0]!);
      if (r instanceof Response) return r;
      const b = bodyOf<S["PriceSurveyRoundAction"]>(ctx);
      const st = (s: Round["status"]) => {
        r.status = s;
        r.seq++;
        r.log_.push({ seq: r.seq, op: b.op, status: s, reason: b.reason ?? "", at: now(), actor_ref: ACTOR });
        return json(detail(r, "kn_tinh"));
      };
      const bad = () => problem(409, "price.survey_round_state", "Đợt điều tra không ở trạng thái cho phép thao tác này");
      if ((b.op === "extend" || b.op === "reopen") && !b.reason?.trim()) return problem(422, "validation_failed", "Lý do bắt buộc");
      switch (b.op) {
        case "open":
          return r.status === "draft" ? st("collecting") : bad();
        case "close_early":
          return r.status === "collecting" ? st("reviewing") : bad();
        case "extend":
          if (r.status === "closed" || r.status === "draft") return bad();
          r.collect_to = b.collect_to ?? r.collect_to;
          return st("collecting");
        case "close": {
          if (r.status !== "collecting" && r.status !== "reviewing") return bad();
          const n = FORMS.filter((f) => f.round === r.id && pending(f.status)).length;
          return n ? problem(409, "price.survey_round_pending", `Còn ${n} phiếu chờ rà soát hoặc chờ xác minh`) : st("closed");
        }
        case "reopen":
          return r.status === "closed" ? st("reviewing") : bad();
      }
      return problem(422, "validation_failed", "op không hợp lệ");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/clone$/,
    handler: (ctx) => {
      seed();
      if (roleOf(ctx) !== "kn_tinh") return forbid();
      const r = findRound(ctx.params[0]!);
      if (r instanceof Response) return r;
      const c: Round = { ...r, id: uid(), name: `${r.name} (bản sao)`, status: "draft", seq: 1, officers_: r.officers_.map((o) => ({ ...o, id: uid() })), log_: [] };
      ROUNDS.unshift(c);
      return json(detail(c, "kn_tinh"), 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/imports\/preview$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      const r = findRound(ctx.params[0]!);
      if (r instanceof Response) return r;
      const f = readCsv(ctx);
      if (f instanceof Response) return f;
      return json(parse(r, f.name, f.text, f.parts).preview);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/imports$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      const r = findRound(ctx.params[0]!);
      if (r instanceof Response) return r;
      const f = readCsv(ctx);
      if (f instanceof Response) return f;
      const p = parse(r, f.name, f.text, f.parts);
      if (f.parts.sha256 !== p.preview.sha256) return problem(409, "price.survey_import_changed", "Tệp hoặc ánh xạ khác lần xem trước — xem trước lại rồi ghi nhận");
      let created = 0;
      const g = (cells: string[], k: string) => (p.map[k] !== undefined && p.map[k]! >= 0 ? cells[p.map[k]!] ?? "" : "");
      for (const x of p.rows.filter((y) => y.ok)) {
        const fm = makeForm(r.id, FORMS.length, x.line.commune, x.line.item, g(x.cells, "farmer"), num(g(x.cells, "price"))!.toString(),
          num(g(x.cells, "area"))!.toString(), num(g(x.cells, "output"))!.toString(), costOf(num(g(x.cells, "seed"))?.toString() ?? "0"), "imported");
        fm.phone = x.phone;
        fm.row_no = x.line.row;
        FORMS.push(fm);
        created++;
      }
      const pv = p.preview;
      return json({ import_id: uid(), rows: pv.lines.length, created, skipped: pv.skipped, warnings: pv.warnings, errors: pv.errors, issues: pv.lines.filter((l) => l.level !== "ok") }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/imports$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      return json({ items: [{ id: "0190c0de-5000-7000-8000-000000000001", file_name: "DieuTraGiaThanh_HeThu2026.xlsx", sha256: "0".repeat(64), rows: 13, created: 11, skipped: 1, warnings: 2, errors: 1, at: "2026-08-20T03:00:00Z", actor_ref: ACTOR }] });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/imports\/([^/]+)\/issues$/,
    handler: (ctx) => (roleOf(ctx) === "kn_tinh" || roleOf(ctx) === "admin" ? xlsx("danh-sach-loi.xlsx") : forbid()),
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/forms\/export$/,
    handler: (ctx) => (roleOf(ctx) === "kn_tinh" || roleOf(ctx) === "admin" ? xlsx("phieu-dieu-tra.xlsx") : forbid()),
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/forms$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      const r = findRound(ctx.params[0]!);
      return r instanceof Response ? r : json(formList(r, ctx.url, role));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/forms\/approve-unflagged$/,
    handler: (ctx) => {
      seed();
      if (roleOf(ctx) !== "kn_tinh") return forbid();
      refresh(ctx.params[0]!);
      let n = 0;
      for (const f of FORMS.filter((x) => x.round === ctx.params[0] && x.status === "imported" && !x.flag)) {
        f.status = "approved";
        n++;
      }
      return json({ approved: n });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/forms\/([^/]+)$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin") return forbid();
      refresh(ctx.params[0]!);
      const f = FORMS.find((x) => x.id === ctx.params[1]);
      return f ? json(formOut(f)) : problem(404, "price.survey_form_not_found", "Không có phiếu điều tra này");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/surveys\/([^/]+)\/forms\/([^/]+)\/actions$/,
    handler: (ctx) => {
      seed();
      if (roleOf(ctx) !== "kn_tinh") return forbid();
      const f = FORMS.find((x) => x.id === ctx.params[1]);
      if (!f) return problem(404, "price.survey_form_not_found", "Không có phiếu điều tra này");
      const b = bodyOf<S["PriceSurveyFormAction"]>(ctx);
      const note = b.note?.trim() ?? "";
      const state = () => problem(409, "price.survey_form_state", "Phiếu điều tra không ở trạng thái cho phép thao tác này");
      const need = (m: string) => problem(422, "validation_failed", m);
      refresh(f.round);
      switch (b.op) {
        case "request_verify":
          if (f.status === "verify") return state();
          f.status = "verify";
          break;
        case "record_verify":
          if (f.status !== "verify") return state();
          if (!note && !b.unverifiable) return need("nội dung xác minh bắt buộc");
          if (b.price) {
            f.price = num(b.price)!.toString();
            f.corrected = true;
          }
          f.unverifiable = !!b.unverifiable;
          f.status = "verified";
          break;
        case "approve":
          if (f.status !== "imported" && f.status !== "verified") return state();
          if (f.flag && f.status !== "verified" && !note) return need("ghi chú bắt buộc khi duyệt phiếu có cờ bất thường");
          f.status = "approved";
          break;
        case "reject":
          if (!pending(f.status)) return state();
          if (!note) return need("lý do loại bắt buộc");
          f.status = "rejected";
          break;
        case "delete":
          if (!note) return need("lý do xóa bắt buộc");
          f.status = "deleted";
          break;
      }
      f.note = note || f.note;
      f.seq++;
      f.log.push({ seq: f.seq, op: b.op, status: f.status, note, price: b.price ?? null, at: now(), actor_ref: ACTOR });
      refresh(f.round);
      return json(formOut(f));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/survey-report$/,
    handler: (ctx) => {
      seed();
      const role = roleOf(ctx);
      if (role !== "kn_tinh" && role !== "admin" && role !== "kn_tw") return forbid();
      const closed = ROUNDS.filter((r) => r.status === "closed");
      const main = closed.find((r) => r.id === ctx.url.searchParams.get("round")) ?? closed[0];
      const cmp = closed.find((r) => r.id === ctx.url.searchParams.get("compare") && r.id !== main?.id);
      return json({
        rounds: closed.map(summary),
        main: main ? cost(main, ctx.url) : null,
        compare: cmp ? cost(cmp, ctx.url) : null,
        regions: Object.entries(REGIONS).map(([code, name]) => ({ code, name })),
        can_export: role !== "kn_tw",
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/survey-report\/export$/,
    handler: (ctx) => (roleOf(ctx) === "kn_tinh" || roleOf(ctx) === "admin" ? xlsx("gia-thanh-san-xuat.xlsx") : forbid()),
  },
];
