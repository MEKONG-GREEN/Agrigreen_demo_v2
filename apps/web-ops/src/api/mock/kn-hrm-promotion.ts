// ============================================================================
// BACKEND GIẢ LẬP — Nâng bậc lương (W11.25, dom/hrm, BRD HCTH v3.0 FN-09, FN-10). Đường dẫn, tham số, hình dạng phản hồi
// theo ĐÚNG contracts/openapi/application.yaml (/v1/hrm/promotion*, /v1/hrm/staff/{id}/ratings). Quy tắc mô phỏng RÚT GỌN
// từ internal/kn/hrm/domain/promotion.go:
//   - Kỳ NLTX = mốc xếp bậc + thời gian giữ bậc của ngạch; bậc cuối → TNVK (lần đầu sau 36 tháng, 5 %, +1 %/12 tháng);
//     NLTH chỉ khi đề xuất từ thành tích (12 tháng cho bằng khen, 9 tháng cho giấy khen); chỉ tiêu = ⌊10 % × viên chức⌋.
//   - Điều kiện: xếp loại 3 năm trước năm đến kỳ (KHTNV → Chưa đủ, thiếu → Cần xem xét).
//   - Quyết định ghi sự kiện "step" vào hồ sơ giả lập (kn-hrm-staff); hủy = dòng hủy (bậc trở lại). Tệp là tệp giả.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { hrmScaleAt } from "./kn-hrm";
import { hrmStaffInternals as H } from "./kn-hrm-staff";

type S = components["schemas"];
type Case = S["HrmPromoCase"];
type Kind = S["HrmPromoKind"];
type Status = S["HrmPromoStatus"];
type Cond = S["HrmPromoCond"];
type Staff = ReturnType<typeof H.staff>[number];
type Ev = Staff["events"][number];

const RULES = { min: "HTNV", years: 3, ach: 5, tStart: 5, tStep: 1, tCycle: 12, tFirst: 36, quotaPct: 10 };
const CONV: Record<string, number> = { bang_khen_bo: 12, bang_khen_ubnd_tinh: 12, cstd_tinh: 12, cstd_co_so: 9, giay_khen_so: 9 };
const RANK = ["HTXSNV", "HTTNV", "HTNV", "KHTNV"];

interface PEvent { id: string; key: string; staff: string; kind: Kind; action: string; reason: string; award?: string; export_id?: string; decision_id?: string; at: string }
interface Export extends Omit<S["HrmPromoExportSummary"], "decision_no"> { items: (S["HrmPromoExportItem"] & { award_event_id?: string })[] }
interface Dec { d: Omit<S["HrmDecision"], "status" | "cancel_reason">; cancel?: string; steps: Record<string, string>; awards: string[] }
interface Store { ratings: S["HrmAnnualRating"][]; events: PEvent[]; exports: Export[]; decs: Dec[]; scans: number }
let ST: Store = { ratings: [], events: [], exports: [], decs: [], scans: 0 };
const uuid = () => crypto.randomUUID();
const now = () => new Date().toISOString();
const vnToday = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);

/** Đặt lại dữ liệu nâng bậc giả lập (kiểm thử). */
export function resetHrmPromotion(): void {
  ST = { ratings: [], events: [], exports: [], decs: [], scans: 0 };
  const vc1 = H.staff().find((s) => s.code === "VC-00001");
  const y = Number(vnToday().slice(0, 4));
  if (vc1) for (let i = 5; i >= 1; i--) ST.ratings.push(rating(vc1.id, y - i + 1, i % 2 ? "HTNV" : "HTTNV"));
}

function rating(staff: string, year: number, r: string, reason = "", sup: string | null = null): S["HrmAnnualRating"] & { staff: string } {
  return { id: uuid(), staff, year, rating: r, source: "conversion", void: false, supersedes_id: sup, reason, actor: "01900000-0000-7000-8000-00000000a001", recorded_at: now() } as S["HrmAnnualRating"] & { staff: string };
}

function addMonths(iso: string, m: number): string {
  const [y, mo, d] = iso.split("-").map(Number) as [number, number, number];
  const first = new Date(Date.UTC(y, mo - 1 + m, 1));
  const last = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0)).getUTCDate();
  first.setUTCDate(Math.min(d, last));
  return first.toISOString().slice(0, 10);
}
const fmt = (iso: string) => `${iso.slice(8, 10)}/${iso.slice(5, 7)}/${iso.slice(0, 4)}`;

function ratingsOf(staff: string): Record<number, string> {
  const out: Record<number, string> = {};
  const dead = new Set(ST.ratings.map((r) => r.supersedes_id).filter(Boolean));
  for (const r of ST.ratings as (S["HrmAnnualRating"] & { staff: string })[]) if (r.staff === staff && !r.void && !dead.has(r.id)) out[r.year] = r.rating;
  return out;
}

