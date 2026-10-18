// GIẢ LẬP lịch trực KSGM BM-03 và lịch tiêm phòng BM-07 (W11.46, BRD CNTS v3.7 FN-15) theo hợp đồng
// /v1/extension/cnts/duty-rosters/{month}, /my-duties, /vaccination-batches*: lịch trực là chuỗi phiên bản theo tháng (khóa
// lạc quan base_version, cảnh báo chưa có người trực), lịch tiêm sinh từ quy trình trong danh mục giả lập (ngày dự kiến =
// ngày nhận + ngày tuổi − tuổi lúc nhận). Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { ADMIN_UNITS, CATALOGS } from "./data";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext, personaForContext } from "./kn";
import { AG_COMMUNES } from "./kn-reports/store";

type S = components["schemas"];
type Roster = S["CNTSDutyRoster"];
type Cell = S["CNTSDutyCell"];
type Batch = S["CNTSVaccinationBatchCore"];

const ROSTERS = new Map<string, Roster[]>(); // tháng → phiên bản
const BATCHES = new Map<string, Batch[]>(); // ref → phiên bản

/** Xóa trạng thái (kiểm thử). */
export function resetCntsCal() {
  ROSTERS.clear();
  BATCHES.clear();
}

const OFFICER = "0190f1c4-7d3e-7000-8000-00000000a0c1";

// Xã của mẫu CNTS BRD nạp sẵn nằm ngoài bộ xã địa lý mô phỏng.
const BRD_COMMUNE_PROVINCE: Readonly<Record<string, string>> = { "30292": "91" };
function provinceOf(code: string): string | undefined {
  const u = ADMIN_UNITS.find(u => u.code === code);
  return u?.level === "province" ? u.code : u?.parent_code ?? (AG_COMMUNES.some(c => c.code === code) ? "91" : BRD_COMMUNE_PROVINCE[code]);
}
function who(ctx: Ctx) {
  const id = ctx.request.headers.get("X-AG-Context");
  const kc = knContext(id ?? "");
  const p = personaForContext(ctx.persona, id);
  const role = kc?.role ?? (p.me.roles.includes("platform_admin") ? "admin" : p.me.roles.includes("kn_national_admin") ? "kn_tw" : p.me.scope.level === "commune" ? "kn_xa" : "kn_tinh");
  const communes = role === "kn_xa" ? (kc?.communes.map(c => c.code) ?? p.me.scope.units.filter(c => ADMIN_UNITS.find(u => u.code === c)?.level === "commune")) : null;
  const provinces = new Set([...p.me.scope.units, ...(communes ?? []), p.me.org?.admin_unit_code ?? ""].map(provinceOf).filter((p): p is string => !!p));
  return { role, provinces, communes, actor: p.me.actor_ref, readOnly: !!kc?.read_only, department: kc?.department_code };
}
function rosterProvince(ctx: Ctx, requested?: string, write = false): string | Response {
  const w = who(ctx);
  if (w.role === "kn_tw" || (w.role !== "admin" && w.department && w.department !== "chan_nuoi_thuy_san")) return problem(403, "forbidden", "Lịch trực thuộc phòng Chăn nuôi – Thủy sản");
  if (requested === "legacy") return w.role === "admin" && !write ? "legacy" : problem(403,"extension.out_of_scope","Lịch chung cũ chỉ Admin xem");
  const province = requested || (w.provinces.size === 1 ? [...w.provinces][0] : undefined);
  if (!province) return validation([{field:"province_code",reason:"Chọn tỉnh để xem hoặc lập lịch trực"}]);
  if (!ADMIN_UNITS.some(u => u.code === province && u.level === "province")) return validation([{field:"province_code",reason:"phải là tỉnh còn hiệu lực"}]);
  return w.role === "admin" || w.provinces.has(province) ? province : problem(403,"extension.out_of_scope","Ngoài phạm vi phụ trách");
}
const rosterKey = (month: string, province: string) => `${province}|${month}`;
const facilities = (province: string) =>
  CATALOGS.cnts_slaughter_facility.filter(f => f.active && (province === "legacy" || provinceOf(f.attrs.commune_code ?? "") === province)).map(f => ({
    ref: f.attrs.facility_ref!, code: f.code, name: f.name, commune_code: f.attrs.commune_code ?? "", kind: f.attrs.facility_kind ?? null,
  }));

