// GIẢ LẬP Kỳ KHTC (W11.62, BRD KHTC v2.2 FN-TI-02.03) theo hợp đồng /v1/extension/khtc/*: tuần báo cáo Thứ Tư – Thứ Ba,
// tháng/quý/năm theo Thứ Ba cuối, tên kỳ + hạn tự điền (hạn nộp Thứ Tư kế tiếp 10:00, hạn xuất 16:00), không mở trùng
// (trừ kỳ đã hủy), tối đa 3 lần gửi, đóng kỳ (Đủ dữ liệu / Trễ hạn ≥ 3 ngày kèm lý do), hủy kỳ (lý do bắt buộc).
// Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Period = S["KHTCPeriod"];
type Form = S["KHTCForm"];
type Input = S["KHTCOpenInput"];

const DEPTS: S["KHTCDept"][] = [
  { code: "trong_trot", name: "Phòng Trồng trọt" },
  { code: "chan_nuoi_thuy_san", name: "Phòng Chăn nuôi - Thủy sản" },
  { code: "thong_tin_truyen_thong", name: "Phòng Thông tin Tuyên truyền" },
  { code: "hanh_chinh_tong_hop", name: "Phòng Hành chính - Tổng hợp" },
];
const MAX = 3;
const LATE_DAYS = 3;
const PERIOD_LABEL: Record<Period["state"], string> = {
  draft: "Nháp",
  opened: "Đã mở",
  collecting: "Đang thu thập",
  late: "Trễ hạn",
  complete: "Đủ dữ liệu",
  closed: "Đã đóng",
  cancelled: "Đã hủy",
};
const FORM_LABEL: Record<Form["state"], string> = {
  not_started: "Chưa nhập",
  draft: "Lưu nháp",
  late: "Trễ hạn",
  submitted: "Đã gửi",
  supplement: "Yêu cầu bổ sung",
  confirmed: "Đã xác nhận",
  locked: "Đã khóa",
  not_sent: "Không gửi",
  withdrawn: "Đã gỡ",
  not_available: "Không đọc được",
};

// --- lịch (bản thu gọn của khtcperiod/domain.PlanOf) ---
const DAY = 86_400_000;
const d0 = (s: string) => Date.parse(`${s}T00:00:00Z`);
const ds = (t: number) => new Date(t).toISOString().slice(0, 10);
const vn = (s: string) => s.split("-").reverse().join("/");
function week1(y: number): number {
  const jan1 = Date.UTC(y, 0, 1);
  return jan1 - ((new Date(jan1).getUTCDay() - 3 + 7) % 7) * DAY;
}
export function weeksInYear(y: number): number {
  return (week1(y + 1) - week1(y)) / (7 * DAY);
}
/** Thứ Ba cuối cùng không sau ngày t. */
function lastTueOnOrBefore(t: number): number {
  return t - ((new Date(t).getUTCDay() - 2 + 7) % 7) * DAY;
}
function span(firstDay: number, nextFirst: number): [number, number] {
  const to = lastTueOnOrBefore(nextFirst - DAY);
  const firstTue = lastTueOnOrBefore(firstDay - DAY) + 7 * DAY;
  return [firstTue - 6 * DAY, to];
}
const ROMAN = ["I", "II", "III", "IV"];

interface Plan {
  key: string | null;
  name: string;
  from: number;
  to: number;
  dueRequired: boolean;
}