function condRatings(staff: string, y: number): S["HrmPromoBasis"] {
  const rs = ratingsOf(staff);
  const years = Array.from({ length: RULES.years }, (_, i) => y - RULES.years + i);
  const bad = years.filter((x) => rs[x] && RANK.indexOf(rs[x]!) > RANK.indexOf(RULES.min)).map((x) => `${rs[x]} ${x}`);
  const miss = years.filter((x) => !rs[x]);
  return {
    label: `Xếp loại ${years.join(" · ")}`,
    value: years.map((x) => rs[x] ?? "—").join(" · "),
    result: bad.length ? "fail" : miss.length ? "review" : "ok",
    note: bad.length ? bad.join(", ") : miss.length ? `thiếu xếp loại ${miss.join(", ")}` : "",
  };
}

function caseState(key: string): PEvent | undefined {
  return [...ST.events].reverse().find((e) => e.key === key);
}
function statusOf(key: string, kind: Kind): { s: Status; reason: string; note: string; gone: boolean } {
  const e = caseState(key);
  if (!e) return { s: kind === "nlth" ? "listed" : "pending", reason: "", note: "", gone: false };
  const map: Record<string, Status> = { include: "listed", propose: "listed", exclude: kind === "nlth" ? "listed" : "pending", export: "exported", undecide: "exported", decide: "decided", return: kind === "nlth" ? "listed" : "pending" };
  const inc = [...ST.events].reverse().find((x) => x.key === key && (x.action === "include" || x.action === "propose"));
  return { s: map[e.action] ?? "pending", reason: inc?.reason ?? "", note: e.action === "return" ? e.reason : "", gone: e.action === "withdraw" };
}

function tnvkPrev(staff: string): { pct: number; since: string } | null {
  let best: { pct: number; since: string } | null = null;
  for (const x of ST.decs) if (!x.cancel && x.d.kind === "tnvk") for (const p of x.d.people) if (p.staff_id === staff && (!best || p.effective_on > best.since)) best = { pct: Number(p.pct_to), since: p.effective_on };
  return best;
}
const usedAwards = () => new Set(ST.decs.filter((x) => !x.cancel && x.d.kind === "nlth").flatMap((x) => x.awards));

function blank(st: Staff, snap: S["HrmSnapshot"], kind: Kind): Case {
  return {
    key: "", staff_id: st.id, code: st.code, name: st.profile.full_name, unit: snap.unit, unit_name: H.units().find((u) => u.code === snap.unit)?.name ?? snap.unit,
    kind, form: kind === "nltx" ? "mau1" : kind === "nlth" ? "mau2" : "mau4", due: "", grade: snap.grade, grade_name: snap.grade, steps: snap.steps,
    from_step: snap.step, to_step: snap.step, from_coeff: snap.coefficient, to_coeff: snap.coefficient, diff: "0.00", step_since: snap.step_since || null,
    reserved: snap.reserved_coeff, next_due: null, normal_due: null, early_months: 0, pct_from: null, pct_to: null, pct_since: null, cond: "ok", why: [],
    basis: [], planned_year: 0, blocked: [], status: "pending", reason: "", note: "", months_enjoyed: "1", decision_id: null, decision_no: "", award: null,
  };
}