const daysIn = (month: string) => {
  const [y, m] = month.split("-").map(Number);
  return new Date(Date.UTC(y!, m!, 0)).getUTCDate();
};
const today = () => new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const addDays = (d: string, n: number) => {
  const x = new Date(`${d}T00:00:00Z`);
  x.setUTCDate(x.getUTCDate() + n);
  return x.toISOString().slice(0, 10);
};

function emptyRoster(month: string, province: string): Roster {
  const fs = facilities(province);
  return {
    province_code: province === "legacy" ? null : province, legacy: province === "legacy", month, days_in_month: daysIn(month), version: 0, source: null, note: null, cells: [], facilities: fs,
    officers: fs.length ? [{ ref: province === "91" ? OFFICER : `0190f1c4-7d3e-7000-8000-${province.padStart(12,"0")}`, display: "Viên chức KSGM (giả lập)", commune_codes: fs.map(f => f.commune_code), facility_refs: [] }] : [],
    warnings: [], updated_at: null, updated_by: null, versions: [], can_edit: province !== "legacy", notified_officer_refs: [],
  };
}

function warnings(r: Roster): Roster["warnings"] {
  return r.facilities.flatMap((f) => {
    const covered = new Set(r.cells.filter((c) => c.facility_ref === f.ref).map((c) => c.day));
    const days = Array.from({ length: r.days_in_month }, (_, i) => i + 1).filter((d) => !covered.has(d));
    return days.length ? [{ code: "facility_uncovered" as const, message: `Cơ sở chưa có người trực ${days.length} ngày`, facility_ref: f.ref, officer_ref: null, days }] : [];
  });
}

const latestRoster = (month: string, province: string) => ROSTERS.get(rosterKey(month,province))?.at(-1) ?? emptyRoster(month,province);

function batchView(b: Batch): S["CNTSVaccinationBatch"] {
  const t = today();
  const doses = b.doses.map((d) => ({ ...d, overdue: !d.actual_date && d.expected_date < t }));
  const overdue = doses.filter((d) => d.overdue).length;
  const status = b.event === "cancelled" ? "cancelled" : overdue ? "overdue" : doses.every((d) => d.actual_date) ? "done" : "open";
  return { ...b, doses, status, overdue_count: overdue, can_plan: b.event !== "cancelled", can_record: b.event !== "cancelled", next_dose: doses.find((d) => !d.actual_date) ?? null };
}
const detail = (ref: string) => {
  const vs = BATCHES.get(ref)!;
  return { ...batchView(vs.at(-1)!), versions: [...vs].reverse() };
};

