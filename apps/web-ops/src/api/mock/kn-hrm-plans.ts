// ============================================================================
// BACKEND GIẢ LẬP — Kế hoạch nhân lực năm (W11.27, dom/hrm, BRD HCTH v3.0 FN-11, D3 H7). Đường dẫn, tham số, hình dạng
// phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/hrm/plans*). Quy tắc mô phỏng RÚT GỌN từ
// internal/kn/hrm/app/plan.go, domain/plan.go:
//   - Vai: Admin, Cấp Tỉnh – HCTH = cấp Trung tâm (mở kỳ, tổng hợp, rà soát, chốt, điều chỉnh sau chốt); lãnh đạo đơn vị
//     (Cấp Tỉnh, manager) lập/gửi kế hoạch của đơn vị nội bộ của phiên (lãnh đạo HCTH: cả hai).
//   - Số hiện có, dự kiến nghỉ hưu tự tính từ hồ sơ giả lập (kn-hrm-staff) tại ngày chốt; nhu cầu theo kế hoạch =
//     hiện có − nghỉ hưu − nghỉ/chuyển khác + cần bổ sung.
//   - Gửi lại không giới hạn tới khi chốt (khóa lạc quan base_version); rà soát bốn mắt; sau chốt chỉ điều chỉnh có lý do.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { hrmStaffInternals as H } from "./kn-hrm-staff";

type S = components["schemas"];
type Input = S["HrmPlanInput"];
type Values = S["HrmPlanValues"];
type Row = S["HrmPlanRow"];

interface Sub {
  id: string;
  version: number;
  values: Values;
  submitted_at: string;
  late: boolean;
  submitter: string;
  decision: "" | "accepted" | "returned";
  review_note: string;
  reviewed_at: string | null;
}
interface Plan {
  period: S["HrmPlanPeriod"];
  subs: Record<string, Sub[]>; // mới trước
  drafts: Record<string, { values: Input; recorded_at: string }>;
  adjs: Record<string, S["HrmPlanAdjustment"][]>;
}

const HCTH = "hanh_chinh_tong_hop";
const ACTOR = "01900000-0000-7000-8000-00000000a001";
const RET_MALE = 61 * 12 + 6; // tham số promotion (mẫu — Q-070)
const RET_FEMALE = 57 * 12;
const now = () => new Date().toISOString();
const vnToday = () =>
  new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const uuid = () => crypto.randomUUID();

let PLANS: Plan[] = [];

/** Đặt lại dữ liệu giả lập (kiểm thử). */
export function resetHrmPlans(): void {
  PLANS = [];
}

interface Caller {
  center: boolean;
  unit: string; // đơn vị lập (lãnh đạo đơn vị)
  ref: string;
  readOnly: boolean;
}

function caller(ctx: Ctx): Caller | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!kc)
    return problem(
      403,
      "forbidden",
      "kế hoạch nhân lực: cần ngữ cảnh phiên Khuyến nông",
    );
  const unit =
    kc.role === "kn_tinh" && kc.manager
      ? kc.internal_unit_code || kc.department_code || ""
      : "";
  const center =
    kc.role === "admin" ||
    (kc.role === "kn_tinh" && kc.department_code === HCTH && !kc.read_only);
  if (!center && !unit)
    return problem(
      403,
      "forbidden",
      "kế hoạch nhân lực: chỉ Phòng HCTH, Admin và lãnh đạo các đơn vị (FN-11)",
    );
  return {
    center,
    unit,
    // Giả lập một người mỗi ngữ cảnh phiên (demo đổi vai bằng ngữ cảnh) — để rà soát bốn mắt chạy được.
    ref: `${ctx.persona.me.actor_ref || ACTOR}:${ctx.request.headers.get("X-AG-Context") ?? ""}`,
    readOnly: kc.role === "kn_tw" || !!kc.read_only,
  };
}

function ageMonths(birth: string, at: string): number {
  const [by, bm, bd] = birth.split("-").map(Number) as [number, number, number];
  const [y, m, d] = at.split("-").map(Number) as [number, number, number];
  let r = (y - by) * 12 + (m - bm);
  if (d < bd) r--;
  return r;
}

function addMonths(birth: string, n: number): string {
  const [y, m, d] = birth.split("-").map(Number) as [number, number, number];
  const t = new Date(Date.UTC(y, m - 1 + n, d));
  return t.toISOString().slice(0, 10);
}

