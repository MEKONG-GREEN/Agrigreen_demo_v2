// GIẢ LẬP cảnh báo CNTS (W11.43, BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5) theo hợp đồng
// /v1/extension/cnts/alerts*, /alert-assignees, /alert-config*: vài cảnh báo mẫu (ổ dịch Khẩn, tổng đàn biến động, lệch KSGM),
// vòng đời Mới → Đã giao → Đang xử lý → Đã đóng (kết quả bắt buộc, khóa lạc quan base_seq), cấu hình nhắc theo biểu và
// xử lý theo loại, ngưỡng mặc định. Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Alert = S["CNTSAlert"];
type Detail = S["CNTSAlertDetail"];
type History = Detail["history"][number];
type Reminder = S["CNTSFormReminder"];
type Cfg = S["CNTSAlertConfig"];

const SYS = { ref: "00000000-0000-7000-8000-00000000c004", display: "Hệ thống" };
const ALERTS = new Map<string, Detail>();
const FORMS = new Map<string, { reminder: Reminder; version: number; at: string }>();
const KINDS = new Map<string, { sla_hours: number; urgent: boolean; version: number }>();

const KIND_TABLE: Cfg["kinds"][number]["kind"][] = [
  { code: "herd_change", no: 1, label: "Tổng đàn biến động", default_level: "attention", level_configurable: false, recipients: ["form_owner"], source: "forms" },
  { code: "slaughter_spike", no: 2, label: "KSGM bất thường", default_level: "attention", level_configurable: false, recipients: ["form_owner"], source: "forms" },
  { code: "slaughter_zero", no: 3, label: "KSGM bằng 0 khi cơ sở hoạt động", default_level: "attention", level_configurable: false, recipients: ["submitter"], source: "forms" },
  { code: "ksgm_mismatch", no: 4, label: "KSGM trong BM-04 lệch BM-01", default_level: "attention", level_configurable: false, recipients: ["commune", "form_owner"], source: "forms" },
  { code: "fishing_change", no: 5, label: "Khai thác thủy sản biến động", default_level: "attention", level_configurable: false, recipients: ["form_owner"], source: "forms" },
  { code: "water_quality", no: 6, label: "Môi trường nước", default_level: "attention", level_configurable: true, recipients: ["program_lead", "site_officer"], source: "log" },
  { code: "low_growth", no: 7, label: "Tăng trọng thấp", default_level: "attention", level_configurable: false, recipients: ["program_lead"], source: "log" },
  { code: "mortality", no: 8, label: "Tỷ lệ chết", default_level: "urgent", level_configurable: false, recipients: ["program_lead", "site_officer"], source: "log" },
  { code: "log_location", no: 9, label: "Vị trí lần ghi", default_level: "attention", level_configurable: false, recipients: ["program_lead"], source: "log" },
  { code: "log_overdue", no: 10, label: "Quá hạn ghi nhật ký", default_level: "attention", level_configurable: false, recipients: ["site_officer", "program_lead"], source: "log" },
  { code: "vaccine_overdue", no: 11, label: "Mũi tiêm quá hạn", default_level: "attention", level_configurable: false, recipients: ["site_officer", "farmer"], source: "schedule" },
  { code: "outbreak", no: 12, label: "Ổ dịch mới", default_level: "urgent", level_configurable: false, recipients: ["manager", "province", "program_lead"], source: "outbreak" },
  { code: "late_streak", no: 0, label: "Trễ nộp liên tiếp", default_level: "attention", level_configurable: false, recipients: ["form_owner", "manager"], source: "job" },
];
const FORM_TABLE = [
  ["CNTS-BM01", "BM-01 Kiểm soát giết mổ hằng ngày"],
  ["CNTS-BM04", "BM-04 Nhập liệu tuần chăn nuôi"],
  ["CNTS-BM17", "BM-17 Khai thác thủy sản hằng tuần"],
  ["CNTS-BM05-I", "BM-05 Phần I — Kết quả công việc tuần"],
  ["CNTS-BM05-II", "BM-05 Phần II — Kế hoạch tuần sau"],
] as const;
const THRESHOLDS: Cfg["thresholds"] = [
  { key: "cnts.herd_change_pct", label: "Tổng đàn biến động (BM-04) quá", unit: "%", value: "15", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.fishing_change_pct", label: "Khai thác thủy sản biến động (BM-17) quá", unit: "%", value: "50", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.slaughter_spike_factor", label: "KSGM bất thường: lớn hơn n lần trung bình 4 tuần", unit: "lần", value: "3", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.slaughter_spike_min_days", label: "KSGM bất thường: số ngày có số liệu tối thiểu", unit: "ngày", value: "7", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.lookback_periods", label: "\"Kỳ gần nhất\" tìm lùi tối đa", unit: "kỳ", value: "4", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.indicator_min:PH", label: "pH tối thiểu", unit: "", value: "7.5", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.indicator_max:PH", label: "pH tối đa", unit: "", value: "8.5", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.indicator_min:DO", label: "Oxy hòa tan tối thiểu", unit: "mg/l", value: "4", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.location_distance_m", label: "Vị trí lần ghi cách điểm quá", unit: "m", value: "200", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.gps_accuracy_m", label: "Sai số GPS quá", unit: "m", value: "50", version: 1, effective_from: "2026-01-01" },
  { key: "cnts.log_overdue_grace_days", label: "Quá hạn ghi nhật ký thêm", unit: "ngày", value: "1", version: 1, effective_from: "2026-01-01" },
];

const defaultReminder = (code: string): Reminder => ({
  before_due_hours: 24, on_overdue: true, repeat_daily: true, repeat_at: "08:00", late_streak: code === "CNTS-BM01" ? 3 : 2,
  channels: ["in_app", "push"], message: "Nhắc nộp {bieu} kỳ {ky}, hạn {han}.",
});
const defaultSLA = (kind: string) => (KIND_TABLE.find((k) => k.code === kind)?.default_level === "urgent" ? 24 : 72);

/** Xóa trạng thái (kiểm thử). */
export function resetCntsAlerts() {
  ALERTS.clear();
  FORMS.clear();
  KINDS.clear();
}

function seed() {
  if (ALERTS.size) return;
  const now = Date.now();
  const iso = (h: number) => new Date(now + h * 3_600_000).toISOString();
  const mk = (kind: string, level: Alert["level"], commune: string, title: string, detail: Record<string, unknown>, hoursAgo: number, extra: Partial<Alert> = {}) => {
    const k = KIND_TABLE.find((x) => x.code === kind)!;
    const a: Detail = {
      alert_ref: uuidv7(), kind, kind_label: k.label, kind_no: k.no, level, status: "new", overdue: false, commune_code: commune, province_code: "91",
      subject_kind: "commune", subject_ref: commune, template_code: null, period_key: null, period_ref: null, source_ref: null, title, detail,
      recipient_refs: [], assignee: null, raised_at: iso(-hoursAgo), due_at: iso(-hoursAgo + defaultSLA(kind)), overdue_at: null, closed_at: null,
      auto_closed: false, result: null, seq: 1, updated_at: iso(-hoursAgo), can_assign: false, can_start: false, can_close: false, ...extra,
      history: [{ seq: 1, action: "raised", status_after: "new", actor: SYS, assignee: null, due_at: iso(-hoursAgo + defaultSLA(kind)), note: null,
        event_time: iso(-hoursAgo), support_reason: null }],
    };
    a.overdue = Date.parse(a.due_at) < now;
    ALERTS.set(a.alert_ref, a);
  };
  mk("outbreak", "urgent", "91301", "Ổ dịch mới · bệnh ASF trên HEO · xã 91301 · đàn 120 con, chết 5",
    { reason: "Báo cáo dịch bệnh mới — xem chi tiết ở màn Báo cáo dịch bệnh", disease_code: "ASF", herd_size: 120, dead: 5 }, 2,
    { subject_kind: "outbreak", subject_ref: uuidv7() });
  mk("herd_change", "attention", "91301", "Tổng đàn biến động · BM-04 Nhập liệu tuần chăn nuôi · Tuần 41 · xã 91301",
    { summary: "Heo: tăng 20% so kỳ gần nhất (1000 → 1200), ngưỡng 15%", findings: [
      { field: "tong_dan_heo", label: "Heo", current: "1200", compare: "1000", change_pct: "20", reason: "tăng 20% so kỳ gần nhất (1000 → 1200), ngưỡng 15%" },
    ], period_label: "Tuần 41" }, 30, { template_code: "CNTS-BM04", period_key: "2026-W41" });
  mk("ksgm_mismatch", "attention", "91302", "KSGM trong BM-04 lệch BM-01 · BM-04 Nhập liệu tuần chăn nuôi · Tuần 41 · xã 91302",
    { summary: "KSGM gia súc: BM-04 ghi 12 con, tổng BM-01 các cơ sở trong xã 15 con", findings: [
      { field: "ksgm_gia_suc", label: "KSGM gia súc", current: "12", compare: "15", reason: "BM-04 ghi 12 con, tổng BM-01 các cơ sở trong xã 15 con" },
    ] }, 80, { template_code: "CNTS-BM04", period_key: "2026-W41" });
}

interface Who { role: string; communes: string[] | null; actor: string; readOnly: boolean; dept: string | null }
function who(ctx: Ctx): Who {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const role = kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh");
  return {
    role, communes: role === "kn_xa" ? (kc?.communes ?? []).map((c) => c.code) : null, actor: ctx.persona.me.actor_ref,
    readOnly: !!kc?.read_only, dept: kc?.department_code ?? null,
  };
}
function reader(w: Who): Response | null {
  if (w.role === "admin") return null;
  if ((w.role === "kn_tinh" || w.role === "kn_xa") && (w.dept === null || w.dept === "chan_nuoi_thuy_san")) return null;
  return problem(403, "forbidden", "Cảnh báo CNTS thuộc phòng Chăn nuôi – Thủy sản (Admin, Cấp Tỉnh, Cấp Xã)");
}
const mgr = (w: Who) => w.role === "admin" || w.role === "kn_tinh";
const sees = (w: Who, a: Alert) =>
  w.communes === null || a.assignee?.ref === w.actor || (a.commune_code !== null && w.communes.includes(a.commune_code));

function view(w: Who, a: Detail): Detail {
  const open = a.status !== "closed" && !w.readOnly;
  const mine = a.assignee?.ref === w.actor;
  return {
    ...a, overdue: a.status !== "closed" && Date.parse(a.due_at) < Date.now(), can_assign: open && mgr(w),
    can_start: open && a.status === "assigned" && (mine || w.role === "admin"), can_close: open && (mgr(w) || mine),
  };
}
const brief = (d: Detail): Alert => {
  const { history: _h, ...a } = d;
  void _h;
  return a;
};

function config(): Cfg {
  return {
    forms: FORM_TABLE.map(([code, label]) => {
      const c = FORMS.get(code);
      return { code, label, reminder: c?.reminder ?? defaultReminder(code), custom: !!c, version: c?.version ?? 0, updated_at: c?.at ?? null,
        updated_by: c ? { ref: "mock", display: "Cán bộ Cấp Tỉnh" } : null, sent_today: code === "CNTS-BM04" ? 3 : 0 };
    }),
    kinds: KIND_TABLE.map((k) => {
      const c = KINDS.get(k.code);
      const urgent = !!c?.urgent;
      return { kind: k, sla_hours: c?.sla_hours ?? defaultSLA(k.code), urgent, level: k.level_configurable && urgent ? "urgent" : k.default_level,
        custom: !!c, version: c?.version ?? 0 };
    }),
    thresholds: THRESHOLDS,
  };
}

function act(ctx: Ctx, w: Who): { a: Detail } | Response {
  seed();
  const a = ALERTS.get(ctx.params[0]!);
  if (reader(w) || !a || !sees(w, a)) return problem(404, "extension.cnts_alert_not_found", "Không có cảnh báo");
  if (w.readOnly) return problem(403, "extension.read_only", "Ngữ cảnh chỉ xem");
  const b = (ctx.body ?? {}) as { base_seq?: number };
  if (b.base_seq && b.base_seq !== a.seq) {
    return problem(409, "extension.cnts_alert_version_conflict", "Cảnh báo đã có người cập nhật — tải lại bản mới",
      { detail: `Đã có người cập nhật lúc ${new Date(a.updated_at).toLocaleString("vi-VN")}` });
  }
  if (a.status === "closed") return problem(409, "extension.cnts_alert_transition", "Trạng thái cảnh báo không cho phép thao tác này");
  return { a };
}

function push(a: Detail, w: Who, h: Omit<History, "seq" | "actor" | "event_time" | "support_reason">) {
  const now = new Date().toISOString();
  a.seq += 1;
  a.updated_at = now;
  a.history.push({ ...h, seq: a.seq, actor: { ref: w.actor, display: null }, event_time: now, support_reason: null });
}

export const cntsAlertRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/alerts$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      seed();
      const q = ctx.url.searchParams;
      const st = q.get("status"), kind = q.get("kind"), level = q.get("level"), mine = q.get("mine") === "true";
      const now = Date.now();
      const items = [...ALERTS.values()]
        .filter((a) => sees(w, a))
        .filter((a) => !st || (st === "open" ? a.status !== "closed" : st === "overdue" ? a.status !== "closed" && Date.parse(a.due_at) < now : a.status === st))
        .filter((a) => (!kind || a.kind === kind) && (!level || a.level === level) && (!mine || a.assignee?.ref === w.actor))
        .sort((x, y) => Number(x.status === "closed") - Number(y.status === "closed") || Number(y.level === "urgent") - Number(x.level === "urgent") ||
          y.raised_at.localeCompare(x.raised_at))
        .map((a) => brief(view(w, a)));
      const openItems = items.filter((a) => a.status !== "closed");
      return json({ items, counts: { open: openItems.length, urgent_open: openItems.filter((a) => a.level === "urgent").length,
        overdue: openItems.filter((a) => a.overdue).length } });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/alerts\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      seed();
      const a = ALERTS.get(ctx.params[0]!);
      if (reader(w) || !a || !sees(w, a)) return problem(404, "extension.cnts_alert_not_found", "Không có cảnh báo");
      return json(view(w, a));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/alerts\/([^/]+)\/assign$/,
    handler: (ctx) => {
      const w = who(ctx);
      const r = act(ctx, w);
      if (r instanceof Response) return r;
      if (!mgr(w)) return problem(403, "forbidden", "chỉ Cấp Tỉnh giao cảnh báo");
      const b = (ctx.body ?? {}) as { assignee_ref?: string; due_at?: string; note?: string };
      if (!b.assignee_ref) return problem(422, "extension.cnts_alert_invalid", "Thao tác cảnh báo không hợp lệ", { errors: [{ field: "assignee_ref", reason: "bắt buộc" }] });
      const a = r.a;
      a.status = "assigned";
      a.assignee = { ref: b.assignee_ref, display: ASSIGNEES.find((x) => x.actor.ref === b.assignee_ref)?.actor.display ?? null };
      a.due_at = b.due_at ?? new Date(Date.now() + defaultSLA(a.kind) * 3_600_000).toISOString();
      a.overdue_at = null;
      push(a, w, { action: "assigned", status_after: "assigned", assignee: a.assignee, due_at: a.due_at, note: b.note || null });
      return json(view(w, a));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/alerts\/([^/]+)\/start$/,
    handler: (ctx) => {
      const w = who(ctx);
      const r = act(ctx, w);
      if (r instanceof Response) return r;
      const a = r.a;
      if (w.role !== "admin" && a.assignee?.ref !== w.actor) return problem(403, "forbidden", "chỉ người được giao nhận xử lý cảnh báo");
      if (a.status !== "assigned") return problem(409, "extension.cnts_alert_transition", "Trạng thái cảnh báo không cho phép thao tác này");
      a.status = "in_progress";
      push(a, w, { action: "started", status_after: "in_progress", assignee: null, due_at: a.due_at, note: null });
      return json(view(w, a));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/alerts\/([^/]+)\/close$/,
    handler: (ctx) => {
      const w = who(ctx);
      const r = act(ctx, w);
      if (r instanceof Response) return r;
      const a = r.a;
      if (!mgr(w) && a.assignee?.ref !== w.actor) return problem(403, "forbidden", "chỉ người được giao hoặc Cấp Tỉnh đóng cảnh báo");
      const result = ((ctx.body ?? {}) as { result?: string }).result?.trim() ?? "";
      if ([...result].length < 10) {
        return problem(422, "extension.cnts_alert_invalid", "Thao tác cảnh báo không hợp lệ", { errors: [{ field: "result", reason: "kết quả xử lý 10–2000 ký tự" }] });
      }
      a.status = "closed";
      a.closed_at = new Date().toISOString();
      a.result = result;
      push(a, w, { action: "closed", status_after: "closed", assignee: null, due_at: a.due_at, note: result });
      return json(view(w, a));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/alert-assignees$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      const commune = ctx.url.searchParams.get("commune_code");
      return json({ items: ASSIGNEES.filter((a) => a.level === "province" || !commune || a.communes.includes(commune)) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/alert-config$/,
    handler: (ctx) => reader(who(ctx)) ?? json(config()),
  },
  {
    method: "PUT",
    pattern: /^\/v1\/extension\/cnts\/alert-config\/forms\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      if (!mgr(w)) return problem(403, "forbidden", "chỉ Cấp Tỉnh giao cảnh báo, cấu hình nhắc nhở");
      if (w.readOnly) return problem(403, "extension.read_only", "Ngữ cảnh chỉ xem");
      const code = ctx.params[0]!;
      const r = (ctx.body ?? {}) as Reminder;
      const errors: { field: string; reason: string }[] = [];
      if (!FORM_TABLE.some(([c]) => c === code)) errors.push({ field: "code", reason: "biểu kỳ CNTS không có" });
      if (!(r.before_due_hours >= 0 && r.before_due_hours <= 168)) errors.push({ field: "before_due_hours", reason: "0–168 giờ" });
      if (r.repeat_daily && !/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(r.repeat_at ?? "")) errors.push({ field: "repeat_at", reason: "giờ dạng HH:MM" });
      if (!(r.late_streak >= 0 && r.late_streak <= 12)) errors.push({ field: "late_streak", reason: "0–12 kỳ" });
      if (errors.length) return problem(422, "extension.cnts_alert_config_invalid", "Cấu hình nhắc nhở / cảnh báo không hợp lệ", { errors });
      const channels = [...new Set(["in_app", ...(r.channels ?? [])])] as Reminder["channels"];
      FORMS.set(code, { reminder: { ...r, channels, message: r.message?.trim() || defaultReminder(code).message }, version: (FORMS.get(code)?.version ?? 0) + 1,
        at: new Date().toISOString() });
      return json(config());
    },
  },
  {
    method: "PUT",
    pattern: /^\/v1\/extension\/cnts\/alert-config\/kinds\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = reader(w);
      if (deny) return deny;
      if (!mgr(w)) return problem(403, "forbidden", "chỉ Cấp Tỉnh giao cảnh báo, cấu hình nhắc nhở");
      const kind = KIND_TABLE.find((k) => k.code === ctx.params[0]);
      const b = (ctx.body ?? {}) as S["CNTSAlertSetting"];
      const errors: { field: string; reason: string }[] = [];
      if (!kind) errors.push({ field: "kind", reason: "loại cảnh báo không có" });
      if (!(b.sla_hours >= 1 && b.sla_hours <= 720)) errors.push({ field: "sla_hours", reason: "1–720 giờ" });
      if (b.urgent && kind && !kind.level_configurable) errors.push({ field: "urgent", reason: "mức của loại này cố định theo Mục III.5" });
      if (errors.length) return problem(422, "extension.cnts_alert_config_invalid", "Cấu hình nhắc nhở / cảnh báo không hợp lệ", { errors });
      KINDS.set(kind!.code, { sla_hours: b.sla_hours, urgent: !!b.urgent, version: (KINDS.get(kind!.code)?.version ?? 0) + 1 });
      return json(config());
    },
  },
];

const ASSIGNEES: S["CNTSAlertAssignees"]["items"] = [
  { actor: { ref: "0192a0a0-0000-7000-8000-0000000c4301", display: "Trần Bình (Cấp Tỉnh)" }, level: "province", communes: [], manager: false },
  { actor: { ref: "0192a0a0-0000-7000-8000-0000000c4302", display: "Lê Hòa (Lãnh đạo phòng)" }, level: "province", communes: [], manager: true },
  { actor: { ref: "0192a0a0-0000-7000-8000-0000000c4303", display: "Nguyễn An (Cấp Xã)" }, level: "commune", communes: ["91301"], manager: false },
];