export const cntsCalRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/duty-rosters\/(\d{4}-\d{2})$/,
    handler: (ctx) => {
      if (who(ctx).role === "kn_xa") return problem(403, "forbidden", "Cấp Xã xem lịch trực của mình (Lịch trực của tôi)");
      const province = rosterProvince(ctx, ctx.url.searchParams.get("province_code") ?? undefined);
      if (province instanceof Response) return province;
      const r = latestRoster(ctx.params[0]!, province);
      return json({ ...r, versions: [...(ROSTERS.get(rosterKey(r.month,province)) ?? [])].reverse().map((v) => ({ version: v.version, source: v.source ?? "grid", cell_count: v.cells.length, note: v.note, at: v.updated_at!, actor_ref: v.updated_by!, actor_display: null })) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/duty-rosters\/(\d{4}-\d{2})$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.role === "kn_xa" || w.readOnly) return problem(403, "forbidden", "chỉ cán bộ tổng hợp Cấp Tỉnh (hoặc Admin) lập lịch trực KSGM");
      const month = ctx.params[0]!;
      const b = (ctx.body ?? {}) as S["CNTSDutyRosterSave"];
      const province = rosterProvince(ctx, b.province_code, true);
      if (province instanceof Response) return province;
      const cur = latestRoster(month, province);
      if (b.base_version !== cur.version) return problem(409, "extension.cnts_roster_version_conflict", "Lịch trực đã có người cập nhật — tải lại bản mới");
      const cells: Cell[] = (b.cells ?? []).filter((c) => c.day >= 1 && c.day <= cur.days_in_month);
      if (cells.some(c => !cur.facilities.some(f => f.ref === c.facility_ref) || !cur.officers.some(o => o.ref === c.officer_ref))) return problem(422,"extension.cnts_roster_invalid","Cơ sở hoặc cán bộ không thuộc tỉnh lập lịch");
      const old = new Set(cur.cells.map((c) => `${c.facility_ref}|${c.officer_ref}|${c.day}|${c.shift}`));
      const now = new Set(cells.map((c) => `${c.facility_ref}|${c.officer_ref}|${c.day}|${c.shift}`));
      const changed = [...new Set([...cur.cells.filter((c) => !now.has(`${c.facility_ref}|${c.officer_ref}|${c.day}|${c.shift}`)), ...cells.filter((c) => !old.has(`${c.facility_ref}|${c.officer_ref}|${c.day}|${c.shift}`))].map((c) => c.officer_ref))];
      const next: Roster = { ...cur, version: cur.version + 1, cells, source: b.source ?? "grid", note: b.note ?? null, updated_at: new Date().toISOString(), updated_by: w.actor, notified_officer_refs: changed };
      next.warnings = warnings(next);
      ROSTERS.set(rosterKey(month,province), [...(ROSTERS.get(rosterKey(month,province)) ?? []), next]);
      return json(next, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/my-duties$/,
    handler: (ctx) => {
      const month = ctx.url.searchParams.get("month") ?? today().slice(0, 7);
      const province = rosterProvince(ctx, ctx.url.searchParams.get("province_code") ?? undefined);
      if (province instanceof Response) return province;
      const r = latestRoster(month, province);
      const me = who(ctx).actor;
      const items = r.cells
        .filter((c) => c.officer_ref === me || r.officers.some(o => o.ref === c.officer_ref))
        .map((c) => {
          const f = r.facilities.find((x) => x.ref === c.facility_ref);
          return { date: `${month}-${String(c.day).padStart(2, "0")}`, shift: c.shift, facility: { ref: c.facility_ref, code: f?.code ?? null, name: f?.name ?? null, commune_code: f?.commune_code ?? null }, colleagues: [] };
        });
      return json({ month, version: r.version, items });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/vaccination-batches$/,
    handler: (ctx) => {
      const w = who(ctx);
      const st = ctx.url.searchParams.get("status");
      const items = [...BATCHES.values()]
        .map((vs) => batchView(vs.at(-1)!))
        .filter((b) => (w.communes === null || w.communes.includes(b.commune_code)) && (!st || b.status === st));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/vaccination-batches$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (w.role === "kn_xa" || w.readOnly) return problem(403, "forbidden", "chỉ chủ nhiệm chương trình Cấp Tỉnh (hoặc Admin) lập lịch tiêm phòng");
      const b = (ctx.body ?? {}) as S["CNTSVaccinationBatchCreate"];
      const sc = CATALOGS.cnts_vaccination_schedule.find((x) => x.attrs.model_type === b.model_type_code);
      if (!sc) return problem(422, "extension.cnts_vaccination_no_procedure", "Không có quy trình tiêm cho loại mô hình/giống tại ngày nhận giống");
      const doses = (sc.attrs.doses ?? "")
        .split(",")
        .map((p) => /^(.+)\/(\d+)@(\d+)$/.exec(p.trim()))
        .filter((m): m is RegExpExecArray => !!m && Number(m[3]) >= b.age_at_receipt)
        .map((m) => ({
          vaccine_code: m[1]!, vaccine_name: CATALOGS.cnts_vaccine.find((v) => v.code === m[1])?.name ?? null, shot: Number(m[2]), age_days: Number(m[3]),
          expected_date: addDays(b.received_date, Number(m[3]) - b.age_at_receipt), actual_date: null, note: null, overdue: false,
        }));
      const ref = b.id ?? uuidv7();
      const batch: Batch = {
        ref, version: 1, event: "created", commune_code: b.commune_code, model_type_code: b.model_type_code,
        model_type_name: CATALOGS.cnts_model_type.find((m) => m.code === b.model_type_code)?.name ?? null, breed_code: b.breed_code ?? null, breed_name: null,
        schedule_code: sc.code, schedule_name: sc.name, schedule_valid_from: "2000-01-01", household_ref: b.household_ref ?? null, locality: b.locality ?? null,
        received_date: b.received_date, age_at_receipt: b.age_at_receipt, head_count: b.head_count ?? null, doses, note: b.note ?? null, status_reason: null,
        updated_at: new Date().toISOString(), updated_by: w.actor,
      };
      BATCHES.set(ref, [batch]);
      return json(detail(ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/vaccination-batches\/([^/]+)$/,
    handler: (ctx) => (BATCHES.has(ctx.params[0]!) ? json(detail(ctx.params[0]!)) : problem(404, "extension.cnts_vaccination_not_found", "Không có lịch tiêm của lứa này")),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/vaccination-batches\/([^/]+)\/(doses|cancel)$/,
    handler: (ctx) => {
      const vs = BATCHES.get(ctx.params[0]!);
      if (!vs) return problem(404, "extension.cnts_vaccination_not_found", "Không có lịch tiêm của lứa này");
      const cur = vs.at(-1)!;
      const b = (ctx.body ?? {}) as { base_version: number; reason?: string; doses?: S["CNTSVaccinationDosesUpdate"]["doses"] };
      if (b.base_version !== cur.version) return problem(409, "extension.cnts_vaccination_version_conflict", "Lịch tiêm đã có người cập nhật — tải lại bản mới");
      let next: Batch;
      if (ctx.params[1] === "cancel") next = { ...cur, version: cur.version + 1, event: "cancelled", status_reason: b.reason ?? null };
      else {
        const doses = cur.doses.map((d) => {
          const u = b.doses?.find((x) => x.vaccine_code === d.vaccine_code && x.shot === d.shot);
          return u ? { ...d, expected_date: u.expected_date ?? d.expected_date, actual_date: u.actual_date ?? d.actual_date } : d;
        });
        next = { ...cur, version: cur.version + 1, event: b.doses?.some((x) => x.expected_date) ? "updated" : "dose_recorded", doses };
      }
      next.updated_at = new Date().toISOString();
      BATCHES.set(cur.ref, [...vs, next]);
      return json(detail(cur.ref));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/work-schedules\/([^/]+)\/items\/([^/]+)\/checkin$/,
    handler: (ctx) => {
      const b = (ctx.body ?? {}) as { lat: number; lon: number; accuracy_m?: number | null; captured_at: string; mock_location?: boolean };
      const checkin: S["ExtWorkScheduleCheckin"] = {
        id: uuidv7(), item_id: ctx.params[1]!, day: b.captured_at.slice(0, 10), captured_at: b.captured_at, recorded_at: new Date().toISOString(),
        accuracy_m: b.accuracy_m ?? null, distance_m: null, far: false, weak_accuracy: (b.accuracy_m ?? 0) > 50, mock_location: !!b.mock_location,
        point_stored: true, location: { lat: b.lat, lon: b.lon },
      };
      return json({ checkin, warnings: [{ code: "checkin_no_place", message: "Mục chưa có địa điểm trên bản đồ — không tính được khoảng cách", item_ids: [checkin.item_id] }] }, 201);
    },
  },
];
