// GIẢ LẬP biểu kỳ Cấp Xã CNTS (W11.42, BRD CNTS v3.7 UR-XA-06…09, 13) theo hợp đồng /v1/extension/cnts/* và /v1/forms/*
// (profile kn_cnts): cài biểu, chia lô kỳ (kỳ ngày, tuần Thứ Ba – Thứ Hai lịch CNTS D8, tuần làm việc BM-05) với hạn riêng,
// công việc theo kỳ × đơn vị, nộp có khóa lạc quan (base_version → 409 forms.submission_conflict), báo không phát sinh,
// lý do trễ, sao chép kỳ trước, chốt / mở lại / hủy kỳ. Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { calendarAt, DEPT_CNTS, ruleTag } from "./kn-reports/domain";
import { callerOf, stableUuid, workScheduleStore } from "./kn-schedules";

/** Mã mục lịch ổn định của ngày `day` trong kế hoạch tuần của xã `unit` (giả lập PlanItemID phía máy chủ: nguồn = tuần × xã). */
const planItemId = (week: string, unit: string, day: string) => stableUuid(`cnts-plan:${week}:${unit}:${day}`);

type S = components["schemas"];
type Field = S["FormsField"];
type Form = S["CNTSPeriodForm"];

const PROFILE = "kn_cnts";
const int = (key: string, label: string, unit: string, group: string): Field => ({ key, label, type: "integer", required: true, unit, non_negative: true, group });
const dec = (key: string, label: string, unit: string, group: string): Field => ({ key, label, type: "decimal", required: true, unit, non_negative: true, group });
const WORK = ["Làm việc chuyên môn thường xuyên tại xã", "Công tác khác"];
// BF-CN-WEEKLY (UP-06): bảng T2–CN; Thứ Bảy, Chủ Nhật không bắt buộc.
const DAYS: [string, string][] = [["t2", "Thứ Hai"], ["t3", "Thứ Ba"], ["t4", "Thứ Tư"], ["t5", "Thứ Năm"], ["t6", "Thứ Sáu"], ["t7", "Thứ Bảy"], ["cn", "Chủ Nhật"]];

function bm05(plan: boolean): Field[] {
  return DAYS.flatMap(([k, g], i) => {
    const req = i < 5;
    return [
      { key: `${k}_loai`, label: "Loại công việc", type: "choice", required: req, options: WORK, group: g },
      { key: `${k}_cong_tac_khac`, label: "Công tác khác (ghi rõ)", type: "text", required: false, group: g },
      ...(plan
        ? [
            { key: `${k}_ke_hoach`, label: "Kế hoạch công việc", type: "text", required: req, group: g },
            { key: `${k}_ket_qua_du_kien`, label: "Kết quả dự kiến", type: "text", required: req, group: g },
            { key: `${k}_dia_diem`, label: "Địa điểm công tác", type: "text", required: false, group: g },
          ]
        : [{ key: `${k}_ket_qua`, label: "Kết quả thực hiện", type: "text", required: req, group: g }]),
    ];
  }) as Field[];
}