export function planOf(b: Input): Plan | { field: string; reason: string } {
  const y = b.year ?? 0;
  const n = b.no ?? 0;
  switch (b.report_type) {
    case "week": {
      if (n < 1 || n > weeksInYear(y))
        return { field: "no", reason: `tuần 1–${weeksInYear(y)} của năm ${y}` };
      const from = week1(y) + (n - 1) * 7 * DAY;
      const to = from + 6 * DAY;
      return {
        key: `${y}-W${String(n).padStart(2, "0")}`,
        name: `Tuần ${n} năm ${y} (từ ngày ${vn(ds(from))} đến ngày ${vn(ds(to))})`,
        from,
        to,
        dueRequired: false,
      };
    }
    case "month": {
      if (n < 1 || n > 12) return { field: "no", reason: "tháng 1–12" };
      const [from, to] = span(Date.UTC(y, n - 1, 1), Date.UTC(y, n, 1));
      return {
        key: `${y}-${String(n).padStart(2, "0")}`,
        name: `Tháng ${n} năm ${y} (số liệu đến ngày ${vn(ds(to))})`,
        from,
        to,
        dueRequired: false,
      };
    }
    case "quarter": {
      if (n < 1 || n > 4) return { field: "no", reason: "quý 1–4" };
      const [qf, to] = span(Date.UTC(y, (n - 1) * 3, 1), Date.UTC(y, n * 3, 1));
      const sc = b.scope ?? "quarter";
      if (sc === "quarter")
        return {
          key: `${y}-Q${n}`,
          name: `Quý ${ROMAN[n - 1]} năm ${y} (số liệu đến ngày ${vn(ds(to))})`,
          from: qf,
          to,
          dueRequired: false,
        };
      const want = sc === "half" ? 2 : 3;
      if (n !== want)
        return {
          field: "scope",
          reason: `${want === 2 ? "6" : "9"} tháng đầu năm đi với quý ${ROMAN[want - 1]}`,
        };
      const label = want === 2 ? "6 tháng" : "9 tháng";
      return {
        key: `${y}-${want === 2 ? "H1" : "9M"}`,
        name: `${label} đầu năm ${y} (số liệu đến ngày ${vn(ds(to))})`,
        from: span(Date.UTC(y, 0, 1), Date.UTC(y + 1, 0, 1))[0],
        to,
        dueRequired: false,
      };
    }
    case "year": {
      const [from, to] = span(Date.UTC(y, 0, 1), Date.UTC(y + 1, 0, 1));
      return {
        key: `${y}`,
        name: `Năm ${y} (số liệu đến ngày ${vn(ds(to))})`,
        from,
        to,
        dueRequired: false,
      };
    }
    case "program": {
      if (!b.as_of_date)
        return { field: "as_of_date", reason: 'Mẫu 5 cần "Đến ngày"' };
      const to = lastTueOnOrBefore(d0(b.as_of_date));
      if (to < week1(y) + 6 * DAY)
        return {
          field: "as_of_date",
          reason: `đến ngày phải có ít nhất một tuần báo cáo của năm ${y} đã kết thúc`,
        };
      return {
        key: null,
        name: `Đến ngày ${vn(b.as_of_date)} (số liệu đến ngày ${vn(ds(to))})`,
        from: week1(y),
        to,
        dueRequired: true,
      };
    }
    case "adhoc": {
      const name = (b.name ?? "").trim();
      if (name.length < 3)
        return { field: "name", reason: "tên báo cáo 3–150 ký tự" };
      if (!b.from_date || !b.to_date || b.to_date < b.from_date)
        return {
          field: "to_date",
          reason: "từ ngày, đến ngày bắt buộc; đến ngày ≥ từ ngày",
        };
      return {
        key: null,
        name,
        from: d0(b.from_date),
        to: d0(b.to_date),
        dueRequired: true,
      };
    }
  }
  return {
    field: "report_type",
    reason: "week | month | quarter | year | program | adhoc",
  };
}

// --- trạng thái ---
export interface Stored {
 reportFinal?: boolean;
  p: Period;
  submitted: Record<
    string,
    { attempt: number; decision: "" | "accepted" | "returned"; at: string }
  >;
  terminal: "" | "closed" | "cancelled";
}
const STORE: Stored[] = [];
/** W11.63: phiếu đã "Lưu nháp" ở máy chủ, khóa `${kỳ}:${phòng}` → số lần gửi đã thấy khi lưu (giả lập phiếu KHTC). */
export const KHTC_DRAFTS = new Map<string, number>();
/** W11.63: kho kỳ KHTC cho giả lập phiếu (kn-khtc-forms.ts). */
export function khtcMockStore(): Stored[] {
  seed();
  return STORE;
}
let TEMPLATE_VERSION = 1;

