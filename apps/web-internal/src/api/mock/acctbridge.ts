// Backend giả lập mkg/acctbridge — theo contracts/openapi/mkg-internal.yaml (tag acctbridge) và
// docs/dev/acctbridge-adapter.md: bảng ánh xạ có phiên bản (đề xuất ≠ duyệt, R-56), bút toán TỔNG HỢP theo kỳ dựng
// tất định từ kết quả đóng kỳ costing (Σ Nợ = Σ Có), duyệt (≠ người dựng) → job đẩy → pushed → kỳ posted; đẩy lỗi
// vĩnh viễn → push_failed (DLQ) → đẩy lại; tệp xuất CSV (CRLF, ';') / XML (LF) cùng byte, header X-Content-SHA256.
import type { AcctbridgeJob, AcctbridgeJournal, AcctbridgeJournalLine, AcctbridgeMapping, AcctbridgeMappingRule, AcctbridgePushAttempt, CostingPeriod } from "../types";
import { cents, journalTotals, moneyOf } from "@/lib/money";
import { sha256Text } from "@/lib/sha256";
import { newId, sid } from "./db";
import { CO, markPosted, onPeriodClosed, periodById } from "./costing";
import { reasonOf, todayVn } from "./g4common";
import { created, daysAgo, forbidden, hasRole, json, nowIso, pageSlice, problem, route, selfApproval, str, traceId, validation, type Route } from "./http";
import { ACTORS } from "./personas";

const OPERATOR = "accounting_bridge_operator";
const CHIEF = "chief_accountant";
const READ = [OPERATOR, CHIEF];

export const AB = {
  mappingV1: sid(0x34, 1),
  journalPushed: sid(0x35, 1),
  journalFailed: sid(0x35, 2),
} as const;

/** Mặc định kiểu VAS TT200 — TẠM, CHỜ KHÁCH (acctbridge/domain.DefaultRules). */
export const DEFAULT_RULES: AcctbridgeMappingRule[] = [
  { category: "straw_purchase", dimension_kind: "*", debit_account: "152", credit_account: "331", dimension_on: "credit" },
  { category: "collection_labour", dimension_kind: "*", debit_account: "622", credit_account: "334", dimension_on: "none" },
  { category: "consumables", dimension_kind: "*", debit_account: "627", credit_account: "152", dimension_on: "none" },
  { category: "transport_road", dimension_kind: "*", debit_account: "627", credit_account: "331", dimension_on: "none" },
  { category: "transport_waterway", dimension_kind: "*", debit_account: "627", credit_account: "331", dimension_on: "none" },
  { category: "hub_processing", dimension_kind: "*", debit_account: "627", credit_account: "331", dimension_on: "none" },
  { category: "storage", dimension_kind: "*", debit_account: "627", credit_account: "331", dimension_on: "none" },
  { category: "handling", dimension_kind: "*", debit_account: "627", credit_account: "331", dimension_on: "none" },
  { category: "loss", dimension_kind: "*", debit_account: "632", credit_account: "154", dimension_on: "none" },
  { category: "sales_revenue", dimension_kind: "*", debit_account: "131", credit_account: "511", dimension_on: "debit" },
  { category: "carbon_revenue", dimension_kind: "*", debit_account: "131", credit_account: "511", dimension_on: "none" },
  { category: "carbon_share", dimension_kind: "*", debit_account: "632", credit_account: "338", dimension_on: "none" },
];

const ACCOUNT_RE = /^[0-9]{3,4}[0-9A-Za-z.]{0,16}$/;

const mappings: AcctbridgeMapping[] = [
  { version_id: AB.mappingV1, version: 1, status: "approved", rules: DEFAULT_RULES, rules_sha256: sha256Text(JSON.stringify(DEFAULT_RULES)), effective_from: todayVn(-200), provisional: true, proposed_by: ACTORS.keToan, proposal_reason: "Bảng mặc định VAS TT200 — tạm, chờ khách chốt hệ thống tài khoản", proposed_at: daysAgo(200), decided_by: ACTORS.keToanTruong, decided_at: daysAgo(199), decision_reason: null },
];

