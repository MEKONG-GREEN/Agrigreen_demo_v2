// BACKEND GIẢ LẬP — pf/mrv G3: đủ hồ sơ theo thửa/vụ + cờ bất thường FR-08, đối chiếu nhật ký HTX ↔
// minh chứng cán bộ KN (KN US-MRV-02), ước tính CO2 TẠM (mrv.co2.estimate.v1), phương pháp ước tính có
// phê duyệt (Admin API, R-56). Dữ liệu tổng hợp; hình dạng đúng contracts/openapi.
import type { MrvComparison, MrvComparisonLine, MrvComparisonView, MrvCompleteness, MrvEstimate, MrvMethodVersion, MrvOfficerRecord, MrvSeason } from "../types";
import { addDays, vnToday } from "@/lib/vnDate";
import { COMMUNES, COOPERATIVES, plotsOf } from "./data";
import { hasRole, json, paginate, problem, scopeUnits, validation, type Route } from "./http";
import type { MockPersona } from "./personas";
import { mockUuid } from "./metrics";

const READ_ROLES = ["mrv_collector", "mrv_reviewer", "mrv_viewer", "mrv_exporter", "platform_admin"];
export const THRESHOLD_DAYS = 14;
export const COMPARE_WINDOW_DAYS = 3;
const FACTOR = "1460";
export const PROVISIONAL_LABEL_API = "Tạm — chờ khách ký phương pháp";

interface MockSeason {
  season: MrvSeason;
  /** Ngày ghi nhận gần nhất (nhật ký hoặc minh chứng), trước hôm nay. */
  lastRecordDaysAgo: number;
  knStatus: "missing" | "present" | "approved" | "rejected";
  logs: number;
  collectedKg: number | null;
}

const at = (daysAgo: number, hour = 9) => new Date(Date.parse(`${addDays(vnToday(), -daysAgo)}T00:00:00Z`) + (hour - 7) * 3_600_000).toISOString();

let SEASONS: MockSeason[] | null = null;

function seasons(): MockSeason[] {
  if (SEASONS) return SEASONS;
  const out: MockSeason[] = [];
  const dt = COMMUNES.filter((c) => c.province_code === "82");
  dt.forEach((commune, ci) => {
    const coop = COOPERATIVES.find((c) => c.api.admin_unit_code === commune.code);
    if (!coop) return;
    const plots = plotsOf(coop).filter((p) => p.properties.status === "active").slice(0, ci === 0 ? 5 : 3);
    plots.forEach((p, i) => {
      const k = ci * 10 + i;
      const status: MrvSeason["status"] = i === 2 && ci > 0 ? "completed" : "open";
      out.push({
        season: {
          season_ref: mockUuid(`mrv-season|${p.properties.ref}`),
          plot_ref: p.properties.ref,
          commune_code: commune.code,
          season_code: "HT",
          season_year: 2026,
          status,
          sowing_date: addDays(vnToday(), -(70 + (k % 9))),
        },
        // Hai vụ đang mở không có bản ghi mới ≥ 14 ngày (FR-08) ở xã đầu, một ở xã khác.
        lastRecordDaysAgo: (ci === 0 && (i === 1 || i === 3)) || (ci === 2 && i === 0) ? 16 + i : 2 + (k % 6),
        knStatus: (["approved", "present", "missing", "approved", "rejected"] as const)[i % 5]!,
        logs: 3 + (i % 2),
        collectedKg: i % 3 === 2 ? null : 4200 + k * 350,
      });
    });
  });
  SEASONS = out;
  return out;
}

function visible(p: MockPersona, s: MrvSeason): boolean {
  const scope = scopeUnits(p);
  return !scope || scope.has(s.commune_code) || scope.has(s.commune_code.slice(0, 2));
}

