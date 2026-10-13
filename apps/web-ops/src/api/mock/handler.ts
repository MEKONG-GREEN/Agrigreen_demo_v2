// ============================================================================
// BACKEND GIẢ LẬP (NEXT_PUBLIC_API_MOCK=1). Thay `fetch` của openapi-fetch nên toàn bộ
// middleware thật (Bearer, Idempotency-Key, parse problem+json) vẫn chạy.
// Đường dẫn và hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml và admin.yaml.
// Mô phỏng quy tắc backend để UI thể hiện đúng: lọc theo phạm vi (ADR-0009), POST thiếu
// Idempotency-Key → 400, phát lại cùng khóa → Idempotent-Replayed, tự phê duyệt → 403
// self_approval_forbidden (R-56), mọi route Admin API cần MFA → 403 mfa_required.
// Route mới của đợt hoàn thiện G1: ops.ts (Application) và admin-ops.ts (Admin).
// ============================================================================
import type { AppGet, ParamVersion, PlotFeature } from "../types";
import { uuidv7 } from "../idempotency";
import {
  ADMIN_UNITS,
  boundaryOf,
  CATALOGS,
  COMMUNES,
  COOPERATIVES,
  DOCUMENTS,
  findCoop,
  householdsOf,
  orgEvents,
  ORG_EVENTS,
  ORGS,
  PARAM_VERSIONS,
  plotsOf,
  PROVINCES,
  TASKS,
  ACTOR_NAMES,
} from "./data";
import { adminOpsRoutes, headerKey, parseCsv } from "./admin-ops";
import { hasRole, inScope, isZip, json, norm, omit, pageSlice, paginate, problem, scopeUnits, type Route } from "./http";
import { coverage as mechCoverage, dashboard as mechDashboard, findRun, panel as mechPanel, RUNS } from "./mechanization";
import { communeKpi, MISSING_BOUNDARY, opsRoutes, resolveUnit, sumKpi } from "./ops";
import { rosterRoutes } from "./roster";
import { gisAdminRoutes, gisAppRoutes } from "./gis";
import { simulationRoutes } from "./simulation";
import { waterwayAdminRoutes, waterwayAppRoutes } from "./waterway";
import { cropcycleAdminRoutes, cropcycleAppRoutes } from "./cropcycle";
import { htxAdminRoutes, htxAppRoutes } from "./htx";
import { htxGapAdminRoutes, htxGapAppRoutes } from "./htx-gaps";
import { personaFromAuthHeader, type MockPersona } from "./personas";
import { checkDisplayProposal, DISPLAY_DEFAULTS, displayConfig, isColorKey, isPercentKey, PERCENT_TITLE, RESTORE_REASON } from "./display";
import { areaSyncRoutes } from "./areasync";
import { htxAnchorRoutes } from "./htx-anchors";
import { syncMonitorRoutes } from "./syncmonitor";
import { metricsRoutes } from "./metrics";
import { mrvAdminRoutes, mrvAppRoutes } from "./mrv";
import { collectionRoutes } from "./collection";
import { contextProblem, knAppRoutes, personaForContext } from "./kn";
import { formsAdminRoutes, formsAppRoutes } from "./forms/routes";
import { workScheduleAppRoutes } from "./kn-schedules";
import { priceConfigRoutes } from "./kn-price";
import { reportCycleAppRoutes } from "./kn-reports/routes";
import { knAdminRoutes, supportGuard, supportRecord } from "./kn-admin";
import { knCollectRoutes } from "./kn-collect";
import { priceItemRoutes } from "./kn-price-items";
import { pricePointRoutes } from "./kn-price-points";
import { cntsCatalogAdminRoutes, cntsCatalogAppRoutes } from "./kn-cnts-catalogs";
import { hrmRoutes } from "./kn-hrm";
import { khtcRoutes } from "./kn-khtc";
import { priceBoardRoutes } from "./kn-price-boards";
import { priceReportRoutes } from "./kn-price-reports";
import { priceTicketRoutes } from "./kn-price-tickets";
import { cntsPeriodRoutes } from "./kn-cnts-periods";
import { khtcPeriodRoutes } from "./kn-khtc-periods";
import { cntsOutbreakRoutes } from "./kn-cnts-outbreak";
import { khtcSourceRoutes } from "./kn-khtc-sources";
import { hrmStaffRoutes } from "./kn-hrm-staff";
import { apiClientAdminRoutes, meteringAdminRoutes } from "./g5";

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

// --------------------------------------------------------------------------
// GET /v1/extension/dashboard (route cũ của pf/extension, vẫn có trong hợp đồng)
// --------------------------------------------------------------------------
type ExtDashboardBody = AppGet<"/v1/extension/dashboard">;