function effectiveMapping(on = todayVn()): AcctbridgeMapping | null {
  return mappings.filter((m) => m.status === "approved" && m.effective_from! <= on).sort((a, b) => b.version! - a.version!)[0] ?? null;
}

const journals: AcctbridgeJournal[] = [];
const attempts: Record<string, AcctbridgePushAttempt[]> = {};
const jobs: (AcctbridgeJob & { polls: number; outcome?: "succeeded" | "rejected" })[] = [];

function dimOf(kind: string | undefined, ref: string | null | undefined): string {
  return kind && kind !== "none" && ref ? `${kind}:${ref}` : "";
}

/** Dựng dòng bút toán tổng hợp tất định từ dòng tổng hợp của kỳ (Nợ trước Có, rồi tài khoản, nhóm, chiều). */
export function buildLines(p: CostingPeriod, rules: AcctbridgeMappingRule[]): AcctbridgeJournalLine[] | "acctbridge.mapping_missing" | "acctbridge.empty_period" {
  const acc = new Map<string, { account: string; side: "debit" | "credit"; category: string; dimension: string; amount: bigint }>();
  const add = (account: string, side: "debit" | "credit", category: string, dimension: string, amount: bigint) => {
    const k = `${side === "debit" ? 0 : 1}|${account}|${category}|${dimension}`;
    const cur = acc.get(k) ?? { account, side, category, dimension, amount: 0n };
    cur.amount += amount;
    acc.set(k, cur);
  };
  for (const l of p.summary?.lines ?? []) {
    const amount = cents(l.amount ?? "0");
    if (amount === 0n) continue;
    const rule = rules.find((r) => r.category === l.category && r.dimension_kind === l.dimension_kind) ?? rules.find((r) => r.category === l.category && r.dimension_kind === "*");
    if (!rule) return "acctbridge.mapping_missing";
    const dim = dimOf(l.dimension_kind, l.dimension_ref);
    // Dòng âm (điều chỉnh kỳ sau) → đảo bên.
    const [dr, cr] = amount > 0n ? [rule.debit_account, rule.credit_account] : [rule.credit_account, rule.debit_account];
    const abs = amount > 0n ? amount : -amount;
    add(dr, "debit", l.category!, rule.dimension_on === "debit" ? dim : "", abs);
    add(cr, "credit", l.category!, rule.dimension_on === "credit" ? dim : "", abs);
  }
  if (!acc.size) return "acctbridge.empty_period";
  return [...acc.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, v], i) => ({ line_no: i + 1, account: v.account, side: v.side, amount: moneyOf(v.amount), category: v.category, dimension: v.dimension, description: `Tổng hợp ${v.category} kỳ ${p.code}` }));
}

function build(p: CostingPeriod, by: string, supersedes: AcctbridgeJournal | null): AcctbridgeJournal | string {
  const m = effectiveMapping();
  if (!m) return "acctbridge.mapping_missing";
  const lines = buildLines(p, m.rules ?? []);
  if (typeof lines === "string") return lines;
  const t = journalTotals(lines);
  const j: AcctbridgeJournal = {
    journal_id: newId(0x35),
    period_id: p.id,
    period_code: p.code,
    version: (supersedes?.version ?? 0) + 1,
    status: "built",
    close_run_id: p.close_run_id!,
    source_hash: p.summary!.result_set_sha256,
    lines_sha256: sha256Text(JSON.stringify(lines)),
    mapping_version: m.version,
    currency: "VND",
    line_count: lines.length,
    total_debit: t.debit,
    total_credit: t.credit,
    built_by: by,
    built_at: nowIso(),
    build_job_id: newId(0x36),
    supersedes_journal_id: supersedes?.journal_id ?? null,
    superseded_by: null,
    approved_by: null,
    approved_at: null,
    adapter: null,
    external_ref: null,
    artifact_sha256: null,
    pushed_at: null,
    push_attempts: 0,
    last_failure_code: null,
    next_retry_at: null,
    lines,
  };
  if (supersedes) {
    supersedes.status = "superseded";
    supersedes.superseded_by = j.journal_id;
  }
  journals.push(j);
  attempts[j.journal_id!] = [];
  return j;
}