const HERD = "CN-01 Tổng đàn hiện có";
const VAC = "CN-02 Tiêm phòng trong tuần";
const FORMS: Omit<Form, "template_id" | "template_version" | "template_up_to_date">[] = [
  { code: "CNTS-BM01", bm: "BM-01", title: "Báo cáo kiểm soát giết mổ hằng ngày", unit_kind: "facility", period_type: "day", deadline_offset_days: 1, deadline_time: "09:00",
    due: "09:00 ngày hôm sau", nil_label: "Cơ sở không hoạt động trong ngày",
    fields: ["trau_bo:Trâu bò", "heo:Heo", "gia_cam:Gia cầm", "de:Dê"].map((x) => int(x.split(":")[0]!, x.split(":")[1]!, "con", "Số con kiểm soát giết mổ")) },
  { code: "CNTS-BM04", bm: "BM-04", title: "Nhập liệu tuần chăn nuôi", unit_kind: "commune", period_type: "week", deadline_offset_days: 1, deadline_time: "12:00",
    due: "12:00 ngày sau ngày cuối tuần", nil_label: "Không phát sinh trong tuần",
    fields: [
      ...["heo:Heo", "de:Dê", "trau:Trâu", "bo:Bò", "vit:Vịt", "ga:Gà", "cho_meo:Chó mèo"].map((x) => int(`tong_dan_${x.split(":")[0]}`, x.split(":")[1]!, "con", HERD)),
      int("tong_dan_yen", "Yến", "nhà", HERD),
      ...[
        "ta_heo_cp:Tả heo Châu Phi", "dich_ta_tht_heo:Dịch tả – THT heo", "lmlm_heo:LMLM heo", "tai_xanh_heo:Tai xanh heo", "vdnc_bo:Viêm da nổi cục bò",
        "tht_bo:THT bò", "lmlm_bo:LMLM bò", "cum_gia_cam:Cúm gia cầm", "dai:Dại chó, mèo",
      ].map((x) => int(`tiem_${x.split(":")[0]}`, x.split(":")[1]!, "liều", VAC)),
      dec("hoa_chat_gia_suc", "Gia súc", "lít", "CN-04 Hóa chất trong tuần"), dec("hoa_chat_gia_cam", "Gia cầm", "lít", "CN-04 Hóa chất trong tuần"),
      int("ksgm_gia_suc", "Gia súc", "con", "CN-06 KSGM trong tuần"), int("ksgm_gia_cam", "Gia cầm", "con", "CN-06 KSGM trong tuần"),
      int("vit_chay_dong_dia_phuong", "Địa phương", "con", "CN-08/09 Vịt chạy đồng"), int("vit_chay_dong_noi_khac", "Nơi khác đến", "con", "CN-08/09 Vịt chạy đồng"),
      { key: "ghi_chu", label: "CN-10 Ghi chú", type: "text", required: false },
    ] },
  { code: "CNTS-BM17", bm: "BM-17", title: "Sản lượng khai thác thủy sản hằng tuần", unit_kind: "commune", period_type: "week", deadline_offset_days: 1,
    deadline_time: "12:00", due: "12:00 ngày sau ngày cuối tuần", nil_label: "Không phát sinh khai thác trong tuần",
    fields: ["ca:TS-01 Cá các loại", "tom:TS-02 Tôm các loại", "muc:TS-03 Mực", "khac:TS-04 Thủy sản khác"].map((x) =>
      dec(x.split(":")[0]!, x.split(":")[1]!, "tấn", "Sản lượng khai thác trong tuần")) },
  { code: "CNTS-BM05-I", bm: "BM-05 Phần I", title: "Kết quả công việc tuần (cán bộ tăng cường)", unit_kind: "commune", period_type: "week", deadline_offset_days: 3,
    deadline_time: "08:00", due: "08:00 Thứ Hai sau tuần làm việc", nil_label: null, fields: bm05(false) },
  { code: "CNTS-BM05-II", bm: "BM-05 Phần II", title: "Kế hoạch công việc tuần sau (cán bộ tăng cường)", unit_kind: "commune", period_type: "week",
    deadline_offset_days: -3, deadline_time: "17:00", due: "17:00 Thứ Sáu trước tuần kế hoạch", nil_label: null, fields: bm05(true) },
];

interface Tpl { id: string; version: number }
interface Sub { id: string; version: number; values: Record<string, string>; nil_report: boolean; note: string; explanation: string; late: boolean; at: string; by: string;
  /** BF-CN-XA-DATA: rà soát của Cấp Tỉnh (xác thực / yêu cầu điều chỉnh). */
  review?: { id: string; decision: "accepted" | "returned"; note: string | null; at: string; by: string } }
interface Period {
  id: string; code: string; title: string; label: string; kind: string; key: string; from: string; to: string; due_at: string; rule: string;
  template_id: string; template_version: number; unit_kind: "commune" | "facility"; units: string[]; parents: string[];
  status: "open" | "closed" | "reopened" | "cancelled"; reopen_units: string[]; subs: Map<string, Sub[]>;
}

const TEMPLATES = new Map<string, Tpl>();
const PERIODS: Period[] = [];

// BF-CN-PUBLISH (CN-07): xã của tỉnh giả lập kèm khu vực, nhóm "có nuôi thủy sản" / "có mô hình", lần phát hành (người
// phụ trách, giờ chốt), nháp của xã, lượt nhắc tay.
const MOCK_COMMUNES: { commune_code: string; region_code: string; region_name: string }[] = [
  { commune_code: "91301", region_code: "KV1", region_name: "Khu vực 1 — Châu Đốc" },
  { commune_code: "91302", region_code: "KV1", region_name: "Khu vực 1 — Châu Đốc" },
  { commune_code: "91303", region_code: "KV2", region_name: "Khu vực 2 — Long Xuyên" },
  { commune_code: "91304", region_code: "KV2", region_name: "Khu vực 2 — Long Xuyên" },
  { commune_code: "91305", region_code: "KV3", region_name: "Khu vực 3 — Tri Tôn" },
];
const AQUA_COMMUNES = ["91302", "91304"];
const MODEL_COMMUNES = ["91301", "91302", "91305"];
interface Publication { id: string; template_code: string; target: string; regions: string[]; owner_ref: string | null; close_after_hours: number | null }
const PUBLICATIONS = new Map<string, Publication>(); // kỳ → lần phát hành
const DRAFTS = new Map<string, { period_id: string; unit: string; values: Record<string, string>; nil_report: boolean; note: string; saved_at: string }>();
const MANUAL = new Set<string>(); // kỳ|đơn vị|giờ
/** W11.44: kỳ đã phát hành (chưa hủy) kèm bài cuối từng đơn vị — giả lập tổng hợp BM-02, BM-18 đọc. */
export function cntsPeriodFigures(code: string) {
  return PERIODS.filter((p) => p.code === code && p.status !== "cancelled").map((p) => ({
    id: p.id, key: p.key, from: p.from, to: p.to, status: p.status, units: p.units, parents: p.parents,
    last: new Map([...p.subs].map(([u, l]) => [u, l.at(-1)!] as const)),
  }));
}

