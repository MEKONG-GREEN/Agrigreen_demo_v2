// GIẢ LẬP các route Application API mới của đợt hoàn thiện G1 — hình dạng theo contracts/openapi/application.yaml:
// /v1/dashboard (G-02), /v1/geo/admin-boundaries?level= (G-05), chi tiết nhiệm vụ/tài liệu, danh mục cá nhân (US-LIB-05),
// chỉ đạo khẩn (US-TASK-03), hộp thư thông báo + đồng bộ ngay (pf/notify), xung đột ranh giới, dự báo CGH (FN-09),
// xuất báo cáo bất đồng bộ (pf/reports). Trạng thái giữ trong bộ nhớ tab.
import { KNOWN_ENTITIES } from "@/lib/syncEntities";
import { uuidv7 } from "../idempotency";
import type {
  BoundaryConflict,
  ExtDirective,
  ExtKPI,
  LibraryItem,
  MechForecastResult,
  MechForecastRun,
  Notification,
  ReportExport,
  ReportParams,
  ReportTemplate,
  UnifiedFigures,
} from "../types";
import {
  addDecimal4,
  COMMUNES,
  COOPERATIVES,
  DOCUMENTS,
  hexagon,
  ORGS,
  PROVINCES,
  TASKS,
  UNREPORTED_COMMUNES,
  boundaryOf,
  plotsOf,
} from "./data";
import { hasRole, inScope, iso, isoHours, json, nowIso, omit, opaque, paginate, problem, reasonOf, scopeUnits, validation, type Route } from "./http";
import type { MockPersona } from "./personas";
import { SYNC_CONFLICTS } from "./conflicts";

/** Mã xã minh hoạ "chưa nhập ranh giới" → 404 / missing[]. */
export const MISSING_BOUNDARY = "96104";

// --------------------------------------------------------------------------
// KPI khuyến nông theo xã (dùng chung cho /v1/extension/dashboard và /v1/dashboard)
// --------------------------------------------------------------------------
export function ratio4(num: number, den: number): string | null {
  if (den === 0) return null;
  const scaled = Math.round((num * 10000) / den);
  return `${Math.floor(scaled / 10000)}.${String(scaled % 10000).padStart(4, "0")}`;
}

export function communeKpi(code: string): ExtKPI {
  const reported = !UNREPORTED_COMMUNES.has(code);
  const coops = COOPERATIVES.filter((c) => c.api.admin_unit_code === code && c.api.active);
  const tasks = TASKS.filter((t) => t.commune_code === code && t.status !== "resolved");
  const area = reported ? coops.reduce((s, c) => addDecimal4(s, c.api.area_ha ?? "0"), "0.0000") : "0.0000";
  const targets = 2;
  const acks = reported ? 2 : 1;
  return {
    sown_area_ha: area,
    cooperatives_reporting: reported ? coops.length : 0,
    open_tasks: tasks.length,
    overdue_tasks: tasks.filter((t) => t.overdue).length,
    directive_targets: targets,
    directive_acks: acks,
    directive_ack_rate: ratio4(acks, targets),
    expected_communes: 1,
    reported_communes: reported ? 1 : 0,
    missing_communes: reported ? [] : [code],
    provisional: !reported,
    provisional_label: reported ? null : "Số liệu tạm tính",
  };
}

export function sumKpi(list: ExtKPI[]): ExtKPI {
  const out = list.reduce<ExtKPI>(
    (s, k) => ({
      sown_area_ha: addDecimal4(s.sown_area_ha, k.sown_area_ha),
      cooperatives_reporting: s.cooperatives_reporting + k.cooperatives_reporting,
      open_tasks: s.open_tasks + k.open_tasks,
      overdue_tasks: s.overdue_tasks + k.overdue_tasks,
      directive_targets: s.directive_targets + k.directive_targets,
      directive_acks: s.directive_acks + k.directive_acks,
      directive_ack_rate: null,
      expected_communes: s.expected_communes + k.expected_communes,
      reported_communes: s.reported_communes + k.reported_communes,
      missing_communes: [...s.missing_communes, ...k.missing_communes],
      provisional: s.provisional || k.provisional,
      provisional_label: null,
    }),
    {
      sown_area_ha: "0.0000", cooperatives_reporting: 0, open_tasks: 0, overdue_tasks: 0, directive_targets: 0, directive_acks: 0,
      directive_ack_rate: null, expected_communes: 0, reported_communes: 0, missing_communes: [], provisional: false, provisional_label: null,
    },
  );
  out.directive_ack_rate = ratio4(out.directive_acks, out.directive_targets);
  out.provisional_label = out.provisional ? "Số liệu tạm tính" : null;
  return out;
}

/** Đơn vị yêu cầu của dashboard: bỏ trống → toàn quốc (nếu All) hoặc đơn vị phụ trách. Lỗi phạm vi → Response. */
export function resolveUnit(p: MockPersona, unit: string | null): { province?: (typeof PROVINCES)[number]; commune?: (typeof COMMUNES)[number] } | Response {
  let u = unit;
  if (!u && !p.me.scope.all) u = p.me.org?.admin_unit_code ?? null;
  if (!u) return {};
  const province = PROVINCES.find((x) => x.code === u);
  const commune = province ? undefined : COMMUNES.find((x) => x.code === u);
  if (!province && !commune) return problem(422, "validation_failed", "Unknown unit", { errors: [{ field: "unit", reason: "Không có đơn vị hành chính còn hiệu lực với mã này" }] });
  if (!inScope(p, province?.code ?? commune!.province_code, commune?.code ?? null) && !(province && p.me.scope.units.includes(province.code))) {
    return problem(403, "extension.out_of_scope", "Out of scope");
  }
  return { province, commune };
}

// --------------------------------------------------------------------------
// GET /v1/dashboard — G-02 (gộp pf/extension + pf/party + pf/geo + mục tiêu)
// --------------------------------------------------------------------------
function communeFigures(code: string): UnifiedFigures {
  const k = communeKpi(code);
  const coops = COOPERATIVES.filter((c) => c.api.admin_unit_code === code && c.api.active);
  const plots = coops.reduce((s, c) => s + (c.api.digitized_plots ?? 0), 0);
  return {
    sown_area_ha: k.sown_area_ha,
    target_area_ha: code.startsWith("80") ? null : "1500.0000",
    cooperatives: coops.length,
    households: coops.reduce((s, c) => s + (c.api.member_households ?? 0), 0),
    cooperatives_reporting: k.cooperatives_reporting,
    digitized_plots: plots,
    digitized_area_ha: `${Math.floor(plots * 1.7)}.${String((plots * 37) % 10000).padStart(4, "0")}`,
    open_tasks: k.open_tasks,
    overdue_tasks: k.overdue_tasks,
    expected_communes: k.expected_communes,
    reported_communes: k.reported_communes,
    missing_communes: k.missing_communes,
    provisional: k.provisional,
    provisional_label: k.provisional_label,
  };
}