/** Bậc khối lượng (cận dưới, kg) theo quy tắc bậc của van bằng chứng thu gom (giả lập bậc 1 tấn). */
function bucketOf(kg: number): { bucket: string; tonnes: string } {
  const lo = Math.floor(kg / 1000);
  return { bucket: `${lo * 1000}-${(lo + 1) * 1000}`, tonnes: String(lo) };
}

function estimateOf(m: MockSeason): MrvEstimate | null {
  if (m.collectedKg === null) return null;
  const counted = m.knStatus === "approved";
  const { bucket, tonnes } = bucketOf(m.collectedKg);
  const countedT = counted ? tonnes : "0";
  const s = m.season;
  return {
    id: mockUuid(`mrv-est|${s.season_ref}`),
    season_ref: s.season_ref,
    plot_ref: s.plot_ref,
    commune_code: s.commune_code,
    season_code: s.season_code,
    rule_version: "mrv.co2.estimate.v1",
    input_sha256: mockUuid(`sha|${s.season_ref}`).replace(/-/g, "").padEnd(64, "0").slice(0, 64),
    counted_tonnes: countedT,
    avoided_burning_kgco2e: String(Number(countedT) * Number(FACTOR)),
    transport_kgco2e: null,
    transport_status: "no_leg_data",
    inputs: [
      {
        evidence_ref: mockUuid(`mrv-coll|${s.season_ref}`),
        revision: 1,
        collected_kg_bucket: bucket,
        bucket_rule_version: "mkg.bucket.v1",
        collected_on: addDays(vnToday(), -3),
        counted,
        tonnes: countedT,
        ...(counted ? {} : { exclusion: "chua_co_minh_chung_dat_chuan" }),
      },
    ],
    params: [{ key: "mrv.co2.open_burning_kg_per_t", version: 0, value: FACTOR, unit: "kgCO2e/t", source: "default" }],
    // Tham số chưa có bản duyệt (source = default) → không bao giờ chính thức (C-1 chưa ký).
    status: "provisional",
    official: false,
    label: PROVISIONAL_LABEL_API,
    method_version_id: null,
    computed_at: at(1, 1),
    recorded_at: at(1, 1),
    supersedes_id: null,
    seq: 1,
  };
}

function completeness(m: MockSeason): MrvCompleteness {
  const s = m.season;
  const kn = { key: "straw_handling", source: "kn_evidence" as const, catalog_version: 2, status: m.knStatus, count: m.knStatus === "missing" ? 0 : 1, approved_count: m.knStatus === "approved" ? 1 : 0 };
  const awd = { key: "awd_log", source: "kn_evidence" as const, catalog_version: 1, status: m.knStatus === "missing" ? ("missing" as const) : ("approved" as const), count: m.knStatus === "missing" ? 0 : 2, approved_count: m.knStatus === "missing" ? 0 : 2 };
  const logs = { key: "htx_farm_log", source: "htx_farm_log" as const, catalog_version: 1, status: "present" as const, count: m.logs, approved_count: m.logs - 1 };
  const coll = { key: "collection_evidence", source: "collection_evidence" as const, catalog_version: null, status: m.collectedKg === null ? ("missing" as const) : ("present" as const), count: m.collectedKg === null ? 0 : 1, approved_count: 0 };
  const noRecord = s.status === "open" && m.lastRecordDaysAgo >= THRESHOLD_DAYS;
  return {
    season: s,
    items: [kn, awd, logs, coll],
    counts_toward_estimate: m.knStatus === "approved",
    anomaly: { no_record: noRecord, last_record_at: at(m.lastRecordDaysAgo, 15), days_since: m.lastRecordDaysAgo, threshold_days: THRESHOLD_DAYS },
    estimate: estimateOf(m),
  };
}

// --- Đối chiếu HTX ↔ KN --------------------------------------------------
const ACTIVITIES = ["sowing", "fertilizing", "awd_drying", "straw_handling"] as const;
const COMPARISONS = new Map<string, MrvComparison[]>();