function compute(day: string, proposals = true, extraAwards?: Map<string, string[]>): { cands: Case[]; activeVC: number; missing: S["HrmPersonBrief"][] } {
  const cands: Case[] = [];
  const missing: S["HrmPersonBrief"][] = [];
  let activeVC = 0;
  const used = usedAwards();
  for (const st of H.staff()) {
    const snap = H.project(st.events, day);
    if (st.type !== "vien_chuc" || snap.status !== "active") continue;
    activeVC++;
    const sc = snap.grade ? hrmScaleAt(snap.grade, day) : null;
    if (!sc || !snap.step || !snap.step_since) {
      missing.push({ id: st.id, code: st.code, type: st.type, name: st.profile.full_name, unit: snap.unit, unit_name: snap.unit, position: snap.position });
      continue;
    }
    const since = snap.step_since.length === 4 ? `${snap.step_since}-01-01` : snap.step_since.length === 7 ? `${snap.step_since}-01` : snap.step_since;
    const coef = (n: number) => sc.coefficients[n - 1] ?? "";
    const moc = { label: "Mốc xếp bậc hiện tại", value: fmt(since), result: "ok" as Cond, note: "" };
    const finish = (c: Case, y: number) => {
      const r = condRatings(st.id, y);
      c.basis.push(r);
      if (snap.disciplined) c.basis.push({ label: "Kỷ luật", value: "Đang thi hành kỷ luật", result: "fail", note: "đang thi hành kỷ luật" });
      for (const b of c.basis) if (b.result !== "ok") { c.cond = c.cond === "fail" || b.result === "fail" ? "fail" : "review"; if (b.note) c.why.push(b.note); }
      const s = statusOf(c.key, c.kind);
      Object.assign(c, { status: s.s, reason: s.reason, note: s.note });
      if (!s.gone) cands.push(c);
    };
    if (snap.step < sc.steps) {
      const due = addMonths(since, sc.hold_months);
      const c = blank(st, snap, "nltx");
      Object.assign(c, { key: `${st.id}:nltx:b${snap.step + 1}-${due.replaceAll("-", "")}`, due, next_due: due, to_step: snap.step + 1, to_coeff: coef(snap.step + 1),
        diff: (Number(coef(snap.step + 1)) - Number(coef(snap.step))).toFixed(2), months_enjoyed: Number(due.slice(8)) <= 15 ? "1" : "0.5" });
      c.basis.push(moc, { label: "Thời gian giữ bậc", value: `${sc.hold_months} tháng (${fmt(since)} → ${fmt(due)})`, result: "ok", note: "" });
      finish(c, Number(due.slice(0, 4)));
      const awardIds = new Set([
        ...(proposals ? ST.events.filter((e) => e.staff === st.id && e.action === "propose" && statusOf(e.key, "nlth").s !== "decided" && !statusOf(e.key, "nlth").gone).map((e) => e.award!) : []),
        ...(extraAwards?.get(st.id) ?? []),
      ]);
      for (const ev of st.events.filter((e: Ev) => e.kind === "award" && !e.void && awardIds.has(e.id))) {
        const m = CONV[ev.payload.form ?? ""] ?? 0;
        const early = addMonths(due, -m);
        const n = blank(st, snap, "nlth");
        const label = `${ev.payload.title || ev.payload.form} ${ev.valid_from.slice(0, 4)}`.replace(/ (\d{4}) \1$/, " $1");
        Object.assign(n, { key: `${st.id}:nlth:${ev.id}`, due: early, normal_due: due, next_due: early, early_months: m, to_step: snap.step + 1, to_coeff: coef(snap.step + 1),
          diff: (Number(coef(snap.step + 1)) - Number(coef(snap.step))).toFixed(2), award: { event_id: ev.id, form: ev.payload.form ?? "", label, year: Number(ev.valid_from.slice(0, 4)), months: m, used: used.has(ev.id) } });
        n.basis.push(moc, { label: `Thành tích ${RULES.ach} năm gần nhất`, value: `${label} (quy đổi ${m} tháng)`, result: used.has(ev.id) || !m ? "fail" : "ok", note: used.has(ev.id) ? "thành tích đã dùng" : !m ? "thành tích không thuộc bảng quy đổi" : "" });
        if (early.slice(0, 4) !== day.slice(0, 4)) { n.planned_year = Number(early.slice(0, 4)); n.blocked.push(`ngày nâng ${fmt(early)} ngoài năm chỉ tiêu ${day.slice(0, 4)}`); }
        if (used.has(ev.id)) n.blocked.push("thành tích đã dùng");
        finish(n, Number(early.slice(0, 4)));
      }
    } else {
      const prev = tnvkPrev(st.id);
      const due = prev ? addMonths(prev.since, RULES.tCycle) : addMonths(since, RULES.tFirst);
      const to = prev ? prev.pct + RULES.tStep : RULES.tStart;
      const c = blank(st, snap, "tnvk");
      Object.assign(c, { key: `${st.id}:tnvk:v-${due.replaceAll("-", "")}`, due, next_due: addMonths(due, RULES.tCycle), pct_from: prev ? String(prev.pct) : null, pct_to: String(to),
        pct_since: prev?.since ?? null, diff: ((Number(snap.coefficient) * (to - (prev?.pct ?? 0))) / 100).toFixed(2) });
      c.basis.push(moc, { label: "Ngạch, bậc", value: `${snap.grade} · bậc cuối ${snap.step}/${sc.steps}`, result: "ok", note: "" });
      finish(c, Number(due.slice(0, 4)));
    }
  }
  return { cands, activeVC, missing };
}