function figures(cutoff: string, year: number, unit: string) {
  const out = {
    available: true,
    current: 0,
    retiring: 0,
    unknown: 0,
    retirees: [] as S["HrmPlanUnit"]["figures"]["retirees"],
  };
  for (const st of H.staff()) {
    if (st.type === "can_bo_xa") continue;
    const snap = H.project(st.events, cutoff);
    if (snap.status !== "active" || snap.unit !== unit) continue;
    out.current++;
    const b = /^\d{4}-\d{2}-\d{2}$/.test(st.profile.birth_date)
      ? st.profile.birth_date
      : "";
    const lim =
      st.profile.gender === "nam"
        ? RET_MALE
        : st.profile.gender === "nu"
          ? RET_FEMALE
          : 0;
    if (!b || !lim) {
      out.unknown++;
      continue;
    }
    if (ageMonths(b, `${year}-12-31`) >= lim) {
      out.retiring++;
      out.retirees.push({
        staff_id: st.id,
        code: st.code,
        full_name: st.profile.full_name,
        retire_on: addMonths(b, lim),
      });
    }
  }
  return out;
}

const demand = (v: Omit<Values, "demand">) =>
  v.current - v.retiring - v.leaving + v.additional;

function withDemand(v: Omit<Values, "demand">): Values {
  return { ...v, positions: v.positions ?? [], note: v.note ?? "", demand: demand(v) };
}

function find(year: string): Plan | undefined {
  return PLANS.find((p) => String(p.period.year) === year);
}

function closed(p: Plan) {
  return p.period.status === "closed" || p.period.status === "cancelled";
}

function rowOf(p: Plan, unit: string, names: Map<string, string>): Row {
  const subs = p.subs[unit] ?? [];
  const last = subs[0];
  const adj = p.adjs[unit] ?? [];
  const d = p.drafts[unit];
  const draftAfter =
    !!d &&
    (!last || d.recorded_at > last.submitted_at) &&
    (!last?.reviewed_at || d.recorded_at > last.reviewed_at);
  let status: Row["status"];
  if (closed(p)) status = last ? "closed" : "missed";
  else if (!last) status = draftAfter ? "draft" : "not_started";
  else if (last.decision === "returned") status = draftAfter ? "draft" : "returned";
  else if (last.decision === "accepted") status = "accepted";
  else status = "submitted";
  let values: Values;
  let source: Row["source"] = "auto";
  if (adj.length) {
    values = adj[adj.length - 1]!.values;
    source = "adjusted";
  } else if (last) {
    values = last.values;
    source = "submitted";
  } else {
    const f = figures(p.period.cutoff_on, p.period.year, unit);
    values = withDemand({
      current: f.current,
      retiring: f.retiring,
      leaving: 0,
      additional: 0,
      positions: [],
      note: "",
    });
  }
  return {
    unit,
    unit_name: names.get(unit) ?? unit,
    status,
    late: last?.late ?? false,
    versions: subs.length,
    decision: last?.decision ?? "",
    review_note: last?.review_note ?? "",
    submitted_at: last?.submitted_at ?? null,
    values,
    source,
    adjusted: adj.length > 0,
  };
}

function submitted(p: Plan) {
  return p.period.units.filter((u) => (p.subs[u] ?? []).length > 0).length;
}

function summary(p: Plan, c: Caller): S["HrmPlanSummary"] {
  const names = new Map(H.units().map((u) => [u.code, u.name]));
  const units = c.center ? p.period.units : p.period.units.filter((u) => u === c.unit);
  const rows = units.map((u) => rowOf(p, u, names));
  const t = { current: 0, retiring: 0, leaving: 0, additional: 0, demand: 0 };
  for (const r of rows) {
    t.current += r.values.current;
    t.retiring += r.values.retiring;
    t.leaving += r.values.leaving;
    t.additional += r.values.additional;
    t.demand += r.values.demand;
  }
  return {
    period: { ...p.period, submitted: submitted(p) },
    rows,
    totals: t,
    figures_available: true,
    can_manage: c.center && !c.readOnly,
    my_unit: c.unit,
  };
}