function officerRecords(m: MockSeason): MrvOfficerRecord[] {
  const s = m.season;
  return ACTIVITIES.slice(0, 3).map((a, i) => ({
    evidence_id: mockUuid(`mrv-ev|${s.season_ref}|${a}|v1`),
    root_id: mockUuid(`mrv-ev|${s.season_ref}|${a}|root`),
    evidence_type: a === "awd_drying" ? "awd_log" : "straw_handling",
    activity_code: a,
    event_time: at(40 - i * 12 + (i === 1 ? 5 : 0), 10),
    review_status: i === 2 ? "pending" : "accepted",
  }));
}

function comparisonView(m: MockSeason): MrvComparisonView {
  const s = m.season;
  const officers = officerRecords(m);
  const lines: MrvComparisonLine[] = ACTIVITIES.slice(0, m.logs).map((a, i) => {
    const logRef = mockUuid(`mrv-log|${s.season_ref}|${a}`);
    const logTime = at(40 - i * 12, 8);
    const off = officers.find((o) => o.activity_code === a) ?? null;
    // Gợi ý: cùng hoạt động trong ± cửa sổ → khớp; lệch ngày → khác biệt; không có → chưa kiểm chứng.
    const gap = off ? Math.abs(Date.parse(off.event_time) - Date.parse(logTime)) / 86_400_000 : null;
    const suggested = gap === null ? "unverified" : gap <= COMPARE_WINDOW_DAYS ? "match" : "mismatch";
    const comps = COMPARISONS.get(`${s.season_ref}|${logRef}`) ?? [];
    const superseded = new Set(comps.map((c) => c.supersedes_id).filter(Boolean));
    const head = comps.filter((c) => !superseded.has(c.id)).at(-1) ?? null;
    return {
      log_ref: logRef,
      log_id: mockUuid(`mrv-log|${s.season_ref}|${a}|v1`),
      log_version: 1,
      activity_code: a,
      log_event_time: logTime,
      location_status: i === 2 ? "outside_plot" : "inside_plot",
      htx_review_decision: i === 3 ? null : "approved",
      catalog_version: 1,
      suggested,
      suggested_officer_record: off,
      result: head?.result ?? suggested,
      confirmed: !!head,
      comparisons: comps,
    };
  });
  return { season: s, window_days: COMPARE_WINDOW_DAYS, lines, officer_records: officers };
}

// --- Phương pháp ước tính (Admin) -----------------------------------------
export const MRV_METHODS: MrvMethodVersion[] = [
  {
    id: mockUuid("mrv-method|1"),
    rule_version: "mrv.co2.estimate.v1",
    version: 1,
    status: "rejected",
    params: [{ key: "mrv.co2.open_burning_kg_per_t", version: 1 }],
    source_note: "Dự thảo phương pháp tránh đốt rơm (IPCC EFDB) — bản nháp nội bộ",
    setter_ref: "mock-admin-approver",
    approver_ref: "mock-admin-setter",
    decided_at: at(20, 10),
    decision_note: "Chưa có văn bản khách hàng ký (C-1)",
    created_at: at(25, 10),
  },
];

const METHOD_ROLES = ["param_setter", "param_approver", "platform_admin"];

function findSeason(p: MockPersona, id: string | undefined): MockSeason | Response {
  const m = seasons().find((x) => x.season.season_ref === id);
  if (!m || !visible(p, m.season)) return problem(404, "mrv.season_not_found", "Season not found");
  return m;
}

