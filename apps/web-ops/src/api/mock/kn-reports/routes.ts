// Định tuyến HTTP giả lập của vòng đời báo cáo KN v7.7 (application.yaml: /v1/extension/report-*, kn-periods,
// consolidations, kn-dashboard, report-badges, commune-reports; /v1/reports/dispatch*). Quy tắc ở store.ts.
import type { FormReportType } from "@/kn/forms/types";
import { mockAssignedForms } from "../forms/routes";
import { json, problem, type Ctx, type Route } from "../http";
import { reason as checkReason } from "./domain";
import * as st from "./store";

const UUID = "([0-9a-f-]{36})";
const re = (s: string) => new RegExp(`^${s.replaceAll("{id}", UUID)}$`);

type H = (ctx: Ctx, c: st.Caller) => Response;

/** Bọc: dữ liệu khởi đầu, việc nền (tự chốt, gửi email), ngữ cảnh phiên bắt buộc. */
const kn =
  (h: H) =>
  (ctx: Ctx): Response => {
    st.ensureSeeded();
    st.tick();
    const c = st.callerOf(ctx.request);
    return st.isErr(c) ? c : h(ctx, c);
  };

const q = (ctx: Ctx, k: string) => ctx.url.searchParams.get(k);
const out = <T>(r: st.Result<T>, f: (v: T) => Response): Response => (st.isErr(r) ? r : f(r));

const notFoundBatch = () => problem(404, "extension.report_batch_not_found", "Không tìm thấy đợt phát hành");
const notFoundTask = () => problem(404, "extension.report_task_not_found", "Không tìm thấy công việc");
const notFoundPeriod = () => problem(404, "extension.report_period_not_found", "Không tìm thấy kỳ báo cáo");

function batchFor(c: st.Caller, id: string | undefined): st.Batch | Response {
  const b = id ? st.batchOf(id) : undefined;
  return b && st.batchVisible(c, b) ? b : notFoundBatch();
}
function taskFor(c: st.Caller, id: string | undefined): st.Task | Response {
  const t = id ? st.taskOf(id) : undefined;
  return t && st.taskVisible(c, t) ? t : notFoundTask();
}
function periodFor(c: st.Caller, id: string | undefined): st.Period | Response {
  const p = id ? st.periodOf(id) : undefined;
  const b = p ? st.batchOf(p.batch_id) : undefined;
  return p && b && st.batchVisible(c, b) ? p : notFoundPeriod();
}
function reasonOf(body: unknown, max = 500): string | Response {
  const [r, ok] = checkReason(String((body as { reason?: unknown } | undefined)?.reason ?? ""), max);
  return ok ? r : st.fieldErr("reason", `bắt buộc, 10–${max} ký tự`);
}
const TINH_READ = ["kn_tinh", "kn_tw", "admin"] as const;