function normInput(b: unknown): Input | Response {
  const x = (b ?? {}) as Partial<Input>;
  const errs: { field: string; reason: string }[] = [];
  const okN = (n: unknown) =>
    typeof n === "number" && Number.isInteger(n) && n >= 0 && n <= 9999;
  if (!okN(x.leaving)) errs.push({ field: "leaving", reason: "số nguyên 0–9999" });
  if (!okN(x.additional))
    errs.push({ field: "additional", reason: "số nguyên 0–9999" });
  const pos = (x.positions ?? []).map((p) => ({
    title: String(p.title ?? "").trim(),
    level: String(p.level ?? "").trim(),
    count: Number(p.count),
    when: String(p.when ?? "").trim(),
    reason: String(p.reason ?? "").trim(),
  }));
  if (pos.length > 10) errs.push({ field: "positions", reason: "tối đa 10 vị trí" });
  pos.forEach((p, i) => {
    if (!p.title) errs.push({ field: `positions[${i}].title`, reason: "vị trí bắt buộc" });
    if (!(p.count >= 1 && p.count <= 9999))
      errs.push({ field: `positions[${i}].count`, reason: "số lượng 1–9999" });
  });
  if ((x.note ?? "").length > 500) errs.push({ field: "note", reason: "tối đa 500 ký tự" });
  if (errs.length) return validation(errs);
  return {
    leaving: x.leaving!,
    additional: x.additional!,
    positions: pos,
    note: (x.note ?? "").trim(),
  };
}

const NOT_FOUND = () =>
  problem(404, "hrm.plan_not_found", "Chưa mở kỳ kế hoạch nhân lực của năm này");
const UNIT_NF = () =>
  problem(404, "hrm.plan_unit", "Đơn vị không thuộc kỳ kế hoạch hoặc ngoài phạm vi");
const CENTER_ONLY = () =>
  problem(
    403,
    "forbidden",
    "kế hoạch nhân lực: chỉ Phòng HCTH và Admin mở kỳ, tổng hợp, rà soát, chốt (FN-11)",
  );
const UNIT_ONLY = () =>
  problem(
    403,
    "forbidden",
    "kế hoạch nhân lực: chỉ lãnh đạo đơn vị lập, gửi kế hoạch của đơn vị mình (FN-11)",
  );

function load(ctx: Ctx, needUnit = false): { c: Caller; p: Plan; unit: string } | Response {
  const c = caller(ctx);
  if (c instanceof Response) return c;
  const p = find(ctx.params[0]!);
  if (!p || (!c.center && !p.period.units.includes(c.unit))) return NOT_FOUND();
  const unit = ctx.params[1] ?? "";
  if (needUnit && (!p.period.units.includes(unit) || (!c.center && unit !== c.unit)))
    return UNIT_NF();
  return { c, p, unit };
}