function project(s: Stored): Period {
  const now = Date.now();
  const overdue = now > Date.parse(s.p.due_at);
  let sent = 0;
  let any = false;
  const forms: Form[] = s.p.departments.map((d) => {
    const sub = s.submitted[d.code];
    let state: Form["state"];
    if (s.terminal === "cancelled") state = "withdrawn";
    else if (s.terminal === "closed")
      state = !sub
        ? "not_sent"
        : sub.decision === "accepted"
          ? "confirmed"
          : "locked";
    else if (!sub)
      state = overdue
        ? "late"
        : KHTC_DRAFTS.get(`${s.p.id}:${d.code}`) === 0
          ? "draft"
          : "not_started";
    else
      state =
        sub.decision === "accepted"
          ? "confirmed"
          : sub.decision === "returned"
            ? "supplement"
            : "submitted";
    if (sub) any = true;
    if (state === "submitted" || state === "confirmed") sent++;
    const limit = !!sub && sub.attempt >= MAX;
    return {
      department: d.code,
      department_name: d.name,
      state,
      state_label: FORM_LABEL[state],
      attempt: sub?.attempt ?? 0,
      limit_reached: limit,
      can_request_supplement: state === "submitted" && !limit,
      can_confirm: state === "submitted",
      last_submitted_at: sub?.at ?? null,
      reviewed_at: null,
      review_note:
        sub?.decision === "returned" ? "Bổ sung số liệu lớp tập huấn" : "",
      late: false,
    };
  });
  const complete = forms.length > 0 && sent === forms.length;
  const state: Period["state"] =
    s.terminal ||
    (complete ? "complete" : overdue ? "late" : any ? "collecting" : "opened");
  const lateEnough = now >= Date.parse(s.p.due_at) + LATE_DAYS * DAY;
  return {
    ...s.p,
    forms,
    state,
    state_label: PERIOD_LABEL[state],
    overdue,
    forms_status: s.terminal || "open",
    counts: {
      forms: forms.length,
      forms_sent: sent,
      sources: 0,
      sources_closed: 0,
    },
    can_close: state === "complete" || (state === "late" && lateEnough),
    close_forced: state === "late" && lateEnough,
    can_cancel: !s.reportFinal && ["opened", "collecting", "late", "complete"].includes(state),
  };
}

function seed() {
  if (STORE.length) return;
  const today = Date.now();
  const y = new Date(today).getUTCFullYear();
  const n = Math.max(1, Math.floor((today - week1(y)) / (7 * DAY)));
  const plan = planOf({ report_type: "week", year: y, no: n }) as Plan;
  const p = build(
    { report_type: "week", year: y, no: n },
    plan,
    DEPTS.map((d) => d.code),
  );
  STORE.push({
    p,
    terminal: "",
    submitted: {
      trong_trot: {
        attempt: 1,
        decision: "",
        at: new Date(today - 3_600_000).toISOString(),
      },
      chan_nuoi_thuy_san: {
        attempt: 2,
        decision: "returned",
        at: new Date(today - 7_200_000).toISOString(),
      },
    },
  });
}

function build(b: Input, plan: Plan, depts: string[]): Period {
  const next = plan.to + DAY;
  const due = b.due_at ?? new Date(next + 3 * 3_600_000).toISOString(); // 10:00 giờ VN
  const exp = b.export_due_at ?? new Date(next + 9 * 3_600_000).toISOString(); // 16:00 giờ VN
  return {
    id: uuidv7(),
    report_type: b.report_type,
    year: b.year ?? new Date(plan.to).getUTCFullYear(),
    no:
      b.report_type === "week" ||
      b.report_type === "month" ||
      b.report_type === "quarter"
        ? (b.no ?? null)
        : null,
    scope: b.report_type === "quarter" ? (b.scope ?? "quarter") : null,
    period_key: plan.key,
    name: plan.name,
    from_date: ds(plan.from),
    to_date: ds(plan.to),
    cutoff_date: b.cutoff_date ?? ds(plan.to),
    due_at: due,
    export_due_at: exp,
    max_submissions: MAX,
    departments: DEPTS.filter((d) => depts.includes(d.code)),
    forms_period_id: uuidv7(),
    forms_status: "open",
    template_code: "KHTC-PHIEU",
    period_rule: "kn.period_rule/v2;week_start=wed;week=year;month=week_end",
    state: "opened",
    state_label: "Đã mở",
    overdue: false,
    counts: { forms: 0, forms_sent: 0, sources: 0, sources_closed: 0 },
    forms: [],
    sources: [],
    events: [],
    can_close: false,
    close_forced: false,
    can_cancel: true,
    close_late_days: LATE_DAYS,
    opened_at: new Date().toISOString(),
    opened_by: uuidv7(),
  };
}