function sumFigures(list: UnifiedFigures[], targetOverride?: string | null): UnifiedFigures {
  const target = list.every((f) => f.target_area_ha === null) ? null : list.reduce((s, f) => addDecimal4(s, f.target_area_ha ?? "0"), "0.0000");
  const provisional = list.some((f) => f.provisional);
  return {
    sown_area_ha: list.reduce((s, f) => addDecimal4(s, f.sown_area_ha), "0.0000"),
    target_area_ha: targetOverride === undefined ? target : targetOverride,
    cooperatives: list.reduce((s, f) => s + f.cooperatives, 0),
    households: list.reduce((s, f) => s + f.households, 0),
    cooperatives_reporting: list.reduce((s, f) => s + f.cooperatives_reporting, 0),
    digitized_plots: list.reduce((s, f) => s + f.digitized_plots, 0),
    digitized_area_ha: list.reduce((s, f) => addDecimal4(s, f.digitized_area_ha), "0.0000"),
    open_tasks: list.reduce((s, f) => s + f.open_tasks, 0),
    overdue_tasks: list.reduce((s, f) => s + f.overdue_tasks, 0),
    expected_communes: list.reduce((s, f) => s + f.expected_communes, 0),
    reported_communes: list.reduce((s, f) => s + f.reported_communes, 0),
    missing_communes: list.flatMap((f) => f.missing_communes),
    provisional,
    provisional_label: provisional ? "Số liệu tạm tính" : null,
  };
}

function unifiedDashboard(p: MockPersona, url: URL): Response {
  const period = url.searchParams.get("period") ?? new Date().toISOString().slice(0, 7);
  const r = resolveUnit(p, url.searchParams.get("unit"));
  if (r instanceof Response) return r;
  const base = { period, as_of: new Date(Date.now() - 40 * 60_000).toISOString(), sources: ["extension", "party", "geo", "masterdata.params"] };
  if (r.commune) {
    return json({ ...base, unit: r.commune.code, level: "commune", total: communeFigures(r.commune.code), children: [] });
  }
  if (r.province) {
    const children = COMMUNES.filter((c) => c.province_code === r.province!.code).map((c) => ({ ...communeFigures(c.code), unit: c.code, name: c.name }));
    return json({ ...base, unit: r.province.code, level: "province", total: sumFigures(children), children });
  }
  const children = PROVINCES.map((pr) => ({
    ...sumFigures(COMMUNES.filter((c) => c.province_code === pr.code).map((c) => communeFigures(c.code))),
    unit: pr.code,
    name: pr.name,
  }));
  return json({ ...base, unit: null, level: "national", total: sumFigures(children), children });
}

// --------------------------------------------------------------------------
// Danh mục cá nhân (US-LIB-05)
// --------------------------------------------------------------------------
const LIBRARY = new Map<string, Map<string, { saved_at: string; note: string | null }>>();

function libraryOf(p: MockPersona) {
  let m = LIBRARY.get(p.id);
  if (!m) {
    m = new Map();
    // Mỗi persona có sẵn một tài liệu đã lưu để tab "Đã lưu" không trống.
    const first = DOCUMENTS[1];
    if (first) m.set(first.ref, { saved_at: iso(3), note: "Đọc trước buổi tập huấn AWD" });
    LIBRARY.set(p.id, m);
  }
  return m;
}

function docVisible(p: MockPersona, ref: string | undefined) {
  const s = scopeUnits(p);
  const d = DOCUMENTS.find((x) => x.ref === ref);
  if (!d) return undefined;
  if (s && d.audience.level !== "national" && !d.audience.units.some((u) => s.has(u))) return undefined;
  return d;
}

function libraryItem(docRef: string, e: { saved_at: string; note: string | null }): LibraryItem {
  const d = DOCUMENTS.find((x) => x.ref === docRef);
  return {
    document_ref: docRef,
    saved_at: e.saved_at,
    note: e.note,
    available: !!d && d.status === "published",
    kind: d?.kind ?? null,
    title: d?.title ?? null,
    summary: d?.summary ?? null,
    published_at: d?.published_at ?? null,
  };
}

// --------------------------------------------------------------------------
// Chỉ đạo khẩn (US-TASK-03)
// --------------------------------------------------------------------------
interface MockDirective {
  d: Omit<ExtDirective, "is_target" | "acked_at" | "ack_count" | "ack_rate" | "target_count">;
  targets: { actor_ref: string; unit_code: string; province_code: string }[];
  acks: Map<string, string>;
}

function targetsFor(units: string[]): MockDirective["targets"] {
  const communes = new Set<string>();
  for (const u of units) {
    if (PROVINCES.some((p) => p.code === u)) COMMUNES.filter((c) => c.province_code === u).forEach((c) => communes.add(c.code));
    else communes.add(u);
  }
  // Tây Ninh (80) chưa có cán bộ trong danh bạ giả lập → không có người nhận.
  return Array.from(communes)
    .filter((c) => !c.startsWith("80"))
    .flatMap((c) => [
      { actor_ref: c === "82101" ? "mock-xa-0001" : `mock-knv-${c}-1`, unit_code: c, province_code: c.slice(0, 2) },
      { actor_ref: `mock-knv-${c}-2`, unit_code: c, province_code: c.slice(0, 2) },
    ]);
}

function seedDirective(i: number, title: string, body: string, units: string[], daysAgo: number, ackShare: number): MockDirective {
  const targets = targetsFor(units);
  const acks = new Map<string, string>();
  targets.slice(0, Math.floor(targets.length * ackShare)).forEach((t) => {
    if (t.actor_ref !== "mock-xa-0001") acks.set(t.actor_ref, iso(daysAgo - 0.2));
  });
  return {
    d: {
      directive_id: `01927b3e-d1a0-7000-8000-${String(i).padStart(12, "0")}`,
      title,
      body,
      severity: "urgent",
      audience_units: units,
      province_codes: Array.from(new Set(units.map((u) => u.slice(0, 2)))),
      issuer_ref: "mock-tinh-0082",
      issued_at: iso(daysAgo),
      expires_at: isoHours(24 * 7),
    },
    targets,
    acks,
  };
}

export const DIRECTIVES: MockDirective[] = [
  seedDirective(1, "Khẩn: kiểm tra rầy nâu di trú vụ Hè Thu", "Đề nghị các tổ khuyến nông kiểm tra mật độ rầy tại ruộng 30–40 ngày tuổi, báo cáo trước 17 giờ.", ["82"], 1, 0.5),
  seedDirective(2, "Chuẩn bị lịch xuống giống Thu Đông né lũ", "Rà soát lịch xuống giống theo khung của tỉnh, hướng dẫn HTX vùng trũng.", ["82101", "82102"], 6, 0.75),
  seedDirective(3, "Ứng phó xâm nhập mặn", "Theo dõi độ mặn tại cống đầu kênh, khuyến cáo giống chịu mặn.", ["96"], 9, 1),
];

