// ============================================================================
// BACKEND GIẢ LẬP pf/cropcycle (App HTX: SOP, vụ, nhật ký, duyệt, sản lượng, bảng điều hành).
// Hình dạng phản hồi theo contracts/openapi/application.yaml + admin.yaml. Mô phỏng quy tắc
// backend để UI thể hiện đúng: phạm vi theo vai (coop_manager — HTX mình; KN — chỉ đọc theo xã;
// platform_admin — tất cả), chặn mở vụ trùng, sao chép cấu hình vụ trước, ghi hộ/ghi hàng loạt
// bỏ qua lô lỗi kèm lý do, người ghi không tự duyệt, sản lượng bất thường cần xác nhận,
// phát hành SOP cần param_approver khác người soạn. DỮ LIỆU MINH HOẠ, không người thật.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type {
  CropFarmLog,
  CropFarmLogState,
  CropHarvest,
  CropSeason,
  CropSeasonEvent,
  CropSOP,
  CropSOPStep,
  CropSOPTemplate,
  CropSOPVersion,
  CropVarietyBinding,
  PlotFeature,
} from "../types";
import { CATALOGS, COOPERATIVES, findCoop, hexId, htxCoop, mulberry32, plotsOf, type MockCooperative } from "./data";
import { headerKey, parseCsv } from "./admin-ops";
import { hasRole, isZip, json, nowIso, paginate, problem, reasonOf, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

// --------------------------------------------------------------------------
// Tiện ích ngày (giờ Việt Nam) và số thập phân (không float cho giá trị lưu)
// --------------------------------------------------------------------------
function todayVN(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}
function dayNo(d: string): number {
  return Math.round(Date.parse(`${d}T00:00:00Z`) / 86_400_000);
}
function addDays(d: string, n: number): string {
  return new Date((dayNo(d) + n) * 86_400_000).toISOString().slice(0, 10);
}
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** Chuỗi thập phân → số nguyên đã nhân 10^4. */
function sc4(s: string | null | undefined): bigint {
  if (!s) return 0n;
  const neg = s.startsWith("-");
  const [i = "0", f = ""] = s.replace("-", "").split(".");
  const v = BigInt(i || "0") * 10000n + BigInt((f + "0000").slice(0, 4));
  return neg ? -v : v;
}
function fmt4(v: bigint, digits = 4): string {
  const neg = v < 0n;
  const a = neg ? -v : v;
  const f = String(a % 10000n).padStart(4, "0").slice(0, digits);
  return `${neg ? "-" : ""}${a / 10000n}${digits > 0 ? `.${f}` : ""}`;
}
/** a × b (hai số 4 lẻ) → 4 lẻ. */
function mul4(a: bigint, b: bigint): bigint {
  return (a * b) / 10000n;
}
/** a ÷ b (4 lẻ); b = 0 → null. */
function div4(a: bigint, b: bigint): bigint | null {
  return b === 0n ? null : (a * 10000n) / b;
}

// --------------------------------------------------------------------------
// Quy trình SOP
// --------------------------------------------------------------------------
const ADMIN_OTHER = "mock-admin-other";
const APPROVER = "mock-admin-approver";
const SETTER = "mock-admin-setter";
const HTX_ACTOR = "mock-htx-0001";

const st = (seq: number, name: string, activity_code: CropSOPStep["activity_code"], day_offset: number, window_days: number, extra: Partial<CropSOPStep> = {}): CropSOPStep => ({
  seq,
  name,
  activity_code,
  day_offset,
  window_days,
  mandatory: true,
  ...extra,
});

const STEPS_CLC: CropSOPStep[] = [
  st(1, "Làm đất, san phẳng mặt ruộng", "lam_dat", -3, 2, { stage_code: "lam_dat" }),
  st(2, "Gieo sạ (máy sạ cụm / sạ hàng)", "gieo_sa", 0, 1, { stage_code: "gieo_sa", evidence_kinds: ["anh_hien_truong"] }),
  st(3, "Bón thúc lần 1", "bon_phan", 10, 3, { stage_code: "cham_soc" }),
  st(4, "Tưới ướt khô xen kẽ (AWD) — rút nước lần 1", "tuoi_nuoc", 18, 4, { stage_code: "cham_soc" }),
  st(5, "Bón thúc lần 2", "bon_phan", 25, 3, { stage_code: "cham_soc" }),
  st(6, "Phun thuốc phòng đạo ôn cổ bông", "phun_thuoc", 60, 5, { stage_code: "cham_soc", mandatory: false, phi_days: 14 }),
  st(7, "Kiểm tra đồng ruộng trước thu hoạch", "kiem_tra", 80, 5, { stage_code: "cham_soc", mandatory: false }),
  st(8, "Thu hoạch bằng máy gặt đập liên hợp", "thu_hoach", 95, 5, { stage_code: "thu_hoach" }),
];
const STEPS_ST25: CropSOPStep[] = [
  st(1, "Làm đất", "lam_dat", -3, 2),
  st(2, "Gieo sạ", "gieo_sa", 0, 1, { evidence_kinds: ["anh_hien_truong"] }),
  st(3, "Bón lót hữu cơ", "bon_phan", 7, 3),
  st(4, "Bón thúc", "bon_phan", 30, 3),
  st(5, "Thu hoạch", "thu_hoach", 105, 5),
];

interface SopRec {
  t: CropSOPTemplate;
  versions: CropSOPVersion[];
}

function harvestOf(steps: CropSOPStep[]): { off: number | null; win: number | null } {
  const h = steps.find((s) => s.activity_code === "thu_hoach");
  return { off: h?.day_offset ?? null, win: h?.window_days ?? null };
}

function version(template_ref: string, no: number, steps: CropSOPStep[], status: "draft" | "published", author: string, daysAgo: number, note: string | null): CropSOPVersion {
  const h = harvestOf(steps);
  const at = new Date(Date.now() - daysAgo * 86_400_000).toISOString();
  return {
    id: hexId(parseInt(template_ref.slice(-4), 16) * 10 + no, "50f7"),
    template_ref,
    version_no: no,
    status,
    steps,
    harvest_day_offset: h.off,
    harvest_window_days: h.win,
    source_version_ref: null,
    change_note: note,
    authored_by: author,
    authored_at: at,
    published_by: status === "published" ? APPROVER : null,
    published_at: status === "published" ? at : null,
  };
}

function template(n: number, code: string, name: string, scope: "system" | "htx", coop: string | null, status: CropSOPTemplate["status"], latest: number, published: number | null, standard: string | null): CropSOPTemplate {
  const at = new Date(Date.now() - (120 - n) * 86_400_000).toISOString();
  return {
    ref: hexId(n, "50b0"),
    code,
    name,
    crop: "lua",
    scope,
    cooperative_ref: coop,
    standard,
    description: null,
    status,
    status_reason: null,
    source_template_ref: null,
    latest_version: latest,
    published_version: published,
    created_at: at,
    updated_at: at,
  };
}

const SOPS: SopRec[] = [];
const BINDINGS: CropVarietyBinding[] = [];

function seedSops(): void {
  const t1 = template(1, "SOP-LUA-CLC", "Quy trình lúa chất lượng cao, phát thải thấp", "system", null, "published", 1, 1, "Đề án 1 triệu ha");
  const t2 = template(2, "SOP-ST25", "Quy trình lúa thơm ST25", "system", null, "published", 1, 1, "VietGAP");
  const t3 = template(3, "SOP-OM18-NHAP", "Quy trình OM18 vùng ngập (bản nháp)", "system", null, "draft", 1, null, null);
  const htx = htxCoop();
  const t4 = template(4, "SOP-HTX-RIENG", `Quy trình riêng ${htx.api.name}`, "htx", htx.api.ref, "published", 1, 1, null);
  SOPS.push(
    { t: t1, versions: [version(t1.ref, 1, STEPS_CLC, "published", ADMIN_OTHER, 90, "Ban hành lần đầu")] },
    { t: t2, versions: [version(t2.ref, 1, STEPS_ST25, "published", ADMIN_OTHER, 80, "Ban hành lần đầu")] },
    // Bản nháp do "Quản trị viên A" (persona setter) soạn — chính người này không được phát hành (R-56).
    { t: t3, versions: [version(t3.ref, 1, STEPS_CLC.map((s) => (s.activity_code === "thu_hoach" ? { ...s, day_offset: 97 } : s)), "draft", SETTER, 2, "Điều chỉnh mốc thu hoạch theo OM18")] },
    { t: t4, versions: [version(t4.ref, 1, STEPS_CLC.slice(0, 3).concat([{ ...STEPS_CLC[7]!, seq: 4 }]), "published", ADMIN_OTHER, 60, null)] },
  );
  const at = new Date(Date.now() - 70 * 86_400_000).toISOString();
  for (const [variety, t] of [["OM5451", t1], ["OM18", t1], ["DT8", t1], ["ST25", t2]] as const) {
    BINDINGS.push({ variety_code: variety, template_ref: t.ref, note: null, bound_by: ADMIN_OTHER, bound_at: at });
  }
}

function findVersion(id: string | null | undefined): { rec: SopRec; v: CropSOPVersion } | undefined {
  for (const rec of SOPS) {
    const v = rec.versions.find((x) => x.id === id);
    if (v) return { rec, v };
  }
  return undefined;
}
function sopOut(rec: SopRec, onlyPublished: boolean): CropSOP {
  return { ...rec.t, versions: onlyPublished ? rec.versions.filter((v) => v.status === "published") : rec.versions };
}
function publishedVersionFor(variety: string): CropSOPVersion | undefined {
  const b = BINDINGS.find((x) => x.variety_code === variety);
  const rec = b ? SOPS.find((s) => s.t.ref === b.template_ref) : undefined;
  if (!rec || rec.t.status !== "published") return undefined;
  return rec.versions.filter((v) => v.status === "published").at(-1);
}
function normName(s: string): string {
  return s.trim().toLowerCase().replace(/\s+/g, " ");
}

// --------------------------------------------------------------------------
// Phạm vi theo vai
// --------------------------------------------------------------------------
type Access = { kind: "all" } | { kind: "coop"; coop: string } | { kind: "kn"; units: Set<string> | null };

function accessOf(p: MockPersona): Access | null {
  if (hasRole(p, "platform_admin")) return { kind: "all" };
  if (hasRole(p, "coop_manager")) return { kind: "coop", coop: p.me.org?.ref ?? "" };
  if (hasRole(p, "kn_officer", "kn_province_admin", "kn_national_admin")) return { kind: "kn", units: p.me.scope.all ? null : new Set(p.me.scope.units) };
  return null;
}
function sees(a: Access, coop: string, commune: string): boolean {
  if (a.kind === "all") return true;
  if (a.kind === "coop") return a.coop === coop;
  return !a.units || a.units.has(commune);
}
/** null = được ghi; ngược lại là problem cần trả. */
function writeDenied(a: Access | null, coop: string): Response | null {
  if (!a) return problem(403, "forbidden", "Forbidden");
  if (a.kind === "kn") return problem(403, "cropcycle.read_only", "Read only");
  if (a.kind === "coop" && a.coop !== coop) return problem(403, "cropcycle.out_of_scope", "Out of scope");
  return null;
}

// --------------------------------------------------------------------------
// Thửa (từ pf/geo giả lập) — chỉ HTX tỉnh Đồng Tháp có dữ liệu mùa vụ
// --------------------------------------------------------------------------
interface PlotRef {
  plot: PlotFeature;
  coop: MockCooperative;
  index: number;
}
const PLOTS = new Map<string, PlotRef>();
const CROP_COOPS: MockCooperative[] = [];

function seedPlots(): void {
  const htx = htxCoop();
  const coops = COOPERATIVES.filter((c) => c.api.active && (c.province_code === "82" || c === htx));
  CROP_COOPS.push(...coops);
  for (const coop of COOPERATIVES) {
    plotsOf(coop).forEach((plot, index) => PLOTS.set(plot.properties.ref, { plot, coop, index }));
  }
}

// --------------------------------------------------------------------------
// Vụ, nhật ký, sản lượng
// --------------------------------------------------------------------------
interface SeasonRec {
  s: CropSeason;
  events: CropSeasonEvent[];
}
const SEASONS: SeasonRec[] = [];
interface LogRec {
  versions: CropFarmLog[];
  state: CropFarmLogState;
}
const LOGS: LogRec[] = [];
const HARVESTS: CropHarvest[] = [];
const VARIETIES = ["OM5451", "OM18", "DT8", "ST25"] as const;

function expectedYieldOf(variety: string): string | null {
  return CATALOGS.rice_variety.find((v) => v.code === variety)?.attrs.expected_yield_t_per_ha ?? null;
}
/** Ngưỡng năng suất bất thường theo giống (HTX US-YIELD-03 AC-2): ST25 8 t/ha, còn lại 12. */
function anomalyThreshold(variety: string): string {
  return variety === "ST25" ? "8" : "12";
}
function farmerOf(householdRef: string | null): string {
  return householdRef ? `01927b3e-fa2e-7000-8000-${householdRef.slice(-12)}` : HTX_ACTOR;
}

function newSeason(ref: PlotRef, o: { code: string; year: number; variety: string; sowing: string; sop: CropSOPVersion; actor: string; onBehalf: boolean; loc: CropSeason["location_status"]; copied?: string | null; at?: string }): SeasonRec {
  const p = ref.plot.properties;
  const h = harvestOf(o.sop.steps);
  const off = h.off ?? 95;
  const win = h.win ?? 5;
  const at = o.at ?? nowIso();
  const tmpl = SOPS.find((x) => x.t.ref === o.sop.template_ref)!;
  const eventId = uuidv7();
  const s: CropSeason = {
    season_ref: uuidv7(),
    plot_ref: p.ref,
    plot_code: p.code,
    household_ref: p.household_ref ?? null,
    cooperative_ref: ref.coop.api.ref,
    commune_code: p.commune_code,
    season_code: o.code,
    season_year: o.year,
    variety_code: o.variety,
    sop_template_ref: tmpl.t.ref,
    sop_version_id: o.sop.id,
    sop_version_no: o.sop.version_no,
    sowing_date: o.sowing,
    expected_harvest_date: addDays(o.sowing, off),
    harvest_open_date: addDays(o.sowing, off - win),
    area_ha: p.area_ha,
    expected_yield_t_per_ha: expectedYieldOf(o.variety),
    status: "open",
    current_event_id: eventId,
    version: 1,
    location_status: o.loc,
    gps_source: o.loc === "missing" ? "no_device" : "device",
    on_behalf: o.onBehalf,
    opened_by: o.actor,
    opened_at: at,
    copied_from_season_ref: o.copied ?? null,
    harvest_id: null,
    completed_at: null,
    updated_at: at,
  };
  const ev: CropSeasonEvent = {
    id: eventId,
    seq: 1,
    action: "opened",
    variety_code: o.variety,
    sop_version_id: o.sop.id,
    sowing_date: o.sowing,
    location_status: o.loc,
    gps_source: s.gps_source,
    distance_m: o.loc === "mismatch" ? "120.0" : null,
    on_behalf: o.onBehalf,
    channel: o.onBehalf ? "web" : "app",
    actor_ref: o.actor,
    event_time: at,
    recorded_at: at,
    supersedes_id: null,
    correction_reason: null,
    harvest_id: null,
  };
  const out = { s, events: [ev] };
  SEASONS.push(out);
  return out;
}

function addLog(season: CropSeason, o: { activity: CropSOPStep["activity_code"]; seq: number | null; status: CropFarmLogState["status"]; author: string; loc: CropFarmLog["location_status"]; daysAgo: number; note?: string; files?: number; reviewNote?: string; batch?: string | null; machines?: string[]; inputs?: CropFarmLog["inputs"] }): LogRec {
  const at = new Date(Date.now() - o.daysAgo * 86_400_000).toISOString();
  const id = uuidv7();
  const log: CropFarmLog = {
    id,
    log_ref: id,
    version: 1,
    season_ref: season.season_ref,
    plot_ref: season.plot_ref,
    household_ref: season.household_ref ?? null,
    cooperative_ref: season.cooperative_ref,
    commune_code: season.commune_code,
    activity_code: o.activity,
    sop_step_seq: o.seq,
    event_time: at,
    gps: o.loc === "missing" ? { source: "denied" } : { lon: "105.6", lat: "10.45", accuracy_m: "6", source: "device" },
    location_status: o.loc,
    distance_m: o.loc === "mismatch" ? "184.5" : o.loc === "match" ? "8.2" : null,
    machine_codes: o.machines ?? [],
    inputs: o.inputs ?? [],
    note: o.note ?? null,
    file_refs: Array.from({ length: o.files ?? 0 }, () => uuidv7()),
    batch_ref: o.batch ?? null,
    on_behalf: o.author === HTX_ACTOR,
    channel: o.author === HTX_ACTOR ? "web" : "app",
    author_ref: o.author,
    recorded_at: at,
    supersedes_id: null,
    correction_reason: null,
    became_current: true,
  };
  const rec: LogRec = { versions: [log], state: stateOf(log, o.status, o.reviewNote) };
  LOGS.push(rec);
  return rec;
}

function stateOf(log: CropFarmLog, status: CropFarmLogState["status"], note?: string): CropFarmLogState {
  const reviewed = status === "approved" || status === "needs_supplement";
  return {
    id: log.id,
    log_ref: log.log_ref,
    version: log.version,
    season_ref: log.season_ref,
    plot_ref: log.plot_ref,
    household_ref: log.household_ref ?? null,
    cooperative_ref: log.cooperative_ref,
    activity_code: log.activity_code,
    sop_step_seq: log.sop_step_seq ?? null,
    event_time: log.event_time,
    location_status: log.location_status,
    gps_source: log.gps?.source ?? "no_device",
    distance_m: log.distance_m ?? null,
    author_ref: log.author_ref,
    on_behalf: log.on_behalf ?? false,
    batch_ref: log.batch_ref ?? null,
    status,
    superseded_by: null,
    review_note: note ?? null,
    reviewed_by: reviewed ? HTX_ACTOR : null,
    reviewed_at: reviewed ? log.recorded_at : null,
  };
}

function harvestFor(rec: SeasonRec, o: { mass: string; date: string; moisture: string | null; confirmed: boolean; source: CropHarvest["source"]; actor: string; anomaly: CropHarvest["anomaly_kind"] }): CropHarvest {
  const s = rec.s;
  const at = nowIso();
  const yieldT = div4(sc4(o.mass), mul4(sc4(s.area_ha), 10_000_000n)) ?? 0n;
  const h: CropHarvest = {
    id: uuidv7(),
    season_ref: s.season_ref,
    plot_ref: s.plot_ref,
    cooperative_ref: s.cooperative_ref,
    household_ref: s.household_ref ?? null,
    variety_code: s.variety_code,
    season_code: s.season_code,
    season_year: s.season_year,
    harvest_date: o.date,
    mass_kg: o.mass,
    area_ha: s.area_ha,
    yield_t_per_ha: fmt4(yieldT, 2),
    moisture_pct: o.moisture,
    anomaly_kind: o.anomaly,
    anomaly_threshold_t_per_ha: o.anomaly ? anomalyThreshold(s.variety_code) : null,
    anomaly_confirmed: o.confirmed,
    source: o.source,
    location_status: "missing",
    gps_source: "no_device",
    distance_m: null,
    on_behalf: o.actor !== farmerOf(s.household_ref ?? null),
    actor_ref: o.actor,
    event_time: at,
    recorded_at: at,
    supersedes_id: null,
    correction_reason: null,
  };
  HARVESTS.push(h);
  const evId = uuidv7();
  Object.assign(s, { status: "completed", harvest_id: h.id, completed_at: at, updated_at: at, current_event_id: evId, version: s.version + 1 });
  rec.events.push({ ...rec.events[0]!, id: evId, seq: rec.events.length + 1, action: "completed", actor_ref: o.actor, event_time: at, recorded_at: at, supersedes_id: null, correction_reason: null, harvest_id: h.id });
  return h;
}

function seedSeasons(): void {
  const today = todayVN();
  const dx = SOPS[0]!.versions[0]!;
  const st25 = SOPS[1]!.versions[0]!;
  for (const coop of CROP_COOPS) {
    const r = mulberry32(parseInt(coop.api.ref.slice(-5), 16) + 31);
    const plots = plotsOf(coop).filter((p) => p.properties.status === "active" && p.properties.cooperative_ref === coop.api.ref);
    plots.forEach((plot, i) => {
      const ref = PLOTS.get(plot.properties.ref)!;
      const variety = VARIETIES[i % VARIETIES.length]!;
      const sop = variety === "ST25" ? st25 : dx;
      const farmer = farmerOf(plot.properties.household_ref ?? null);
      const loc: CropSeason["location_status"] = i % 7 === 2 ? "mismatch" : i % 7 === 5 ? "missing" : "match";
      if (i % 5 !== 4) {
        // Vụ Đông Xuân 2026 đã hoàn thành (có khai sản lượng) — sao chép cấu hình vụ trước được.
        const prev = newSeason(ref, { code: "DX", year: 2026, variety, sowing: addDays(today, -230), sop, actor: farmer, onBehalf: false, loc: "match", at: new Date(Date.now() - 230 * 86_400_000).toISOString() });
        const tPerHa = sc4(expectedYieldOf(variety) ?? "6") + BigInt(Math.floor((r() - 0.5) * 12000));
        const mass = fmt4(mul4(mul4(tPerHa, sc4(plot.properties.area_ha)), 10_000_000n), 0);
        harvestFor(prev, { mass, date: addDays(today, -130), moisture: "14.5", confirmed: false, source: "app", actor: farmer, anomaly: null });
      }
      if (i % 5 === 1 || i % 5 === 2 || i % 5 === 3) {
        const sowing = i % 5 === 3 ? addDays(today, -100) : addDays(today, -40 + (i % 3));
        const rec = newSeason(ref, { code: "HT", year: 2026, variety, sowing, sop, actor: i % 2 ? HTX_ACTOR : farmer, onBehalf: i % 2 === 1, loc, at: new Date(Date.parse(`${sowing}T02:00:00Z`)).toISOString() });
        const s = rec.s;
        addLog(s, { activity: "gieo_sa", seq: 2, status: "approved", author: farmer, loc: "match", daysAgo: 40, note: "Sạ hàng 80 kg giống/ha", machines: ["MGS"], files: 1 });
        addLog(s, {
          activity: "bon_phan",
          seq: 3,
          status: "pending",
          author: i === 1 ? HTX_ACTOR : farmer,
          loc,
          daysAgo: 3 + (i % 4),
          note: loc === "missing" ? "Bón thúc đợt 1 — máy không bật định vị" : "Bón thúc đợt 1",
          files: 1 + (i % 2),
          inputs: [{ name: "NPK 20-20-15", qty: "50", unit: "kg" }],
        });
        if (i % 5 === 3) addLog(s, { activity: "kiem_tra", seq: 7, status: "needs_supplement", author: farmer, loc: "missing", daysAgo: 8, note: "Kiểm tra rầy nâu", reviewNote: "Bổ sung ảnh hiện trường mật độ rầy" });
      }
    });
  }
}

let seeded = false;
function ensure(): void {
  if (seeded) return;
  seeded = true;
  seedSops();
  seedPlots();
  seedSeasons();
}

/** Cho kiểm thử E2E: dữ liệu mùa vụ giả lập của một HTX (sau khi sinh). */
export function cropSeasonsOf(coopRef: string): CropSeason[] {
  ensure();
  return SEASONS.filter((x) => x.s.cooperative_ref === coopRef).map((x) => x.s);
}
export function cropLogsOf(coopRef: string): CropFarmLogState[] {
  ensure();
  return LOGS.filter((x) => x.state.cooperative_ref === coopRef).map((x) => x.state);
}
export { HTX_ACTOR };

// --------------------------------------------------------------------------
// Quy tắc mở vụ (dùng chung mở lẻ / hàng loạt)
// --------------------------------------------------------------------------
type OpenCheck = { ok: true; ref: PlotRef } | { ok: false; res: Response; reason: string };

function checkPlot(a: Access | null, plotRef: string | undefined): OpenCheck {
  const ref = plotRef ? PLOTS.get(plotRef) : undefined;
  if (!ref) return { ok: false, res: problem(404, "cropcycle.plot_not_found", "Plot not found"), reason: "Không tìm thấy lô ruộng" };
  const p = ref.plot.properties;
  if (p.status !== "active" || !p.cooperative_ref) return { ok: false, res: problem(422, "cropcycle.plot_invalid", "Plot invalid"), reason: "Lô ruộng chưa gán HTX hoặc đã ngừng dùng" };
  const denied = writeDenied(a, p.cooperative_ref);
  if (denied) return { ok: false, res: denied, reason: "Lô không thuộc quyền quản lý" };
  return { ok: true, ref };
}
function openSeasonOf(plotRef: string): SeasonRec | undefined {
  return SEASONS.find((x) => x.s.plot_ref === plotRef && x.s.status === "open");
}
function lastSeasonOf(plotRef: string): SeasonRec | undefined {
  return SEASONS.filter((x) => x.s.plot_ref === plotRef && x.s.status !== "cancelled").sort((x, y) => Date.parse(x.s.opened_at ?? "") - Date.parse(y.s.opened_at ?? "")).at(-1);
}

function resolveSop(body: Record<string, unknown>, variety: string): CropSOPVersion | Response {
  const id = typeof body.sop_version_id === "string" && body.sop_version_id ? body.sop_version_id : null;
  if (id) {
    const f = findVersion(id);
    if (!f || f.v.status !== "published" || f.rec.t.status === "retired") return problem(422, "cropcycle.sop_not_published", "SOP not published");
    return f.v;
  }
  return publishedVersionFor(variety) ?? problem(422, "cropcycle.no_sop_for_variety", "No SOP for variety");
}

function commonOpenInput(body: Record<string, unknown>): { code: string; year: number; variety: string; sowing: string } | Response {
  const errors: { field: string; reason: string }[] = [];
  const code = typeof body.season_code === "string" ? body.season_code : "";
  const year = typeof body.season_year === "number" ? body.season_year : 0;
  const variety = typeof body.variety_code === "string" ? body.variety_code : "";
  const sowing = typeof body.sowing_date === "string" ? body.sowing_date : "";
  if (!CATALOGS.season.some((s) => s.code === code && s.active)) errors.push({ field: "season_code", reason: "không có mã vụ đang dùng" });
  if (year < 2020 || year > 2100) errors.push({ field: "season_year", reason: "năm không hợp lệ" });
  if (!DATE.test(sowing)) errors.push({ field: "sowing_date", reason: "phải có dạng YYYY-MM-DD" });
  if (errors.length) return validation(errors);
  if (!CATALOGS.rice_variety.some((v) => v.code === variety && v.active)) {
    return problem(422, "cropcycle.catalog_invalid", "Catalog invalid", { errors: [{ field: "variety_code", reason: `không có mã ${variety} đang dùng trong danh mục rice_variety` }] });
  }
  const t = dayNo(todayVN());
  const d = dayNo(sowing);
  if (d < t - 15 || d > t + 30) return problem(422, "cropcycle.sowing_date_out_of_window", "Sowing date out of window");
  return { code, year, variety, sowing };
}

function bodyOf(body: unknown): Record<string, unknown> {
  return (body ?? {}) as Record<string, unknown>;
}

// --------------------------------------------------------------------------
// Sản lượng (dùng chung bảng nhập nhanh / nhập tệp)
// --------------------------------------------------------------------------
interface HarvestRowIn {
  season_ref?: string;
  plot_code?: string;
  harvest_date?: string;
  mass_kg?: string | null;
  moisture_pct?: string | null;
  confirm_anomaly?: boolean;
}
type RowOut = { row: number; status: "accepted" | "duplicate" | "rejected"; code?: string; reason?: string; needs_confirm?: boolean; season_ref?: string; harvest_id?: string };

function declareRow(a: Access | null, p: MockPersona, coopRef: string | null, row: number, input: HarvestRowIn, source: CropHarvest["source"]): RowOut {
  const today = todayVN();
  let rec: SeasonRec | undefined;
  if (input.season_ref) rec = SEASONS.find((x) => x.s.season_ref === input.season_ref);
  else if (input.plot_code) {
    const code = input.plot_code.trim();
    const candidates = SEASONS.filter((x) => x.s.plot_code === code && (!coopRef || x.s.cooperative_ref === coopRef));
    rec = candidates.find((x) => x.s.status === "open") ?? candidates.filter((x) => x.s.status === "completed").at(-1);
  }
  const code = rec?.s.plot_code ?? input.plot_code;
  if (!rec) return { row, status: "rejected", code, reason: "Không tìm thấy vụ đang mở của lô ruộng" };
  const denied = writeDenied(a, rec.s.cooperative_ref);
  if (denied) return { row, status: "rejected", code, reason: "Nông hộ hoặc lô ruộng này không thuộc phạm vi quản lý của bạn." };
  if (rec.s.status === "completed") return { row, status: "duplicate", code, reason: "Vụ đã khai sản lượng — sửa bằng bản ghi bù", season_ref: rec.s.season_ref, harvest_id: rec.s.harvest_id ?? undefined };
  if (rec.s.status !== "open") return { row, status: "rejected", code, reason: "Vụ đã hủy" };
  const mass = (input.mass_kg ?? "").toString().trim();
  if (!mass) return { row, status: "rejected", code, reason: "Thiếu sản lượng" };
  if (!/^-?\d+(\.\d{1,3})?$/.test(mass)) return { row, status: "rejected", code, reason: "Sản lượng sai định dạng số" };
  if (mass.startsWith("-")) return { row, status: "rejected", code, reason: "Sản lượng không được âm" };
  const date = input.harvest_date && DATE.test(input.harvest_date) ? input.harvest_date : today;
  if (dayNo(date) > dayNo(today)) return { row, status: "rejected", code, reason: "Ngày thu hoạch ở tương lai" };
  if (dayNo(date) < dayNo(rec.s.harvest_open_date)) return { row, status: "rejected", code, reason: "Lô ruộng chưa đến giai đoạn thu hoạch, chưa thể khai báo sản lượng." };
  const yieldT = div4(sc4(mass), mul4(sc4(rec.s.area_ha), 10_000_000n)) ?? 0n;
  const anomaly: CropHarvest["anomaly_kind"] = sc4(mass) === 0n ? "zero" : yieldT > sc4(anomalyThreshold(rec.s.variety_code)) ? "above_threshold" : null;
  if (anomaly && !input.confirm_anomaly) {
    return {
      row,
      status: "rejected",
      code,
      needs_confirm: true,
      season_ref: rec.s.season_ref,
      reason:
        anomaly === "zero"
          ? "Sản lượng bằng 0 — xác nhận lại trước khi lưu"
          : `Năng suất ${fmt4(yieldT, 2)} t/ha vượt ngưỡng ${anomalyThreshold(rec.s.variety_code)} t/ha của giống ${rec.s.variety_code} — xác nhận lại trước khi lưu`,
    };
  }
  const h = harvestFor(rec, { mass, date, moisture: input.moisture_pct ?? null, confirmed: !!anomaly, source, actor: p.me.actor_ref, anomaly });
  return { row, status: "accepted", code, season_ref: rec.s.season_ref, harvest_id: h.id };
}

function bulkResult(rows: RowOut[]) {
  return {
    accepted: rows.filter((r) => r.status === "accepted").length,
    duplicate: rows.filter((r) => r.status === "duplicate").length,
    rejected: rows.filter((r) => r.status === "rejected").length,
    rows,
  };
}

// --------------------------------------------------------------------------
// Bảng điều hành
// --------------------------------------------------------------------------
function dashGroup(key: string | undefined, seasons: CropSeason[]) {
  const live = seasons.filter((s) => s.status !== "cancelled");
  let area = 0n;
  let expected = 0n;
  let actual = 0n;
  let harvested = 0n;
  let steps = 0;
  let stepsDone = 0;
  let approved = 0;
  let pending = 0;
  for (const s of live) {
    area += sc4(s.area_ha);
    expected += mul4(mul4(sc4(s.area_ha), sc4(s.expected_yield_t_per_ha ?? "0")), 10_000_000n);
    const h = s.harvest_id ? HARVESTS.find((x) => x.id === s.harvest_id) : undefined;
    if (h) {
      actual += sc4(h.mass_kg);
      harvested += sc4(s.area_ha);
    }
    const sop = findVersion(s.sop_version_id)?.v;
    steps += sop?.steps.length ?? 0;
    const logs = LOGS.filter((l) => l.state.season_ref === s.season_ref);
    approved += logs.filter((l) => l.state.status === "approved").length;
    pending += logs.filter((l) => l.state.status === "pending").length;
    stepsDone += s.status === "completed" ? (sop?.steps.length ?? 0) : new Set(logs.filter((l) => l.state.status === "approved" && l.state.sop_step_seq).map((l) => l.state.sop_step_seq)).size;
  }
  const yieldT = div4(actual, mul4(harvested, 10_000_000n));
  return {
    key,
    seasons: live.length,
    open_seasons: live.filter((s) => s.status === "open").length,
    completed_seasons: live.filter((s) => s.status === "completed").length,
    households: new Set(live.map((s) => s.household_ref).filter(Boolean)).size,
    area_ha: fmt4(area),
    expected_kg: fmt4(expected, 3),
    actual_kg: fmt4(actual, 3),
    harvested_area_ha: fmt4(harvested),
    actual_yield_t_per_ha: harvested > 0n && yieldT !== null ? fmt4(yieldT, 2) : null,
    sop_progress_pct: steps > 0 ? fmt4(BigInt(Math.round((stepsDone / steps) * 1_000_000)), 2) : null,
    approved_logs: approved,
    pending_logs: pending,
  };
}
function groupBy(seasons: CropSeason[], key: (s: CropSeason) => string) {
  const m = new Map<string, CropSeason[]>();
  for (const s of seasons) m.set(key(s), [...(m.get(key(s)) ?? []), s]);
  return [...m.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => dashGroup(k, v));
}

// --------------------------------------------------------------------------
// Application API
// --------------------------------------------------------------------------
const NO_PHONE = /(^|\D)(0|\+?84)\d{9}(\D|$)/;

function visibleSeasons(a: Access): SeasonRec[] {
  return SEASONS.filter((x) => sees(a, x.s.cooperative_ref, x.s.commune_code));
}
function findLog(id: string | undefined): LogRec | undefined {
  return LOGS.find((l) => l.versions.some((v) => v.id === id) || l.state.log_ref === id);
}

function seasonDetail(rec: SeasonRec) {
  const sop = findVersion(rec.s.sop_version_id)?.v;
  const logs = LOGS.filter((l) => l.state.season_ref === rec.s.season_ref);
  const approvedSeqs = new Set(logs.filter((l) => l.state.status === "approved").map((l) => l.state.sop_step_seq));
  const steps = sop?.steps ?? [];
  const h = rec.s.harvest_id ? HARVESTS.find((x) => x.id === rec.s.harvest_id) : undefined;
  const g = dashGroup(undefined, [rec.s]);
  return {
    ...rec.s,
    events: rec.events,
    steps,
    progress: {
      sop_steps: steps.length,
      sop_mandatory_steps: steps.filter((s) => s.mandatory).length,
      sop_steps_done: steps.filter((s) => approvedSeqs.has(s.seq)).length,
      sop_mandatory_done: steps.filter((s) => s.mandatory && approvedSeqs.has(s.seq)).length,
      approved_logs: g.approved_logs,
      pending_logs: g.pending_logs,
      expected_kg: g.expected_kg,
      actual_kg: h ? h.mass_kg : null,
      actual_yield_t_per_ha: h ? h.yield_t_per_ha : null,
    },
    harvest: h ?? null,
  };
}

function recordLogFor(p: MockPersona, rec: SeasonRec, body: Record<string, unknown>, batch: string | null): CropFarmLog {
  const at = nowIso();
  const id = uuidv7();
  const gps = (body.gps ?? null) as CropFarmLog["gps"];
  const hasPoint = !!gps && gps.lat !== null && gps.lat !== undefined && gps.lon !== null && gps.lon !== undefined;
  const log: CropFarmLog = {
    id,
    log_ref: id,
    version: 1,
    season_ref: rec.s.season_ref,
    plot_ref: rec.s.plot_ref,
    household_ref: rec.s.household_ref ?? null,
    cooperative_ref: rec.s.cooperative_ref,
    commune_code: rec.s.commune_code,
    activity_code: String(body.activity_code),
    sop_step_seq: typeof body.sop_step_seq === "number" ? body.sop_step_seq : null,
    event_time: typeof body.event_time === "string" && body.event_time ? body.event_time : at,
    gps: gps ?? { source: "no_device" },
    location_status: hasPoint ? "match" : "missing",
    distance_m: hasPoint ? "12.0" : null,
    machine_codes: Array.isArray(body.machine_codes) ? (body.machine_codes as string[]) : [],
    inputs: Array.isArray(body.inputs) ? (body.inputs as { name: string; qty: string | number; unit: string }[]).map((i) => ({ name: i.name, qty: String(i.qty), unit: i.unit })) : [],
    note: typeof body.note === "string" && body.note ? body.note : null,
    file_refs: Array.isArray(body.file_refs) ? (body.file_refs as string[]) : [],
    batch_ref: batch,
    on_behalf: !hasRole(p, "farmer"),
    channel: "web",
    author_ref: p.me.actor_ref,
    recorded_at: at,
    supersedes_id: null,
    correction_reason: null,
    became_current: true,
  };
  LOGS.push({ versions: [log], state: stateOf(log, "pending") });
  return log;
}

function logContentErrors(body: Record<string, unknown>): Response | null {
  const errors: { field: string; reason: string }[] = [];
  const activity = body.activity_code;
  if (typeof activity !== "string" || !["lam_dat", "gieo_sa", "tuoi_nuoc", "bon_phan", "phun_thuoc", "kiem_tra", "thu_hoach"].includes(activity)) errors.push({ field: "activity_code", reason: "hoạt động không hợp lệ" });
  const note = typeof body.note === "string" ? body.note : "";
  if (note.length > 2000) errors.push({ field: "note", reason: "tối đa 2000 ký tự" });
  if (NO_PHONE.test(note)) errors.push({ field: "note", reason: "Không ghi SĐT/CCCD trong ghi chú" });
  const machines = Array.isArray(body.machine_codes) ? (body.machine_codes as string[]) : [];
  for (const m of machines) if (!CATALOGS.machine_type.some((x) => x.code === m && x.active)) return problem(422, "cropcycle.catalog_invalid", "Catalog invalid", { errors: [{ field: "machine_codes", reason: `không có mã ${m} đang dùng trong danh mục machine_type` }] });
  return errors.length ? validation(errors) : null;
}

export const cropcycleAppRoutes: Route[] = [
  // --- SOP đã ban hành ---
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/sops$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = accessOf(persona);
      if (!a && !hasRole(persona, "farmer")) return problem(403, "forbidden", "Forbidden");
      const list = SOPS.filter((x) => x.t.status === "published" && (x.t.scope === "system" || !a || a.kind !== "coop" || x.t.cooperative_ref === a.coop)).map((x) => x.t);
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/sops\/([\w-]+)$/,
    handler: ({ params }) => {
      ensure();
      const rec = SOPS.find((x) => x.t.ref === params[0] && x.t.status !== "draft");
      return rec ? json(sopOut(rec, true)) : problem(404, "cropcycle.sop_not_found", "Not found");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/sop-versions\/([\w-]+)$/,
    handler: ({ params }) => {
      ensure();
      const f = findVersion(params[0]);
      return f && f.v.status === "published" ? json({ version: f.v, template: f.rec.t }) : problem(404, "cropcycle.sop_not_found", "Not found");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/variety-bindings$/,
    handler: () => {
      ensure();
      return json({ items: BINDINGS });
    },
  },

  // --- Vụ ---
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/seasons$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = accessOf(persona);
      if (!a) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const year = Number(q("season_year")) || null;
      const list = visibleSeasons(a)
        .map((x) => x.s)
        .filter(
          (s) =>
            (!q("cooperative_ref") || s.cooperative_ref === q("cooperative_ref")) &&
            (!q("plot_ref") || s.plot_ref === q("plot_ref")) &&
            (!q("status") || s.status === q("status")) &&
            (!q("season_code") || s.season_code === q("season_code")) &&
            (!year || s.season_year === year) &&
            (!q("variety_code") || s.variety_code === q("variety_code")),
        )
        .sort((x, y) => Date.parse(y.updated_at ?? "") - Date.parse(x.updated_at ?? "") || x.plot_code.localeCompare(y.plot_code));
      return json(paginate(list, url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/seasons$/,
    handler: ({ body, persona }) => {
      ensure();
      const b = bodyOf(body);
      const a = accessOf(persona);
      const chk = checkPlot(a, typeof b.plot_ref === "string" ? b.plot_ref : undefined);
      if (!chk.ok) return chk.res;
      const plotRef = chk.ref.plot.properties.ref;
      const open = openSeasonOf(plotRef);
      if (open) return problem(409, "cropcycle.season_already_open", "Season already open", { open_season_ref: open.s.season_ref });
      let variety = typeof b.variety_code === "string" ? b.variety_code : "";
      let sopBody = b;
      let copied: string | null = null;
      if (b.copy_previous === true) {
        const last = lastSeasonOf(plotRef);
        if (!last) return problem(409, "cropcycle.no_previous_season", "No previous season");
        if (last.s.status !== "completed") return problem(409, "cropcycle.previous_season_not_completed", "Previous season not completed");
        variety = last.s.variety_code;
        sopBody = { ...b, sop_version_id: last.s.sop_version_id };
        copied = last.s.season_ref;
      }
      const common = commonOpenInput({ ...b, variety_code: variety });
      if (common instanceof Response) return common;
      const sop = resolveSop(sopBody, variety);
      if (sop instanceof Response) return sop;
      const gps = (b.gps ?? null) as { lat?: unknown } | null;
      const rec = newSeason(chk.ref, { ...common, sop, actor: persona.me.actor_ref, onBehalf: !hasRole(persona, "farmer"), loc: gps && gps.lat ? "match" : "missing", copied });
      return json({ season: rec.s, event: rec.events[0] }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/seasons\/bulk-open$/,
    handler: ({ body, persona }) => {
      ensure();
      if (!hasRole(persona, "coop_manager", "platform_admin")) return hasRole(persona, "kn_officer", "kn_province_admin", "kn_national_admin") ? problem(403, "cropcycle.read_only", "Read only") : problem(403, "forbidden", "Forbidden");
      const b = bodyOf(body);
      const refs = Array.isArray(b.plot_refs) ? (b.plot_refs as string[]) : [];
      if (refs.length === 0 || refs.length > 200) return validation([{ field: "plot_refs", reason: "chọn từ 1 đến 200 lô" }]);
      const common = commonOpenInput(b);
      if (common instanceof Response) return common;
      const sop = resolveSop(b, common.variety);
      if (sop instanceof Response) return sop;
      const a = accessOf(persona);
      const items = refs.map((plotRef) => {
        const chk = checkPlot(a, plotRef);
        const code = PLOTS.get(plotRef)?.plot.properties.code;
        if (!chk.ok) return { plot_ref: plotRef, status: "skipped" as const, code, reason: chk.reason };
        if (openSeasonOf(plotRef)) return { plot_ref: plotRef, status: "skipped" as const, code, reason: "Lô đang canh tác vụ khác" };
        const rec = newSeason(chk.ref, { ...common, sop, actor: persona.me.actor_ref, onBehalf: true, loc: "missing" });
        return { plot_ref: plotRef, status: "opened" as const, code, season_ref: rec.s.season_ref };
      });
      return json({ opened: items.filter((i) => i.status === "opened").length, skipped: items.filter((i) => i.status === "skipped").length, items });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/seasons\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      ensure();
      const a = accessOf(persona);
      const rec = SEASONS.find((x) => x.s.season_ref === params[0]);
      if (!a || !rec || !sees(a, rec.s.cooperative_ref, rec.s.commune_code)) return problem(404, "cropcycle.season_not_found", "Not found");
      return json(seasonDetail(rec));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/seasons\/([\w-]+)\/(corrections|cancel)$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const a = accessOf(persona);
      const rec = SEASONS.find((x) => x.s.season_ref === params[0]);
      if (!a || !rec || !sees(a, rec.s.cooperative_ref, rec.s.commune_code)) return problem(404, "cropcycle.season_not_found", "Not found");
      const denied = writeDenied(a, rec.s.cooperative_ref);
      if (denied) return denied;
      const b = bodyOf(body);
      const cancel = params[1] === "cancel";
      const reason = reasonOf(b, cancel ? "reason" : "correction_reason");
      if (reason.length < 10) return validation([{ field: cancel ? "reason" : "correction_reason", reason: "tối thiểu 10 ký tự" }]);
      if (rec.s.status !== "open") return problem(409, "cropcycle.season_state", "Season not open");
      if (b.supersedes_id !== rec.s.current_event_id) return problem(409, "cropcycle.concurrent_edit", "Concurrent edit", { current_event_id: rec.s.current_event_id });
      const at = nowIso();
      const evId = uuidv7();
      if (!cancel) {
        const variety = typeof b.variety_code === "string" && b.variety_code ? b.variety_code : rec.s.variety_code;
        const sowing = typeof b.sowing_date === "string" && b.sowing_date ? b.sowing_date : rec.s.sowing_date;
        const common = commonOpenInput({ season_code: rec.s.season_code, season_year: rec.s.season_year, variety_code: variety, sowing_date: sowing });
        if (common instanceof Response) return common;
        const sop = variety !== rec.s.variety_code || b.sop_version_id ? resolveSop(b, variety) : findVersion(rec.s.sop_version_id)!.v;
        if (sop instanceof Response) return sop;
        const h = harvestOf(sop.steps);
        Object.assign(rec.s, {
          variety_code: variety,
          sowing_date: sowing,
          sop_version_id: sop.id,
          sop_version_no: sop.version_no,
          sop_template_ref: sop.template_ref,
          expected_yield_t_per_ha: expectedYieldOf(variety),
          expected_harvest_date: addDays(sowing, h.off ?? 95),
          harvest_open_date: addDays(sowing, (h.off ?? 95) - (h.win ?? 5)),
        });
      } else {
        rec.s.status = "cancelled";
      }
      Object.assign(rec.s, { current_event_id: evId, version: rec.s.version + 1, updated_at: at });
      const ev: CropSeasonEvent = {
        ...rec.events[0]!,
        id: evId,
        seq: rec.events.length + 1,
        action: cancel ? "cancelled" : "corrected",
        variety_code: rec.s.variety_code,
        sop_version_id: rec.s.sop_version_id,
        sowing_date: rec.s.sowing_date,
        actor_ref: persona.me.actor_ref,
        on_behalf: true,
        channel: "web",
        event_time: at,
        recorded_at: at,
        supersedes_id: String(b.supersedes_id),
        correction_reason: reason,
      };
      rec.events.push(ev);
      return json({ season: rec.s, event: ev }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/plots\/([\w-]+)\/previous-config$/,
    handler: ({ params, persona }) => {
      ensure();
      const a = accessOf(persona);
      const ref = PLOTS.get(params[0] ?? "");
      if (!a || !ref || !sees(a, ref.coop.api.ref, ref.plot.properties.commune_code)) return problem(404, "cropcycle.plot_not_found", "Not found");
      const last = lastSeasonOf(ref.plot.properties.ref);
      if (!last) return problem(409, "cropcycle.no_previous_season", "No previous season");
      if (last.s.status !== "completed") return problem(409, "cropcycle.previous_season_not_completed", "Previous season not completed");
      return json({
        season_ref: last.s.season_ref,
        season_code: last.s.season_code,
        season_year: last.s.season_year,
        variety_code: last.s.variety_code,
        sop_template_ref: last.s.sop_template_ref ?? "",
        sop_version_id: last.s.sop_version_id,
        sop_version_no: last.s.sop_version_no ?? 1,
      });
    },
  },

  // --- Sản lượng ---
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/harvests\/bulk$/,
    handler: ({ body, persona }) => {
      ensure();
      if (!hasRole(persona, "coop_manager", "platform_admin")) return problem(403, hasRole(persona, "kn_officer", "kn_province_admin", "kn_national_admin") ? "cropcycle.read_only" : "forbidden", "Forbidden");
      const b = bodyOf(body);
      const rows = Array.isArray(b.rows) ? (b.rows as HarvestRowIn[]) : [];
      if (rows.length === 0 || rows.length > 200) return validation([{ field: "rows", reason: "từ 1 đến 200 dòng" }]);
      const a = accessOf(persona);
      const coop = a?.kind === "coop" ? a.coop : typeof b.cooperative_ref === "string" ? b.cooperative_ref : null;
      return json(bulkResult(rows.map((r, i) => declareRow(a, persona, coop, i + 1, r, "web"))));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/harvests\/import$/,
    handler: ({ body, persona, url, request }) => {
      ensure();
      if (!hasRole(persona, "coop_manager", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const a = accessOf(persona);
      const coop = a?.kind === "coop" ? a.coop : url.searchParams.get("cooperative_ref");
      if (isZip(body)) {
        // .xlsx: giả lập không giải nén — kết quả minh hoạ (một dòng hợp lệ, hai dòng lỗi).
        const open = SEASONS.find((x) => x.s.status === "open" && x.s.cooperative_ref === coop && dayNo(x.s.harvest_open_date) <= dayNo(todayVN()));
        const first: RowOut = open
          ? declareRow(a, persona, coop, 2, { season_ref: open.s.season_ref, mass_kg: fmt4(mul4(mul4(sc4(open.s.area_ha), sc4("6.2")), 10_000_000n), 0) }, "excel")
          : { row: 2, status: "rejected", reason: "Không tìm thấy vụ đang mở của lô ruộng" };
        return json(bulkResult([first, { row: 3, status: "rejected", code: "TH-KHONG-CO", reason: "Không tìm thấy vụ đang mở của lô ruộng" }, { row: 4, status: "rejected", reason: "Thiếu sản lượng" }]));
      }
      if (!(request.headers.get("Content-Type") ?? "").startsWith("text/csv")) return problem(422, "cropcycle.import_invalid", "Expected CSV or XLSX");
      const rows = parseCsv(typeof body === "string" ? body : "");
      const header = (rows[0] ?? []).map(headerKey);
      const col = (...names: string[]) => header.findIndex((h) => names.includes(h));
      const iPlot = col("plot_code", "ma_lo", "ma_thua");
      const iSeason = col("season_ref", "ma_vu");
      const iKg = col("mass_kg", "san_luong_kg");
      const iT = col("tonnes", "san_luong_tan");
      const iDate = col("harvest_date", "ngay_thu_hoach");
      const iMoist = col("moisture_pct", "do_am");
      const iConfirm = col("confirm_anomaly", "xac_nhan_bat_thuong");
      if ((iPlot < 0 && iSeason < 0) || (iKg < 0 && iT < 0)) return problem(422, "cropcycle.import_invalid", "Missing columns");
      const data = rows.slice(1);
      if (data.length === 0 || data.length > 200) return problem(422, "cropcycle.import_invalid", "Row count out of range");
      if (data.some((r) => r.some((c) => c.startsWith("=")))) return problem(422, "cropcycle.import_invalid", "Formula cells");
      const out = data.map((r, i) => {
        const cell = (j: number) => (j >= 0 ? (r[j] ?? "").trim() : "");
        let mass = cell(iKg).replace(",", ".");
        if (!mass && cell(iT)) {
          const t = cell(iT).replace(",", ".");
          mass = /^\d+(\.\d+)?$/.test(t) ? fmt4(sc4(t) * 1000n, 3).replace(/\.?0+$/, "") : t;
        }
        return declareRow(a, persona, coop, i + 2, {
          plot_code: cell(iPlot) || undefined,
          season_ref: cell(iSeason) || undefined,
          mass_kg: mass,
          harvest_date: cell(iDate) || undefined,
          moisture_pct: cell(iMoist) || null,
          confirm_anomaly: /^(1|x|có|co|true|yes)$/i.test(cell(iConfirm)),
        }, "excel");
      });
      return json(bulkResult(out));
    },
  },

  // --- Nhật ký ---
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/farm-logs$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = accessOf(persona);
      if (!a) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const list = LOGS.map((l) => l.state)
        .filter(
          (s) =>
            s.status !== "superseded" &&
            sees(a, s.cooperative_ref ?? "", SEASONS.find((x) => x.s.season_ref === s.season_ref)?.s.commune_code ?? "") &&
            (!q("cooperative_ref") || s.cooperative_ref === q("cooperative_ref")) &&
            (!q("season_ref") || s.season_ref === q("season_ref")) &&
            (!q("status") || s.status === q("status")),
        )
        .sort((x, y) => Date.parse(y.event_time ?? "") - Date.parse(x.event_time ?? ""));
      return json(paginate(list, url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/farm-logs$/,
    handler: ({ body, persona }) => {
      ensure();
      const a = accessOf(persona);
      const b = bodyOf(body);
      const bad = logContentErrors(b);
      if (bad) return bad;
      let rec: SeasonRec | undefined;
      if (typeof b.season_ref === "string" && b.season_ref) rec = SEASONS.find((x) => x.s.season_ref === b.season_ref);
      else if (typeof b.plot_ref === "string") {
        const chk = checkPlot(a, b.plot_ref);
        if (!chk.ok) return chk.res;
        rec = openSeasonOf(b.plot_ref);
        if (!rec) return problem(409, "cropcycle.season_not_open", "Season not open");
      }
      if (!rec) return problem(404, "cropcycle.season_not_found", "Not found");
      const denied = writeDenied(a, rec.s.cooperative_ref);
      if (denied) return denied;
      if (rec.s.status !== "open") return problem(409, "cropcycle.season_not_open", "Season not open");
      const log = recordLogFor(persona, rec, b, null);
      return json({ log, state: LOGS.at(-1)!.state }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/farm-logs\/batch$/,
    handler: ({ body, persona }) => {
      ensure();
      const a = accessOf(persona);
      if (a?.kind === "kn") return problem(403, "cropcycle.read_only", "Read only");
      const b = bodyOf(body);
      const bad = logContentErrors(b);
      if (bad) return bad;
      const refs = Array.isArray(b.plot_refs) ? (b.plot_refs as string[]) : [];
      if (refs.length === 0 || refs.length > 200) return validation([{ field: "plot_refs", reason: "chọn từ 1 đến 200 lô" }]);
      const batch = uuidv7();
      const skipped: { plot_ref: string; code?: string; reason: string }[] = [];
      const lines: CropFarmLog[] = [];
      for (const plotRef of refs) {
        const code = PLOTS.get(plotRef)?.plot.properties.code;
        const chk = checkPlot(a, plotRef);
        if (!chk.ok) {
          skipped.push({ plot_ref: plotRef, code, reason: chk.reason });
          continue;
        }
        const rec = openSeasonOf(plotRef);
        if (!rec) {
          skipped.push({ plot_ref: plotRef, code, reason: "Lô ruộng chưa được mở vụ" });
          continue;
        }
        lines.push(recordLogFor(persona, rec, b, batch));
      }
      if (lines.length === 0) return problem(422, "cropcycle.batch_empty", "Batch empty", { skipped });
      return json({ batch_ref: batch, requested: refs.length, accepted: lines.length, skipped, lines }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/farm-logs\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      ensure();
      const a = accessOf(persona);
      const rec = findLog(params[0]);
      const season = rec ? SEASONS.find((x) => x.s.season_ref === rec.state.season_ref) : undefined;
      if (!a || !rec || !season || !sees(a, season.s.cooperative_ref, season.s.commune_code)) return problem(404, "cropcycle.log_not_found", "Not found");
      const log = rec.versions.find((v) => v.id === params[0]) ?? rec.versions.at(-1)!;
      return json({ log, state: rec.state, versions: rec.versions });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/farm-logs\/([\w-]+)\/corrections$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const a = accessOf(persona);
      const rec = findLog(params[0]);
      if (!a || !rec || !sees(a, rec.state.cooperative_ref ?? "", SEASONS.find((x) => x.s.season_ref === rec.state.season_ref)?.s.commune_code ?? "")) return problem(404, "cropcycle.log_not_found", "Not found");
      const denied = writeDenied(a, rec.state.cooperative_ref ?? "");
      if (denied) return denied;
      const b = bodyOf(body);
      const bad = logContentErrors(b);
      if (bad) return bad;
      const reason = reasonOf(b, "correction_reason");
      if (reason.length < 10) return validation([{ field: "correction_reason", reason: "tối thiểu 10 ký tự" }]);
      const cur = rec.versions.at(-1)!;
      if (cur.id !== params[0]) return problem(409, "cropcycle.concurrent_edit", "Concurrent edit", { current_id: cur.id });
      if (typeof b.event_time === "string" && b.event_time && Date.parse(b.event_time) !== Date.parse(cur.event_time)) return problem(422, "cropcycle.locked_fields", "Locked fields");
      if (b.gps && JSON.stringify(b.gps) !== JSON.stringify(cur.gps)) return problem(422, "cropcycle.locked_fields", "Locked fields");
      const at = nowIso();
      const next: CropFarmLog = {
        ...cur,
        id: uuidv7(),
        version: cur.version + 1,
        activity_code: String(b.activity_code),
        sop_step_seq: typeof b.sop_step_seq === "number" ? b.sop_step_seq : null,
        machine_codes: Array.isArray(b.machine_codes) ? (b.machine_codes as string[]) : cur.machine_codes,
        inputs: Array.isArray(b.inputs) ? (b.inputs as { name: string; qty: string | number; unit: string }[]).map((i) => ({ name: i.name, qty: String(i.qty), unit: i.unit })) : cur.inputs,
        note: typeof b.note === "string" ? b.note : cur.note,
        file_refs: Array.isArray(b.file_refs) ? (b.file_refs as string[]) : cur.file_refs,
        author_ref: persona.me.actor_ref,
        on_behalf: true,
        channel: "web",
        recorded_at: at,
        supersedes_id: cur.id,
        correction_reason: reason,
      };
      rec.versions.push(next);
      rec.state = stateOf(next, "pending");
      return json({ log: next, state: rec.state }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/farm-logs\/([\w-]+)\/reviews$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const a = accessOf(persona);
      if (a?.kind === "kn") return problem(403, "cropcycle.read_only", "Read only");
      if (!hasRole(persona, "coop_manager", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const rec = findLog(params[0]);
      if (!a || !rec || !sees(a, rec.state.cooperative_ref ?? "", "")) return problem(404, "cropcycle.log_not_found", "Not found");
      const b = bodyOf(body);
      const decision = b.decision;
      if (decision !== "approved" && decision !== "needs_supplement") return validation([{ field: "decision", reason: "approved | needs_supplement" }]);
      const note = reasonOf(b, "note");
      if (decision === "needs_supplement" && !note) return validation([{ field: "note", reason: "Bắt buộc ghi nội dung cần bổ sung" }]);
      if (rec.state.id !== params[0] || rec.state.status !== "pending") return problem(409, "cropcycle.log_state", "Log not pending");
      if (rec.state.author_ref === persona.me.actor_ref) return problem(403, "cropcycle.self_review", "Self review");
      Object.assign(rec.state, { status: decision, review_note: note || null, reviewed_by: persona.me.actor_ref, reviewed_at: nowIso() });
      return json({ review_id: uuidv7(), decision, state: rec.state }, 201);
    },
  },

  // --- Bảng điều hành ---
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/dashboard$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = accessOf(persona);
      if (!a) return problem(403, "forbidden", "Forbidden");
      const q = (k: string) => url.searchParams.get(k);
      const year = Number(q("season_year")) || null;
      const coop = q("cooperative_ref");
      if (a.kind === "coop" && coop && coop !== a.coop) return problem(403, "cropcycle.out_of_scope", "Out of scope");
      const list = visibleSeasons(a)
        .map((x) => x.s)
        .filter((s) => (!q("season_code") || s.season_code === q("season_code")) && (!year || s.season_year === year) && (!coop || s.cooperative_ref === coop) && (!q("variety_code") || s.variety_code === q("variety_code")));
      return json({
        totals: dashGroup(undefined, list),
        by_variety: groupBy(list, (s) => s.variety_code),
        by_season: groupBy(list, (s) => `${s.season_code}:${s.season_year}`),
        by_cooperative: groupBy(list, (s) => s.cooperative_ref),
      });
    },
  },

  // --- Ảnh minh chứng (luồng tệp theo mục đích, R-42): ảnh SVG tổng hợp ---
  {
    method: "GET",
    pattern: /^\/v1\/files\/([\w-]+)\/content$/,
    handler: ({ params, request }) => {
      const purpose = request.headers.get("X-Purpose");
      if (!purpose) return problem(400, "sync.purpose_required", "Purpose required");
      if (purpose !== "extension.field_support" && purpose !== "mrv.evidence") return problem(403, "sync.purpose_not_allowed", "Purpose not allowed");
      const hue = parseInt((params[0] ?? "0").slice(-2), 16) % 60;
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200" viewBox="0 0 320 200"><rect width="320" height="200" fill="hsl(${90 + hue},45%,62%)"/><path d="M0 150 Q80 120 160 150 T320 150 V200 H0Z" fill="hsl(${100 + hue},40%,38%)"/><text x="16" y="30" font-family="sans-serif" font-size="14" fill="#fff">Ảnh minh chứng giả lập</text></svg>`;
      return new Response(svg, { status: 200, headers: { "Content-Type": "image/svg+xml", "Cache-Control": "no-store" } });
    },
  },
];

// --------------------------------------------------------------------------
// Admin API — soạn / phát hành SOP, gắn giống, thống kê HTX
// --------------------------------------------------------------------------
const CODE = /^[A-Z0-9][A-Z0-9_.-]{1,39}$/;

function stepsError(steps: unknown, required: boolean): Response | CropSOPStep[] {
  if (!Array.isArray(steps)) return required ? validation([{ field: "steps", reason: "bắt buộc" }]) : [];
  const list = steps as CropSOPStep[];
  if (list.length > 100) return validation([{ field: "steps", reason: "tối đa 100 bước" }]);
  if (list.some((s, i) => s.seq !== i + 1)) return problem(422, "cropcycle.sop_step_order", "Step order", { errors: [{ field: "steps", reason: "Thứ tự bước không hợp lệ, vui lòng kiểm tra lại." }] });
  const bad = list.findIndex((s) => !s.name?.trim() || s.day_offset < -60 || s.day_offset > 365 || s.window_days < 0 || s.window_days > 60);
  if (bad >= 0) return validation([{ field: `steps[${bad}]`, reason: "tên bắt buộc; ngày trong −60…365; cửa sổ 0…60" }]);
  return list;
}
function sopAdminDenied(p: MockPersona, write: boolean): Response | null {
  const ok = write ? hasRole(p, "sop_admin", "platform_admin") : hasRole(p, "sop_admin", "param_approver", "platform_admin");
  return ok ? null : problem(403, "forbidden", "Forbidden");
}
function newTemplate(p: MockPersona, o: { code: string; name: string; scope: "system" | "htx"; coop: string | null; standard: string | null; description: string | null; steps: CropSOPStep[]; note: string | null; source: string | null }): SopRec {
  const at = nowIso();
  const ref = uuidv7();
  const t: CropSOPTemplate = { ref, code: o.code, name: o.name.trim(), crop: "lua", scope: o.scope, cooperative_ref: o.coop, standard: o.standard, description: o.description, status: "draft", status_reason: null, source_template_ref: o.source, latest_version: 1, published_version: null, created_at: at, updated_at: at };
  const h = harvestOf(o.steps);
  const v: CropSOPVersion = { id: uuidv7(), template_ref: ref, version_no: 1, status: "draft", steps: o.steps, harvest_day_offset: h.off, harvest_window_days: h.win, source_version_ref: null, change_note: o.note, authored_by: p.me.actor_ref, authored_at: at, published_by: null, published_at: null };
  const rec = { t, versions: [v] };
  SOPS.push(rec);
  return rec;
}

export const cropcycleAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/sops$/,
    handler: ({ url, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, false);
      if (d) return d;
      const status = url.searchParams.get("status");
      return json(paginate(SOPS.map((x) => x.t).filter((t) => !status || t.status === status), url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/sops$/,
    handler: ({ body, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, true);
      if (d) return d;
      const b = bodyOf(body);
      const code = typeof b.code === "string" ? b.code.trim() : "";
      const name = typeof b.name === "string" ? b.name : "";
      const errors: { field: string; reason: string }[] = [];
      if (!CODE.test(code)) errors.push({ field: "code", reason: "Mã chữ in hoa/số, 2–40 ký tự (A-Z 0-9 _ . -)" });
      if (!name.trim() || name.length > 200) errors.push({ field: "name", reason: "Tên từ 1 đến 200 ký tự" });
      if (errors.length) return validation(errors);
      const steps = stepsError(b.steps, true);
      if (steps instanceof Response) return steps;
      if (SOPS.some((x) => normName(x.t.name) === normName(name))) return problem(409, "cropcycle.sop_name_taken", "Name taken");
      if (SOPS.some((x) => x.t.code === code)) return problem(409, "cropcycle.sop_code_taken", "Code taken");
      const scope = b.scope === "htx" ? "htx" : "system";
      const rec = newTemplate(persona, {
        code,
        name,
        scope,
        coop: scope === "htx" && typeof b.cooperative_ref === "string" ? b.cooperative_ref : null,
        standard: typeof b.standard === "string" && b.standard ? b.standard : null,
        description: typeof b.description === "string" && b.description ? b.description : null,
        steps,
        note: typeof b.change_note === "string" && b.change_note ? b.change_note : null,
        source: null,
      });
      return json(sopOut(rec, false), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/sops\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, false);
      if (d) return d;
      const rec = SOPS.find((x) => x.t.ref === params[0]);
      return rec ? json(sopOut(rec, false)) : problem(404, "cropcycle.sop_not_found", "Not found");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/sops\/([\w-]+)\/versions$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, true);
      if (d) return d;
      const rec = SOPS.find((x) => x.t.ref === params[0]);
      if (!rec) return problem(404, "cropcycle.sop_not_found", "Not found");
      if (rec.t.status === "retired" || rec.versions.some((v) => v.status === "draft")) return problem(409, "cropcycle.sop_state", "SOP state");
      const b = bodyOf(body);
      const steps = b.steps === null || b.steps === undefined ? rec.versions.at(-1)!.steps : stepsError(b.steps, true);
      if (steps instanceof Response) return steps;
      const at = nowIso();
      const h = harvestOf(steps);
      const prev = rec.versions.at(-1)!;
      rec.versions.push({ id: uuidv7(), template_ref: rec.t.ref, version_no: prev.version_no + 1, status: "draft", steps, harvest_day_offset: h.off, harvest_window_days: h.win, source_version_ref: prev.id, change_note: typeof b.change_note === "string" ? b.change_note : null, authored_by: persona.me.actor_ref, authored_at: at, published_by: null, published_at: null });
      Object.assign(rec.t, { latest_version: prev.version_no + 1, updated_at: at });
      return json(sopOut(rec, false), 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/sops\/([\w-]+)\/clone$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, true);
      if (d) return d;
      const src = SOPS.find((x) => x.t.ref === params[0]);
      if (!src) return problem(404, "cropcycle.sop_not_found", "Not found");
      const b = bodyOf(body);
      const name = typeof b.name === "string" ? b.name : "";
      if (!name.trim()) return validation([{ field: "name", reason: "bắt buộc" }]);
      if (SOPS.some((x) => normName(x.t.name) === normName(name))) return problem(409, "cropcycle.sop_name_taken", "Name taken");
      let code = typeof b.code === "string" && b.code.trim() ? b.code.trim() : `${src.t.code}-BS${SOPS.length + 1}`.slice(0, 40);
      if (!CODE.test(code)) return validation([{ field: "code", reason: "Mã chữ in hoa/số, 2–40 ký tự" }]);
      if (SOPS.some((x) => x.t.code === code)) {
        if (b.code) return problem(409, "cropcycle.sop_code_taken", "Code taken");
        code = `${code}-${Date.now() % 1000}`;
      }
      const base = src.versions.filter((v) => v.status === "published").at(-1) ?? src.versions.at(-1)!;
      const rec = newTemplate(persona, {
        code,
        name,
        scope: b.scope === "htx" ? "htx" : src.t.scope,
        coop: typeof b.cooperative_ref === "string" ? b.cooperative_ref : src.t.cooperative_ref ?? null,
        standard: src.t.standard ?? null,
        description: src.t.description ?? null,
        steps: base.steps.map((s) => ({ ...s })),
        note: `Nhân bản từ ${src.t.code} phiên bản ${base.version_no}`,
        source: src.t.ref,
      });
      rec.versions[0]!.source_version_ref = base.id;
      return json(sopOut(rec, false), 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/sops\/([\w-]+)\/retire$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, true);
      if (d) return d;
      const rec = SOPS.find((x) => x.t.ref === params[0]);
      if (!rec) return problem(404, "cropcycle.sop_not_found", "Not found");
      const reason = reasonOf(body);
      if (reason.length < 10) return validation([{ field: "reason", reason: "tối thiểu 10 ký tự" }]);
      if (rec.t.status === "retired") return problem(409, "cropcycle.sop_state", "Already retired");
      Object.assign(rec.t, { status: "retired", status_reason: reason, updated_at: nowIso() });
      return json(sopOut(rec, false));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/sop-versions\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, false);
      if (d) return d;
      const f = findVersion(params[0]);
      return f ? json({ version: f.v, template: f.rec.t }) : problem(404, "cropcycle.sop_not_found", "Not found");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/sop-versions\/([\w-]+)\/steps$/,
    handler: ({ params, body, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, true);
      if (d) return d;
      const f = findVersion(params[0]);
      if (!f) return problem(404, "cropcycle.sop_not_found", "Not found");
      if (f.v.status !== "draft" || f.rec.t.status === "retired") return problem(409, "cropcycle.sop_state", "Published is immutable");
      const b = bodyOf(body);
      const steps = stepsError(b.steps, true);
      if (steps instanceof Response) return steps;
      const h = harvestOf(steps);
      Object.assign(f.v, { steps, harvest_day_offset: h.off, harvest_window_days: h.win, change_note: typeof b.change_note === "string" ? b.change_note : f.v.change_note, authored_by: persona.me.actor_ref, authored_at: nowIso() });
      f.rec.t.updated_at = nowIso();
      return json(sopOut(f.rec, false));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/sop-versions\/([\w-]+)\/publish$/,
    handler: ({ params, persona }) => {
      ensure();
      if (!hasRole(persona, "param_approver")) return problem(403, "forbidden", "Forbidden");
      const f = findVersion(params[0]);
      if (!f) return problem(404, "cropcycle.sop_not_found", "Not found");
      if (f.v.authored_by === persona.me.actor_ref) return problem(403, "self_approval_forbidden", "Self approval forbidden");
      if (f.v.status !== "draft" || f.rec.t.status === "retired") return problem(409, "cropcycle.sop_state", "SOP state");
      if (!f.v.steps.some((s) => s.activity_code === "thu_hoach")) return problem(422, "cropcycle.sop_harvest_step_missing", "Harvest step missing");
      const at = nowIso();
      Object.assign(f.v, { status: "published", published_by: persona.me.actor_ref, published_at: at });
      Object.assign(f.rec.t, { status: "published", published_version: f.v.version_no, updated_at: at });
      return json(sopOut(f.rec, false));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/variety-bindings$/,
    handler: ({ body, persona }) => {
      ensure();
      const d = sopAdminDenied(persona, true);
      if (d) return d;
      const b = bodyOf(body);
      const variety = typeof b.variety_code === "string" ? b.variety_code : "";
      if (!CATALOGS.rice_variety.some((v) => v.code === variety && v.active)) return problem(422, "cropcycle.catalog_invalid", "Catalog invalid", { errors: [{ field: "variety_code", reason: "không có giống đang dùng" }] });
      const rec = SOPS.find((x) => x.t.ref === b.template_ref);
      if (!rec || rec.t.scope !== "system" || rec.t.status !== "published") return problem(422, "cropcycle.binding_invalid", "Binding invalid");
      const bound: CropVarietyBinding = { variety_code: variety, template_ref: rec.t.ref, note: typeof b.note === "string" && b.note ? b.note : null, bound_by: persona.me.actor_ref, bound_at: nowIso() };
      const i = BINDINGS.findIndex((x) => x.variety_code === variety);
      if (i >= 0) BINDINGS.splice(i, 1, bound);
      else BINDINGS.push(bound);
      return json(bound);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/stats\/cooperatives\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      ensure();
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const ref = params[0] ?? "";
      if (!findCoop(ref) && !/^[0-9a-f-]{36}$/.test(ref)) return problem(404, "not_found", "Not found");
      return json({
        cooperative_ref: ref,
        open_seasons: SEASONS.filter((x) => x.s.cooperative_ref === ref && x.s.status === "open").length,
        pending_logs: LOGS.filter((l) => l.state.cooperative_ref === ref && l.state.status === "pending").length,
      });
    },
  },
];