function writer(ctx: Ctx): Response | null {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (
    kc &&
    (kc.role !== "kn_tinh" || kc.department_code !== "ke_hoach_tai_chinh")
  ) {
    return problem(
      403,
      "forbidden",
      "chỉ Cấp Tỉnh Phòng Kế hoạch – Tài chính mở, sửa hạn, đóng, hủy kỳ",
    );
  }
  return null;
}

const find = (id: string) => STORE.find((s) => s.p.id === id);
const notFound = () =>
  problem(404, "extension.khtc_period_not_found", "Không tìm thấy kỳ KHTC");
const reasonOk = (r: unknown) =>
  typeof r === "string" && r.trim().length >= 10 && r.trim().length <= 500;

function draftOf(b: Input): Response {
  const plan = planOf(b);
  if ("field" in plan) return validation([plan]);
  const depts = b.departments?.length
    ? b.departments
    : DEPTS.map((d) => d.code);
  const p = build(b, plan, depts);
  const dup = plan.key
    ? STORE.find(
        (s) =>
          s.terminal !== "cancelled" &&
          s.p.report_type === b.report_type &&
          s.p.period_key === plan.key,
      )
    : undefined;
  return json({
    report_type: p.report_type,
    year: p.year,
    no: p.no,
    scope: p.scope,
    period_key: p.period_key,
    name: p.name,
    from_date: p.from_date,
    to_date: p.to_date,
    cutoff_date: p.cutoff_date,
    due_at: p.due_at,
    export_due_at: p.export_due_at,
    max_submissions: MAX,
    departments: p.departments,
    due_required: plan.dueRequired,
    plan_departments: false,
    departments_loaded: true,
    sources: [],
    sources_linked: false,
    duplicate: dup ? { id: dup.p.id, name: dup.p.name } : null,
    template_version: TEMPLATE_VERSION,
  } satisfies S["KHTCDraft"]);
}