function directiveView(md: MockDirective, p: MockPersona): ExtDirective {
  const mine = md.targets.find((t) => t.actor_ref === p.me.actor_ref);
  return {
    ...md.d,
    target_count: md.targets.length,
    ack_count: md.acks.size,
    ack_rate: ratio4(md.acks.size, md.targets.length),
    is_target: !!mine,
    acked_at: mine ? (md.acks.get(p.me.actor_ref) ?? null) : null,
  };
}

const isSupervisor = (p: MockPersona) => hasRole(p, "kn_province_admin", "kn_national_admin", "platform_admin");

function directiveVisible(md: MockDirective, p: MockPersona): boolean {
  if (md.targets.some((t) => t.actor_ref === p.me.actor_ref)) return true;
  if (!isSupervisor(p)) return false;
  return md.d.audience_units.some((u) => inScope(p, u.slice(0, 2), u.length > 2 ? u : null) || p.me.scope.units.includes(u.slice(0, 2)));
}

// --------------------------------------------------------------------------
// Hộp thư thông báo (pf/notify)
// --------------------------------------------------------------------------
const INBOX = new Map<string, Notification[]>();

function inboxOf(p: MockPersona): Notification[] {
  let list = INBOX.get(p.id);
  if (list) return list;
  const n = (i: number, x: Omit<Notification, "id" | "created_at" | "read_at" | "subject_ref"> & { hoursAgo: number; read?: boolean; subject?: string | null }): Notification => ({
    id: `01927b3e-4e70-7000-8000-${String(i).padStart(12, "0")}`,
    kind: x.kind,
    severity: x.severity,
    title: x.title,
    body: x.body,
    deep_link: x.deep_link,
    topic: x.topic,
    subject_ref: x.subject ?? null,
    created_at: isoHours(-x.hoursAgo),
    read_at: x.read ? isoHours(-x.hoursAgo + 0.5) : null,
  });
  const task = TASKS.find((t) => inScope(p, t.province_code, t.commune_code) && t.status !== "resolved") ?? TASKS[0]!;
  const alertDoc = DOCUMENTS.find((d) => d.kind === "alert") ?? DOCUMENTS[0]!;
  const inactiveOrg = ORGS.find((o) => !o.active && o.kind !== "cooperative") ?? ORGS[0]!;
  const admin = hasRole(p, "platform_admin", "sync_conflict_resolver");
  list = [
    n(1, {
      kind: "directive", severity: "urgent", topic: true, hoursAgo: 2,
      title: DIRECTIVES[0]!.d.title, body: "Chỉ đạo khẩn mới từ TTKN tỉnh — xác nhận đã đọc trong ngày.",
      deep_link: `/extension/directives/${DIRECTIVES[0]!.d.directive_id}`, subject: DIRECTIVES[0]!.d.directive_id,
    }),
    n(2, {
      kind: "alert", severity: "urgent", topic: true, hoursAgo: 5,
      title: alertDoc.title ?? "Cảnh báo mới", body: "Cảnh báo sâu bệnh mới được xuất bản trong thư viện số.",
      deep_link: `/documents/${alertDoc.ref}`, subject: alertDoc.ref,
    }),
    n(3, {
      kind: "task_escalated", severity: "normal", topic: false, hoursAgo: 9,
      title: "Nhiệm vụ quá hạn SLA đã chuyển lên tỉnh", body: task.description,
      deep_link: `/extension/tasks/${task.task_id}`, subject: task.task_id,
    }),
    ...(admin
      ? [
          n(4, {
            kind: "sync_conflict", severity: "normal", topic: false, hoursAgo: 20,
            title: "Xung đột đồng bộ ranh giới thửa cần xử lý", body: "Hai phiên bản ranh giới cùng sửa từ máy khác nhau — SLA 72 giờ.",
            deep_link: `/sync/conflicts/${SYNC_CONFLICTS[0]!.conflict_id}`, subject: SYNC_CONFLICTS[0]!.conflict_id,
          }),
        ]
      : []),
    n(5, {
      kind: "org_deactivated", severity: "normal", topic: true, hoursAgo: 30, read: true,
      title: `Tổ chức đã ngừng hoạt động: ${inactiveOrg.name}`, body: "Tài khoản thuộc tổ chức đã được khóa tự động.",
      deep_link: `/orgs/${inactiveOrg.ref}`, subject: inactiveOrg.ref,
    }),
  ];
  INBOX.set(p.id, list);
  return list;
}

// --------------------------------------------------------------------------
// Đồng bộ ngay (US-SYNC-02) — giới hạn 1 lần/5 phút mỗi đích
// --------------------------------------------------------------------------
const NUDGED = new Map<string, number>();
export const MOCK_DEVICES = Array.from({ length: 8 }, (_, i) => ({
  device_id: `01927b3e-de71-7000-8000-${String(i + 1).padStart(12, "0")}`,
  registered_at: iso(60 - i * 5),
  platform: i % 3 === 0 ? ("ios" as const) : ("android" as const),
  app_version: `1.${2 + (i % 3)}.${i}`,
  revoked: i === 7,
  actor_ref: i === 0 ? "mock-xa-0001" : `mock-knv-${COMMUNES[i % COMMUNES.length]!.code}-1`,
  org_ref: ORGS.find((o) => o.kind === "extension_team" && o.admin_unit_code === COMMUNES[i % COMMUNES.length]!.code)?.ref ?? ORGS[0]!.ref,
  commune_code: COMMUNES[i % COMMUNES.length]!.code,
  last_seen_at: isoHours(-(i * 7 + 1)),
}));