export const mrvAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/mrv\/completeness$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, ...READ_ROLES)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const unit = q("admin_unit_code");
      const scope = scopeUnits(persona);
      if (unit && scope && !scope.has(unit)) return problem(403, "mrv.out_of_scope", "Out of scope");
      const list = seasons()
        .filter((m) => visible(persona, m.season))
        .filter((m) => !unit || m.season.commune_code === unit || m.season.commune_code.startsWith(unit))
        .filter((m) => !q("season_code") || m.season.season_code === q("season_code"))
        .filter((m) => !q("plot_ref") || m.season.plot_ref === q("plot_ref"))
        .map(completeness)
        .filter((c) => q("anomaly") !== "true" || c.anomaly.no_record);
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mrv\/seasons\/([\w-]+)\/completeness$/,
    handler: ({ persona, params }) => {
      if (!hasRole(persona, ...READ_ROLES)) return problem(403, "forbidden", "Forbidden");
      const m = findSeason(persona, params[0]);
      return m instanceof Response ? m : json(completeness(m));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mrv\/seasons\/([\w-]+)\/comparison$/,
    handler: ({ persona, params }) => {
      if (!hasRole(persona, ...READ_ROLES)) return problem(403, "forbidden", "Forbidden");
      const m = findSeason(persona, params[0]);
      return m instanceof Response ? m : json(comparisonView(m));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/mrv\/comparisons$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, "mrv_collector")) return problem(403, "forbidden", "Forbidden");
      const b = (body ?? {}) as Record<string, unknown>;
      const str = (k: string) => (typeof b[k] === "string" ? (b[k] as string) : "");
      const m = findSeason(persona, str("season_ref"));
      if (m instanceof Response) return m;
      const view = comparisonView(m);
      const line = view.lines.find((l) => l.log_ref === str("log_ref"));
      if (!line) return problem(404, "mrv.farm_log_not_found", "Farm log not linked");
      const officer = view.officer_records.find((o) => o.evidence_id === str("evidence_id"));
      if (!officer) return problem(404, "mrv.evidence_not_found", "Evidence not found");
      const result = str("result");
      const note = str("note").trim();
      const errors: { field: string; reason: string }[] = [];
      if (result !== "match" && result !== "mismatch") errors.push({ field: "result", reason: "match hoặc mismatch" });
      if (result === "mismatch" && !note) errors.push({ field: "note", reason: "Khác biệt bắt buộc ghi chú" });
      const supersedes = str("supersedes_id") || null;
      const reason = str("correction_reason").trim();
      if (supersedes && reason.length < 10) errors.push({ field: "correction_reason", reason: "Tối thiểu 10 ký tự" });
      if (errors.length) return validation(errors);
      const key = `${m.season.season_ref}|${line.log_ref}`;
      const list = COMPARISONS.get(key) ?? [];
      const pair = list.filter((c) => c.evidence_id === officer.evidence_id);
      if (!supersedes && pair.length > 0) return problem(409, "mrv.comparison_exists", "Comparison exists");
      if (supersedes) {
        const target = pair.find((c) => c.id === supersedes);
        if (!target) return problem(404, "mrv.comparison_not_found", "Comparison not found");
        if (list.some((c) => c.supersedes_id === supersedes)) return problem(409, "mrv.comparison_superseded", "Superseded");
      }
      const now = new Date().toISOString();
      const c: MrvComparison = {
        id: mockUuid(`mrv-cmp|${key}|${list.length}|${now}`),
        evidence_id: officer.evidence_id,
        log_ref: line.log_ref,
        log_id: line.log_id,
        season_ref: m.season.season_ref,
        result: result as MrvComparison["result"],
        note: note || null,
        file_refs: [],
        compared_by: persona.me.actor_ref,
        event_time: now,
        recorded_at: now,
        supersedes_id: supersedes,
        correction_reason: supersedes ? reason : null,
      };
      COMPARISONS.set(key, [...list, c]);
      return json(c, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mrv\/estimates$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, ...READ_ROLES)) return problem(403, "forbidden", "Forbidden");
      const ref = url.searchParams.get("season_ref");
      const unit = url.searchParams.get("admin_unit_code");
      const list = seasons()
        .filter((m) => visible(persona, m.season) && (!ref || m.season.season_ref === ref) && (!unit || m.season.commune_code.startsWith(unit)))
        .map(estimateOf)
        .filter((e): e is MrvEstimate => !!e);
      return json(paginate(list, url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/mrv\/seasons\/([\w-]+)\/estimate\/recompute$/,
    handler: ({ persona, params }) => {
      if (!hasRole(persona, "mrv_reviewer")) return problem(403, "forbidden", "Forbidden");
      const m = findSeason(persona, params[0]);
      // Cùng đầu vào → không ghi thêm dòng: trả ước tính hiện hành.
      return m instanceof Response ? m : json({ estimate: estimateOf(m) });
    },
  },
];