export const hrmPlanRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/plans$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      return json({
        items: PLANS.filter((p) => c.center || p.period.units.includes(c.unit))
          .sort((a, b) => b.period.year - a.period.year)
          .map((p) => ({ ...p.period, submitted: submitted(p) })),
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/plans$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (!c.center || c.readOnly) return CENTER_ONLY();
      const b = (ctx.body ?? {}) as {
        year?: number;
        due_on?: string;
        cutoff_on?: string;
        units?: string[];
      };
      if (!b.year || b.year < 2020 || b.year > 2100)
        return validation([{ field: "year", reason: "năm 2020–2100" }]);
      if (!b.due_on) return validation([{ field: "due_on", reason: "bắt buộc" }]);
      const cutoff = b.cutoff_on || vnToday();
      if (b.due_on < cutoff)
        return validation([
          { field: "due_on", reason: "hạn gửi không trước ngày chốt số liệu" },
        ]);
      const active = H.units().filter((u) => u.active).map((u) => u.code);
      const units = b.units?.length ? b.units : active;
      const bad = units.find((u) => !active.includes(u));
      if (bad)
        return validation([
          { field: "units", reason: `đơn vị nội bộ không có hoặc đã ngừng: ${bad}` },
        ]);
      if (find(String(b.year)))
        return problem(409, "hrm.plan_exists", "Kỳ kế hoạch nhân lực của năm này đã mở");
      const p: Plan = {
        period: {
          year: b.year,
          label: `Kế hoạch nhân lực năm ${b.year}`,
          period_ref: uuid(),
          status: "open",
          due_at: `${b.due_on}T16:59:59Z`,
          cutoff_on: cutoff,
          units,
          submitted: 0,
          opened_at: now(),
        },
        subs: {},
        drafts: {},
        adjs: {},
      };
      PLANS.push(p);
      return json(p.period, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})$/,
    handler: (ctx) => {
      const l = load(ctx);
      if (l instanceof Response) return l;
      return json(summary(l.p, l.c));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/export$/,
    handler: (ctx) => {
      const l = load(ctx);
      if (l instanceof Response) return l;
      if (!l.c.center) return CENTER_ONLY();
      const s = summary(l.p, l.c);
      const csv = [
        "Đơn vị,Hiện có,Dự kiến nghỉ hưu,Dự kiến nghỉ chuyển khác,Cần bổ sung,Nhu cầu theo kế hoạch",
        ...s.rows.map(
          (r) =>
            `${r.unit_name},${r.values.current},${r.values.retiring},${r.values.leaving},${r.values.additional},${r.values.demand}`,
        ),
      ].join("\n");
      const name = `ke-hoach-nhan-luc-${l.p.period.year}.xlsx`;
      return new Response(new Blob([`Tệp giả lập ${name}\n${csv}`]), {
        status: 200,
        headers: {
          "Content-Type":
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
          "Cache-Control": "no-store",
          "Content-Disposition": `attachment; filename="${name}"`,
        },
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/close$/,
    handler: (ctx) => {
      const l = load(ctx);
      if (l instanceof Response) return l;
      if (!l.c.center || l.c.readOnly) return CENTER_ONLY();
      if (closed(l.p))
        return problem(409, "hrm.plan_closed", "Kế hoạch đã chốt — chỉ điều chỉnh có lý do");
      l.p.period.status = "closed";
      return json(summary(l.p, l.c));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/units\/([a-z][a-z0-9_]*)$/,
    handler: (ctx) => {
      const l = load(ctx, true);
      if (l instanceof Response) return l;
      const { c, p, unit } = l;
      const names = new Map(H.units().map((u) => [u.code, u.name]));
      const row = rowOf(p, unit, names);
      const fig = figures(p.period.cutoff_on, p.period.year, unit);
      if (!c.center) fig.retirees = fig.retirees.map((r) => ({ ...r }));
      const open = !closed(p);
      const d = p.drafts[unit];
      const body: S["HrmPlanUnit"] = {
        period: { ...p.period, submitted: submitted(p) },
        row,
        figures: fig,
        draft: d ? { values: d.values, recorded_at: d.recorded_at } : null,
        submissions: (p.subs[unit] ?? []).map((s) => ({
          id: s.id,
          version: s.version,
          values: s.values,
          submitted_at: s.submitted_at,
          late: s.late,
          decision: s.decision,
          review_note: s.review_note,
          reviewed_at: s.reviewed_at,
        })),
        adjustments: p.adjs[unit] ?? [],
        can_edit: open && c.unit === unit && !c.readOnly && row.decision !== "accepted",
        can_review: open && c.center && !c.readOnly && row.versions > 0 && row.decision === "",
        can_adjust: p.period.status === "closed" && c.center && !c.readOnly,
      };
      return json(body);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/units\/([a-z][a-z0-9_]*)\/previous$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const unit = ctx.params[1]!;
      if (!c.center && c.unit !== unit) return UNIT_NF();
      const prev = find(String(Number(ctx.params[0]) - 1));
      const subs = prev?.subs[unit] ?? [];
      const adj = prev?.adjs[unit] ?? [];
      const v = adj.length ? adj[adj.length - 1]!.values : subs[0]?.values;
      if (!prev || !v)
        return problem(
          404,
          "hrm.plan_no_previous",
          "Năm trước chưa có kế hoạch của đơn vị để sao chép",
        );
      return json({
        from_year: prev.period.year,
        values: {
          leaving: v.leaving,
          additional: v.additional,
          positions: v.positions ?? [],
          note: v.note ?? "",
        },
      });
    },
  },
  {
    method: "PUT",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/units\/([a-z][a-z0-9_]*)\/draft$/,
    handler: (ctx) => {
      const l = load(ctx, true);
      if (l instanceof Response) return l;
      if (l.c.unit !== l.unit || l.c.readOnly) return UNIT_ONLY();
      if (closed(l.p))
        return problem(409, "hrm.plan_closed", "Kế hoạch đã chốt — chỉ điều chỉnh có lý do");
      const v = normInput(ctx.body);
      if (v instanceof Response) return v;
      const d = { values: v, recorded_at: now() };
      l.p.drafts[l.unit] = d;
      return json(d);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/units\/([a-z][a-z0-9_]*)\/submit$/,
    handler: (ctx) => {
      const l = load(ctx, true);
      if (l instanceof Response) return l;
      const { c, p, unit } = l;
      if (c.unit !== unit || c.readOnly) return UNIT_ONLY();
      if (closed(p))
        return problem(409, "forms.period_closed", "Kỳ đã chốt — mở lại kỳ trước khi nộp hoặc chốt lại");
      const v = normInput(ctx.body);
      if (v instanceof Response) return v;
      const subs = (p.subs[unit] ??= []);
      if (subs[0]?.decision === "accepted")
        return problem(409, "forms.submission_accepted", "Bài nộp đã được chấp nhận — mở lại kỳ để nộp lại");
      const base = Number((ctx.body as { base_version?: number })?.base_version ?? 0);
      if (base !== subs.length)
        return problem(
          409,
          "forms.submission_conflict",
          "Đã có người cập nhật báo cáo — tải bản mới rồi sửa lại (không ghi đè)",
        );
      const f = figures(p.period.cutoff_on, p.period.year, unit);
      const s: Sub = {
        id: uuid(),
        version: subs.length + 1,
        values: withDemand({ current: f.current, retiring: f.retiring, ...v }),
        submitted_at: now(),
        late: Date.now() > Date.parse(p.period.due_at),
        submitter: c.ref,
        decision: "",
        review_note: "",
        reviewed_at: null,
      };
      subs.unshift(s);
      return json(
        { id: s.id, version: s.version, values: s.values, submitted_at: s.submitted_at, late: s.late },
        201,
      );
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/units\/([a-z][a-z0-9_]*)\/review$/,
    handler: (ctx) => {
      const l = load(ctx, true);
      if (l instanceof Response) return l;
      const { c, p, unit } = l;
      if (!c.center || c.readOnly) return CENTER_ONLY();
      const b = (ctx.body ?? {}) as { decision?: string; note?: string };
      if (b.decision !== "accepted" && b.decision !== "returned")
        return validation([{ field: "decision", reason: "accepted | returned" }]);
      if (b.decision === "returned" && !(b.note ?? "").trim())
        return validation([{ field: "note", reason: "yêu cầu điều chỉnh phải ghi chú" }]);
      const s = p.subs[unit]?.[0];
      if (!s) return problem(404, "forms.submission_not_found", "Không tìm thấy bài nộp");
      if (s.decision)
        return problem(409, "forms.already_reviewed", "Phiên bản bài nộp đã được rà soát");
      if (s.submitter === c.ref)
        return problem(403, "self_approval_forbidden", "Không được phê duyệt thay đổi do chính mình đặt");
      s.decision = b.decision;
      s.review_note = (b.note ?? "").trim();
      s.reviewed_at = now();
      return json({
        id: s.id,
        version: s.version,
        decision: s.decision,
        review_note: s.review_note,
        reviewed_at: s.reviewed_at,
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/plans\/(\d{4})\/units\/([a-z][a-z0-9_]*)\/adjustments$/,
    handler: (ctx) => {
      const l = load(ctx, true);
      if (l instanceof Response) return l;
      const { c, p, unit } = l;
      if (!c.center || c.readOnly) return CENTER_ONLY();
      if (p.period.status !== "closed")
        return problem(409, "hrm.plan_not_closed", "Kế hoạch chưa chốt — sửa bằng cách gửi lại, không điều chỉnh");
      const reason = String((ctx.body as { reason?: string })?.reason ?? "").trim();
      if (reason.length < 10 || reason.length > 500)
        return validation([{ field: "reason", reason: "lý do điều chỉnh 10–500 ký tự" }]);
      const v = normInput(ctx.body);
      if (v instanceof Response) return v;
      const base = rowOf(p, unit, new Map()).values;
      const list = (p.adjs[unit] ??= []);
      const a: S["HrmPlanAdjustment"] = {
        seq: list.length + 1,
        values: withDemand({ current: base.current, retiring: base.retiring, ...v }),
        reason,
        actor_ref: c.ref,
        recorded_at: now(),
      };
      list.push(a);
      return json(a, 201);
    },
  },
];