function extensionDashboard(p: MockPersona, url: URL): ExtDashboardBody | Response {
  const period = url.searchParams.get("period") ?? new Date().toISOString().slice(0, 7);
  const asOf = new Date(Date.now() - 40 * 60_000).toISOString();
  const r = resolveUnit(p, url.searchParams.get("unit"));
  if (r instanceof Response) return r;
  if (r.commune) {
    return { unit: r.commune.code, level: "commune", period, as_of: asOf, total: communeKpi(r.commune.code), period_closed: false, closed_provinces: [], children: [] };
  }
  const children = r.province
    ? COMMUNES.filter((c) => c.province_code === r.province!.code).map((c) => ({ unit: c.code, kpi: communeKpi(c.code) }))
    : PROVINCES.map((pr) => ({ unit: pr.code, kpi: sumKpi(COMMUNES.filter((c) => c.province_code === pr.code).map((c) => communeKpi(c.code))) }));
  return {
    unit: r.province?.code ?? null,
    level: r.province ? "province" : "national",
    period,
    as_of: asOf,
    total: sumKpi(children.map((c) => c.kpi)),
    period_closed: false,
    closed_provinces: ["92"],
    children,
  };
}

// --------------------------------------------------------------------------
// Trạng thái thửa sau khi gán (mock giữ trong bộ nhớ tab)
// --------------------------------------------------------------------------
const PLOT_STATE = new Map<string, Pick<PlotFeature["properties"], "cooperative_ref" | "status" | "needs_review">>();

function plotsWithState(coop: (typeof COOPERATIVES)[number]): PlotFeature[] {
  return plotsOf(coop).map((p) => {
    const s = PLOT_STATE.get(p.properties.ref);
    return s ? { ...p, properties: { ...p.properties, ...s, revision: p.properties.revision + 1 } } : p;
  });
}

function findPlot(ref: string | undefined): { plot: PlotFeature; index: number; coop: (typeof COOPERATIVES)[number] } | undefined {
  for (const coop of COOPERATIVES) {
    const list = plotsWithState(coop);
    const index = list.findIndex((p) => p.properties.ref === ref);
    if (index >= 0) return { plot: list[index]!, index, coop };
  }
  return undefined;
}

// --------------------------------------------------------------------------
// Bảng định tuyến Application API
// --------------------------------------------------------------------------
const cghAllowed = (p: MockPersona) => p.me.org?.kind === "dcrd" || hasRole(p, "platform_admin", "cgh_viewer", "cgh_admin");