/** Xóa trạng thái (kiểm thử). */
export function resetCntsPeriods() {
  TEMPLATES.clear();
  PERIODS.length = 0;
  PUBLICATIONS.clear();
  DRAFTS.clear();
  MANUAL.clear();
}

function viewForm(f: (typeof FORMS)[number]): Form {
  const t = TEMPLATES.get(f.code);
  return { ...f, template_id: t?.id ?? null, template_version: t?.version ?? null, template_up_to_date: !!t };
}

interface Who { role: string; communes: string[] | null; actor: string }
function who(ctx: Ctx): Who {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const role = kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh");
  return { role, communes: role === "kn_xa" ? (kc?.communes ?? []).map((c) => c.code) : null, actor: ctx.persona.me.actor_ref };
}

const facilityCommune = (ref: string) => CATALOGS.cnts_slaughter_facility.find((x) => x.attrs.facility_ref === ref)?.attrs.commune_code;

// --- lịch kỳ (bản thu gọn của reportcycle/domain SplitCNTS) ---
const D = 86_400_000;
const ymd = (t: number) => new Date(t).toISOString().slice(0, 10);
const dmy = (t: number) => ymd(t).split("-").reverse().join("/");
const dueOf = (day: number, offset: number, time: string) => new Date(day + offset * D + Number(time.slice(0, 2)) * 3_600_000 + Number(time.slice(3)) * 60_000 - 7 * 3_600_000).toISOString();
function isoWeek(t: number): [number, number] {
  const d = new Date(t);
  const day = (d.getUTCDay() + 6) % 7;
  const th = t - day * D + 3 * D;
  const y = new Date(th).getUTCFullYear();
  return [y, 1 + Math.floor((th - Date.UTC(y, 0, 4) + ((new Date(Date.UTC(y, 0, 4)).getUTCDay() + 6) % 7) * D) / (7 * D))];
}

function split(f: Form, from: string, to: string, deadline?: { offset_days: number; time: string }) {
  const off = deadline?.offset_days ?? f.deadline_offset_days;
  const time = deadline?.time ?? f.deadline_time;
  const a = Date.parse(from + "T00:00:00Z");
  const b = Date.parse(to + "T00:00:00Z");
  const out: { kind: string; key: string; label: string; from_date: string; to_date: string; due_at: string; rule: string; id: null }[] = [];
  // D8: lịch tuần CNTS Thứ Ba – Thứ Hai (tham số extension.report_period_rule:chan_nuoi_thuy_san → nạp sẵn).
  const cal = calendarAt(DEPT_CNTS, from);
  const v2 = ruleTag(cal.rule);
  const ws = cal.rule.week_start;
  if (f.code.startsWith("CNTS-BM05")) {
    for (let d = a - ((new Date(a).getUTCDay() + 6) % 7) * D; d <= b; d += 7 * D) {
      const fri = d + 4 * D;
      const sun = d + 6 * D;
      if (fri < a) continue;
      const [y, w] = isoWeek(d);
      out.push({ kind: "week", key: `${y}-LV${String(w).padStart(2, "0")}`, label: `Tuần làm việc ${w}/${y} (${dmy(d).slice(0, 5)}–${dmy(sun)})`, from_date: ymd(d),
        to_date: ymd(sun), due_at: dueOf(off < 0 ? d : fri, off, time), rule: "cnts.work_week/v1", id: null });
    }
  } else if (f.period_type === "day") {
    for (let d = a; d <= b; d += D) out.push({ kind: "day", key: ymd(d), label: `Ngày ${dmy(d)}`, from_date: ymd(d), to_date: ymd(d), due_at: dueOf(d, off, time), rule: v2, id: null });
  } else {
    const startOf = (t: number) => t - ((new Date(t).getUTCDay() - ws + 7) % 7) * D;
    for (let d = startOf(a); d <= b; d += 7 * D) {
      const end = d + 6 * D;
      const y = new Date(end).getUTCFullYear();
      const no = Math.floor((d - startOf(Date.UTC(y, 0, 1))) / (7 * D)) + 1;
      out.push({ kind: "week", key: `${y}-W${String(no).padStart(2, "0")}`, label: `Tuần ${no}/${y}`, from_date: ymd(d), to_date: ymd(end), due_at: dueOf(end, off, time),
        rule: v2, id: null });
    }
  }
  return out;
}