export function nudge(p: MockPersona, body: unknown): Response {
  if (!hasRole(p, "kn_province_admin", "kn_national_admin", "device_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
  const input = (body ?? {}) as { device_id?: unknown; unit?: unknown; entities?: unknown };
  // KN US-SYNC-02: entities[] — tên hợp lệ, có entity chiều xuống; rỗng = tất cả.
  const entities = Array.isArray(input.entities) ? input.entities.filter((e): e is string => typeof e === "string") : [];
  if (entities.length > 30) return validation([{ field: "entities", reason: "tối đa 30 nhóm dữ liệu" }]);
  const unknown = entities.find((e) => !KNOWN_ENTITIES.has(e));
  if (unknown) return problem(422, "notify.nudge_entity_unknown", "Unknown entity", { errors: [{ field: "entities", reason: `không có entity ${unknown}` }] });
  const device = typeof input.device_id === "string" && input.device_id ? input.device_id : null;
  const unit = typeof input.unit === "string" && input.unit ? input.unit : null;
  if ((device ? 1 : 0) + (unit ? 1 : 0) !== 1) return validation([{ field: "device_id", reason: "Chọn đúng một trong mã thiết bị hoặc đơn vị" }]);
  let devices = 0;
  if (unit) {
    const province = PROVINCES.find((x) => x.code === unit);
    const commune = COMMUNES.find((x) => x.code === unit);
    if (!province && !commune) return validation([{ field: "unit", reason: "Mã đơn vị không còn hiệu lực" }]);
    if (!inScope(p, province?.code ?? commune!.province_code, commune?.code) && !p.me.scope.units.includes(unit)) return problem(403, "notify.out_of_scope", "Out of scope");
    devices = MOCK_DEVICES.filter((d) => !d.revoked && (d.commune_code === unit || d.commune_code.startsWith(unit))).length;
  } else {
    const d = MOCK_DEVICES.find((x) => x.device_id === device && !x.revoked);
    const tenantWide = hasRole(p, "device_admin", "platform_admin");
    if (!d || (!tenantWide && !inScope(p, d.commune_code.slice(0, 2), d.commune_code))) return problem(404, "notify.nudge_target_not_found", "Target not found");
    devices = 1;
  }
  const key = device ? `device:${device}` : `unit:${unit}`;
  const last = NUDGED.get(key);
  if (last && Date.now() - last < 300_000) {
    const retry = Math.ceil((300_000 - (Date.now() - last)) / 1000);
    return problem(429, "notify.nudge_rate_limited", "Rate limited", { retry_after_s: retry }, { "Retry-After": String(retry) });
  }
  NUDGED.set(key, Date.now());
  return json({ nudge_id: uuidv7(), target_kind: device ? "device" : "unit", target: device ?? unit, devices, entities }, 202);
}

// --------------------------------------------------------------------------
// Xung đột ranh giới (pf/geo)
// --------------------------------------------------------------------------
const GEO_CONFLICTS: (BoundaryConflict & { province_code: string })[] = COOPERATIVES.filter((c) => c.api.active && c.plots > 12)
  .slice(0, 5)
  .map((c, i) => {
    const plots = plotsOf(c);
    const a = plots[1]!;
    const b = plots[2]!;
    return {
      id: `01927b3e-9c0f-7000-8000-${String(i + 1).padStart(12, "0")}`,
      kind: i % 2 === 0 ? "same_coop_overlap" : "concurrent_edit",
      plot_ref: a.properties.ref,
      boundary_id: a.properties.boundary_id,
      other_plot_refs: [b.properties.ref],
      max_overlap_pct: i % 2 === 0 ? "3.10" : null,
      commune_code: c.api.admin_unit_code ?? "",
      owner_ref: c.api.ref,
      due_at: isoHours(72 - (i + 1) * 20),
      status: "open",
      detected_at: isoHours(-(i + 1) * 20),
      resolved_by: null,
      resolved_at: null,
      resolution_note: null,
      province_code: c.province_code,
    };
  });

function geoConflictVersions(c: (typeof GEO_CONFLICTS)[number]) {
  const coop = COOPERATIVES.find((x) => x.api.ref === c.owner_ref)!;
  const plots = plotsOf(coop);
  const a = plots.find((x) => x.properties.ref === c.plot_ref)!;
  const b = plots.find((x) => x.properties.ref === c.other_plot_refs[0])!;
  const ring = (a.geometry.coordinates[0] as number[][])[0]!;
  return [
    { role: "record" as const, boundary_id: uuidv7(), plot_ref: a.properties.ref, plot_code: a.properties.code, version: a.properties.version + 1, area_ha: a.properties.area_ha, current: false, geometry: { type: "Polygon" as const, coordinates: hexagon([ring[0]! + 0.0006, ring[1]!], 0.0013) } },
    { role: "current" as const, boundary_id: a.properties.boundary_id, plot_ref: a.properties.ref, plot_code: a.properties.code, version: a.properties.version, area_ha: a.properties.area_ha, current: true, geometry: a.geometry },
    { role: "overlapping" as const, boundary_id: b.properties.boundary_id, plot_ref: b.properties.ref, plot_code: b.properties.code, version: b.properties.version, area_ha: b.properties.area_ha, current: true, geometry: b.geometry },
  ];
}

// --------------------------------------------------------------------------
// Dự báo nhu cầu CGH (FN-09, phương pháp tạm cgh.forecast/v1)
// --------------------------------------------------------------------------
interface StoredForecast {
  run: MechForecastRun;
  results: MechForecastResult[];
}

const STAGES = [
  { stage: "lam_dat", machine: "MLD", norm: 120 },
  { stage: "gieo_sa", machine: "MGS", norm: 100 },
  { stage: "cham_soc", machine: "MPT", norm: 260 },
  { stage: "thu_hoach", machine: "MGD", norm: 90 },
] as const;

export function computeForecast(seasonCode: string, seasonYear: number, seq: number): StoredForecast {
  const startMonth = seasonCode === "DX" ? "11" : seasonCode === "HT" ? "04" : "08";
  const results: MechForecastResult[] = COOPERATIVES.filter((c) => c.api.active).flatMap((c, ci) =>
    STAGES.map((s, si): MechForecastResult => {
      const noData = c.province_code === "80";
      const base = Number((c.api.area_ha ?? "0").split(".")[0]);
      const growthX100 = ((ci * 37 + si * 11) % 41) * 100 - 2000; // −20,00 … +20,00 %
      const forecast = Math.round(base * (10000 + growthX100)) / 10000;
      const requiredX100 = Math.round((forecast * 100) / s.norm);
      const requiredWhole = Math.ceil(requiredX100 / 100);
      const capacity = Math.max(0, requiredWhole + ((ci + si) % 5) - 2);
      const d2 = (x: number) => `${Math.trunc(x / 100)}.${String(Math.abs(x % 100)).padStart(2, "0")}`;
      return {
        cooperative_ref: c.api.ref,
        commune_code: c.api.admin_unit_code,
        province_code: c.province_code,
        farm_stage: s.stage,
        machine_type: s.machine,
        base_area_ha: noData ? null : `${base}.0000`,
        growth_pct: noData ? null : `${growthX100 < 0 ? "-" : ""}${d2(Math.abs(growthX100))}`,
        forecast_area_ha: noData ? null : forecast.toFixed(4),
        norm_key: `cgh.norm_ha_per_machine:${s.machine}:${s.stage}`,
        norm: noData ? null : opaque({ value: String(s.norm), version: 1 }),
        required: noData ? null : d2(requiredX100),
        required_whole: noData ? null : requiredWhole,
        capacity_now: capacity,
        gap: noData ? null : capacity - requiredWhole,
        status: noData ? "no_data" : "ok",
        inputs: {
          history: noData ? {} : { [`${seasonCode}-${seasonYear - 1}`]: `${base}.0000`, [`${seasonCode}-${seasonYear - 2}`]: `${Math.round(base * 0.95)}.0000` },
          rates: noData ? {} : { [`${seasonCode}-${seasonYear - 1}`]: "5.26" },
          raw_growth: noData ? null : d2(growthX100),
          capped: Math.abs(growthX100) >= 2000,
          norm_at: `${seasonYear}-${startMonth}-01`,
        },
        seq: ci * STAGES.length + si + 1,
      };
    }),
  );
  return {
    results,
    run: {
      id: `01927b3e-f0c5-7000-8000-${String(seasonYear * 100 + seq).padStart(12, "0")}`,
      season_code: seasonCode,
      season_year: seasonYear,
      season_key: `${seasonCode}-${seasonYear}`,
      reference_date: `${seasonYear}-${startMonth}-01`,
      rule_version: "cgh.forecast/v1",
      method_provisional: true,
      method: {
        formula: "diện tích cùng vụ năm trước × (1 + tăng trưởng TB tối đa 3 vụ cùng loại, chặn ±20 %) ÷ định mức tại ngày bắt đầu vụ",
        max_history: 3,
        growth_cap: "20",
        note: "Phương pháp TẠM — cần Cục KTHT (DCRD) xác nhận trước khi dùng chính thức (docs/dev/cgh-forecast.md).",
      },
      pilot_stages: STAGES.map((s) => s.stage),
      result_count: results.length,
      missing_norms: [],
      computed_at: iso(seq === 1 ? 2 : 0),
      seq,
    },
  };
}

export const FORECASTS: StoredForecast[] = [computeForecast("DX", 2026, 1)];

const cghRead = (p: MockPersona) => p.me.org?.kind === "dcrd" || hasRole(p, "platform_admin", "cgh_viewer", "cgh_admin");

// --------------------------------------------------------------------------
// pf/reports — mẫu, việc xuất bất đồng bộ, URL tải
// --------------------------------------------------------------------------
const col = (key: string, title: string) => ({ key, title });
export const REPORT_TEMPLATES: ReportTemplate[] = [
  { key: "kn.sowing_progress", version: 0, builtin: true, status: "active", definition: { title: "Tiến độ gieo sạ theo đơn vị (FR-01)", source: "extension.sowing", grouping: "commune", columns: [col("unit_name", "Đơn vị"), col("sown_area_ha", "Diện tích gieo sạ (ha)"), col("reported", "Đã báo cáo")], formats: ["xlsx", "pdf"], provisional_layout: true, form_note: "Bố cục tạm — chưa có mẫu biểu chính thức của Bộ." } },
  { key: "kn.growth_pest", version: 0, builtin: true, status: "active", definition: { title: "Tình hình sinh trưởng và sâu bệnh (FR-02)", source: "extension.growth_pest", grouping: "commune", columns: [col("unit_name", "Đơn vị"), col("pest", "Sâu bệnh"), col("affected_ha", "Diện tích nhiễm (ha)")], formats: ["xlsx", "pdf"], provisional_layout: true } },
  { key: "kn.program_progress", version: 0, builtin: true, status: "active", definition: { title: "Tiến độ Đề án 1 triệu ha (FR-03)", source: "extension.program_progress", grouping: "province", columns: [col("unit_name", "Tỉnh"), col("area_ha", "Diện tích tham gia (ha)")], formats: ["xlsx", "pdf"], provisional_layout: true } },
  { key: "kn.tasks_sla", version: 0, builtin: true, status: "active", definition: { title: "Nhiệm vụ hỗ trợ và SLA", source: "extension.tasks_sla", grouping: "commune", columns: [col("unit_name", "Đơn vị"), col("open", "Đang mở"), col("overdue", "Quá hạn")], formats: ["xlsx"] } },
  { key: "cgh.summary", version: 0, builtin: true, status: "active", definition: { title: "Tổng hợp cân đối máy theo tỉnh (FN-10)", source: "mechanization.summary", grouping: "province", columns: [col("unit_name", "Tỉnh"), col("required", "Nhu cầu"), col("capacity", "Năng lực")], formats: ["xlsx", "pdf"] } },
  { key: "cgh.shortage_by_coop", version: 0, builtin: true, status: "active", definition: { title: "Thiếu hụt máy theo HTX (FN-10)", source: "mechanization.shortage_by_coop", grouping: "cooperative", columns: [col("coop_code", "Mã HTX"), col("coop_name", "HTX"), col("gap", "Chênh lệch")], formats: ["xlsx", "pdf"] } },
  { key: "cgh.season_compare", version: 0, builtin: true, status: "active", definition: { title: "Đối chiếu vụ A – vụ B (FN-10)", source: "mechanization.season_compare", grouping: "province", columns: [col("unit_name", "Tỉnh"), col("a", "Vụ A"), col("b", "Vụ B")], formats: ["xlsx"] } },
  // HTX US-DASH-02: sản lượng HTX × vụ × giống (nguồn cropcycle.production).
  { key: "htx.production", version: 0, builtin: true, status: "active", definition: { title: "Báo cáo sản lượng HTX (dự kiến – thực tế)", source: "cropcycle.production", grouping: "cooperative", columns: [col("cooperative_name", "HTX"), col("season", "Vụ"), col("variety_name", "Giống lúa"), col("households", "Số nông hộ"), col("area_ha", "Diện tích (ha)"), col("expected_t", "Sản lượng dự kiến (tấn)"), col("actual_t", "Sản lượng thực tế (tấn)"), col("sop_progress_pct", "Tiến độ SOP (%)")], formats: ["xlsx", "pdf"] } },
];

/** Mẫu mới nhất theo khóa (mẫu hệ thống hoặc phiên bản của tenant). */
export function latestTemplate(key: string): ReportTemplate | undefined {
  return REPORT_TEMPLATES.filter((t) => t.key === key).sort((a, b) => b.version - a.version)[0];
}

export function templateKeys(): string[] {
  return Array.from(new Set(REPORT_TEMPLATES.map((t) => t.key)));
}

const KN_ROLES = ["kn_officer", "kn_province_admin", "kn_national_admin", "platform_admin"];
const CGH_ROLES = ["cgh_viewer", "cgh_admin", "platform_admin"];
const canExport = (p: MockPersona, t: ReportTemplate) => {
  if (t.key.startsWith("cgh.")) return hasRole(p, ...CGH_ROLES) || p.me.org?.kind === "dcrd";
  if (t.key.startsWith("htx.")) return hasRole(p, "coop_manager", ...KN_ROLES);
  return hasRole(p, ...KN_ROLES);
};
/** Quản lý HTX thuần: số liệu HTX chỉ của HTX mình (pf/reports coopOnly). */
const coopOnly = (p: MockPersona) => hasRole(p, "coop_manager") && !hasRole(p, ...KN_ROLES);

interface StoredExport {
  exp: ReportExport;
  owner: string;
  readyAt: number;
}
const EXPORTS = new Map<string, StoredExport>();
const QUOTA = new Map<string, number>();
export const DAILY_EXPORT_QUOTA = 50;

function exportView(s: StoredExport): ReportExport {
  if (s.exp.status === "queued" && Date.now() >= s.readyAt) {
    const unit = s.exp.params.unit_code ?? "";
    const communes = COMMUNES.filter((c) => !unit || c.code === unit || c.province_code === unit);
    const htx = s.exp.template_key.startsWith("htx.");
    // Sản lượng HTX: còn lô đang canh tác → tạm tính; số dòng = HTX × vụ × giống giả lập.
    const provisional = s.exp.template_key.startsWith("kn.") ? communes.some((c) => UNREPORTED_COMMUNES.has(c.code)) : htx;
    s.exp = {
      ...s.exp,
      status: "completed",
      row_count: htx ? (s.exp.params.variety_code ? 1 : 3) : communes.length,
      provisional,
      size_bytes: 18_432 + communes.length * 211,
      sha256: "9f2c".padEnd(64, "0"),
      error_code: null,
      completed_at: nowIso(),
    };
  }
  return s.exp;
}

function mockFile(s: StoredExport): { blob: Blob; name: string; type: string } {
  const title = s.exp.title;
  const note = s.exp.provisional ? "Số liệu tạm tính — còn đơn vị chưa gửi báo cáo kỳ này." : "";
  if (s.exp.format === "pdf") {
    // PDF 1.4 tối giản (chỉ ASCII) để trình duyệt mở được; bản thật do pf/reports dựng với phông tiếng Việt.
    const text = `AgriGreen - ${s.exp.template_key} (du lieu gia lap)`;
    const stream = `BT /F1 14 Tf 60 780 Td (${text}) Tj ET`;
    const objs = [
      "<< /Type /Catalog /Pages 2 0 R >>",
      "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
      "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
      `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
      "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    ];
    let pdf = "%PDF-1.4\n";
    const offsets: number[] = [];
    objs.forEach((o, i) => {
      offsets.push(pdf.length);
      pdf += `${i + 1} 0 obj\n${o}\nendobj\n`;
    });
    const xref = pdf.length;
    pdf += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offsets.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}`;
    pdf += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;
    return { blob: new Blob([pdf], { type: "application/pdf" }), name: `${s.exp.template_key}-gia-lap.pdf`, type: "application/pdf" };
  }
  // Giả lập không đóng gói .xlsx (cần thư viện zip) — trả CSV UTF-8 có BOM, tên tệp ghi rõ.
  const csv = `﻿${title}\r\n${note}\r\nĐơn vị,Giá trị\r\n${COMMUNES.slice(0, 4).map((c) => `${c.name},${c.code}`).join("\r\n")}\r\n`;
  return { blob: new Blob([csv], { type: "text/csv" }), name: `${s.exp.template_key}-gia-lap.csv`, type: "text/csv" };
}

function requestExport(p: MockPersona, body: unknown): Response {
  const input = (body ?? {}) as { template_key?: unknown; format?: unknown; params?: ReportParams };
  const key = typeof input.template_key === "string" ? input.template_key : "";
  const format = input.format === "pdf" ? "pdf" : input.format === "xlsx" ? "xlsx" : null;
  if (!format) return validation([{ field: "format", reason: "Chọn xlsx hoặc pdf" }]);
  const t = latestTemplate(key);
  if (!t || !canExport(p, t)) return problem(404, "reports.template_not_found", "Template not found");
  if (t.status === "retired") return problem(409, "reports.template_retired", "Template retired");
  if (!t.definition.formats.includes(format)) return problem(422, "reports.format_unsupported", "Format unsupported");
  const params: ReportParams = { ...(input.params ?? {}) };
  if (t.key.startsWith("htx.")) {
    // Như pf/reports: năm vụ 2000–2100; quản lý HTX chỉ HTX của mình (gán cooperative_ref).
    if (params.season_year !== undefined && (params.season_year < 2000 || params.season_year > 2100)) {
      return validation([{ field: "season_year", reason: "năm vụ không hợp lệ" }]);
    }
    if (coopOnly(p)) {
      const own = p.me.org?.ref;
      if (!own) return problem(403, "reports.out_of_scope", "Out of scope");
      if (params.cooperative_ref && params.cooperative_ref !== own) return problem(403, "reports.out_of_scope", "Out of scope");
      params.cooperative_ref = own;
    }
  } else if (params.unit_code) {
    const province = PROVINCES.find((x) => x.code === params.unit_code);
    const commune = COMMUNES.find((x) => x.code === params.unit_code);
    if (!province && !commune) return problem(422, "reports.unit_invalid", "Unit invalid");
    if (!inScope(p, province?.code ?? commune!.province_code, commune?.code) && !p.me.scope.units.includes(params.unit_code)) {
      return problem(403, "reports.out_of_scope", "Out of scope");
    }
  } else if (!p.me.scope.all && p.me.org?.admin_unit_code) {
    params.unit_code = p.me.org.admin_unit_code;
  }
  const used = QUOTA.get(p.id) ?? 0;
  if (used >= DAILY_EXPORT_QUOTA) {
    const midnight = new Date();
    midnight.setUTCHours(17, 0, 0, 0); // 00:00 giờ Việt Nam
    if (midnight.getTime() <= Date.now()) midnight.setUTCDate(midnight.getUTCDate() + 1);
    const retry = Math.ceil((midnight.getTime() - Date.now()) / 1000);
    return problem(429, "reports.export_quota_exceeded", "Daily export quota exceeded", { retry_after_s: retry }, { "Retry-After": String(retry) });
  }
  QUOTA.set(p.id, used + 1);
  const exp: ReportExport = {
    id: uuidv7(),
    template_key: t.key,
    template_version: t.version,
    title: t.definition.title,
    format,
    params,
    status: "queued",
    trigger: "api",
    schedule_id: null,
    requested_by: p.me.actor_ref,
    requested_at: nowIso(),
    row_count: null,
    provisional: null,
    size_bytes: null,
    sha256: null,
    error_code: null,
    completed_at: null,
  };
  EXPORTS.set(exp.id, { exp, owner: p.id, readyAt: Date.now() + 1500 });
  return json(exp, 202, { Location: `/v1/reports/exports/${exp.id}` });
}

/** Cho kiểm thử: đặt lại hạn mức xuất của persona. */
export function resetExportQuota(personaId?: string) {
  if (personaId) QUOTA.delete(personaId);
  else QUOTA.clear();
}

// --------------------------------------------------------------------------
// Bảng định tuyến
// --------------------------------------------------------------------------
export const opsRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/dashboard$/, handler: ({ persona, url }) => unifiedDashboard(persona, url) },

  // --- pf/geo: ranh giới theo cấp (G-05) ---
  {
    method: "GET",
    pattern: /^\/v1\/geo\/admin-boundaries$/,
    handler: ({ url }) => {
      const level = url.searchParams.get("level");
      const parent = url.searchParams.get("parent");
      if (level !== "province" && level !== "commune") return validation([{ field: "level", reason: "province hoặc commune" }]);
      if (level === "commune" && !parent) return validation([{ field: "parent", reason: "Bắt buộc với level=commune" }]);
      const codes = level === "province" ? PROVINCES.map((p) => p.code) : COMMUNES.filter((c) => c.province_code === parent).map((c) => c.code);
      const features = codes.filter((c) => c !== MISSING_BOUNDARY).map((c) => {
        return boundaryOf(c)!;
      });
      return new Response(JSON.stringify({ type: "FeatureCollection", features, missing: codes.filter((c) => c === MISSING_BOUNDARY) }), {
        status: 200,
        headers: { "Content-Type": "application/geo+json" },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/conflicts$/,
    handler: ({ persona, url }) => {
      const status = url.searchParams.get("status");
      const list = GEO_CONFLICTS.filter((c) => inScope(persona, c.province_code, c.commune_code) && (!status || c.status === status)).map((c) => omit(c, "province_code"));
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/conflicts\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      const c = GEO_CONFLICTS.find((x) => x.id === params[0]);
      if (!c || !inScope(persona, c.province_code, c.commune_code)) return problem(404, "geo.conflict_not_found", "Not found");
      return json({ ...omit(c, "province_code"), versions: geoConflictVersions(c) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/geo\/conflicts\/([\w-]+)\/resolve$/,
    handler: ({ persona, params, body }) => {
      if (!hasRole(persona, "plot_editor", "kn_officer", "kn_province_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const c = GEO_CONFLICTS.find((x) => x.id === params[0]);
      if (!c || !inScope(persona, c.province_code, c.commune_code)) return problem(404, "geo.conflict_not_found", "Not found");
      const note = reasonOf(body, "note");
      if (note.length < 10) return validation([{ field: "note", reason: "Ghi chú xử lý tối thiểu 10 ký tự" }]);
      if (c.status === "resolved") return problem(409, "geo.conflict_state", "Already resolved");
      Object.assign(c, { status: "resolved", resolved_by: persona.me.actor_ref, resolved_at: nowIso(), resolution_note: note });
      return json(omit(c, "province_code"));
    },
  },

  // --- pf/extension: chi tiết nhiệm vụ, chỉ đạo khẩn ---
  {
    method: "GET",
    pattern: /^\/v1\/extension\/tasks\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      const t = TASKS.find((x) => x.task_id === params[0]);
      if (!t || !inScope(persona, t.province_code, t.commune_code)) return problem(404, "extension.task_not_found", "Not found");
      const ev = (seq: number, action: "created" | "assigned" | "accepted" | "escalated" | "resolved", at: string, note: string | null) => ({
        event_id: `${t.task_id.slice(0, -2)}${String(seq).padStart(2, "0")}`, action, status_after: t.status, assignee_ref: t.assignee_ref ?? null,
        due_at: t.due_at, note, actor_ref: t.created_by, event_time: at, recorded_at: at, seq,
      });
      const events = [ev(1, "created", t.created_at, null)];
      if (t.assignee_ref) events.push(ev(2, "assigned", t.created_at, `Giao cho ${t.assignee_name ?? "cán bộ xã"}`));
      if (t.overdue && t.status !== "resolved") events.push(ev(3, "escalated", t.due_at, "Quá hạn SLA — chuyển lên TTKN tỉnh"));
      if (t.resolved_at) events.push(ev(4, "resolved", t.resolved_at, "Đã hướng dẫn tại ruộng"));
      return json({ task: t, events });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/directives$/,
    handler: ({ persona, url }) => {
      const mine = url.searchParams.get("mine") === "true";
      const list = DIRECTIVES.filter((d) => directiveVisible(d, persona) && (!mine || d.targets.some((t) => t.actor_ref === persona.me.actor_ref)))
        .sort((a, b) => Date.parse(b.d.issued_at) - Date.parse(a.d.issued_at))
        .map((d) => directiveView(d, persona));
      return json(paginate(list, url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/directives$/,
    handler: ({ persona, body }) => {
      if (!isSupervisor(persona)) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as { title?: unknown; body?: unknown; severity?: unknown; audience_units?: unknown; expires_at?: unknown };
      const title = typeof input.title === "string" ? input.title.trim() : "";
      const text = typeof input.body === "string" ? input.body.trim() : "";
      const units = Array.isArray(input.audience_units) ? input.audience_units.filter((u): u is string => typeof u === "string") : [];
      const errors: { field: string; reason: string }[] = [];
      if (title.length < 5 || title.length > 200) errors.push({ field: "title", reason: "Tiêu đề từ 5 đến 200 ký tự" });
      if (text.length < 10) errors.push({ field: "body", reason: "Nội dung tối thiểu 10 ký tự" });
      if (units.length === 0) errors.push({ field: "audience_units", reason: "Chọn ít nhất một đơn vị nhận" });
      if (errors.length) return validation(errors);
      for (const u of units) {
        const province = PROVINCES.find((x) => x.code === u);
        const commune = COMMUNES.find((x) => x.code === u);
        if (!province && !commune) return validation([{ field: "audience_units", reason: `Mã đơn vị ${u} không còn hiệu lực` }]);
        if (!inScope(persona, province?.code ?? commune!.province_code, commune?.code) && !persona.me.scope.units.includes(u)) {
          return problem(403, "extension.out_of_scope", "Out of scope");
        }
      }
      const targets = targetsFor(units);
      if (targets.length === 0) return problem(422, "extension.no_directive_targets", "No targets");
      const md: MockDirective = {
        d: {
          directive_id: uuidv7(),
          title,
          body: text,
          severity: input.severity === "normal" ? "normal" : "urgent",
          audience_units: units,
          province_codes: Array.from(new Set(units.map((u) => u.slice(0, 2)))),
          issuer_ref: persona.me.actor_ref,
          issued_at: nowIso(),
          expires_at: typeof input.expires_at === "string" ? input.expires_at : null,
        },
        targets,
        acks: new Map(),
      };
      DIRECTIVES.unshift(md);
      return json(directiveView(md, persona), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/directives\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      const md = DIRECTIVES.find((d) => d.d.directive_id === params[0]);
      if (!md || !directiveVisible(md, persona)) return problem(404, "extension.directive_not_found", "Not found");
      const pending = isSupervisor(persona) ? md.targets.filter((t) => !md.acks.has(t.actor_ref)) : null;
      return json({ directive: directiveView(md, persona), pending_targets: pending });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/directives\/([\w-]+)\/ack$/,
    handler: ({ persona, params }) => {
      const md = DIRECTIVES.find((d) => d.d.directive_id === params[0]);
      if (!md || !directiveVisible(md, persona)) return problem(404, "extension.directive_not_found", "Not found");
      if (!md.targets.some((t) => t.actor_ref === persona.me.actor_ref)) return problem(403, "extension.not_directive_target", "Not a target");
      if (!md.acks.has(persona.me.actor_ref)) md.acks.set(persona.me.actor_ref, nowIso());
      return json(directiveView(md, persona));
    },
  },

  // --- pf/content: chi tiết tài liệu, danh mục cá nhân ---
  {
    method: "GET",
    pattern: /^\/v1\/documents\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      const d = docVisible(persona, params[0]);
      if (!d) return problem(404, "content.document_not_found", "Not found");
      const supplements = DOCUMENTS.filter((x) => x.parent_ref === d.ref && docVisible(persona, x.ref)).map((x) => ({
        ref: x.ref, title: x.title ?? "", province_code: x.audience.province_code, published_at: x.published_at,
      }));
      return json({
        document: d,
        version: {
          id: `${d.ref.slice(0, -4)}0001`, version_no: d.published_version_no ?? 1, status: "published", title: d.title ?? "", summary: d.summary ?? "",
          body: `${d.summary ?? ""}\n\n(Nội dung đầy đủ — dữ liệu giả lập.)`, body_format: "markdown", category: d.category, tags: d.tags,
          author_ref: d.publisher_ref ?? "mock-admin-other", recorded_at: d.published_at ?? nowIso(), attachments: [],
        },
        supplements,
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/library$/,
    handler: ({ persona, url }) => {
      const items = Array.from(libraryOf(persona).entries())
        .map(([ref, e]) => libraryItem(ref, e))
        .sort((a, b) => Date.parse(b.saved_at) - Date.parse(a.saved_at));
      return json(paginate(items, url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/documents\/([\w-]+)\/save$/,
    handler: ({ persona, params, body }) => {
      const d = docVisible(persona, params[0]);
      if (!d) return problem(404, "content.document_not_found", "Not found");
      const note = typeof (body as { note?: unknown } | undefined)?.note === "string" ? ((body as { note: string }).note || null) : null;
      const lib = libraryOf(persona);
      const e = { saved_at: lib.get(d.ref)?.saved_at ?? nowIso(), note };
      lib.set(d.ref, e);
      return json(libraryItem(d.ref, e));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/documents\/([\w-]+)\/unsave$/,
    handler: ({ persona, params }) => {
      const lib = libraryOf(persona);
      if (!params[0] || !lib.has(params[0])) return problem(404, "content.library_item_not_found", "Not saved");
      lib.delete(params[0]);
      return json({ document_ref: params[0], saved: false, removed_at: nowIso() });
    },
  },

  // --- pf/notify ---
  {
    method: "GET",
    pattern: /^\/v1\/notifications$/,
    handler: ({ persona, url }) => {
      const unread = url.searchParams.get("unread") === "true";
      const list = inboxOf(persona)
        .filter((n) => !unread || !n.read_at)
        .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/notifications\/unread-count$/,
    handler: ({ persona }) => json({ unread: inboxOf(persona).filter((n) => !n.read_at).length }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/notifications\/read-all$/,
    handler: ({ persona }) => {
      const now = nowIso();
      let marked = 0;
      for (const n of inboxOf(persona)) {
        if (!n.read_at) {
          n.read_at = now;
          marked++;
        }
      }
      return json({ marked });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/notifications\/([\w-]+)\/read$/,
    handler: ({ persona, params }) => {
      const n = inboxOf(persona).find((x) => x.id === params[0]);
      if (!n) return problem(404, "notify.notification_not_found", "Not found");
      n.read_at ??= nowIso();
      return json(n);
    },
  },
  { method: "POST", pattern: /^\/v1\/sync\/nudge$/, handler: ({ persona, body }) => nudge(persona, body) },

  // --- pf/mechanization: dự báo (chỉ đọc) ---
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/forecasts$/,
    handler: ({ persona, url }) => {
      if (!cghRead(persona)) return problem(403, "forbidden", "Forbidden");
      const code = url.searchParams.get("season_code");
      const year = Number(url.searchParams.get("season_year")) || null;
      return json({
        items: FORECASTS.map((f) => f.run)
          .filter((r) => (!code || r.season_code === code) && (!year || r.season_year === year))
          .sort((a, b) => Date.parse(b.computed_at) - Date.parse(a.computed_at)),
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/forecasts\/([\w-]+)$/,
    handler: ({ persona, params, url }) => {
      if (!cghRead(persona)) return problem(403, "forbidden", "Forbidden");
      const f = FORECASTS.find((x) => x.run.id === params[0]);
      if (!f) return problem(404, "mechanization.forecast_not_found", "Not found");
      const q = (k: string) => url.searchParams.get(k);
      const results = f.results.filter(
        (r) =>
          (!q("province_code") || r.province_code === q("province_code")) &&
          (!q("commune_code") || r.commune_code === q("commune_code")) &&
          (!q("cooperative_ref") || r.cooperative_ref === q("cooperative_ref")) &&
          (!q("farm_stage") || r.farm_stage === q("farm_stage")) &&
          (!q("machine_type") || r.machine_type === q("machine_type")),
      );
      const byStage: Record<string, number> = {};
      let area = "0.0000";
      for (const r of results) {
        if (r.required_whole !== null) byStage[r.farm_stage] = (byStage[r.farm_stage] ?? 0) + r.required_whole;
        if (r.forecast_area_ha && r.farm_stage === "lam_dat") area = addDecimal4(area, r.forecast_area_ha);
      }
      return json({
        run: f.run,
        results,
        summary: { forecast_area_ha: area, required_by_stage: Object.fromEntries(Object.entries(byStage).map(([k, v]) => [k, String(v)])) },
      });
    },
  },

  // --- pf/reports ---
  {
    method: "GET",
    pattern: /^\/v1\/reports\/templates$/,
    handler: ({ persona }) =>
      json({ items: templateKeys().map((k) => latestTemplate(k)!).filter((t) => t.status === "active" && canExport(persona, t)) }),
  },
  { method: "POST", pattern: /^\/v1\/reports\/exports$/, handler: ({ persona, body }) => requestExport(persona, body) },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/exports$/,
    handler: ({ persona, url }) => {
      // Lịch sử của CHÍNH người gọi, mới nhất trước; cursor = id việc cuối trang trước (UUIDv7 tăng theo thời gian).
      const limit = Math.min(Math.max(Number(url.searchParams.get("limit") ?? 50) || 50, 1), 200);
      const cursor = url.searchParams.get("cursor");
      const mine = Array.from(EXPORTS.values())
        .filter((s) => s.owner === persona.id)
        .map(exportView)
        .sort((a, b) => (a.id < b.id ? 1 : -1))
        .filter((e) => !cursor || e.id < cursor);
      const page = mine.slice(0, limit).map((e) => ({ ...e, downloadable: e.status === "completed" }));
      return json({ items: page, next_cursor: mine.length > limit ? page[page.length - 1]!.id : null });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/exports\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      const s = EXPORTS.get(params[0] ?? "");
      if (!s || (s.owner !== persona.id && !persona.me.scope.all)) return problem(404, "reports.export_not_found", "Not found");
      return json(exportView(s));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/exports\/([\w-]+)\/download$/,
    handler: ({ persona, params }) => {
      const s = EXPORTS.get(params[0] ?? "");
      if (!s || (s.owner !== persona.id && !persona.me.scope.all)) return problem(404, "reports.export_not_found", "Not found");
      const exp = exportView(s);
      if (exp.status !== "completed") return problem(409, "reports.export_not_ready", "Not ready");
      const file = mockFile(s);
      // Giả lập: URL blob: cục bộ thay cho URL có chữ ký của kho đối tượng (hạn 10 phút).
      let url = `data:${file.type},gia-lap`;
      try {
        url = URL.createObjectURL(file.blob);
      } catch {
        // môi trường kiểm thử (jsdom) không có createObjectURL cho Blob — dùng data: URL
      }
      return json({ export_id: exp.id, url, expires_at: isoHours(1 / 6), file_name: file.name, content_type: file.type, sha256: exp.sha256 ?? "" });
    },
  },
];