// ---------------------------------------------------------------------------
// Tệp xuất (acctbridge.export.v1) — tất định, cùng byte với bộ xuất tệp
// ---------------------------------------------------------------------------
function csvField(s: string): string {
  return /[;"\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}
export function exportCsv(j: AcctbridgeJournal): string {
  const head = "schema;journal_id;period_id;period_code;close_run_id;currency;source_hash;mapping_version;line_no;account;side;debit;credit;category;dimension;description";
  const rows = (j.lines ?? []).map((l) =>
    ["acctbridge.export.v1", j.journal_id, j.period_id, j.period_code, j.close_run_id, "VND", j.source_hash, String(j.mapping_version), String(l.line_no), l.account, l.side, l.side === "debit" ? l.amount : "", l.side === "credit" ? l.amount : "", l.category, l.dimension, l.description].map((x) => csvField(x ?? "")).join(";"),
  );
  return `${[head, ...rows].join("\r\n")}\r\n`;
}
function xmlAttr(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
export function exportXml(j: AcctbridgeJournal): string {
  const lines = (j.lines ?? []).map((l) => `  <Line no="${l.line_no}" account="${xmlAttr(l.account ?? "")}" side="${l.side}" amount="${l.amount}" category="${xmlAttr(l.category ?? "")}" dimension="${xmlAttr(l.dimension ?? "")}">${xmlAttr(l.description ?? "")}</Line>`);
  return `<?xml version="1.0" encoding="UTF-8"?>\n<SummaryJournal xmlns="urn:agrigreen:mkg:acctbridge:summary-journal:v1" schema="acctbridge.export.v1" journalId="${j.journal_id}" periodId="${j.period_id}" periodCode="${j.period_code}" closeRunId="${j.close_run_id}" currency="VND" sourceHash="${j.source_hash}" mappingVersion="${j.mapping_version}" totalDebit="${j.total_debit}" totalCredit="${j.total_credit}" lineCount="${j.line_count}">\n${lines.join("\n")}\n</SummaryJournal>\n`;
}

function pushJob(j: AcctbridgeJournal, by: string, outcome: "succeeded" | "rejected" = "succeeded") {
  const job = { job_id: newId(0x36), kind: "push" as const, status: "queued" as const, period_id: j.period_id!, period_code: j.period_code!, close_run_id: j.close_run_id!, expected_source_hash: j.source_hash!, mapping_version: j.mapping_version!, journal_id: j.journal_id!, attempt: (j.push_attempts ?? 0) + 1, failure_code: null, requested_by: by, requested_at: nowIso(), completed_at: null, polls: 0, outcome };
  jobs.push(job);
  return job;
}

function runPush(job: (typeof jobs)[number]): void {
  const j = journals.find((x) => x.journal_id === job.journal_id)!;
  const csv = exportCsv(j);
  const at = nowIso();
  j.push_attempts = (j.push_attempts ?? 0) + 1;
  const ref = `acctbridge/${sid(0, 0xf001)}/${j.period_code}_${j.journal_id}`;
  const a: AcctbridgePushAttempt = { attempt: j.push_attempts, job_id: job.job_id, adapter: "file_export.v1", outcome: job.outcome ?? "succeeded", error_code: null, external_ref: null, artifact_sha256: null, next_retry_at: null, started_at: at, finished_at: at };
  if (job.outcome === "rejected") {
    a.error_code = "acctbridge.adapter_rejected";
    j.status = "push_failed";
    j.last_failure_code = "acctbridge.adapter_rejected";
    job.status = "failed";
    job.failure_code = "acctbridge.adapter_rejected";
  } else {
    a.external_ref = ref;
    a.artifact_sha256 = sha256Text(`${sha256Text(csv)}  ${j.period_code}_${j.journal_id}.csv\n`);
    j.status = "pushed";
    j.adapter = "file_export.v1";
    j.external_ref = ref;
    j.artifact_sha256 = a.artifact_sha256;
    j.pushed_at = at;
    j.last_failure_code = null;
    job.status = "done";
    markPosted(j.period_id!, j.journal_id!);
  }
  job.completed_at = at;
  attempts[j.journal_id!]!.push(a);
}

/** Job dựng đã chạy khi kỳ đóng (hạt giống cho danh sách job). */
function seedBuildJob(j: AcctbridgeJournal, at: string): void {
  jobs.push({ job_id: newId(0x36), kind: "build", status: "done", period_id: j.period_id!, period_code: j.period_code!, close_run_id: j.close_run_id!, expected_source_hash: j.source_hash!, mapping_version: j.mapping_version ?? null, journal_id: j.journal_id!, attempt: null, failure_code: null, requested_by: j.built_by!, requested_at: at, completed_at: at, polls: 2 });
}

// Hạt giống: kỳ M2 đã đẩy (posted), kỳ M3 đẩy bị đích từ chối (DLQ, cần đẩy lại).
{
  const p2 = periodById(CO.periodPrev2)!;
  const j2 = build(p2, ACTORS.keToanTruong, null) as AcctbridgeJournal;
  j2.journal_id = AB.journalPushed;
  seedBuildJob(j2, daysAgo(32));
  attempts[AB.journalPushed] = [];
  j2.approved_by = ACTORS.keToan;
  j2.approved_at = daysAgo(30);
  runPush(pushJob(j2, ACTORS.keToan));
  const p3 = periodById(CO.periodPrev3)!;
  const j3 = build(p3, ACTORS.keToanTruong, null) as AcctbridgeJournal;
  j3.journal_id = AB.journalFailed;
  seedBuildJob(j3, daysAgo(62));
  attempts[AB.journalFailed] = [];
  j3.approved_by = ACTORS.keToan;
  j3.approved_at = daysAgo(60);
  for (let i = 0; i < 3; i++) runPush(pushJob(j3, ACTORS.keToan, "rejected"));
}

// Kỳ costing đóng → job dựng (người dựng = người đóng kỳ).
onPeriodClosed((p) => {
  const cur = journals.find((x) => x.period_id === p.id && x.status !== "superseded");
  if (cur?.status === "pushed") return;
  build(p, p.closed_by!, cur ?? null);
});

function journalOr404(id: string) {
  return journals.find((j) => j.journal_id === id) ?? problem(404, "acctbridge.journal_not_found", "Journal not found");
}

function listView(j: AcctbridgeJournal): AcctbridgeJournal {
  const { lines: _lines, ...rest } = j;
  void _lines;
  return rest;
}

function stripJob(j: (typeof jobs)[number]): AcctbridgeJob {
  const { polls: _p, outcome: _o, ...rest } = j;
  void _p;
  void _o;
  return rest;
}

const readRoutes: Route[] = [
  route("GET", "/v1/acctbridge/journals", ({ url, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const period = url.searchParams.get("period_id");
    const st = url.searchParams.get("status");
    if (st && !["built", "approved", "pushed", "push_failed", "superseded"].includes(st)) return validation([{ field: "status", reason: "Trạng thái lạ" }]);
    const list = journals.filter((j) => (!period || j.period_id === period) && (!st || j.status === st)).sort((a, b) => (b.period_code ?? "").localeCompare(a.period_code ?? "") || b.version! - a.version!);
    return json({ items: list.map(listView), next_cursor: null });
  }),
  route("GET", "/v1/acctbridge/journals/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const j = journalOr404(p[0]!);
    return j instanceof Response ? j : json({ journal: j, push_attempts: attempts[j.journal_id!] ?? [] });
  }),
  route("GET", "/v1/acctbridge/mappings", ({ persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    return json({ versions: [...mappings].sort((a, b) => b.version! - a.version!), effective: effectiveMapping(), default_rules: DEFAULT_RULES, default_provisional: true });
  }),
];

export const acctbridgeRoutes: Route[] = [
  ...readRoutes,
  route("GET", "/v1/acctbridge/journals/:id/export", ({ params: p, url, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const j = journalOr404(p[0]!);
    if (j instanceof Response) return j;
    const fmt = url.searchParams.get("format") ?? "csv";
    if (fmt !== "csv" && fmt !== "xml") return problem(422, "acctbridge.format_unsupported", "Unsupported format");
    const body = fmt === "csv" ? exportCsv(j) : exportXml(j);
    return new Response(body, {
      status: 200,
      headers: { "Content-Type": fmt === "csv" ? "text/csv; charset=utf-8" : "application/xml; charset=utf-8", "X-Content-SHA256": sha256Text(body), "Content-Disposition": `attachment; filename="${j.period_code}_${j.journal_id}.${fmt}"`, "X-Trace-Id": traceId() },
    });
  }),
  route("POST", "/v1/acctbridge/journals/:id/retry-push", ({ params: p, persona }) => {
    if (!hasRole(persona, OPERATOR)) return forbidden();
    const j = journalOr404(p[0]!);
    if (j instanceof Response) return j;
    if (j.status !== "push_failed") return problem(409, "acctbridge.journal_not_retryable", "Not retryable");
    j.status = "approved";
    const job = pushJob(j, persona.actor);
    return json({ journal: listView(j), job: stripJob(job) }, 202);
  }),
  route("POST", "/v1/acctbridge/periods/:id/journal-builds", ({ params: p, persona }) => {
    if (!hasRole(persona, OPERATOR)) return forbidden();
    const per = periodById(p[0]!);
    if (!per || !per.close_run_id) return problem(404, "acctbridge.period_unknown", "Period unknown");
    const cur = journals.find((x) => x.period_id === per.id && x.status !== "superseded");
    if (cur && (cur.status === "pushed" || cur.status === "approved")) return problem(409, "acctbridge.journal_pushed_immutable", "Journal pushed");
    const m = effectiveMapping();
    if (cur && m && cur.mapping_version === m.version && cur.close_run_id === per.close_run_id) return problem(409, "acctbridge.rebuild_no_change", "No change");
    const job = { job_id: newId(0x36), kind: "build" as const, status: "queued" as const, period_id: per.id!, period_code: per.code!, close_run_id: per.close_run_id, expected_source_hash: per.summary?.result_set_sha256 ?? "", mapping_version: m?.version ?? null, journal_id: null, attempt: null, failure_code: null, requested_by: persona.actor, requested_at: nowIso(), completed_at: null, polls: 0 };
    jobs.push(job);
    return json({ job: stripJob(job) }, 202);
  }),
  route("GET", "/v1/acctbridge/jobs", ({ url, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const kind = url.searchParams.get("kind");
    const st = url.searchParams.get("status");
    const per = url.searchParams.get("period_id");
    if (kind && kind !== "build" && kind !== "push") return validation([{ field: "kind", reason: "build | push" }]);
    if (st && !["queued", "done", "failed"].includes(st)) return validation([{ field: "status", reason: "queued | done | failed" }]);
    // Mới nhất trước; cùng thời điểm (hạt giống) → job ghi sau trước.
    const list = jobs
      .map((j, i) => ({ j, i }))
      .filter(({ j }) => (!kind || j.kind === kind) && (!st || j.status === st) && (!per || j.period_id === per))
      .sort((a, b) => b.j.requested_at!.localeCompare(a.j.requested_at!) || b.i - a.i)
      .map(({ j }) => stripJob(j));
    return json(pageSlice(list, url));
  }),
  route("GET", "/v1/acctbridge/jobs/:id", ({ params: p, persona }) => {
    if (!hasRole(persona, ...READ)) return forbidden();
    const job = jobs.find((x) => x.job_id === p[0]);
    if (!job) return problem(404, "acctbridge.job_not_found", "Job not found");
    job.polls += 1;
    if (job.status === "queued" && job.polls >= 2) {
      if (job.kind === "push") runPush(job);
      else {
        const per = periodById(job.period_id!)!;
        const cur = journals.find((x) => x.period_id === per.id && x.status !== "superseded") ?? null;
        const r = build(per, job.requested_by!, cur);
        if (typeof r === "string") {
          job.status = "failed";
          job.failure_code = r;
        } else {
          job.status = "done";
          job.journal_id = r.journal_id!;
        }
        job.completed_at = nowIso();
      }
    }
    return json({ job: stripJob(job) });
  }),
];

export const acctbridgeAdminRoutes: Route[] = [
  ...readRoutes,
  route("POST", "/v1/acctbridge/mappings", ({ body, persona }) => {
    if (!hasRole(persona, OPERATOR)) return forbidden();
    const reason = reasonOf(body);
    if (reason instanceof Response) return reason;
    if (mappings.some((m) => m.status === "pending")) return problem(409, "acctbridge.mapping_pending", "Mapping pending");
    const rulesIn = Array.isArray(body.rules) ? (body.rules as AcctbridgeMappingRule[]) : null;
    const rules = rulesIn ?? DEFAULT_RULES;
    const errs: { field: string; reason: string }[] = [];
    const seen = new Set<string>();
    rules.forEach((r, i) => {
      if (!ACCOUNT_RE.test(r.debit_account ?? "")) errs.push({ field: `rules[${i}].debit_account`, reason: "Số hiệu tài khoản" });
      if (!ACCOUNT_RE.test(r.credit_account ?? "")) errs.push({ field: `rules[${i}].credit_account`, reason: "Số hiệu tài khoản" });
      if (r.debit_account === r.credit_account) errs.push({ field: `rules[${i}]`, reason: "Nợ trùng Có" });
      if (!["none", "debit", "credit"].includes(r.dimension_on)) errs.push({ field: `rules[${i}].dimension_on`, reason: "none | debit | credit" });
      const k = `${r.category}|${r.dimension_kind}`;
      if (seen.has(k)) errs.push({ field: `rules[${i}]`, reason: "Trùng nhóm × chiều" });
      seen.add(k);
    });
    const eff = str(body, "effective_from") || todayVn();
    if (!/^\d{4}-\d{2}-\d{2}$/.test(eff)) errs.push({ field: "effective_from", reason: "YYYY-MM-DD" });
    if (errs.length) return validation(errs);
    const m: AcctbridgeMapping = { version_id: newId(0x34), version: mappings.length + 1, status: "pending", rules, rules_sha256: sha256Text(JSON.stringify(rules)), effective_from: eff, provisional: rulesIn ? body.provisional === true : true, proposed_by: persona.actor, proposal_reason: reason, proposed_at: nowIso(), decided_by: null, decided_at: null, decision_reason: null };
    mappings.push(m);
    return created({ mapping: m });
  }),
  route("POST", "/v1/acctbridge/mappings/:id/decision", ({ params: p, body, persona }) => {
    if (!hasRole(persona, CHIEF)) return forbidden();
    const m = mappings.find((x) => x.version_id === p[0]);
    if (!m) return problem(404, "acctbridge.mapping_not_found", "Mapping not found");
    if (m.status !== "pending") return problem(409, "acctbridge.mapping_decided", "Mapping decided");
    if (m.proposed_by === persona.actor) return selfApproval();
    const d = str(body, "decision");
    if (d !== "approved" && d !== "rejected") return validation([{ field: "decision", reason: "approved | rejected" }]);
    if (d === "rejected") {
      const r = reasonOf(body);
      if (r instanceof Response) return r;
      m.decision_reason = r;
    }
    m.status = d;
    m.decided_by = persona.actor;
    m.decided_at = nowIso();
    return created({ mapping: m });
  }),
  route("POST", "/v1/acctbridge/journals/:id/approve", ({ params: p, persona }) => {
    if (!hasRole(persona, CHIEF)) return forbidden();
    const j = journalOr404(p[0]!);
    if (j instanceof Response) return j;
    if (j.status === "pushed") return problem(409, "acctbridge.journal_pushed_immutable", "Journal pushed");
    if (j.status !== "built") return problem(409, "acctbridge.journal_not_built", "Journal not built");
    if (j.built_by === persona.actor) return selfApproval();
    j.status = "approved";
    j.approved_by = persona.actor;
    j.approved_at = nowIso();
    const job = pushJob(j, persona.actor);
    return json({ journal: listView(j), job: stripJob(job) }, 202);
  }),
  route("POST", "/v1/acctbridge/projections/rebuild", ({ persona }) => (hasRole(persona, CHIEF) ? json({ mapping_events: mappings.length, journal_events: journals.length, journals: journals.length }) : forbidden())),
];