function unitStatus(p: Period, unit: string, now = Date.now()): S["FormsTask"]["status"] {
  const last = p.subs.get(unit)?.at(-1);
  const closedFor = p.status === "closed" || (p.status === "reopened" && p.reopen_units.length > 0 && !p.reopen_units.includes(unit));
  if (closedFor) return last ? (last.review?.decision ?? "submitted") : "missed";
  if (last) return last.review?.decision ?? "submitted";
  return now > Date.parse(p.due_at) ? "overdue" : "not_submitted";
}

function task(p: Period, unit: string): S["FormsTask"] {
  const f = FORMS.find((x) => x.code === p.code)!;
  const last = p.subs.get(unit)?.at(-1);
  const i = p.units.indexOf(unit);
  return {
    id: `${p.id}:${unit}`, profile: PROFILE, period_id: p.id, unit, unit_kind: p.unit_kind, parent: p.parents[i] ?? unit, template_id: p.template_id,
    template_code: p.code, template_title: `${f.bm} — ${f.title}`, template_version: p.template_version, fields: f.fields, label: p.label, period_kind: p.kind,
    period_key: p.key, period_from: p.from, period_to: p.to, due_at: p.due_at, period_status: p.status,
    open: p.status === "open" || (p.status === "reopened" && (p.reopen_units.length === 0 || p.reopen_units.includes(unit))), status: unitStatus(p, unit),
    version: last?.version ?? 0, submission_id: last?.id ?? null, values: last?.values ?? {}, nil_report: last?.nil_report ?? false, note: last?.note ?? "",
    explanation: last?.explanation ?? "", late: last?.late ?? false, submitted_at: last?.at ?? null,
    return_note: last?.review?.decision === "returned" ? last.review.note : null, held_at: null,
  };
}

const sees = (w: Who, p: Period, unit: string) => w.communes === null || w.communes.includes(p.parents[p.units.indexOf(unit)] ?? unit);
const findPeriod = (id: string) => PERIODS.find((p) => p.id === id);

function periodView(p: Period, w: Who): S["FormsPeriodView"] {
  return {
    period: { id: p.id, profile: PROFILE, template_id: p.template_id, template_code: p.code, template_version: p.template_version, label: p.label, due_at: p.due_at,
      units: p.units, owner_ref: null, publisher_level: "TINH", published_by: "mock", published_at: p.from + "T00:00:00Z" },
    spec: { unit_kind: p.unit_kind, unit_parents: p.unit_kind === "facility" ? p.parents : [], max_submissions: 0, review_flow: "single", period_kind: p.kind,
      period_key: p.key, period_from: p.from, period_to: p.to, period_rule: p.rule },
    status: p.status, reopen_units: p.reopen_units, events: [], snapshots: [],
    units: p.units.filter((u) => sees(w, p, u)).map((u) => ({ unit: u, parent: p.parents[p.units.indexOf(u)] ?? u, status: unitStatus(p, u), submission: null, held: null })),
  };
}

const body = <T>(ctx: Ctx) => (ctx.body ?? {}) as T;
const reviewOut = (s: Sub) => (s.review ? { id: s.review.id, decision: s.review.decision, reviewer_ref: s.review.by, note: s.review.note, event_time: s.review.at } : null);
const tinhOnly = (w: Who) => (w.role === "kn_tinh" || w.role === "admin" ? null : problem(403, "forbidden", "phát hành, chốt kỳ cần Cấp Tỉnh"));