export const khtcPeriodRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/weeks$/,
    handler: (ctx) => {
      const y = Number(ctx.url.searchParams.get("year"));
      if (!(y >= 2000 && y <= 2100))
        return validation([{ field: "year", reason: "năm 2000–2100" }]);
      const items = Array.from({ length: weeksInYear(y) }, (_, i) => {
        const from = week1(y) + i * 7 * DAY;
        return { no: i + 1, from_date: ds(from), to_date: ds(from + 6 * DAY) };
      });
      return json({ year: y, items });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/form-template$/,
    handler: () =>
      json({
        code: "KHTC-PHIEU",
        version: TEMPLATE_VERSION,
        title: "Báo cáo cho KHTC — phiếu cơ sở",
        installed: TEMPLATE_VERSION > 0,
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/khtc\/form-template\/install$/,
    handler: () => {
      TEMPLATE_VERSION ||= 1;
      return json({
        code: "KHTC-PHIEU",
        version: TEMPLATE_VERSION,
        title: "Báo cáo cho KHTC — phiếu cơ sở",
        installed: true,
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/periods$/,
    handler: (ctx) => {
      seed();
      const y = Number(ctx.url.searchParams.get("year") ?? 0);
      return json({
        items: STORE.filter((s) => !y || s.p.year === y)
          .map(project)
          .reverse(),
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/khtc\/periods\/preview$/,
    handler: (ctx) => writer(ctx) ?? draftOf(ctx.body as Input),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/khtc\/periods$/,
    handler: (ctx) => {
      const deny = writer(ctx);
      if (deny) return deny;
      const b = ctx.body as Input;
      const plan = planOf(b);
      if ("field" in plan) return validation([plan]);
      if (plan.dueRequired && !b.due_at)
        return validation([
          {
            field: "due_at",
            reason: "nhập hạn nộp theo văn bản yêu cầu / hạn chỉ đạo",
          },
        ]);
      if (
        plan.key &&
        STORE.some(
          (s) =>
            s.terminal !== "cancelled" &&
            s.p.report_type === b.report_type &&
            s.p.period_key === plan.key,
        )
      ) {
        return problem(
          409,
          "extension.khtc_period_duplicate",
          "Kỳ cùng loại, cùng năm, cùng số tuần/tháng/quý đã mở (chưa hủy)",
        );
      }
      const s: Stored = {
        p: build(
          b,
          plan,
          b.departments?.length ? b.departments : DEPTS.map((d) => d.code),
        ),
        submitted: {},
        terminal: "",
      };
      STORE.push(s);
      return json(project(s), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)$/,
    handler: (ctx) => {
      seed();
      const s = find(ctx.params[0]!);
      return s ? json(project(s)) : notFound();
    },
  },
  {
    method: "POST",
    pattern:
      /^\/v1\/extension\/khtc\/periods\/([^/]+)\/(deadline|close|cancel)$/,
    handler: (ctx) => {
      const deny = writer(ctx);
      if (deny) return deny;
      const s = find(ctx.params[0]!);
      if (!s) return notFound();
      const cur = project(s);
      if (s.terminal)
        return problem(
          409,
          "extension.khtc_period_terminal",
          "Kỳ đã đóng hoặc đã hủy",
        );
      const b = (ctx.body ?? {}) as {
        reason?: string;
        due_at?: string;
        export_due_at?: string;
      };
      const ev = {
        id: uuidv7(),
        forced: false,
        reason: null as string | null,
        due_at: null as string | null,
        export_due_at: null as string | null,
        state: cur.state as string | null,
        actor_ref: uuidv7(),
        at: new Date().toISOString(),
      };
      switch (ctx.params[1]) {
        case "deadline":
          if (!b.due_at)
            return validation([{ field: "due_at", reason: "bắt buộc" }]);
          s.p = {
            ...s.p,
            due_at: b.due_at,
            export_due_at:
              b.export_due_at ??
              (s.p.export_due_at < b.due_at ? b.due_at : s.p.export_due_at),
          };
          s.p.events = [
            ...s.p.events,
            {
              ...ev,
              action: "deadline_changed",
              due_at: s.p.due_at,
              export_due_at: s.p.export_due_at,
              state: null,
            },
          ];
          break;
        case "close":
          if (!cur.can_close)
            return problem(
              409,
              "extension.khtc_period_not_closable",
              "Kỳ chưa đóng được",
            );
          if (cur.close_forced && !reasonOk(b.reason))
            return validation([
              {
                field: "reason",
                reason:
                  "đóng kỳ khi còn thiếu dữ liệu: lý do bắt buộc, 10–500 ký tự",
              },
            ]);
          s.terminal = "closed";
          s.p.events = [
            ...s.p.events,
            {
              ...ev,
              action: "closed",
              forced: cur.close_forced,
              reason: reasonOk(b.reason) ? b.reason!.trim() : null,
            },
          ];
          break;
        default:
          if (s.reportFinal) return problem(409, "extension.khtc_report_final", "Báo cáo đã có Bản cuối");
          if (!reasonOk(b.reason))
            return validation([
              { field: "reason", reason: "bắt buộc, 10–500 ký tự" },
            ]);
          s.terminal = "cancelled";
          s.p.events = [
            ...s.p.events,
            { ...ev, action: "cancelled", reason: b.reason!.trim() },
          ];
      }
      return json(project(s));
    },
  },
  // W11.11 — "Báo cáo cho KHTC" phía phòng (FN-PN-03.01): phiếu của phòng theo kỳ (Cấp Tỉnh phòng của phiên, Admin ?department=).
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/department-forms$/,
    handler: (ctx) => departmentForms(ctx),
  },
];

type InboxRow = S["KHTCDepartmentForm"];
const TYPE_LABEL: Record<InboxRow["report_type"], string> = {
  week: "Báo cáo tuần",
  month: "Báo cáo tháng",
  quarter: "Báo cáo quý",
  year: "Báo cáo năm",
  program: "Báo cáo tiến độ các CT",
  adhoc: "Báo cáo phát sinh",
};
const ACTION_LABEL: Record<InboxRow["action"], string> = {
  enter: "Nhập",
  continue: "Tiếp tục",
  resubmit: "Sửa, gửi lại",
  view: "Xem",
};

/** Bản thu gọn của khtcperiod/app.DepartmentForms. */
function departmentForms(ctx: Ctx): Response {
  seed();
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const q = ctx.url.searchParams;
  const view = q.get("view") || "current";
  if (view !== "current" && view !== "sent")
    return validation([{ field: "view", reason: "current hoặc sent" }]);
  const admin = !kc || kc.role === "admin";
  let dept = q.get("department") ?? "";
  if (admin) {
    if (!/^[a-z][a-z0-9_]{0,39}$/.test(dept))
      return validation([
        { field: "department", reason: "Admin chọn phòng cần xem" },
      ]);
  } else if (
    kc.role !== "kn_tinh" ||
    !kc.department_code ||
    (dept && dept !== kc.department_code)
  ) {
    return problem(403, "forbidden", "chỉ xem phiếu KHTC của phòng mình");
  } else dept = kc.department_code;
  const readOnly = !!kc?.read_only;
  const items: InboxRow[] = [];
  for (const s of STORE) {
    if (s.terminal === "cancelled") continue;
    const p = project(s);
    const f = p.forms.find((x) => x.department === dept);
    if (!f) continue;
    const periodState: InboxRow["period_state"] =
      s.terminal === "closed" ? "closed" : p.overdue ? "late" : "opened";
    if (view === "current" ? periodState === "closed" : f.attempt === 0)
      continue;
    const needs = ["not_started", "late", "draft", "supplement"].includes(
      f.state,
    );
    const action: InboxRow["action"] = readOnly
      ? "view"
      : f.state === "not_started" || f.state === "late"
        ? "enter"
        : f.state === "draft"
          ? "continue"
          : f.state === "supplement"
            ? "resubmit"
            : "view";
    items.push({
      period_id: p.id,
      forms_period_id: p.forms_period_id,
      report_type: p.report_type as InboxRow["report_type"],
      report_type_label: TYPE_LABEL[p.report_type as InboxRow["report_type"]],
      year: p.year,
      name: p.name,
      from_date: p.from_date,
      to_date: p.to_date,
      due_at: p.due_at,
      period_state: periodState,
      period_state_label: PERIOD_LABEL[periodState],
      state: f.state as InboxRow["state"],
      state_label: f.state_label,
      attempt: f.attempt,
      max_submissions: p.max_submissions,
      limit_reached: f.limit_reached,
      late: f.late,
      last_submitted_at: f.last_submitted_at,
      reviewed_at: f.reviewed_at,
      supplement_note: f.state === "supplement" ? f.review_note : null,
      needs_action: needs,
      action,
      action_label: ACTION_LABEL[action],
      entry_route: `/kn/khtc/phieu/${p.id}${admin ? `?department=${dept}` : ""}`,
    });
  }
  items.sort((a, b) =>
    view === "current"
      ? Number(b.needs_action) - Number(a.needs_action) ||
        Date.parse(a.due_at) - Date.parse(b.due_at)
      : Date.parse(b.last_submitted_at ?? "") -
        Date.parse(a.last_submitted_at ?? ""),
  );
  const name = DEPTS.find((d) => d.code === dept)?.name ?? "";
  return json({
    department: dept,
    department_name: name,
    view: view as "current" | "sent",
    read_only: readOnly,
    counts: {
      total: items.length,
      needs_action: items.filter((i) => i.needs_action).length,
      supplement: items.filter((i) => i.state === "supplement").length,
      late: items.filter((i) => i.state === "late").length,
      sent: items.filter((i) => i.attempt > 0).length,
    },
    items,
  } satisfies S["KHTCDepartmentForms"]);
}