const appRoutes: Route[] = [
  // Hoàn thiện App HTX (G2) — đặt trước để ghi đè danh sách nông hộ (lọc ?phone=).
  ...htxGapAppRoutes,
  // --- pf/party: người gọi + phạm vi ---
  { method: "GET", pattern: /^\/v1\/me$/, handler: ({ persona }) => json(persona.me) },
  // --- KN v7.7 (hợp đồng tạm P1/P2/P4): ngữ cảnh phiên, menu theo cấu hình, số đếm ---
  ...knAppRoutes,
  // --- KN v7.7 P3: mẫu báo cáo dạng lưới (chọn mẫu, layout, mẫu Excel, tính thử) ---
  ...formsAppRoutes,
  // --- KN v7.7 P5: lịch công tác tuần (/v1/extension/work-schedules*) ---
  ...workScheduleAppRoutes,
  // --- Báo giá W11.74: lịch báo giá, giờ khóa, nhắc, ngưỡng (/v1/price/calendar*, /v1/price/thresholds*) ---
  ...priceConfigRoutes,
  // --- KN v7.7 P4: vòng đời báo cáo (phát hành, công việc, nộp, rà soát, kỳ, tổng hợp, dashboard, Chi cục) ---
  ...reportCycleAppRoutes,
  // --- KN v7.7 P7: thu thập dữ liệu Cấp Xã (/v1/kn/cooperatives*, machine-counts*, plots, materials*) ---
  ...knCollectRoutes,
  // --- cap/price W11.73: mặt hàng theo thời kỳ (/v1/price/items*, groups*, assignments*, point-items, item-sets*, proposals*) ---
  ...priceItemRoutes,
  ...pricePointRoutes, // W11.72 xã báo giá + phân công
  // --- W11.41: danh mục CNTS (/v1/masterdata/catalogs/cnts_*, đọc theo ngày ?at=) — trước route danh mục chung ---
  ...cntsCatalogAppRoutes,
  // --- dom/hrm W11.21: danh mục nhân sự HCTH (/v1/hrm/*) ---
  ...hrmRoutes,
  ...khtcRoutes, // W11.61 Danh mục KHTC, CT/DA, Kế hoạch năm (/v1/kn/khtc/*)
  ...priceTicketRoutes, // W11.75 phiếu báo giá + xác minh
  ...cntsPeriodRoutes, // W11.42 biểu kỳ Cấp Xã CNTS (/v1/extension/cnts/*, /v1/forms/* profile kn_cnts)
  ...khtcPeriodRoutes, // W11.62 Kỳ KHTC (/v1/extension/khtc/*)
  ...cntsOutbreakRoutes, // W11.49 Báo cáo dịch bệnh BM-10, báo nghi dịch (/v1/extension/cnts/outbreaks*, /suspects*)
  ...khtcSourceRoutes, // W11.64 Cập nhật số liệu (/v1/extension/khtc/periods/{id}/sources*)
  // --- dom/hrm W11.22: hồ sơ nhân sự (/v1/hrm/access|units|staff*|permissions*) ---
  ...hrmStaffRoutes,
  ...priceBoardRoutes, // W11.76 duyệt / mở duyệt bảng giá ngày
  ...priceReportRoutes, // W11.77 báo cáo giá

  // --- pf/extension ---
  {
    method: "GET",
    pattern: /^\/v1\/extension\/dashboard$/,
    handler: ({ persona, url }) => {
      const d = extensionDashboard(persona, url);
      return d instanceof Response ? d : json(d);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/tasks$/,
    handler: ({ url, persona }) => {
      const status = url.searchParams.get("status");
      const priority = url.searchParams.get("priority");
      const overdue = url.searchParams.get("overdue") === "true";
      // Quản lý HTX (không kiêm vai KN) chỉ thấy nhiệm vụ của HTX mình — như pf/extension.
      const coopOnly = hasRole(persona, "coop_manager") && !hasRole(persona, "kn_officer", "kn_province_admin", "platform_admin");
      const list = TASKS.filter(
        (t) =>
          inScope(persona, t.province_code, t.commune_code) &&
          (!coopOnly || t.cooperative_ref === persona.me.org?.ref) &&
          (!status || t.status === status) &&
          (!priority || t.priority === priority) &&
          (!overdue || (t.overdue && t.status !== "resolved")),
      ).sort((a, b) => Date.parse(a.due_at) - Date.parse(b.due_at));
      return json(paginate(list, url));
    },
  },

  // --- pf/masterdata ---
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/admin-units$/,
    handler: ({ url }) => {
      const parent = url.searchParams.get("parent");
      if (parent && !ADMIN_UNITS.some((u) => u.code === parent)) return problem(404, "masterdata.admin_unit_not_found", "Not found");
      return json(paginate(ADMIN_UNITS.filter((u) => (parent ? u.parent_code === parent : u.parent_code === null)), url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/admin-units\/([0-9A-Z.-]+)$/,
    handler: ({ params }) => {
      const u = ADMIN_UNITS.find((x) => x.code === params[0]);
      return u ? json(u) : problem(404, "masterdata.admin_unit_not_found", "Not found");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/([a-z_]+)$/,
    handler: ({ params, url }) => {
      const items = CATALOGS[params[0] as keyof typeof CATALOGS];
      if (!items) return problem(404, "masterdata.catalog_not_found", "Not found");
      const all = url.searchParams.get("include_inactive") === "true";
      return json(paginate(items.filter((i) => all || i.active), url));
    },
  },

  // GIS US-CFG-01/02: quy chuẩn hiển thị bản đồ đang hiệu lực (mọi người dùng đã xác thực).
  { method: "GET", pattern: /^\/v1\/masterdata\/display-config$/, handler: () => json(displayConfig()) },

  // --- pf/party ---
  {
    method: "GET",
    pattern: /^\/v1\/orgs$/,
    handler: ({ url, persona }) => {
      const kind = url.searchParams.get("kind");
      const inactive = url.searchParams.get("include_inactive") === "true";
      const s = scopeUnits(persona);
      const list = ORGS.filter(
        (o) => (!s || (o.admin_unit_code !== null && s.has(o.admin_unit_code))) && (!kind || o.kind === kind) && (inactive || o.active),
      );
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives$/,
    handler: ({ url, persona }) => {
      const q = norm(url.searchParams.get("q") ?? "");
      const inactive = url.searchParams.get("include_inactive") === "true";
      // G-03: admin_unit = mã tỉnh hoặc xã; ngoài phạm vi → danh sách rỗng (không lỗi).
      const unit = url.searchParams.get("admin_unit");
      const list = COOPERATIVES.filter(
        (c) =>
          inScope(persona, c.province_code, c.api.admin_unit_code) &&
          (!unit || c.province_code === unit || c.api.admin_unit_code === unit) &&
          (inactive || c.api.active) &&
          (!q || norm(`${c.api.name} ${c.api.code} ${c.api.tax_code ?? ""}`).includes(q)),
      ).map((c) => c.api);
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      const c = findCoop(params[0]);
      // Ngoài phạm vi trả 404 như backend (không lộ sự tồn tại).
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(404, "party.org_not_found", "Not found");
      return json(c.api);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/households$/,
    handler: ({ params, url, persona }) => {
      const c = findCoop(params[0]);
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(404, "party.org_not_found", "Not found");
      return json(paginate(householdsOf(c), url));
    },
  },

  // --- pf/geo ---
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/plots$/,
    handler: ({ params, url, persona }) => {
      const c = findCoop(params[0]);
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(403, "geo.out_of_scope", "Out of scope");
      const own = plotsWithState(c).filter((p) => p.properties.cooperative_ref === c.api.ref);
      const { slice, next } = pageSlice(own, url, 200, 1000);
      return json({ type: "FeatureCollection", features: slice, next_cursor: next });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/plots$/,
    handler: ({ url, persona }) => {
      const parts = (url.searchParams.get("bbox") ?? "").split(",").map(Number);
      if (parts.length !== 4 || parts.some((n) => !Number.isFinite(n))) {
        return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "bbox", reason: "Cần minLon,minLat,maxLon,maxLat (WGS84)" }] });
      }
      const [minLon, minLat, maxLon, maxLat] = parts as [number, number, number, number];
      const limit = Math.min(Number(url.searchParams.get("limit") ?? 1000) || 1000, 1000);
      const inside = COOPERATIVES.filter((c) => inScope(persona, c.province_code, c.api.admin_unit_code))
        .flatMap((c) => plotsWithState(c))
        .filter((p) => {
          if (p.properties.status === "retired") return false;
          const pt = (p.geometry.coordinates[0] as number[][] | undefined)?.[0];
          return !!pt && pt[0]! >= minLon && pt[0]! <= maxLon && pt[1]! >= minLat && pt[1]! <= maxLat;
        });
      return json({ type: "FeatureCollection", features: inside.slice(0, limit), truncated: inside.length > limit, limit });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/geo\/admin-boundaries\/([0-9A-Z.-]+)$/,
    handler: ({ params }) => {
      const f = params[0] === MISSING_BOUNDARY ? null : boundaryOf(params[0] ?? "");
      if (!f) return problem(404, "geo.admin_boundary_not_found", "Boundary not imported");
      // Route theo mã không có parent_code trong hợp đồng.
      return json({ ...f, properties: omit(f.properties, "parent_code") });
    },
  },

  // --- pf/content ---
  {
    method: "GET",
    pattern: /^\/v1\/documents$/,
    handler: ({ url, persona }) => {
      const q = norm(url.searchParams.get("q") ?? "");
      const kind = url.searchParams.get("kind");
      const category = url.searchParams.get("category");
      const tag = url.searchParams.get("tag");
      const s = scopeUnits(persona);
      const list = DOCUMENTS.filter(
        (d) =>
          (!s || d.audience.level === "national" || d.audience.units.some((u) => s.has(u))) &&
          (!q || norm(`${d.title ?? ""} ${d.summary ?? ""}`).includes(q)) &&
          (!kind || d.kind === kind) &&
          (!category || d.category === category) &&
          (!tag || d.tags.includes(tag)),
      );
      return json(paginate(list, url));
    },
  },

  // --- pf/geo: gán thửa (quy tắc chồng lấn: HTX khác chặn, cùng HTX/thửa nháp cần xác nhận) ---
  {
    method: "POST",
    pattern: /^\/v1\/plots\/([\w-]+)\/assign$/,
    handler: ({ params, body, persona }) => {
      const found = findPlot(params[0]);
      if (!found || !inScope(persona, found.coop.province_code, found.coop.api.admin_unit_code)) return problem(404, "geo.plot_not_found", "Not found");
      const { plot, index } = found;
      if (plot.properties.status === "retired") return problem(409, "geo.plot_state_conflict", "Retired");
      const input = (body ?? {}) as { cooperative_ref?: string | null; reason?: string; confirm_overlap?: boolean };
      const target = input.cooperative_ref ?? null;
      if (target === plot.properties.cooperative_ref) return problem(409, "geo.plot_state_conflict", "No change");
      const overlaps = [{ plot_ref: plot.properties.ref, code: `${plot.properties.code.slice(0, -1)}9`, cooperative_ref: target, intersect_ha: "0.0420", overlap_pct: "3.10" }];
      // Minh hoạ: 1/3 thửa nháp chồng lấn HTX khác (không bỏ qua được), còn lại cần xác nhận.
      if (target && index % 3 === 0) return problem(409, "geo.overlap_cross_coop", "Cross-coop overlap", { overlaps });
      if (target && !input.confirm_overlap) return problem(409, "geo.overlap_needs_confirm", "Needs confirm", { needs_confirm: true, overlaps });
      const now = new Date().toISOString();
      PLOT_STATE.set(plot.properties.ref, { cooperative_ref: target, status: target ? "active" : "draft", needs_review: !!input.confirm_overlap });
      const updated = findPlot(params[0])!.plot;
      return json({
        plot: updated,
        conflicts: input.confirm_overlap
          ? [{
              id: uuidv7(), kind: "same_coop_overlap", plot_ref: updated.properties.ref, boundary_id: updated.properties.boundary_id, other_plot_refs: [],
              max_overlap_pct: "3.10", commune_code: updated.properties.commune_code, owner_ref: target, due_at: new Date(Date.now() + 72 * 3_600_000).toISOString(),
              status: "open", detected_at: now, resolved_by: null, resolved_at: null, resolution_note: null,
            }]
          : [],
        event: { id: uuidv7(), action: "assigned", seq: updated.properties.revision, event_time: now, reason: input.reason ?? null, actor_ref: persona.me.actor_ref },
      });
    },
  },

  // --- pf/mechanization (Bản đồ CGH — chỉ xem) ---
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/balance-runs$/,
    handler: ({ url, persona }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const code = url.searchParams.get("season_code");
      const year = Number(url.searchParams.get("season_year")) || null;
      return json({ items: RUNS.map((r) => r.run).filter((r) => (!code || r.season_code === code) && (!year || r.season_year === year)) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/coverage$/,
    handler: ({ url, persona }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const stored = findRun({ run_id: q("run_id"), season_code: q("season_code"), season_year: Number(q("season_year")) || null });
      if (!stored) return problem(404, "mechanization.run_not_found", "Run not found");
      return json(mechCoverage(stored, { level: q("level") ?? "province", province_code: q("province_code"), commune_code: q("commune_code"), farm_stage: q("farm_stage") }));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/cooperatives\/([\w-]+)\/panel$/,
    handler: ({ url, persona, params }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const stored = findRun({ run_id: q("run_id"), season_code: q("season_code"), season_year: Number(q("season_year")) || null }) ?? RUNS[0]!;
      const p = mechPanel(stored, params[0] ?? "");
      return p ? json(p) : problem(422, "mechanization.cooperative_invalid", "Cooperative invalid");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/mechanization\/dashboard$/,
    handler: ({ url, persona }) => {
      if (!cghAllowed(persona)) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const stored = findRun({ run_id: q("run_id"), season_code: q("season_code"), season_year: Number(q("season_year")) || null });
      if (!stored) return json({ kpis: { total_machines: 0, total_cooperatives: 0, cooperatives_sufficient: 0, cooperatives_shortage: 0 }, cooperatives_by_status: {}, priority: [], run: null });
      return json(mechDashboard(stored));
    },
  },
  ...areaSyncRoutes,
  ...opsRoutes,
  ...rosterRoutes,
  ...gisAppRoutes,
  ...waterwayAppRoutes,
  ...simulationRoutes,
  ...cropcycleAppRoutes,
  ...htxAppRoutes,
  ...metricsRoutes,
  ...mrvAppRoutes,
  ...collectionRoutes,
];

// --------------------------------------------------------------------------
// Admin API (host riêng) — mọi route cần MFA
// --------------------------------------------------------------------------
const DECIMAL = /^-?\d+(\.\d+)?$/;
const PARAM_KEY = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+(:[A-Za-z0-9_.-]+)*$/;

function normEffectiveFrom(v: string): string | null {
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) return `${v}T00:00:00+07:00`;
  return Number.isFinite(Date.parse(v)) ? v : null;
}

function findVersion(id: string | undefined): ParamVersion | undefined {
  return PARAM_VERSIONS.find((v) => v.id === id);
}

const importedPhones = new Set<string>();

const adminRoutes: Route[] = [
  // KN v7.7 (P1/P2): tài khoản nhiều vai trò, Menu các cấp (hỗ trợ), phòng ban, khu vực, cây tổ chức — đặt trước
  // để GET /v1/accounts/users trong ngữ cảnh Admin KN trả tài khoản KN.
  ...knAdminRoutes,
  ...htxGapAdminRoutes,
  // W11.41: danh mục CNTS — thêm/sửa/ngừng có ngày áp dụng, phiên bản hiệu lực.
  ...cntsCatalogAdminRoutes,
  // --- KN v7.7 P3: trình thiết kế mẫu báo cáo (FN-AD-04.06) ---
  ...formsAdminRoutes,
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/([a-z_]+)\/items$/,
    handler: ({ params, url, persona }) => {
      if (!hasRole(persona, "masterdata_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const items = CATALOGS[params[0] as keyof typeof CATALOGS];
      if (!items) return problem(404, "masterdata.catalog_not_found", "Not found");
      const all = url.searchParams.get("include_inactive") !== "false";
      return json(paginate(items.filter((i) => all || i.active), url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/params\/versions$/,
    handler: ({ url, persona }) => {
      if (!hasRole(persona, "param_setter", "param_approver", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const key = url.searchParams.get("key");
      const status = url.searchParams.get("status");
      return json(paginate(PARAM_VERSIONS.filter((v) => (!key || v.key === key) && (!status || v.status === status)), url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/versions$/,
    handler: ({ body, persona }) => {
      if (!hasRole(persona, "param_setter")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      const errors: { field: string; reason: string }[] = [];
      const key = typeof input.key === "string" ? input.key : "";
      const value = typeof input.value === "string" ? input.value.trim() : typeof input.value === "number" ? String(input.value) : "";
      const text = typeof input.text === "string" ? input.text.trim() : "";
      const eff = typeof input.effective_from === "string" ? normEffectiveFrom(input.effective_from) : null;
      const sourceDoc = typeof input.source_doc === "string" ? input.source_doc.trim() : "";
      const correction = typeof input.correction_reason === "string" ? input.correction_reason.trim() : "";
      // Tham số màu (GIS US-CFG-01): giá trị nằm ở `text` (HEX), không có `value`.
      const colorKey = isColorKey(key);
      if (!PARAM_KEY.test(key)) errors.push({ field: "key", reason: "Khóa tham số không đúng mẫu" });
      if (colorKey) {
        if (value) errors.push({ field: "value", reason: "Tham số màu chỉ nhận text (mã HEX)" });
      } else if (!DECIMAL.test(value)) errors.push({ field: "value", reason: "Giá trị phải là số thập phân (dạng chuỗi)" });
      const display = checkDisplayProposal(key, value, text);
      if (display?.errors) errors.push(...display.errors);
      if (!eff) errors.push({ field: "effective_from", reason: "Ngày hiệu lực không hợp lệ" });
      if (!sourceDoc) errors.push({ field: "source_doc", reason: "Cần số/ngày văn bản ban hành" });
      if (correction && correction.length < 10) errors.push({ field: "correction_reason", reason: "Lý do sửa tối thiểu 10 ký tự" });
      if (errors.length) return problem(422, "validation_failed", "Validation failed", { errors });
      if (display?.percent) return problem(422, "masterdata.param_percent_thresholds", PERCENT_TITLE);
      const sameKey = PARAM_VERSIONS.filter((v) => v.key === key);
      if (!correction && sameKey.some((v) => v.status === "approved" && Date.parse(v.effective_from) === Date.parse(eff!))) {
        return problem(422, "masterdata.param_correction_required", "Correction reason required");
      }
      const last = sameKey.reduce((m, v) => Math.max(m, v.version), 0);
      const v: ParamVersion = {
        id: uuidv7(),
        key,
        version: last + 1,
        value: colorKey ? null : value,
        text: colorKey ? text.toUpperCase() : null,
        unit: typeof input.unit === "string" && input.unit ? input.unit : (sameKey[0]?.unit ?? ""),
        effective_from: eff!,
        source_doc: sourceDoc,
        correction_reason: correction || null,
        status: "draft",
        setter_ref: persona.me.actor_ref,
        setter_name: ACTOR_NAMES[persona.me.actor_ref] ?? null,
        submitted_at: null,
        approver_ref: null,
        approver_name: null,
        decided_at: null,
        decision_note: null,
        created_at: new Date().toISOString(),
      };
      PARAM_VERSIONS.push(v);
      return json(v, 201, { Location: `/v1/masterdata/params/versions/${v.id}` });
    },
  },
  {
    // GIS US-CFG-02 AC-2: "Xóa cấu hình" = phiên bản NHÁP mới mang giá trị mặc định nhúng (không xóa gì — RB-1).
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/restore-default$/,
    handler: ({ body, persona }) => {
      if (!hasRole(persona, "param_setter")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      const key = typeof input.key === "string" ? input.key : "";
      const reason = typeof input.reason === "string" ? input.reason.trim() : "";
      const def = DISPLAY_DEFAULTS[key];
      if (!def) return problem(404, "masterdata.param_no_default", "Param has no embedded default");
      if (reason && reason.length < 10) return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "reason", reason: "Lý do tối thiểu 10 ký tự" }] });
      const last = PARAM_VERSIONS.filter((v) => v.key === key).reduce((m, v) => Math.max(m, v.version), 0);
      const now = new Date().toISOString();
      const v: ParamVersion = {
        id: uuidv7(),
        key,
        version: last + 1,
        value: def.kind === "color" ? null : def.value,
        text: def.kind === "color" ? def.value : null,
        unit: def.unit,
        effective_from: now,
        source_doc: reason || RESTORE_REASON,
        correction_reason: null,
        status: "draft",
        setter_ref: persona.me.actor_ref,
        setter_name: ACTOR_NAMES[persona.me.actor_ref] ?? null,
        submitted_at: null,
        approver_ref: null,
        approver_name: null,
        decided_at: null,
        decision_note: null,
        created_at: now,
      };
      PARAM_VERSIONS.push(v);
      return json(v, 201, { Location: `/v1/masterdata/params/versions/${v.id}` });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/versions\/([\w-]+)\/submit$/,
    handler: ({ params, persona }) => {
      const v = findVersion(params[0]);
      if (!v) return problem(404, "masterdata.param_version_not_found", "Not found");
      if (v.setter_ref !== persona.me.actor_ref) return problem(403, "masterdata.param_not_setter", "Not setter");
      if (v.status !== "draft") return problem(409, "masterdata.param_invalid_transition", "Invalid transition");
      v.status = "submitted";
      v.submitted_at = new Date().toISOString();
      return json(v);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/versions\/([\w-]+)\/(approve|reject)$/,
    handler: ({ params, persona, body }) => {
      if (!hasRole(persona, "param_approver")) return problem(403, "forbidden", "Forbidden");
      const v = findVersion(params[0]);
      if (!v) return problem(404, "masterdata.param_version_not_found", "Not found");
      if (v.setter_ref === persona.me.actor_ref) return problem(403, "self_approval_forbidden", "Self approval forbidden");
      if (v.status !== "submitted") return problem(409, "masterdata.param_invalid_transition", "Invalid transition");
      const note = typeof (body as { note?: unknown } | undefined)?.note === "string" ? (body as { note: string }).note.trim() : "";
      if (params[1] === "reject" && note.length < 10) {
        return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "note", reason: "Lý do từ chối tối thiểu 10 ký tự" }] });
      }
      // Bộ ngưỡng % kiểm lại lúc duyệt (mốc còn lại có thể đã đổi trong lúc chờ — GIS US-CFG-01 AC-2).
      if (params[1] === "approve" && isPercentKey(v.key) && checkDisplayProposal(v.key, v.value ?? "", "")) {
        return problem(422, "masterdata.param_percent_thresholds", PERCENT_TITLE);
      }
      v.status = params[1] === "approve" ? "approved" : "rejected";
      v.approver_ref = persona.me.actor_ref;
      v.approver_name = ACTOR_NAMES[persona.me.actor_ref] ?? null;
      v.decided_at = new Date().toISOString();
      v.decision_note = note || null;
      if (v.status === "rejected") return json(v);
      const approved: AppGet<"/v1/masterdata/params/value"> = {
        key: v.key,
        value: v.value,
        text: v.text,
        unit: v.unit,
        version: v.version,
        effective_from: v.effective_from,
        source_doc: v.source_doc,
        approved_at: v.decided_at,
        param_version_id: v.id,
        supersedes_id: null,
        seq: PARAM_VERSIONS.filter((x) => x.status === "approved").length,
      };
      return json({ version: v, approved });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/orgs\/([\w-]+)\/events$/,
    handler: ({ params }) => {
      const org = ORGS.find((o) => o.ref === params[0]);
      return org ? json({ items: orgEvents(org) }) : problem(404, "party.org_not_found", "Not found");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/orgs\/([\w-]+)\/rename$/,
    handler: ({ params, body, persona }) => {
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const org = ORGS.find((o) => o.ref === params[0]);
      if (!org) return problem(404, "party.org_not_found", "Not found");
      const input = (body ?? {}) as { name?: unknown; reason?: unknown };
      const name = typeof input.name === "string" ? input.name.trim() : "";
      if (!name || name.length > 200) return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "name", reason: "Tên từ 1 đến 200 ký tự" }] });
      if (name === org.name) return problem(409, "party.org_state_conflict", "No change");
      const events = orgEvents(org);
      org.name = name;
      const now = new Date().toISOString();
      const event = {
        ...events[events.length - 1]!,
        seq: events.length + 1,
        action: "renamed" as const,
        event_time: now,
        recorded_at: now,
        actor_ref: persona.me.actor_ref,
        reason: typeof input.reason === "string" && input.reason ? input.reason : null,
        name,
      };
      events.push(event);
      ORG_EVENTS.set(org.ref, events);
      return json({ org, event, active_households: 0 });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/households\/import$/,
    handler: ({ body, persona, request }) => {
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      if (isZip(body)) {
        // .xlsx (chữ ký ZIP): giả lập không giải nén — trả kết quả minh hoạ cho trang tính đầu.
        return json({
          accepted: 2,
          duplicate: 0,
          rejected: 1,
          rows: [
            { row: 2, status: "accepted", reason: null, household_ref: uuidv7() },
            { row: 3, status: "accepted", reason: null, household_ref: uuidv7() },
            { row: 4, status: "rejected", reason: "Số điện thoại không đúng định dạng", household_ref: null },
          ],
        });
      }
      if (!(request.headers.get("Content-Type") ?? "").startsWith("text/csv")) return problem(422, "party.import_invalid", "Expected text/csv or .xlsx");
      const rows = parseCsv(typeof body === "string" ? body : "");
      const header = (rows[0] ?? []).map(headerKey);
      const col = (...names: string[]) => header.findIndex((h) => names.includes(h));
      const iName = col("full_name", "ho_ten");
      const iPhone = col("phone", "so_dien_thoai");
      const iCoop = col("cooperative_code", "ma_htx");
      if (iName < 0 || iPhone < 0 || iCoop < 0) return problem(422, "party.import_invalid", "Missing required columns");
      const data = rows.slice(1);
      if (data.length === 0 || data.length > 200) return problem(422, "party.import_invalid", "Row count out of range");
      const out = data.map((r, i) => {
        const row = i + 2;
        const phone = (r[iPhone] ?? "").replace(/\s+/g, "");
        if (!(r[iName] ?? "").trim()) return { row, status: "rejected" as const, reason: "Thiếu họ tên chủ hộ", household_ref: null };
        if (!/^0\d{9}$/.test(phone)) return { row, status: "rejected" as const, reason: "Số điện thoại không đúng định dạng", household_ref: null };
        if (!COOPERATIVES.some((c) => c.api.code === (r[iCoop] ?? "").trim())) return { row, status: "rejected" as const, reason: "Không tìm thấy HTX theo mã đã nhập", household_ref: null };
        if (importedPhones.has(phone)) return { row, status: "duplicate" as const, reason: "Trùng chủ hộ đã có — dùng lại mã cũ", household_ref: uuidv7() };
        importedPhones.add(phone);
        return { row, status: "accepted" as const, reason: null, household_ref: uuidv7() };
      });
      return json({
        accepted: out.filter((r) => r.status === "accepted").length,
        duplicate: out.filter((r) => r.status === "duplicate").length,
        rejected: out.filter((r) => r.status === "rejected").length,
        rows: out,
      });
    },
  },
  ...adminOpsRoutes,
  ...syncMonitorRoutes,
  ...htxAnchorRoutes,
  ...gisAdminRoutes,
  ...waterwayAdminRoutes,
  ...cropcycleAdminRoutes,
  ...htxAdminRoutes,
  ...mrvAdminRoutes,
  // G5: pf/apiclient (khách hàng Public API) + pf/metering (sổ sử dụng, bảng kê, DLP).
  ...apiClientAdminRoutes,
  ...meteringAdminRoutes,
];

// --------------------------------------------------------------------------
// fetch giả lập
// --------------------------------------------------------------------------
const replay = new Map<string, { status: number; body: string; headers: Record<string, string>; fingerprint: string }>();

/** Dấu vân tay thân nhị phân (độ dài + tổng kiểm đơn giản) — đủ để phát hiện dùng lại khóa cho tệp khác. */
function binaryFingerprint(bytes: Uint8Array): string {
  let h = 2166136261;
  for (let i = 0; i < bytes.length; i++) h = Math.imul(h ^ (bytes[i] ?? 0), 16777619) >>> 0;
  return `bin:${bytes.length}:${h.toString(16)}`;
}

/**
 * Thân lưu cho phát lại Idempotency-Key. Phản hồi `Cache-Control: no-store` (phát hành/xoay khóa API, ADR-0022 §4)
 * KHÔNG được giữ bí mật: bỏ `api_key` — gọi lặp trả bản không có khóa, như backend.
 */
function replayBody(res: Response, text: string): string {
  if (res.headers.get("Cache-Control") !== "no-store") return text;
  try {
    const parsed = JSON.parse(text) as Record<string, unknown>;
    delete parsed.api_key;
    return JSON.stringify(parsed);
  } catch {
    return "{}";
  }
}

function createMockFetch(routes: Route[], opts: { surface: "app" | "admin"; latency?: boolean }) {
  return async function mockFetch(request: Request): Promise<Response> {
    if (opts.latency !== false) await sleep(120 + Math.random() * 220);
    const url = new URL(request.url);
    const tokenPersona = personaFromAuthHeader(request.headers.get("Authorization"));
    if (!tokenPersona) return problem(401, "unauthenticated", "Unauthenticated");
    // KN v7.7: header X-AG-Context chọn vai trò + phạm vi của lần gán (middleware P1 giả lập): lạ/của người khác → 403.
    const badContext = contextProblem(tokenPersona, request.headers.get("X-AG-Context"));
    if (badContext) return badContext;
    const persona = personaForContext(tokenPersona, request.headers.get("X-AG-Context"));
    // Bề mặt Admin: mọi route bắt buộc token đã qua yếu tố thứ hai (mục 11).
    if (opts.surface === "admin" && !persona.amr.includes("otp")) return problem(403, "mfa_required", "MFA required");

    let body: unknown = undefined;
    // POST bắt buộc Idempotency-Key; PUT/PATCH (KN v7.7 P1/P7) đọc thân như POST, khóa tùy chọn.
    if (request.method === "POST" || ((request.method === "PUT" || request.method === "PATCH") && request.headers.get("Content-Type")?.includes("json"))) {
      const key = request.headers.get("Idempotency-Key") ?? (request.method === "POST" ? null : uuidv7());
      if (!key) return problem(400, "idempotency_key_required", "Idempotency-Key required");
      const type = request.headers.get("Content-Type") ?? "application/json";
      let fingerprintBody: string;
      if (type.includes("json")) {
        const text = await request.text();
        fingerprintBody = text;
        try {
          body = text ? JSON.parse(text) : undefined;
        } catch {
          return problem(400, "bad_request", "Invalid JSON");
        }
      } else if (type.startsWith("text/") || /[+/]xml\b/.test(type)) {
        body = await request.text();
        fingerprintBody = body as string;
      } else {
        const bytes = new Uint8Array(await request.arrayBuffer());
        body = bytes;
        fingerprintBody = binaryFingerprint(bytes);
      }
      const fingerprint = `${opts.surface}|${persona.id}|${url.pathname}${url.search}|${fingerprintBody}`;
      const prev = replay.get(key);
      if (prev) {
        if (prev.fingerprint !== fingerprint) return problem(422, "idempotency_key_reused", "Idempotency key reused");
        return new Response(prev.body, { status: prev.status, headers: { ...prev.headers, "Idempotent-Replayed": "true" } });
      }
      // FN-AD-04.07: chế độ hỗ trợ — lý do bắt buộc, nhật ký attempted + completed.
      const guard = supportGuard(request, url);
      if (guard) return guard;
      supportRecord(request, url, null, "attempted", key);
      const res = dispatch(routes, request, url, persona, body);
      supportRecord(request, url, res.status, "completed", key);
      if (res.ok) {
        replay.set(key, { status: res.status, body: replayBody(res, await res.clone().text()), headers: { "Content-Type": res.headers.get("Content-Type") ?? "application/json" }, fingerprint });
      }
      return res;
    }
    return dispatch(routes, request, url, persona, body);
  };
}

function dispatch(routes: Route[], request: Request, url: URL, persona: MockPersona, body: unknown): Response {
  for (const r of routes) {
    if (r.method !== request.method) continue;
    const m = r.pattern.exec(url.pathname);
    if (!m || (r.match && !r.match(url, request))) continue;
    try {
      return r.handler({ url, persona, params: m.slice(1), body, request });
    } catch (e) {
      // Lỗi của chính backend giả lập → 500 internal_error (không thành "mất kết nối").
      return problem(500, "internal_error", "Mock handler failed", { detail: e instanceof Error ? e.message : String(e) });
    }
  }
  return problem(404, "not_found", "Not found");
}

/** fetch giả lập cho client Application API. */
export const mockFetch = createMockFetch(appRoutes, { surface: "app" });
/** fetch giả lập cho client Admin API (MFA bắt buộc). */
export const mockAdminFetch = createMockFetch(adminRoutes, { surface: "admin" });

/** Cho kiểm thử: không chờ độ trễ giả lập. */
export const mockFetchNoDelay = createMockFetch(appRoutes, { surface: "app", latency: false });
export const mockAdminFetchNoDelay = createMockFetch(adminRoutes, { surface: "admin", latency: false });