export const cntsPeriodRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/extension\/cnts\/period-forms$/, handler: () => json({ items: FORMS.map(viewForm) }) },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/period-forms\/install$/,
    handler: (ctx) => {
      if (who(ctx).role !== "admin") return problem(403, "forbidden", "định nghĩa biểu cần vai define_by của cấu hình");
      for (const f of FORMS) if (!TEMPLATES.has(f.code)) TEMPLATES.set(f.code, { id: uuidv7(), version: 1 });
      return json({ items: FORMS.map(viewForm) });
    },
  },
  ...(["preview", ""] as const).map(
    (kind): Route => ({
      method: "POST",
      pattern: kind ? /^\/v1\/extension\/cnts\/period-series\/preview$/ : /^\/v1\/extension\/cnts\/period-series$/,
      handler: (ctx) => {
        const b0 = body<S["CNTSSeriesInput"]>(ctx);
        const f = FORMS.map(viewForm).find((x) => x.code === b0.code);
        if (!f) return validation([{ field: "code", reason: "biểu kỳ CNTS không có" }]);
        const target = b0.target ?? (f.unit_kind === "facility" ? "facilities" : "communes");
        if ((f.unit_kind === "facility") !== (target === "facilities")) return validation([{ field: "target", reason: "đối tượng không hợp với đơn vị nhập của biểu" }]);
        const regions = b0.regions ?? [];
        if (target === "regions" && !regions.length) return validation([{ field: "regions", reason: "chọn ít nhất một khu vực" }]);
        if (b0.close_after_hours != null && (b0.close_after_hours < 0 || b0.close_after_hours > 720)) return validation([{ field: "close_after_hours", reason: "0–720 giờ sau hạn nộp" }]);
        const pick: Record<string, (c: (typeof MOCK_COMMUNES)[number]) => boolean> = {
          province: () => true, regions: (c) => regions.includes(c.region_code), aquaculture: (c) => AQUA_COMMUNES.includes(c.commune_code),
          model: (c) => MODEL_COMMUNES.includes(c.commune_code),
        };
        const units = pick[target] ? MOCK_COMMUNES.filter(pick[target]).map((c) => c.commune_code) : [...new Set(b0.units ?? [])];
        const b = { ...b0, units };
        if (!b.units.length) return validation([{ field: "units", reason: "chọn ít nhất một đơn vị nhận" }]);
        if (!(b.from_date <= b.to_date)) return validation([{ field: "to_date", reason: "phải không trước from_date" }]);
        const periods = split(f, b.from_date, b.to_date, b.deadline ?? undefined);
        const workWeek = f.code.startsWith("CNTS-BM05");
        const cal = calendarAt(DEPT_CNTS, b.from_date);
        const series = { code: f.code, calendar_id: workWeek ? null : cal.calendar_id, calendar_version: workWeek ? null : cal.calendar_version,
          unit_kind: f.unit_kind, units: b.units, tasks: periods.length * b.units.length, periods, target: target as S["CNTSSeries"]["target"], regions,
          owner_ref: b0.owner_ref ?? null, close_after_hours: b0.close_after_hours ?? null, communes: f.unit_kind === "commune" ? MOCK_COMMUNES : [],
          publication_id: null as string | null };
        if (kind) return json(series);
        const deny = tinhOnly(who(ctx));
        if (deny) return deny;
        const t = TEMPLATES.get(f.code);
        if (!t) return problem(404, "forms.template_not_found", `Không tìm thấy phiên bản biểu — biểu ${f.bm} chưa cài`);
        const parents = b.units.map((u) => (f.unit_kind === "facility" ? facilityCommune(u) : u));
        const bad = b.units.find((_, i) => !parents[i]);
        if (bad) return problem(422, "forms.facility_invalid", "Cơ sở không có trong danh mục cơ sở giết mổ đang dùng", { errors: [{ field: "units", reason: bad }] });
        for (const p of periods) {
          const id = uuidv7();
          p.id = id as never;
          PERIODS.unshift({ id, code: f.code, title: f.title, label: p.label, kind: p.kind, key: p.key, from: p.from_date, to: p.to_date, due_at: p.due_at, rule: p.rule,
            template_id: t.id, template_version: t.version, unit_kind: f.unit_kind, units: b.units, parents: parents as string[], status: "open", reopen_units: [],
            subs: new Map() });
        }
        const pub: Publication = { id: uuidv7(), template_code: f.code, target, regions, owner_ref: b0.owner_ref ?? null, close_after_hours: b0.close_after_hours ?? null };
        for (const p of periods) PUBLICATIONS.set(p.id as unknown as string, pub);
        series.publication_id = pub.id;
        return json(series, 201);
      },
    }),
  ),
  {
    method: "GET",
    pattern: /^\/v1\/forms\/tasks$/,
    handler: (ctx) => {
      const w = who(ctx);
      const code = ctx.url.searchParams.get("code");
      const from = ctx.url.searchParams.get("from");
      const to = ctx.url.searchParams.get("to");
      const items = PERIODS.filter((p) => p.status !== "cancelled" && (!code || p.code === code) && (!from || p.to >= from) && (!to || p.from <= to)).flatMap((p) => p.units.filter((u) => sees(w, p, u)).map((u) => task(p, u)));
      return json({ items });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/forms\/periods\/([^/]+)\/previous$/,
    handler: (ctx) => {
      const p = findPeriod(ctx.params[0]!);
      const unit = ctx.url.searchParams.get("unit") ?? "";
      if (!p) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
      const prev = PERIODS.filter((x) => x.code === p.code && x.from < p.from && x.subs.get(unit)?.length).sort((x, y) => (x.from < y.from ? 1 : -1))[0];
      const s = prev?.subs.get(unit)?.at(-1);
      if (!prev || !s) return json({ previous: null });
      return json({
        previous: { period_id: prev.id, label: prev.label, submission: { id: s.id, period_id: prev.id, unit, version: s.version, values: s.values, submitter_ref: s.by,
          event_time: s.at, status: "submitted", review: null, nil_report: s.nil_report, note: s.note || null, explanation: s.explanation || null, late: s.late, captured_at: null } },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/forms\/periods\/([^/]+)$/,
    handler: (ctx) => {
      const p = findPeriod(ctx.params[0]!);
      return p ? json(periodView(p, who(ctx))) : problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/forms\/periods\/([^/]+)\/submissions$/,
    handler: (ctx) => {
      const p = findPeriod(ctx.params[0]!);
      const unit = ctx.url.searchParams.get("unit") ?? "";
      if (!p) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
      if (!sees(who(ctx), p, unit)) return problem(403, "forbidden", `đơn vị ${unit} ngoài phạm vi`);
      const items = [...(p.subs.get(unit) ?? [])].reverse().map((s) => ({ id: s.id, period_id: p.id, unit, version: s.version, values: s.values, submitter_ref: s.by,
        event_time: s.at, status: s.review?.decision ?? "submitted", review: reviewOut(s), nil_report: s.nil_report, note: s.note || null, explanation: s.explanation || null, late: s.late, captured_at: null }));
      return json({ items });
    },
  },
  {
    // BF-CN-WEEKLY (UP-06): kế hoạch BM-05 Phần II đã đăng ký → Lịch công tác chờ duyệt (sổ lịch giả lập kn-schedules).
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/weekly-plan\/schedule$/,
    handler: (ctx) => {
      const b = body<{ period_id?: string; unit?: string }>(ctx);
      const p = findPeriod(b.period_id ?? "");
      const w = who(ctx);
      if (w.role !== "kn_xa") return problem(403, "forbidden", "chỉ cán bộ xã Phòng Chăn nuôi – Thủy sản đăng ký kế hoạch tuần");
      const unit = b.unit ?? "";
      if (!p || p.code !== "CNTS-BM05-II" || !p.subs.get(unit)?.length) {
        return problem(409, "extension.cnts_plan_not_registered", "Chưa đăng ký kế hoạch tuần (BM-05 Phần II) của kỳ và xã này");
      }
      if (!sees(w, p, unit)) return problem(403, "forbidden", `xã ${unit} không thuộc các xã phụ trách`);
      const sub = p.subs.get(unit)!.at(-1)!;
      if (sub.by !== w.actor) return problem(403, "extension.cnts_plan_not_owner", "Kế hoạch tuần do cán bộ khác đăng ký");
      const c = callerOf(ctx.persona, ctx.request.headers.get("X-AG-Context"));
      if (!c.ok) return problem(c.status, c.code, c.title);
      const store = workScheduleStore();
      const mine = store.mine(c.body, p.from);
      const cur = mine.ok ? mine.body.schedule : null;
      const planIds = new Set(DAYS.map(([k]) => planItemId(p.from, unit, k)));
      const keep = (cur?.items ?? []).filter((it) => !planIds.has(it.id));
      const plan = DAYS.flatMap(([k], i) => {
        const v = (x: string) => (sub.values[`${k}_${x}`] ?? "").trim();
        if (!v("loai") && !v("ke_hoach")) return [];
        const note = [v("loai") === WORK[1] && v("cong_tac_khac") ? `Công tác khác: ${v("cong_tac_khac")}` : "", v("ket_qua_du_kien") ? `Kết quả dự kiến: ${v("ket_qua_du_kien")}` : ""]
          .filter(Boolean)
          .join(" · ");
        return [{ id: planItemId(p.from, unit, k), day: ymd(Date.parse(p.from + "T00:00:00Z") + i * D), session: "morning" as const, start_time: "07:00", end_time: "17:00",
          content: v("ke_hoach") || v("loai"), commune_code: unit, location: v("dia_diem") || "Làm việc tại xã", note: note || null }];
      });
      const r = store.save(c.body, { week_start: p.from, action: "submit", base_version: cur?.schedule.version ?? null, items: [...keep, ...plan] });
      return r.ok ? json(r.body, 201) : problem(r.status, r.code, r.title, r.extra ?? {});
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/forms\/periods\/([^/]+)\/submissions$/,
    handler: (ctx) => {
      const p = findPeriod(ctx.params[0]!);
      const w = who(ctx);
      const b = body<{ unit: string; values: Record<string, string>; base_version?: number; nil_report?: boolean; note?: string; explanation?: string }>(ctx);
      if (!p) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
      if (w.role !== "kn_xa") return problem(403, "forbidden", "nộp bài cần cấp XA");
      if (!p.units.includes(b.unit) || !sees(w, p, b.unit)) return problem(403, "forbidden", `đơn vị ${b.unit} ngoài phạm vi`);
      if (p.status === "cancelled") return problem(409, "forms.period_cancelled", "Kỳ đã hủy");
      if (!task(p, b.unit).open) return problem(409, "forms.period_closed", "Kỳ đã chốt — mở lại kỳ trước khi nộp hoặc chốt lại");
      const list = p.subs.get(b.unit) ?? [];
      const cur = list.at(-1);
      if (cur?.review?.decision === "accepted" && p.status !== "reopened") return problem(409, "forms.submission_accepted", "Bài nộp đã được chấp nhận — mở lại kỳ để nộp lại");
      if (b.base_version !== undefined && b.base_version !== (cur?.version ?? 0)) {
        return problem(409, "forms.submission_conflict", "Đã có người cập nhật báo cáo — tải bản mới rồi sửa lại (không ghi đè)", {
          detail: `Đã có người cập nhật lúc ${cur ? new Date(cur.at).toLocaleString("vi-VN") : "—"} (phiên bản ${cur?.version ?? 0})`,
        });
      }
      const late = Date.now() > Date.parse(p.due_at);
      if (late && !(b.explanation ?? "").trim()) {
        return problem(422, "forms.late_reason_required", "Gửi sau hạn nộp phải ghi lý do trễ (giải trình)", { errors: [{ field: "explanation", reason: "bắt buộc" }] });
      }
      const f = FORMS.find((x) => x.code === p.code)!;
      const values = { ...b.values };
      for (const fd of f.fields) {
        if (b.nil_report && (fd.type === "integer" || fd.type === "decimal")) values[fd.key] = "0";
        if (fd.required && !(values[fd.key] ?? "").trim()) return problem(422, "forms.values_invalid", "Số liệu không khớp định nghĩa biểu", { errors: [{ field: fd.key, reason: "bắt buộc" }] });
      }
      const s: Sub = { id: uuidv7(), version: (cur?.version ?? 0) + 1, values, nil_report: !!b.nil_report, note: b.note ?? "", explanation: b.explanation ?? "", late,
        at: new Date().toISOString(), by: w.actor };
      list.push(s);
      p.subs.set(b.unit, list);
      return json({ id: s.id, period_id: p.id, unit: b.unit, version: s.version, values, submitter_ref: s.by, event_time: s.at, status: "submitted", review: null,
        nil_report: s.nil_report, note: s.note || null, explanation: s.explanation || null, late, captured_at: null }, 201);
    },
  },
  {
    // BF-CN-XA-DATA (FN-09): Cấp Tỉnh xác thực / yêu cầu điều chỉnh phiên bản bài nộp cuối (người rà soát ≠ người nộp).
    method: "POST",
    pattern: /^\/v1\/forms\/submissions\/([^/]+)\/review$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = tinhOnly(w);
      if (deny) return deny;
      const b = body<{ decision?: string; note?: string }>(ctx);
      const note = (b.note ?? "").trim();
      if (b.decision !== "accepted" && b.decision !== "returned") return validation([{ field: "decision", reason: "accepted | returned" }]);
      if (b.decision === "returned" && !note) return validation([{ field: "note", reason: "bắt buộc khi trả lại" }]);
      for (const p of PERIODS) {
        for (const [unit, list] of p.subs) {
          const s = list.find((x) => x.id === ctx.params[0]);
          if (!s) continue;
          if (p.status === "cancelled") return problem(409, "forms.period_cancelled", "Kỳ đã hủy");
          if (p.status === "closed") return problem(409, "forms.period_closed", "Kỳ đã chốt");
          if (list.at(-1) !== s) return problem(409, "forms.submission_superseded", "Đã có phiên bản mới hơn");
          if (s.review) return problem(409, "forms.already_reviewed", "Phiên bản đã được rà soát");
          if (s.by === w.actor) return problem(403, "self_approval_forbidden", "người rà soát trùng người nộp");
          s.review = { id: uuidv7(), decision: b.decision, note: note || null, at: new Date().toISOString(), by: w.actor };
          return json({ id: s.id, period_id: p.id, unit, version: s.version, values: s.values, submitter_ref: s.by, event_time: s.at, status: b.decision,
            review: reviewOut(s), nil_report: s.nil_report, note: s.note || null, explanation: s.explanation || null, late: s.late, captured_at: null });
        }
      }
      return problem(404, "forms.submission_not_found", "Không tìm thấy bài nộp");
    },
  },
  ...(["close", "reopen", "cancel"] as const).map(
    (action): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/forms/periods/([^/]+)/${action}$`),
      handler: (ctx) => {
        const p = findPeriod(ctx.params[0]!);
        const w = who(ctx);
        if (!p) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
        const deny = tinhOnly(w);
        if (deny) return deny;
        const b = body<{ reason?: string; units?: string[] }>(ctx);
        if (action !== "close" && (b.reason ?? "").trim().length < 10) return validation([{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }]);
        if (p.status === "cancelled") return problem(409, "forms.period_cancelled", "Kỳ đã hủy");
        if (action === "close") {
          if (p.status === "closed") return problem(409, "forms.period_closed", "Kỳ đã chốt");
          p.status = "closed";
        } else if (action === "reopen") {
          if (p.status !== "closed") return problem(409, "forms.period_not_closed", "Kỳ chưa chốt");
          p.status = "reopened";
          p.reopen_units = b.units ?? [];
        } else {
          if (p.status === "closed") return problem(409, "forms.period_closed", "Kỳ đã chốt");
          p.status = "cancelled";
        }
        return json(periodView(p, w));
      },
    }),
  ),
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/period-tracking$/,
    handler: (ctx) => {
      const w = who(ctx);
      const code = ctx.url.searchParams.get("code");
      const extra = (ctx.url.searchParams.get("period_ids") ?? "").split(",").filter(Boolean);
      const periods = [...PUBLICATIONS].filter(([, p]) => !code || p.template_code === code).map(([id, p]) => ({
        period_id: id, publication_id: p.id, template_code: p.template_code, target: p.target, regions: p.regions, owner_ref: p.owner_ref,
        owner_name: p.owner_ref ? "Cán bộ tỉnh phụ trách (giả lập)" : null, close_after_hours: p.close_after_hours }));
      const ids = new Set([...periods.map((p) => p.period_id), ...extra]);
      const drafts = [...DRAFTS.values()].filter((d) => {
        const p = findPeriod(d.period_id);
        return ids.has(d.period_id) && !!p && sees(w, p, d.unit);
      }).map((d) => ({ period_id: d.period_id, unit: d.unit, saved_at: d.saved_at }));
      return json({ periods, drafts });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/period-drafts\/([^/]+)$/,
    handler: (ctx) => {
      const p = findPeriod(ctx.params[0]!);
      const unit = ctx.url.searchParams.get("unit") ?? "";
      if (!p || !p.units.includes(unit) || !sees(who(ctx), p, unit)) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
      const d = DRAFTS.get(`${p.id}|${unit}`);
      const last = p.subs.get(unit)?.at(-1);
      return json({ draft: d && (!last || d.saved_at > last.at) ? d : null });
    },
  },
  {
    method: "PUT",
    pattern: /^\/v1\/extension\/cnts\/period-drafts\/([^/]+)$/,
    handler: (ctx) => {
      const p = findPeriod(ctx.params[0]!);
      const w = who(ctx);
      const b = body<{ unit: string; values: Record<string, string>; nil_report?: boolean; note?: string }>(ctx);
      if (!p || !p.units.includes(b.unit) || !sees(w, p, b.unit)) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
      if (w.role !== "kn_xa") return problem(403, "forbidden", "chỉ cán bộ Cấp Xã lưu nháp báo cáo kỳ");
      if (p.status !== "open" && p.status !== "reopened") return problem(409, "extension.period_closed", "Kỳ đã chốt");
      const d = { period_id: p.id, unit: b.unit, values: b.values ?? {}, nil_report: !!b.nil_report, note: (b.note ?? "").trim(), saved_at: new Date().toISOString() };
      DRAFTS.set(`${p.id}|${b.unit}`, d);
      return json(d);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/period-reminders$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = tinhOnly(w);
      if (deny) return deny;
      const b = body<{ period_id: string; units?: string[] }>(ctx);
      const p = findPeriod(b.period_id);
      if (!p) return problem(404, "forms.period_not_found", "Không tìm thấy kỳ");
      if (p.status !== "open" && p.status !== "reopened") return problem(409, "conflict", "kỳ đã chốt hoặc đã hủy — không nhắc");
      const hour = new Date().toISOString().slice(0, 13);
      let reminded = 0;
      const skipped: string[] = [];
      for (const u of p.units) {
        if (b.units?.length && !b.units.includes(u)) continue;
        const st = unitStatus(p, u);
        if (!((st === "not_submitted" || st === "overdue") && !p.subs.get(u)?.length) && st !== "returned") continue;
        const k = `${p.id}|${u}|${hour}`;
        if (MANUAL.has(k)) skipped.push(u);
        else {
          MANUAL.add(k);
          reminded++;
        }
      }
      if (!reminded && !skipped.length) return validation([{ field: "period_id", reason: "không có đơn vị cần nhắc (mọi đơn vị đã gửi)" }]);
      return json({ reminded, skipped });
    },
  },
];
