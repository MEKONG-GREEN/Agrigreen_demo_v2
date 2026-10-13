// GIẢ LẬP biểu kỳ Cấp Xã CNTS (W11.42, BRD CNTS v3.7 UR-XA-06…09, 13) theo hợp đồng /v1/extension/cnts/* và /v1/forms/*
// (profile kn_cnts): cài biểu, chia lô kỳ (kỳ ngày, tuần Thứ Tư – Thứ Ba W11.03 v2, tuần làm việc BM-05) với hạn riêng,
// công việc theo kỳ × đơn vị, nộp có khóa lạc quan (base_version → 409 forms.submission_conflict), báo không phát sinh,
// lý do trễ, sao chép kỳ trước, chốt / mở lại / hủy kỳ. Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Field = S["FormsField"];
type Form = S["CNTSPeriodForm"];

const PROFILE = "kn_cnts";
const int = (key: string, label: string, unit: string, group: string): Field => ({ key, label, type: "integer", required: true, unit, non_negative: true, group });
const dec = (key: string, label: string, unit: string, group: string): Field => ({ key, label, type: "decimal", required: true, unit, non_negative: true, group });
const WORK = ["Làm việc chuyên môn thường xuyên tại xã", "Công tác khác"];
const DAYS: [string, string][] = [["t2", "Thứ Hai"], ["t3", "Thứ Ba"], ["t4", "Thứ Tư"], ["t5", "Thứ Năm"], ["t6", "Thứ Sáu"]];

function bm05(plan: boolean): Field[] {
  return DAYS.flatMap(([k, g]) => [
    { key: `${k}_loai`, label: "Loại công việc", type: "choice", required: true, options: WORK, group: g },
    { key: `${k}_cong_tac_khac`, label: "Công tác khác (ghi rõ)", type: "text", required: false, group: g },
    ...(plan
      ? [
          { key: `${k}_ke_hoach`, label: "Kế hoạch công việc", type: "text", required: true, group: g },
          { key: `${k}_ket_qua_du_kien`, label: "Kết quả dự kiến", type: "text", required: true, group: g },
          { key: `${k}_dia_diem`, label: "Địa điểm công tác", type: "text", required: false, group: g },
        ]
      : [{ key: `${k}_ket_qua`, label: "Kết quả thực hiện", type: "text", required: true, group: g }]),
  ]) as Field[];
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
    deadline_time: "17:00", due: "17:00 Thứ Hai sau tuần làm việc", nil_label: null, fields: bm05(false) },
  { code: "CNTS-BM05-II", bm: "BM-05 Phần II", title: "Kế hoạch công việc tuần sau (cán bộ tăng cường)", unit_kind: "commune", period_type: "week",
    deadline_offset_days: -3, deadline_time: "17:00", due: "17:00 Thứ Sáu trước tuần kế hoạch", nil_label: null, fields: bm05(true) },
];

interface Tpl { id: string; version: number }
interface Sub { id: string; version: number; values: Record<string, string>; nil_report: boolean; note: string; explanation: string; late: boolean; at: string; by: string }
interface Period {
  id: string; code: string; title: string; label: string; kind: string; key: string; from: string; to: string; due_at: string; rule: string;
  template_id: string; template_version: number; unit_kind: "commune" | "facility"; units: string[]; parents: string[];
  status: "open" | "closed" | "reopened" | "cancelled"; reopen_units: string[]; subs: Map<string, Sub[]>;
}

const TEMPLATES = new Map<string, Tpl>();
const PERIODS: Period[] = [];

/** Xóa trạng thái (kiểm thử). */
export function resetCntsPeriods() {
  TEMPLATES.clear();
  PERIODS.length = 0;
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
  const v2 = "kn.period_rule/v2;week_start=wed;week=year;month=week_end";
  if (f.code.startsWith("CNTS-BM05")) {
    for (let d = a - ((new Date(a).getUTCDay() + 6) % 7) * D; d <= b; d += 7 * D) {
      const fri = d + 4 * D;
      if (fri < a) continue;
      const [y, w] = isoWeek(d);
      out.push({ kind: "week", key: `${y}-LV${String(w).padStart(2, "0")}`, label: `Tuần làm việc ${w}/${y} (${dmy(d).slice(0, 5)}–${dmy(fri)})`, from_date: ymd(d),
        to_date: ymd(fri), due_at: dueOf(off < 0 ? d : fri, off, time), rule: "cnts.work_week/v1", id: null });
    }
  } else if (f.period_type === "day") {
    for (let d = a; d <= b; d += D) out.push({ kind: "day", key: ymd(d), label: `Ngày ${dmy(d)}`, from_date: ymd(d), to_date: ymd(d), due_at: dueOf(d, off, time), rule: v2, id: null });
  } else {
    const wedOf = (t: number) => t - ((new Date(t).getUTCDay() + 4) % 7) * D;
    for (let d = wedOf(a); d <= b; d += 7 * D) {
      const end = d + 6 * D;
      const y = new Date(end).getUTCFullYear();
      const no = Math.floor((d - wedOf(Date.UTC(y, 0, 1))) / (7 * D)) + 1;
      out.push({ kind: "week", key: `${y}-W${String(no).padStart(2, "0")}`, label: `Tuần ${no}/${y}`, from_date: ymd(d), to_date: ymd(end), due_at: dueOf(end, off, time),
        rule: v2, id: null });
    }
  }
  return out;
}

function unitStatus(p: Period, unit: string, now = Date.now()): S["FormsTask"]["status"] {
  const last = p.subs.get(unit)?.at(-1);
  const closedFor = p.status === "closed" || (p.status === "reopened" && p.reopen_units.length > 0 && !p.reopen_units.includes(unit));
  if (closedFor) return last ? "submitted" : "missed";
  if (last) return "submitted";
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
    explanation: last?.explanation ?? "", late: last?.late ?? false, submitted_at: last?.at ?? null, return_note: null, held_at: null,
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
        const b = body<S["CNTSSeriesInput"]>(ctx);
        const f = FORMS.map(viewForm).find((x) => x.code === b.code);
        if (!f) return validation([{ field: "code", reason: "biểu kỳ CNTS không có" }]);
        if (!b.units?.length) return validation([{ field: "units", reason: "chọn ít nhất một đơn vị nhận" }]);
        if (!(b.from_date <= b.to_date)) return validation([{ field: "to_date", reason: "phải không trước from_date" }]);
        const periods = split(f, b.from_date, b.to_date, b.deadline ?? undefined);
        const series = { code: f.code, unit_kind: f.unit_kind, units: b.units, tasks: periods.length * b.units.length, periods };
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
      const items = PERIODS.filter((p) => p.status !== "cancelled" && (!code || p.code === code)).flatMap((p) => p.units.filter((u) => sees(w, p, u)).map((u) => task(p, u)));
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
];