export const mrvAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/mrv\/methods$/,
    handler: ({ persona, url }) => {
      if (!hasRole(persona, ...METHOD_ROLES)) return problem(403, "forbidden", "Forbidden");
      return json(paginate([...MRV_METHODS].sort((a, b) => b.version - a.version), url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/mrv\/methods$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, "param_setter")) return problem(403, "forbidden", "Forbidden");
      const b = (body ?? {}) as { rule_version?: unknown; source_note?: unknown; params?: unknown };
      const rule = typeof b.rule_version === "string" ? b.rule_version.trim() : "";
      const note = typeof b.source_note === "string" ? b.source_note.trim() : "";
      const pins = Array.isArray(b.params) ? (b.params as { key?: unknown; version?: unknown }[]) : [];
      const errors: { field: string; reason: string }[] = [];
      if (!rule) errors.push({ field: "rule_version", reason: "bắt buộc" });
      if (note.length < 5) errors.push({ field: "source_note", reason: "Tối thiểu 5 ký tự" });
      if (pins.length < 1 || pins.length > 20) errors.push({ field: "params", reason: "1–20 tham số" });
      pins.forEach((p, i) => {
        if (typeof p.key !== "string" || !p.key) errors.push({ field: `params[${i}].key`, reason: "bắt buộc" });
        if (typeof p.version !== "number" || !Number.isInteger(p.version) || p.version < 1) errors.push({ field: `params[${i}].version`, reason: "số nguyên ≥ 1" });
      });
      if (errors.length) return validation(errors);
      const now = new Date().toISOString();
      const v: MrvMethodVersion = {
        id: mockUuid(`mrv-method|${MRV_METHODS.length + 1}|${now}`),
        rule_version: rule,
        version: MRV_METHODS.filter((x) => x.rule_version === rule).reduce((mx, x) => Math.max(mx, x.version), 0) + 1,
        status: "submitted",
        params: pins.map((p) => ({ key: p.key as string, version: p.version as number })),
        source_note: note,
        setter_ref: persona.me.actor_ref,
        approver_ref: null,
        decided_at: null,
        decision_note: null,
        created_at: now,
      };
      MRV_METHODS.push(v);
      return json(v, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/mrv\/methods\/([\w-]+)\/(approve|reject)$/,
    handler: ({ persona, params, body }) => {
      if (!hasRole(persona, "param_approver")) return problem(403, "forbidden", "Forbidden");
      const v = MRV_METHODS.find((x) => x.id === params[0]);
      if (!v) return problem(404, "mrv.method_not_found", "Method not found");
      if (v.setter_ref === persona.me.actor_ref) return problem(403, "self_approval_forbidden", "Self approval forbidden");
      if (v.status !== "submitted") return problem(409, "mrv.method_invalid_transition", "Invalid transition");
      const note = typeof (body as { note?: unknown } | undefined)?.note === "string" ? ((body as { note: string }).note.trim()) : "";
      if (params[1] === "reject" && note.length < 5) return validation([{ field: "note", reason: "Lý do từ chối tối thiểu 5 ký tự" }]);
      v.status = params[1] === "approve" ? "approved" : "rejected";
      v.approver_ref = persona.me.actor_ref;
      v.decided_at = new Date().toISOString();
      v.decision_note = note || null;
      return json(v);
    },
  },
];