function caller(ctx: Ctx, write = false) {
  const c = H.caller(ctx);
  if (c instanceof Response) return c;
  if ((c.m.export ?? "none") !== "all") return problem(403, "forbidden", "nâng bậc: chỉ Cấp Tỉnh – HCTH, Admin (FN-09, FN-10)");
  if (write && (c.readOnly || (c.m["profile.edit"] ?? "none") !== "all")) return problem(403, "forbidden", "nâng bậc: cần quyền sửa hồ sơ toàn Trung tâm");
  return c;
}
const body = <T>(ctx: Ctx) => (ctx.body ?? {}) as T;
function pevent(c: { ref: string }, e: Omit<PEvent, "id" | "at">) {
  ST.events.push({ ...e, id: uuid(), at: now() });
  void c;
}
function decided(day: string, from: string, to: string): Case[] {
  const out: Case[] = [];
  for (const x of ST.decs) {
    if (x.cancel) continue;
    for (const p of x.d.people) {
      if (p.effective_on < from || p.effective_on > to) continue;
      const st = H.staff().find((s) => s.id === p.staff_id);
      if (!st) continue;
      const c = blank(st, H.project(st.events, day), x.d.kind);
      Object.assign(c, { key: p.case_key || `${p.staff_id}:${x.d.kind}:${p.effective_on.replaceAll("-", "")}`, due: p.effective_on, from_step: p.from_step, to_step: p.to_step,
        from_coeff: p.from_coeff, to_coeff: p.to_coeff, pct_from: p.pct_from || null, pct_to: p.pct_to || null, status: "decided", decision_id: x.d.id, decision_no: x.d.decision_no });
      out.push(c);
    }
  }
  return out;
}
function quota(day: string, cands: Case[], activeVC: number): S["HrmPromoQuota"] {
  const y = day.slice(0, 4);
  return {
    year: Number(y),
    total: Math.floor((activeVC * RULES.quotaPct) / 100),
    used: ST.decs.filter((x) => !x.cancel && x.d.kind === "nlth").flatMap((x) => x.d.people).filter((p) => p.effective_on.startsWith(y)).length,
    pending: cands.filter((c) => c.kind === "nlth" && !c.planned_year && c.status !== "decided").length,
  };
}
const xlsx = (name: string, extra: Record<string, string> = {}) =>
  new Response(new Blob([`Tệp giả lập ${name}`]), {
    status: 200,
    headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Cache-Control": "no-store", "Content-Disposition": `attachment; filename="${name}"`, ...extra },
  });

function decisionOut(x: Dec, full: boolean): S["HrmDecision"] {
  return { ...x.d, status: x.cancel ? "cancelled" : "applied", cancel_reason: x.cancel ?? null, ...(full ? {} : { history: undefined }) } as S["HrmDecision"];
}