const taskList = kn((ctx, c) => {
  const status = q(ctx, "status");
  const valid = ["not_started", "draft", "submitted", "overdue", "adjustment_requested", "confirmed", "closed"];
  if (status && !valid.includes(status)) return st.fieldErr("status", valid.join(" | "));
  const commune = q(ctx, "commune");
  if (c.role === "kn_xa" && commune && !c.communes.includes(commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
  const r = st.listTasks(c, { commune, period_type: q(ctx, "period_type"), status, form_code: q(ctx, "form_code"), batch_id: q(ctx, "batch_id"), period_id: q(ctx, "period_id") });
  const limit = Number(q(ctx, "limit") ?? 0);
  const items = (limit > 0 ? r.items.slice(0, limit) : r.items).map((t) => st.taskWire(t));
  return json({ items, counts: r.counts, last_reminder: r.last_reminder ? st.reminderWire(r.last_reminder) : null });
});

export const reportCycleAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: re("/v1/extension/report-forms"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const rt = q(ctx, "report_type");
      if (!rt) return st.fieldErr("report_type", "bắt buộc");
      const dept = c.dept ?? st.AG_DEPARTMENT;
      const at = q(ctx, "from_date") ?? new Date(st.nowMs()).toISOString().slice(0, 10);
      const items = formsFor(dept, rt, at);
      return json({ items });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-batches/preview"),
    handler: kn((ctx, c) => st.writer(c, "kn_tinh") ?? out(st.planBatch(c, ctx.body), (p) => json(st.previewWire(p)))),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-batches"),
    handler: kn(
      (ctx, c) =>
        st.writer(c, "kn_tinh") ??
        out(st.planBatch(c, ctx.body), (p) =>
          out(st.issueBatch(c, p), (r) => json({ batch: r.batch, periods: r.periods, warnings: p.warnings, communes_without_officer: p.without_officer }, 201)),
        ),
    ),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-batches"),
    handler: kn((ctx, c) => st.reader(c, ...TINH_READ) ?? json({ items: st.listBatches(c, { province: q(ctx, "province"), report_type: q(ctx, "report_type") }), next_cursor: null })),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-batches/{id}"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      return b instanceof Response ? b : json({ batch: b, periods: st.periodsOf(b.id) });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-batches/{id}/log"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      return b instanceof Response ? b : json({ items: st.batchLog(b.id).map(st.logWire), next_cursor: null });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-batches/{id}/withdraw"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      if (b instanceof Response) return b;
      const r = reasonOf(ctx.body);
      return typeof r !== "string" ? r : out(st.withdrawBatch(c, b, r), (x) => json(x));
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-batches/{id}/deadline"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      if (b instanceof Response) return b;
      const r = reasonOf(ctx.body);
      return typeof r !== "string" ? r : out(st.changeDeadline(c, b, (ctx.body ?? {}) as Record<string, unknown>, r), (x) => json(x));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-batches/{id}/provincial-values"),
    handler: kn((ctx, c) => {
      const b = batchFor(c, ctx.params[0]);
      return b instanceof Response ? b : json({ items: st.provincialOf(b.id, q(ctx, "template_id"), c.role === "kn_xa" ? c.communes : null) });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-batches/{id}/status"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      if (b instanceof Response) return b;
      const pid = q(ctx, "period_id");
      const p = pid ? st.periodsOf(b.id).find((x) => x.id === pid) : st.defaultPeriod(b.id);
      if (!p) return notFoundPeriod();
      const board = st.statusBoard(b, p, { template_id: q(ctx, "template_id"), status: q(ctx, "status") });
      return json({ period: board.period, counts: board.counts, items: board.items });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-batches/{id}/reminders"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      if (b instanceof Response) return b;
      return out(st.remind(c, b, ctx.body), (r) => json({ ...r, reminder: { ...r.reminder, actor_display: "" } }, 201));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-batches/{id}/reminders"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const b = batchFor(c, ctx.params[0]);
      return b instanceof Response ? b : json({ items: st.remindersOf(b, q(ctx, "period_id")).map(st.reminderWire) });
    }),
  },
  { method: "GET", pattern: re("/v1/extension/report-tasks"), handler: taskList },
  { method: "GET", pattern: re("/v1/extension/commune-reports"), handler: taskList },
  {
    method: "GET",
    pattern: re("/v1/extension/commune-reports/stats"),
    handler: kn((ctx, c) => {
      const commune = q(ctx, "commune");
      if (!commune) return st.fieldErr("commune", "bắt buộc");
      if (c.role === "kn_xa" && !c.communes.includes(commune)) return problem(403, "extension.out_of_scope", "Ngoài phạm vi phụ trách");
      return json(st.communeStats(c, commune, q(ctx, "batch_id")));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-tasks/{id}"),
    handler: kn((ctx, c) => {
      const t = taskFor(c, ctx.params[0]);
      return t instanceof Response ? t : json(st.taskDetail(t));
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-tasks/{id}/submissions"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_xa");
      if (deny) return deny;
      const t = taskFor(c, ctx.params[0]);
      if (t instanceof Response) return t;
      return out(st.submit(c, t, ctx.body), (r) => json({ submission: st.subWire(r.submission), task: st.taskWire(t), issues: r.issues }, r.status));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-tasks/{id}/history"),
    handler: kn((ctx, c) => {
      const t = taskFor(c, ctx.params[0]);
      return t instanceof Response ? t : json({ items: [...t.subs].reverse().map(st.subWire) });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-tasks/{id}/log"),
    handler: kn((ctx, c) => {
      const t = taskFor(c, ctx.params[0]);
      return t instanceof Response ? t : json({ items: st.taskLog(t).map(st.logWire), next_cursor: null });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-tasks/{id}/adjustment-request"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const t = taskFor(c, ctx.params[0]);
      return t instanceof Response ? t : out(st.requestAdjustment(c, t, ctx.body), (x) => json({ task: st.taskWire(x) }));
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-tasks/{id}/confirm"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const t = taskFor(c, ctx.params[0]);
      return t instanceof Response ? t : out(st.confirmTask(c, t, ctx.body), (x) => json({ task: st.taskWire(x) }));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/kn-periods"),
    handler: kn((ctx, c) => {
      const items = st.listPeriods(c, { province: q(ctx, "province"), report_type: q(ctx, "report_type"), batch_id: q(ctx, "batch_id") });
      const limit = Number(q(ctx, "limit") ?? 0);
      return json({ items: limit > 0 ? items.slice(0, limit) : items });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-periods/{id}"),
    handler: kn((ctx, c) => {
      const p = periodFor(c, ctx.params[0]);
      return p instanceof Response ? p : json(st.periodView(c, p));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/report-periods/{id}/log"),
    handler: kn((ctx, c) => {
      const p = periodFor(c, ctx.params[0]);
      return p instanceof Response ? p : json({ items: st.periodLog(p.id).map(st.logWire), next_cursor: null });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-periods/{id}/close"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const p = periodFor(c, ctx.params[0]);
      return p instanceof Response ? p : out(st.closePeriod(c, p), () => json(st.periodView(c, p)));
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/report-periods/{id}/reopen"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const p = periodFor(c, ctx.params[0]);
      return p instanceof Response ? p : out(st.reopenPeriod(c, p, ctx.body), (r) => json({ ...st.periodView(c, p), ...r }));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/consolidations"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const pid = q(ctx, "period_id");
      if (!pid) return st.fieldErr("period_id", "bắt buộc");
      const p = periodFor(c, pid);
      if (p instanceof Response) return p;
      return out(
        st.consolidationView(p, { template_id: q(ctx, "template_id"), form_code: q(ctx, "form_code"), run_id: q(ctx, "run_id"), region: q(ctx, "region"), compare: q(ctx, "compare") === "previous_year" }),
        (v) => json(v),
      );
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/extension/consolidations"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const x = (ctx.body ?? {}) as { period_id?: string; template_ids?: string[] };
      const p = periodFor(c, x.period_id);
      if (p instanceof Response) return p;
      const r = st.consolidate(p, Array.isArray(x.template_ids) ? x.template_ids : [], "manual", c.actor, c.support?.onBehalfOf ?? null);
      return json({ runs: r.runs.map(st.runWire), up_to_date: r.up_to_date, message: r.runs.length ? "" : "Dữ liệu đã mới nhất" }, r.runs.length ? 201 : 200);
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/consolidations/pending-changes"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const p = periodFor(c, q(ctx, "period_id") ?? undefined);
      return p instanceof Response ? p : json(st.pendingOf(p, null));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/consolidations/runs"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const p = periodFor(c, q(ctx, "period_id") ?? undefined);
      return p instanceof Response ? p : json({ items: st.runsOf(p.id, q(ctx, "template_id")).map(st.runWire) });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/consolidations/{id}/changes"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const run = st.runOf(ctx.params[0] ?? "");
      const p = run ? periodFor(c, run.period_id) : undefined;
      if (!run || !p || p instanceof Response) return problem(404, "extension.consolidation_not_found", "Kỳ chưa có dữ liệu tổng hợp");
      const items = st.changesOf(run.id).map((ch) => ({ ...ch, changed_by_display: ch.kind === "computed" ? "(công thức)" : st.actorDisplay(ch.changed_by) }));
      return json({ run: st.runWire(run), items, next_cursor: null });
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/extension/kn-dashboard"),
    handler: kn((ctx, c) => st.reader(c, ...TINH_READ) ?? out(st.dashboard(c, { province: q(ctx, "province"), region: q(ctx, "region"), period_id: q(ctx, "period_id"), batch_id: q(ctx, "batch_id") }), (d) => json(d))),
  },
  { method: "GET", pattern: re("/v1/extension/report-badges"), handler: kn((_ctx, c) => json({ counts: st.badgesFor(c) })) },

  // --- pf/reports: báo cáo Chi cục ---
  {
    method: "GET",
    pattern: re("/v1/reports/dispatch-recipients"),
    handler: kn((ctx, c) => {
      const deny = st.reader(c, ...TINH_READ);
      if (deny) return deny;
      const province = c.province ?? q(ctx, "province");
      if (!province) return st.fieldErr("province", "bắt buộc");
      return json({ items: st.recipientsOf(c, province).map((r) => st.recipientWire(r, c.role !== "kn_tinh")) });
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/reports/dispatch-recipients"),
    handler: kn((ctx, c) => st.writer(c, "kn_tinh") ?? out(st.addRecipient(c, ctx.body), (r) => json(st.recipientWire(r, false), 201))),
  },
  {
    method: "POST",
    pattern: re("/v1/reports/dispatch-recipients/{id}/deactivate"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const r = st.recipientOf(ctx.params[0] ?? "");
      if (!r || !r.active) return problem(404, "reports.dispatch_recipient_not_found", "Không tìm thấy người nhận");
      const why = reasonOf(ctx.body);
      if (typeof why !== "string") return why;
      r.active = false;
      return json(st.recipientWire(r, false));
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/reports/dispatches"),
    handler: kn((ctx, c) => st.writer(c, "kn_tinh") ?? out(st.createDispatch(c, ctx.body), (d) => json(st.dispatchWire(d), 201))),
  },
  {
    method: "GET",
    pattern: re("/v1/reports/dispatches"),
    handler: kn((ctx, c) => st.reader(c, ...TINH_READ) ?? json({ items: st.dispatchesOf(c, { period_id: q(ctx, "period_id"), province: q(ctx, "province") }).map((d) => st.dispatchWire(d).dispatch), next_cursor: null })),
  },
  {
    method: "GET",
    pattern: re("/v1/reports/dispatches/{id}"),
    handler: kn((ctx, c) => {
      const d = st.dispatchOf(ctx.params[0] ?? "");
      if (!d || !st.dispatchesOf(c, {}).includes(d)) return problem(404, "reports.dispatch_not_found", "Không tìm thấy lần xuất/gửi báo cáo Chi cục");
      return json(st.dispatchWire(d));
    }),
  },
  {
    method: "POST",
    pattern: re("/v1/reports/dispatches/{id}/send"),
    handler: kn((ctx, c) => {
      const deny = st.writer(c, "kn_tinh");
      if (deny) return deny;
      const d = st.dispatchOf(ctx.params[0] ?? "");
      if (!d || !st.dispatchesOf(c, {}).includes(d)) return problem(404, "reports.dispatch_not_found", "Không tìm thấy lần xuất/gửi báo cáo Chi cục");
      return out(st.sendDispatch(c, d, ctx.body), (x) => json(st.dispatchWire(x), 202));
    }),
  },
  {
    method: "GET",
    pattern: re("/v1/reports/dispatches/{id}/files/{id}/download"),
    handler: kn((ctx, c) => {
      const d = st.dispatchOf(ctx.params[0] ?? "");
      const f = d?.files.find((x) => x.id === ctx.params[1]);
      if (!d || !f || !st.dispatchesOf(c, {}).includes(d)) return problem(404, "reports.dispatch_not_found", "Không tìm thấy lần xuất/gửi báo cáo Chi cục");
      return json({ url: st.downloadUrl(d, f), expires_in_seconds: 600 });
    }),
  },
];

function formsFor(dept: string, rt: string, at: string) {
  return mockAssignedForms(dept, "commune", rt as FormReportType, at).map((f) => ({
    template_id: f.template.id,
    version_id: f.version.id,
    code: f.template.code,
    name: f.template.name,
    version: f.version.version,
    report_types: f.reportTypes,
    period_block: f.definition.period_blocks.length > 0,
    completeness: f.definition.completeness,
    provincial_columns: f.definition.columns.filter((c) => c.kind === "provincial").map((c) => ({ letter: c.letter, title: c.title, unit: c.unit ?? "" })),
  }));
}