export const hrmPromotionRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const day = ctx.url.searchParams.get("at") || vnToday();
      const w = [1, 3, 6, 12].includes(Number(ctx.url.searchParams.get("window"))) ? Number(ctx.url.searchParams.get("window")) : 3;
      const end = addMonths(day, w);
      const { cands, activeVC, missing } = compute(day);
      const items = [...cands.filter((x) => x.due <= end && x.status !== "decided"), ...decided(day, addMonths(day, -3), end)].sort((a, b) => a.due.localeCompare(b.due) || a.code.localeCompare(b.code));
      const by = (f: (x: Case) => string) => items.reduce<Record<string, number>>((m, x) => ({ ...m, [f(x)]: (m[f(x)] ?? 0) + 1 }), {});
      const out: S["HrmPromotionList"] = {
        as_of: day, window: w, items, missing, last_scan: ST.scans ? { day, counts: { due_3: items.length }, recorded_at: now() } : null,
        summary: { total: items.length, nltx: items.filter((x) => x.kind === "nltx").length, tnvk: items.filter((x) => x.kind === "tnvk").length, nlth: items.filter((x) => x.kind === "nlth").length,
          by_status: { pending: 0, listed: 0, exported: 0, decided: 0, ...by((x) => x.status) }, by_month: by((x) => x.due.slice(0, 7)), quota: quota(day, cands, activeVC) },
        rules: { min_rating: RULES.min, rating_years: RULES.years, achievement_years: RULES.ach, tnvk_start_pct: String(RULES.tStart), tnvk_step_pct: String(RULES.tStep),
          tnvk_cycle_months: RULES.tCycle, tnvk_first_months: RULES.tFirst, nlth_quota_pct: String(RULES.quotaPct), remind_months: [1, 3, 6] },
      };
      return json(out, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/cases\/([^/]+)$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const key = decodeURIComponent(ctx.params[0]!);
      const day = vnToday();
      const x = compute(day).cands.find((k) => k.key === key) ?? decided(day, "1900-01-01", "2200-01-01").find((k) => k.key === key);
      if (!x) return problem(404, "hrm.promotion_case_not_found", "Không có kỳ nâng bậc này (đã xử lý hoặc dữ liệu hồ sơ đã đổi)");
      const st = H.staff().find((s) => s.id === x.staff_id)!;
      const out: S["HrmPromoCaseDetail"] = {
        ...x,
        history: ST.events.filter((e) => e.key === key).map((e) => ({ id: e.id, action: e.action as S["HrmPromoEvent"]["action"], due_on: x.due, export_id: e.export_id ?? null, decision_id: e.decision_id ?? null, reason: e.reason, actor: c.ref, recorded_at: e.at })),
        ratings: (ST.ratings as (S["HrmAnnualRating"] & { staff: string })[]).filter((r) => r.staff === x.staff_id),
        awards: st.events.filter((e) => e.kind === "award" && !e.void).map((e) => ({ event_id: e.id, form: e.payload.form ?? "", label: e.payload.title || e.payload.form || "", year: Number(e.valid_from.slice(0, 4)), months: CONV[e.payload.form ?? ""] ?? 0, used: usedAwards().has(e.id) })),
      };
      return json(out, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/cases\/(include|exclude)$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const b = body<S["HrmPromoKeysInput"]>(ctx);
      const inc = ctx.params[0] === "include";
      const cands = compute(vnToday()).cands;
      let n = 0;
      for (const k of b.keys ?? []) {
        const x = cands.find((y) => y.key === k);
        if (!x) return problem(404, "hrm.promotion_case_not_found", "Không có kỳ nâng bậc này");
        if (x.kind === "nlth") return problem(409, "hrm.promotion_conflict", "Kỳ trước hạn được lập từ thành tích");
        if (inc && x.status === "pending") {
          if (x.cond !== "ok" && !b.reason?.trim()) return validation([{ field: "reason", reason: "thêm người chưa đủ điều kiện hoặc cần xem xét phải có lý do (FN-09)" }]);
          pevent(c, { key: k, staff: x.staff_id, kind: x.kind, action: "include", reason: x.cond === "ok" ? "" : b.reason!.trim() });
          n++;
        } else if (!inc) {
          if (x.status !== "listed") return problem(409, "hrm.promotion_conflict", "Chỉ bỏ được kỳ đang ở trạng thái Đã đưa vào danh sách");
          pevent(c, { key: k, staff: x.staff_id, kind: x.kind, action: "exclude", reason: "" });
          n++;
        }
      }
      return json({ changed: n });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/nlth-pool$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const day = vnToday();
      const end = addMonths(day, 3);
      const base = compute(day);
      const due = new Set(base.cands.filter((x) => x.kind === "nltx" && x.due <= end && x.status !== "decided").map((x) => x.staff_id));
      const open = new Set(base.cands.filter((x) => x.kind === "nlth" && (x.status === "listed" || x.status === "exported")).map((x) => x.staff_id));
      const extra = new Map<string, string[]>();
      for (const st of H.staff()) extra.set(st.id, st.events.filter((e) => e.kind === "award" && !e.void && !usedAwards().has(e.id) && Number(e.valid_from.slice(0, 4)) >= Number(day.slice(0, 4)) - RULES.ach).map((e) => e.id));
      const all = compute(day, false, extra).cands;
      const rows: S["HrmNLTHPool"]["rows"] = [];
      let excluded = 0;
      for (const st of H.staff()) {
        const snap = H.project(st.events, day);
        if (st.type !== "vien_chuc" || snap.status !== "active" || open.has(st.id)) continue;
        if (due.has(st.id)) { excluded++; continue; }
        const opts = all.filter((x) => x.staff_id === st.id && x.kind === "nlth");
        const nltx = all.find((x) => x.staff_id === st.id && x.kind === "nltx");
        rows.push({ staff_id: st.id, code: st.code, name: st.profile.full_name, unit_name: snap.unit, step: snap.step, steps: snap.steps, normal_due: nltx?.due ?? null,
          block: !snap.grade ? "Chưa có ngạch, bậc hoặc bảng hệ số" : snap.step >= snap.steps ? "Đang ở bậc cuối (xét thâm niên vượt khung)" : opts.length ? "" : "Chưa có thành tích ghi trong hồ sơ",
          options: opts.map((x) => ({ award: x.award!, key: x.key, due: x.due, early_months: x.early_months, cond: x.cond, why: x.why, planned_year: x.planned_year })) });
      }
      return json({ as_of: day, excluded, rows, quota: quota(day, base.cands, base.activeVC) } satisfies S["HrmNLTHPool"], 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/proposals$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const b = body<{ items?: { staff_id: string; award_event_id: string }[]; reason?: string }>(ctx);
      const day = vnToday();
      const base = compute(day);
      const extra = new Map((b.items ?? []).map((i) => [i.staff_id, [i.award_event_id]]));
      const xs = compute(day, false, extra).cands.filter((x) => x.kind === "nlth" && (b.items ?? []).some((i) => i.award_event_id === x.award?.event_id));
      if (!xs.length) return validation([{ field: "items", reason: "chọn 1–500 người" }]);
      const q = quota(day, base.cands, base.activeVC);
      const add = xs.filter((x) => !x.planned_year).length;
      if (add && q.used + q.pending + add > q.total) return problem(422, "hrm.nlth_quota_exceeded", "Vượt chỉ tiêu nâng bậc trước thời hạn của năm", { detail: `năm ${q.year}: ${q.used + q.pending + add}/${q.total} người` });
      if (xs.some((x) => x.cond !== "ok") && !b.reason?.trim()) return validation([{ field: "reason", reason: "người chưa đủ điều kiện phải có lý do" }]);
      for (const x of xs) pevent(c, { key: x.key, staff: x.staff_id, kind: "nlth", action: "propose", reason: x.cond === "ok" ? "" : b.reason!.trim(), award: x.award!.event_id });
      return json({ keys: xs.map((x) => x.key) }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/proposals\/withdraw$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const keys = body<S["HrmPromoKeysInput"]>(ctx).keys ?? [];
      for (const k of keys) {
        if (statusOf(k, "nlth").s !== "listed") return problem(409, "hrm.promotion_conflict", "Chỉ gỡ được kỳ trước hạn chưa xuất danh sách");
        pevent(c, { key: k, staff: k.slice(0, 36), kind: "nlth", action: "withdraw", reason: "" });
      }
      return json({ changed: keys.length });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/exports$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const items: S["HrmPromoExportSummary"][] = ST.exports.map((x) => ({
        id: x.id, form: x.form, review_month: x.review_month, as_of: x.as_of, count: x.count, file_name: x.file_name, sha256: x.sha256, byte_size: x.byte_size,
        actor: x.actor, recorded_at: x.recorded_at, signer_title: x.signer_title, preparer_id: x.preparer_id,
        decision_no: ST.decs.find((d) => !d.cancel && d.d.export_id === x.id)?.d.decision_no ?? "",
      }));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/exports$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const b = body<S["HrmPromoExportInput"]>(ctx);
      if (!/^\d{4}-\d{2}$/.test(b.review_month ?? "")) return validation([{ field: "review_month", reason: "YYYY-MM" }]);
      const day = vnToday();
      const { cands, activeVC } = compute(day);
      const sel = (b.keys ?? []).map((k) => cands.find((x) => x.key === k));
      if (sel.some((x) => !x)) return problem(404, "hrm.promotion_case_not_found", "Không có kỳ nâng bậc này");
      const nlth = sel.filter((x) => x!.kind === "nlth");
      const blocked = nlth.flatMap((x) => x!.blocked.map((r) => `${x!.code}: ${r}`));
      const q = quota(day, cands, activeVC);
      if (nlth.length && q.used + nlth.length > q.total) blocked.push(`vượt chỉ tiêu nâng bậc trước thời hạn năm ${q.year}: ${q.used + nlth.length}/${q.total}`);
      if (blocked.length) return problem(422, "hrm.promotion_export_blocked", "Danh sách không xuất được", { detail: `Không xuất được Mẫu số 2: ${blocked.join("; ")}` });
      const out: S["HrmPromoExported"][] = [];
      for (const form of ["mau1", "mau2", "mau4"] as const) {
        const rows = sel.filter((x) => x!.form === form) as Case[];
        if (!rows.length) continue;
        const id = uuid();
        const file_name = `MauSo${form.slice(3)}_T${b.review_month.slice(5)}_${b.review_month.slice(0, 4)}.xlsx`;
        ST.exports.unshift({ id, form, review_month: b.review_month, as_of: day, count: rows.length, file_name, sha256: "0".repeat(64), byte_size: 4096, actor: c.ref, recorded_at: now(),
          signer_title: b.signer_title ?? "Giám đốc", preparer_id: "",
          items: rows.map((x) => ({ case_key: x.key, staff_id: x.staff_id, code: x.code, name: x.name, kind: x.kind, due: x.due, unit_name: x.unit_name, grade: x.grade, grade_name: x.grade_name,
            from_step: x.from_step, to_step: x.to_step, from_coeff: x.from_coeff, to_coeff: x.to_coeff, diff: x.diff, pct_from: b.tnvk_pct?.[x.key] ?? x.pct_from ?? "", pct_to: x.pct_to ?? "",
            next_due: x.next_due ?? "", months_enjoyed: x.months_enjoyed, early_months: x.early_months, award_label: x.award?.label ?? "", note: b.notes?.[x.key] ?? "", reason: x.reason,
            status: "exported", ...(x.award ? { award_event_id: x.award.event_id } : {}) })) });
        for (const x of rows) pevent(c, { key: x.key, staff: x.staff_id, kind: x.kind, action: "export", reason: "", export_id: id, ...(x.award ? { award: x.award.event_id } : {}) });
        out.push({ id, form, file_name, sha256: "0".repeat(64), byte_size: 4096, count: rows.length, download: `/v1/hrm/promotion/exports/${id}/file` });
      }
      return json({ items: out }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/exports\/([^/]+)(\/file)?$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const x = ST.exports.find((e) => e.id === ctx.params[0]);
      if (!x) return problem(404, "hrm.promotion_export_not_found", "Không có lần xuất danh sách nâng bậc này");
      if (ctx.params[1]) return xlsx(x.file_name, { "X-Export-Drift": "false", "X-Export-Sha256": x.sha256 });
      const items = x.items.map((i) => ({ ...i, status: statusOf(i.case_key, i.kind).s }));
      return json({ ...x, items, decision_no: ST.decs.find((d) => !d.cancel && d.d.export_id === x.id)?.d.decision_no ?? "" }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/decision-book$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const y = ctx.url.searchParams.get("year");
      return xlsx(y ? `SoQuyetDinhNangBac_${y}.xlsx` : "SoQuyetDinhNangBac.xlsx");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/decisions$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const p = ctx.url.searchParams;
      const q = (p.get("q") ?? "").toLowerCase();
      const items = [...ST.decs]
        .reverse()
        .filter((x) => (!p.get("year") || x.d.decided_on.startsWith(p.get("year")!)) && (!p.get("kind") || x.d.kind === p.get("kind")) && (!p.get("issuer") || x.d.issuer === p.get("issuer")))
        .filter((x) => !q || x.d.decision_no.toLowerCase().includes(q) || x.d.people.some((pp) => pp.name.toLowerCase().includes(q)))
        .map((x) => decisionOut(x, false));
      return json({ items }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/decisions$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const b = body<S["HrmDecisionInput"]>(ctx);
      if (!b.decision_no?.trim() || !b.decided_on || !b.issuer || !b.people?.length)
        return validation([{ field: "decision_no", reason: "nhập số, ngày quyết định, cơ quan và người có tên" }]);
      if (!b.acknowledge_warnings && ST.decs.some((x) => !x.cancel && x.d.decision_no.toLowerCase() === b.decision_no.trim().toLowerCase()))
        return problem(409, "hrm.decision_warnings", "Quyết định có cảnh báo — gửi lại với acknowledge_warnings = true để lưu", { detail: `Số quyết định ${b.decision_no} đã có trong sổ` });
      const exp = b.export_id ? ST.exports.find((e) => e.id === b.export_id) : undefined;
      if (b.export_id && !exp) return problem(404, "hrm.promotion_export_not_found", "Không có lần xuất danh sách nâng bậc này");
      const kind: Kind = exp ? (exp.form === "mau1" ? "nltx" : exp.form === "mau2" ? "nlth" : "tnvk") : (b.kind ?? "nltx");
      const id = uuid();
      const dec: Dec = { d: { id, decision_no: b.decision_no.trim(), decided_on: b.decided_on, issuer: b.issuer, kind, export_id: b.export_id ?? null, source: exp?.file_name ?? "Nhập từ quyết định đã ban hành",
        count: b.people.length, doc_id: null, recorded_at: now(), actor: c.ref, people: [], history: [{ id: uuid(), action: "issue", doc_id: null, reason: "", actor: c.ref, recorded_at: now() }] }, steps: {}, awards: [] };
      for (const p of b.people) {
        const it = exp?.items.find((i) => i.case_key === p.case_key);
        const st = H.staff().find((s) => s.id === (it?.staff_id ?? p.staff_id));
        if (!st) return validation([{ field: "people", reason: "người không có trong danh sách đã xuất" }]);
        const eff = p.effective_on ?? it?.due ?? b.decided_on;
        const snap = H.project(st.events, eff);
        const sc = hrmScaleAt(snap.grade, eff);
        const to = it ? it.to_step : (p.to_step ?? snap.step);
        if (kind !== "tnvk" && (!sc || to < 1 || to > sc.steps)) return problem(422, "hrm.step_out_of_range", "Bậc vượt số bậc của ngạch hoặc ngạch chưa có bảng hệ số");
        dec.d.people.push({ staff_id: st.id, name: st.profile.full_name, case_key: it?.case_key ?? "", effective_on: eff, grade: snap.grade, from_step: snap.step, to_step: to,
          from_coeff: snap.coefficient, to_coeff: sc?.coefficients[to - 1] ?? snap.coefficient, pct_from: it?.pct_from ?? "", pct_to: it?.pct_to ?? p.pct_to ?? "" });
        if (kind !== "tnvk") {
          const ev = { ...st.events[st.events.length - 1]!, id: uuid(), kind: "step" as Ev["kind"], valid_from: eff, precision: "day" as const, valid_to: null, void: false, supersedes: null,
            reason: "", payload: { step: String(to), step_since: eff, decision_no: dec.d.decision_no }, version: st.events.length + 1000, recorded_at: now() };
          (st.events as Ev[]).push(ev);
          dec.steps[st.id] = ev.id;
        }
        if (it?.award_event_id) dec.awards.push(it.award_event_id);
        if (it) pevent(c, { key: it.case_key, staff: st.id, kind, action: "decide", reason: "", decision_id: id });
      }
      let returned = 0;
      for (const i of exp?.items ?? []) if (!b.people.some((p) => p.case_key === i.case_key)) { pevent(c, { key: i.case_key, staff: i.staff_id, kind: i.kind, action: "return", reason: `Không có tên trong quyết định ${dec.d.decision_no}`, decision_id: id }); returned++; }
      ST.decs.push(dec);
      return json({ id, updated: kind === "tnvk" ? 0 : dec.d.people.length, returned, warnings: [] }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/promotion\/decisions\/([^/]+)$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const x = ST.decs.find((d) => d.d.id === ctx.params[0]);
      return x ? json(decisionOut(x, true), 200, { "Cache-Control": "no-store" }) : problem(404, "hrm.decision_not_found", "Không có quyết định nâng bậc này");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/promotion\/decisions\/([^/]+)\/(cancel|file)$/,
    handler: (ctx) => {
      const c = caller(ctx, true);
      if (c instanceof Response) return c;
      const x = ST.decs.find((d) => d.d.id === ctx.params[0]);
      if (!x) return problem(404, "hrm.decision_not_found", "Không có quyết định nâng bậc này");
      if (x.cancel) return problem(409, "hrm.decision_cancelled", "Quyết định đã bị hủy");
      if (ctx.params[1] === "file") {
        x.d.doc_id = uuid();
        x.d.history!.push({ id: uuid(), action: "attach", doc_id: x.d.doc_id, reason: "", actor: c.ref, recorded_at: now() });
        return json({ doc_id: x.d.doc_id }, 201);
      }
      const reason = body<{ reason?: string }>(ctx).reason?.trim() ?? "";
      if (reason.length < 10) return validation([{ field: "reason", reason: "hủy quyết định cần lý do 10–500 ký tự (BR-03)" }]);
      for (const [staff, evId] of Object.entries(x.steps)) {
        const st = H.staff().find((s) => s.id === staff);
        const ev = st?.events.find((e) => e.id === evId);
        if (st && ev) (st.events as Ev[]).push({ ...ev, id: uuid(), void: true, supersedes: ev.id, reason, version: ev.version + 1, recorded_at: now() });
      }
      x.cancel = reason;
      x.d.history!.push({ id: uuid(), action: "cancel", doc_id: null, reason, actor: c.ref, recorded_at: now() });
      for (const p of x.d.people) if (p.case_key) pevent(c, { key: p.case_key, staff: p.staff_id, kind: x.d.kind, action: "undecide", reason, decision_id: x.d.id });
      return json({ id: x.d.id, status: "cancelled" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/ratings$/,
    handler: (ctx) => {
      const c = H.caller(ctx);
      if (c instanceof Response) return c;
      return json({ items: (ST.ratings as (S["HrmAnnualRating"] & { staff: string })[]).filter((r) => r.staff === ctx.params[0]) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/ratings$/,
    handler: (ctx) => {
      const c = H.caller(ctx);
      if (c instanceof Response) return c;
      if ((c.m["profile.edit"] ?? "none") === "none" || c.readOnly) return problem(403, "forbidden", "hồ sơ nhân sự: không có quyền profile.edit (FN-20)");
      const b = body<{ year: number; rating: string; source?: string; reason?: string }>(ctx);
      if (!(b.year >= 1990 && b.year <= 2100)) return validation([{ field: "year", reason: "năm 1990–2100" }]);
      const staff = ctx.params[0]!;
      const cur = (ST.ratings as (S["HrmAnnualRating"] & { staff: string })[]).filter((r) => r.staff === staff && r.year === b.year).pop();
      if (cur && (b.reason ?? "").trim().length < 10) return validation([{ field: "reason", reason: "sửa/hủy xếp loại đã có cần lý do 10–500 ký tự" }]);
      const r = rating(staff, b.year, b.rating, cur ? b.reason!.trim() : "", cur?.id ?? null);
      ST.ratings.push(r);
      return json(r, 201);
    },
  },
];

resetHrmPromotion();
