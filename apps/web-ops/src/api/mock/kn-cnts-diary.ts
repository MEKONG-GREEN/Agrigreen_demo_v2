// GIẢ LẬP nhật ký nuôi BM-08/09/19 (W11.48, BRD CNTS v3.7 UR-XA-11, UR-XA-12, UR-TINH-12) theo hợp đồng
// /v1/extension/cnts/diary-cycles*, /diary-entries*, /diary-photos*. Hộ mô hình lấy từ giả lập W11.47. Quy tắc rút gọn của máy
// chủ: chặn NC-05/NC-07/NT-12/NT-13 khi gửi, cảnh báo pH/oxy/vị trí, người xác thực khác người ghi, sửa trong 7 ngày, điều
// chỉnh sau xác thực có lý do. Ảnh trả hình giả lập. Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { cntsMockHousehold } from "./kn-cnts-model";

type S = components["schemas"];
type CycleCore = Omit<S["CNTSDiaryCycle"], "status" | "site_name" | "project_code" | "owner_label" | "can_edit" | "suggest" | "updated_by_display">;
type EntryCore = Omit<
  S["CNTSDiaryEntry"],
  | "site_name"
  | "project_code"
  | "breed"
  | "owner_label"
  | "editable_until"
  | "can_edit"
  | "can_submit"
  | "can_review"
  | "can_correct"
  | "updated_by_display"
  | "author_display"
  | "reviewer_display"
>;

const CYCLES = new Map<string, CycleCore[]>();
const ENTRIES = new Map<string, EntryCore[]>();
const PHOTOS = new Map<string, S["CNTSDiaryPhoto"]>();
const FIXES = new Map<string, { lat: number; lng: number; accuracy_m: string | null; captured_at: string }>();
const HH = "0190f1c4-7d3e-7000-8000-0000000c7301"; // hộ giả lập của W11.47

/** Xóa trạng thái (kiểm thử). */
export function resetCntsDiary() {
  for (const m of [CYCLES, ENTRIES, PHOTOS, FIXES]) m.clear();
  seeded = false;
}

const now = () => new Date().toISOString();
const today = () => now().slice(0, 10);
const last = <T>(m: Map<string, T[]>, ref: string) => m.get(ref)?.at(-1);
const push = <T>(m: Map<string, T[]>, ref: string, v: T) => m.set(ref, [...(m.get(ref) ?? []), v]);
const meta = (actor: string, version: number) => ({
  version,
  updated_at: now(),
  updated_by: actor,
});
const addDays = (d: string, n: number) => new Date(Date.parse(d) + n * 86_400_000).toISOString().slice(0, 10);
const reasonBad = (r: unknown) => typeof r !== "string" || r.trim().length < 5;
const ZERO_ACC = {
  cost_seed: null,
  cost_feed: null,
  cost_depreciation: null,
  cost_transport: null,
  cost_vet: null,
  cost_probiotic: null,
  revenue_heads: null,
  revenue_output_kg: null,
  revenue_price: null,
  revenue_amount: null,
  total_cost: "0",
  profit: null,
  unit_cost: null,
};

let seeded = false;
function seed(ctx: Ctx) {
  if (seeded) return;
  const h = cntsMockHousehold(ctx, HH);
  if (!h) return;
  seeded = true;
  const actor = h.site.officer_ref ?? h.actor;
  const cref = "0190f1c4-7d3e-7000-8000-0000000d7101";
  CYCLES.set(cref, [
    {
      ref: cref,
      event: "created",
      household_ref: HH,
      site_ref: h.site.ref,
      project_ref: h.project.ref,
      commune_code: h.site.commune_code,
      form: "BM-08",
      breed: "Gà lai Mía",
      received_date: addDays(today(), -20),
      initial_count: 500,
      initial_weight_g: "38.5",
      age_days: 1,
      supplies: [
        {
          date: addDays(today(), -20),
          kind: "Thức ăn gà con",
          quantity: "200",
          unit: "kg",
        },
      ],
      vaccination_batch_ref: null,
      note: null,
      status_reason: null,
      accounting: ZERO_ACC,
      ...meta(actor, 1),
    },
  ]);
  const mk = (n: number, day: number, alive: string, dead: string, extra: Partial<EntryCore> = {}): void => {
    const ref = `0190f1c4-7d3e-7000-8000-0000000d72${String(n).padStart(2, "0")}`;
    ENTRIES.set(ref, [
      {
        ref,
        event: "submitted",
        status: "submitted",
        cycle_ref: cref,
        household_ref: HH,
        site_ref: h.site.ref,
        project_ref: h.project.ref,
        commune_code: h.site.commune_code,
        form: "BM-08",
        entry_kind: "daily",
        entry_date: addDays(today(), -day),
        captured_at: `${addDays(today(), -day)}T01:30:00Z`,
        metrics: {
          alive,
          dead,
          ...(dead !== "0" ? { death_cause: "yeu" } : {}),
          feed_kg: "12.5",
        },
        tables: {},
        photo_refs: [],
        checks: [],
        location: {
          has_fix: true,
          accuracy_m: "8",
          distance_m: "35.2",
          no_target_point: false,
          far: false,
          weak_accuracy: false,
          mock_location: false,
          mock_suspected: false,
          fix_stored: true,
        },
        author_ref: actor,
        reviewer_ref: null,
        status_reason: null,
        ...meta(actor, 1),
        ...extra,
      },
    ]);
    FIXES.set(ref, {
      lat: 10.3812,
      lng: 105.4312,
      accuracy_m: "8",
      captured_at: `${addDays(today(), -day)}T01:30:00Z`,
    });
  };
  mk(1, 3, "498", "2");
  mk(2, 2, "497", "1", {
    metrics: {
      alive: "497",
      dead: "1",
      death_cause: "benh",
      feed_kg: "13",
      age_week: "3",
      weight_avg_g: "310",
    },
    location: {
      has_fix: true,
      accuracy_m: "6",
      distance_m: "1850.4",
      no_target_point: false,
      far: true,
      weak_accuracy: false,
      mock_location: false,
      mock_suspected: false,
      fix_stored: true,
    },
    checks: [
      {
        code: "far_from_point",
        level: "warn",
        field: "location",
        alert: "log_location",
        message: "Cách điểm mô hình 1850.4 m (> 200 m)",
      },
    ],
  });
}

function access(ctx: Ctx, household: string) {
  return cntsMockHousehold(ctx, household);
}

function suggest(cycle: string): S["CNTSDiarySuggest"] {
  const es = [...ENTRIES.values()].map((v) => v.at(-1)!).filter((e) => e.cycle_ref === cycle);
  const counts: Record<string, number> = {};
  let feed = 0,
    heads = 0,
    kg = 0,
    amount = 0,
    alive: number | null = null,
    lastDay: string | null = null,
    aliveDay = "";
  for (const e of es) {
    counts[e.status] = (counts[e.status] ?? 0) + 1;
    if (e.status === "draft") continue;
    for (const f of e.tables.feed_purchases ?? []) feed += Number(f.quantity_kg) * Number(f.unit_price);
    for (const s of e.tables.sales ?? []) {
      heads += s.heads;
      kg += Number(s.weight_kg);
      amount += Number(s.weight_kg) * Number(s.unit_price);
    }
    if (e.metrics.harvest_kg) {
      kg += Number(e.metrics.harvest_kg);
      amount += Number(e.metrics.harvest_kg) * Number(e.metrics.harvest_price ?? 0);
    }
    if (e.metrics.alive && e.entry_date >= aliveDay) {
      alive = Number(e.metrics.alive);
      aliveDay = e.entry_date;
    }
    if (!lastDay || e.entry_date > lastDay) lastDay = e.entry_date;
  }
  return {
    feed_cost: String(Math.round(feed)),
    sold_heads: heads,
    sold_kg: String(kg),
    sales_amount: String(Math.round(amount)),
    current_heads: alive,
    last_entry_date: lastDay,
    counts,
  };
}

function cycleView(ctx: Ctx, c: CycleCore): S["CNTSDiaryCycle"] | null {
  const a = access(ctx, c.household_ref);
  if (!a) return null;
  return {
    ...c,
    status: c.event === "closed" ? "closed" : "open",
    site_name: a.site.name,
    project_code: a.project.code,
    owner_label: a.household.owner_label,
    can_edit: a.keeper || (c.event === "closed" && a.reviewer),
    suggest: suggest(c.ref),
    updated_by_display: null,
  };
}

function entryView(ctx: Ctx, e: EntryCore): S["CNTSDiaryEntry"] | null {
  const a = access(ctx, e.household_ref);
  if (!a || (e.status === "draft" && !a.keeper)) return null;
  const c = last(CYCLES, e.cycle_ref)!;
  const until = addDays(e.entry_date, 7);
  const open = c.event !== "closed" && a.keeper;
  return {
    ...e,
    site_name: a.site.name,
    project_code: a.project.code,
    breed: c.breed,
    owner_label: a.household.owner_label,
    editable_until: until,
    can_edit: open && (e.status === "draft" || e.status === "returned" || (e.status === "submitted" && today() <= until)),
    can_submit: open && (e.status === "draft" || e.status === "returned"),
    can_review: a.reviewer && e.status === "submitted" && e.author_ref !== a.actor,
    can_correct: open && e.status === "verified",
    updated_by_display: null,
    author_display: "Cán bộ theo dõi điểm (giả lập)",
    reviewer_display: e.reviewer_ref ? "Chủ nhiệm (giả lập)" : null,
  };
}

function cycleDetail(ctx: Ctx, ref: string): S["CNTSDiaryCycleDetail"] {
  const v = cycleView(ctx, last(CYCLES, ref)!)!;
  return {
    ...v,
    versions: CYCLES.get(ref)!.map((x) => ({
      event: x.event,
      status_reason: x.status_reason,
      version: x.version,
      updated_at: x.updated_at,
      updated_by: x.updated_by,
      updated_by_display: null,
    })),
  };
}

function entryDetail(ctx: Ctx, ref: string): S["CNTSDiaryEntryDetail"] | null {
  const e = last(ENTRIES, ref)!;
  const v = entryView(ctx, e);
  if (!v) return null;
  const a = access(ctx, e.household_ref)!;
  const priv = a.keeper || a.reviewer;
  const others = [...ENTRIES.values()].map((x) => x.at(-1)!).filter((x) => x.cycle_ref === e.cycle_ref && x.status !== "draft");
  const prev = others.filter((x) => x.ref !== ref && x.entry_date < e.entry_date).sort((x, y) => y.entry_date.localeCompare(x.entry_date))[0];
  return {
    ...v,
    versions: ENTRIES.get(ref)!.map((x) => ({
      event: x.event,
      status: x.status,
      status_reason: x.status_reason,
      metrics: x.metrics,
      checks: x.checks,
      version: x.version,
      updated_at: x.updated_at,
      updated_by: x.updated_by,
      updated_by_display: null,
    })),
    fix: priv ? (FIXES.get(ref) ?? null) : null,
    target: a.point ? { lat: a.point.lat, lng: a.point.lng } : null,
    photos: priv ? e.photo_refs.map((r) => PHOTOS.get(r)).filter((p): p is S["CNTSDiaryPhoto"] => !!p) : [],
    previous: prev ? { ref: prev.ref, entry_date: prev.entry_date, metrics: prev.metrics } : null,
    weights: others
      .filter((x) => x.metrics.weight_avg_g)
      .sort((x, y) => x.entry_date.localeCompare(y.entry_date))
      .map((x) => ({
        date: x.entry_date,
        weight_avg_g: x.metrics.weight_avg_g!,
      })),
  };
}

const dist = (a: { lat: number; lng: number }, b: { lat: number; lng: number }) => {
  const r = Math.PI / 180,
    dLat = (b.lat - a.lat) * r,
    dLng = (b.lng - a.lng) * r;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * r) * Math.cos(b.lat * r) * Math.sin(dLng / 2) ** 2;
  return 2 * 6371008.8 * Math.asin(Math.min(1, Math.sqrt(h)));
};

/** Kiểm tra tự động rút gọn (máy chủ là chuẩn). */
function evaluate(
  c: CycleCore,
  e: Pick<EntryCore, "entry_kind" | "entry_date" | "metrics" | "tables" | "photo_refs" | "location" | "ref">,
): S["CNTSDiaryCheck"][] {
  const out: S["CNTSDiaryCheck"][] = [];
  const m = e.metrics;
  if (c.form !== "BM-19") {
    const prev = [...ENTRIES.values()]
      .map((v) => v.at(-1)!)
      .filter((x) => x.cycle_ref === c.ref && x.ref !== e.ref && x.status !== "draft" && x.entry_date < e.entry_date)
      .sort((x, y) => y.entry_date.localeCompare(x.entry_date))[0];
    const prevAlive = prev?.metrics.alive ? Number(prev.metrics.alive) : c.initial_count;
    const sold = (e.tables.sales ?? []).filter((s) => s.date === e.entry_date).reduce((n, s) => n + s.heads, 0);
    const want = prevAlive - Number(m.dead ?? 0) - sold;
    if (m.alive && Number(m.alive) !== want)
      out.push({
        code: "headcount_mismatch",
        level: "block",
        field: "metrics.alive",
        message: `NC-05: số con sống phải bằng ${prevAlive} − ${m.dead ?? 0} − ${sold} = ${want}`,
      });
    if (Number(m.dead ?? 0) > 0 && !m.death_cause)
      out.push({
        code: "death_cause_missing",
        level: "block",
        field: "metrics.death_cause",
        message: "NC-07: có con chết thì phải ghi nguyên nhân chết",
      });
  } else {
    let water = false;
    if (m.ph && (Number(m.ph) < 7.5 || Number(m.ph) > 8.5)) {
      water = true;
      out.push({
        code: "ph_out_of_range",
        level: "warn",
        field: "metrics.ph",
        alert: "water_quality",
        message: `pH ${m.ph} ngoài ngưỡng 7.5–8.5`,
      });
    }
    if (m.do_mgl && Number(m.do_mgl) < 4) {
      water = true;
      out.push({
        code: "do_low",
        level: "warn",
        field: "metrics.do_mgl",
        alert: "water_quality",
        message: `Oxy hòa tan ${m.do_mgl} mg/l dưới ngưỡng 4 mg/l`,
      });
    }
    if (water && !m.action_note)
      out.push({
        code: "action_note_missing",
        level: "block",
        field: "metrics.action_note",
        message: "NT-12: có cảnh báo môi trường nước thì ghi biện pháp xử lý",
      });
    if (e.entry_kind === "growth_env" && !e.photo_refs.some((r) => PHOTOS.get(r)?.kind === "weighing"))
      out.push({
        code: "photo_missing",
        level: "block",
        field: "photo_refs",
        message: "NT-13: lần cân đo phải có ảnh cân mẫu",
      });
  }
  const l = e.location;
  if (!l.has_fix)
    out.push({
      code: "no_fix",
      level: "warn",
      field: "location",
      message: "Lần ghi không có vị trí GPS",
    });
  else if (l.no_target_point)
    out.push({
      code: "no_target_point",
      level: "warn",
      field: "location",
      alert: "log_location",
      message: "Hộ / điểm mô hình chưa có tọa độ",
    });
  else if (l.far)
    out.push({
      code: "far_from_point",
      level: "warn",
      field: "location",
      alert: "log_location",
      message: `Cách điểm mô hình ${l.distance_m} m (> 200 m)`,
    });
  if (l.weak_accuracy)
    out.push({
      code: "weak_gps",
      level: "warn",
      field: "location",
      alert: "log_location",
      message: `Sai số GPS ${l.accuracy_m} m (> 50 m)`,
    });
  return out;
}

const blocked = (cs: S["CNTSDiaryCheck"][]) => {
  const b = cs.filter((c) => c.level === "block");
  return b.length
    ? problem(422, "extension.cnts_diary_entry_blocked", "Lần ghi chưa qua kiểm tra — sửa lỗi chặn trước khi gửi", {
        errors: b.map((x) => ({ field: x.field, reason: x.message })),
      })
    : null;
};

const entryNF = () => problem(404, "extension.cnts_diary_entry_not_found", "Không có lần ghi nhật ký");
const cycleNF = () => problem(404, "extension.cnts_diary_cycle_not_found", "Không có lứa nuôi");
const conflict = () => problem(409, "extension.cnts_diary_entry_version_conflict", "Lần ghi đã có người cập nhật — tải lại bản mới");

function change(ctx: Ctx, act: "update" | "submit" | "return" | "verify" | "correct"): Response {
  seed(ctx);
  const ref = ctx.params[0]!;
  const e = last(ENTRIES, ref);
  if (!e) return entryNF();
  const a = access(ctx, e.household_ref);
  if (!a || (e.status === "draft" && !a.keeper)) return entryNF();
  const b = (ctx.body ?? {}) as Partial<S["CNTSDiaryEntrySave"] & { reason: string }>;
  const c = last(CYCLES, e.cycle_ref)!;
  const next: Record<string, Partial<Record<string, string>>> = {
    update: { draft: "draft", submitted: "submitted", returned: "returned" },
    submit: { draft: "submitted", returned: "submitted" },
    return: { submitted: "returned" },
    verify: { submitted: "verified" },
    correct: { verified: "submitted" },
  };
  const to = next[act]![e.status];
  if (!to)
    return act === "update" && e.status === "verified"
      ? problem(409, "extension.cnts_diary_entry_locked", "Lần ghi đã khóa sửa (đã xác thực hoặc quá hạn sửa)")
      : problem(409, "extension.cnts_diary_entry_state", "Thao tác không hợp lệ ở trạng thái hiện tại của lần ghi");
  const review = act === "return" || act === "verify";
  if (review && !a.reviewer) return problem(403, "forbidden", "chỉ chủ nhiệm dự án hoặc lãnh đạo phòng rà soát nhật ký");
  if (!review && !a.keeper) return problem(403, "forbidden", "chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ sửa lần ghi");
  if (act === "verify" && e.author_ref === a.actor) return problem(403, "extension.cnts_diary_self_verify", "Người xác thực phải khác người ghi");
  if (b.base_version !== e.version) return conflict();
  if ((act === "return" || act === "correct") && reasonBad(b.reason)) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
  if (act === "update" && e.status === "submitted" && today() > addDays(e.entry_date, 7))
    return problem(409, "extension.cnts_diary_entry_locked", "Quá hạn sửa lần ghi đã gửi (7 ngày)");
  const n: EntryCore = {
    ...e,
    ...meta(a.actor, e.version + 1),
    event: (
      {
        update: "updated",
        submit: "submitted",
        return: "returned",
        verify: "verified",
        correct: "corrected",
      } as const
    )[act],
    status: to as EntryCore["status"],
    status_reason: null,
    reviewer_ref: review ? a.actor : null,
  };
  if (act === "update" || act === "correct") {
    n.entry_kind = b.entry_kind ?? e.entry_kind;
    n.entry_date = b.entry_date ?? e.entry_date;
    n.metrics = b.metrics ?? e.metrics;
    n.tables = b.tables ?? e.tables;
    n.photo_refs = b.photo_refs ?? e.photo_refs;
    if (act === "correct") n.status_reason = b.reason!.trim();
  }
  if (act === "return") n.status_reason = (b.reason ?? "").trim();
  if (!review) {
    n.checks = evaluate(c, n);
    if (to === "submitted") {
      const bad = blocked(n.checks);
      if (bad) return bad;
    }
  }
  push(ENTRIES, ref, n);
  if (act === "update" && b.submit && n.status !== "submitted") {
    const s: EntryCore = {
      ...n,
      ...meta(a.actor, n.version + 1),
      event: "submitted",
      status: "submitted",
    };
    const bad = blocked(s.checks);
    if (bad) return bad;
    push(ENTRIES, ref, s);
  }
  return json(entryDetail(ctx, ref));
}

export const cntsDiaryRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/diary-cycles$/,
    handler: (ctx) => {
      seed(ctx);
      const q = ctx.url.searchParams;
      const hh = q.get("household_ref"),
        site = q.get("site_ref"),
        st = q.get("status");
      const items = [...CYCLES.values()]
        .map((v) => v.at(-1)!)
        .filter((c) => (!hh || c.household_ref === hh) && (!site || c.site_ref === site))
        .map((c) => cycleView(ctx, c))
        .filter((c): c is S["CNTSDiaryCycle"] => !!c && (!st || c.status === st));
      const closed = items.filter((c) => c.accounting.profit !== null);
      const a = hh ? access(ctx, hh) : null;
      return json({
        items,
        closed_count: closed.length,
        avg_profit: closed.length ? String(Math.round(closed.reduce((s, c) => s + Number(c.accounting.profit), 0) / closed.length)) : null,
        can_create: !!a?.keeper && !items.some((c) => c.status === "open"),
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-cycles$/,
    handler: (ctx) => {
      seed(ctx);
      const b = (ctx.body ?? {}) as S["CNTSDiaryCycleSave"];
      const a = b.household_ref ? access(ctx, b.household_ref) : null;
      if (!a) return problem(404, "not_found", "Không có hộ mô hình");
      if (!a.keeper) return problem(403, "forbidden", "chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ mở lứa");
      const form = b.form ?? (a.site.diary_form as S["CNTSDiaryCycleSave"]["form"]) ?? "BM-08";
      const errs = [
        !b.breed?.trim() && {
          field: "breed",
          reason: "nhập giống / đối tượng nuôi",
        },
        !(b.initial_count >= 1) && {
          field: "initial_count",
          reason: "1–100.000.000 con",
        },
        !b.received_date && { field: "received_date", reason: "bắt buộc" },
      ].filter(Boolean) as { field: string; reason: string }[];
      if (errs.length) return validation(errs);
      if ([...CYCLES.values()].some((v) => v.at(-1)!.household_ref === b.household_ref && v.at(-1)!.event !== "closed"))
        return problem(409, "extension.cnts_diary_cycle_open_exists", "Hộ đang có lứa chưa kết thúc");
      const ref = b.id ?? uuidv7();
      CYCLES.set(ref, [
        {
          ref,
          event: "created",
          household_ref: b.household_ref!,
          site_ref: a.site.ref,
          project_ref: a.project.ref,
          commune_code: a.site.commune_code,
          form: form!,
          breed: b.breed.trim(),
          received_date: b.received_date,
          initial_count: b.initial_count,
          initial_weight_g: b.initial_weight_g ?? null,
          age_days: b.age_days ?? null,
          supplies: b.supplies ?? [],
          vaccination_batch_ref: b.vaccination_batch_ref ?? null,
          note: b.note || null,
          status_reason: null,
          accounting: ZERO_ACC,
          ...meta(a.actor, 1),
        },
      ]);
      return json(cycleDetail(ctx, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/diary-cycles\/([\w-]+)$/,
    handler: (ctx) => {
      seed(ctx);
      const c = last(CYCLES, ctx.params[0]!);
      return c && access(ctx, c.household_ref) ? json(cycleDetail(ctx, c.ref)) : cycleNF();
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-cycles\/([\w-]+)\/(close|reopen)$/,
    handler: (ctx) => {
      seed(ctx);
      const c = last(CYCLES, ctx.params[0]!);
      const a = c ? access(ctx, c.household_ref) : null;
      if (!c || !a) return cycleNF();
      const b = (ctx.body ?? {}) as S["CNTSDiaryCycleClose"] & {
        reason?: string;
      };
      if (b.base_version !== c.version) return problem(409, "extension.cnts_diary_cycle_version_conflict", "Lứa đã có người cập nhật — tải lại bản mới");
      if (ctx.params[1] === "close") {
        if (!a.keeper) return problem(403, "forbidden", "chỉ người ghi nhật ký kết thúc lứa");
        if (c.event === "closed") return problem(409, "extension.cnts_diary_cycle_closed", "Lứa đã kết thúc");
        if (!b.revenue_amount)
          return validation([
            {
              field: "revenue_amount",
              reason: "nhập tổng thu cuối lứa (NC-15)",
            },
          ]);
        const n = (v: string | null | undefined) => Number(v ?? 0);
        const total = n(b.cost_seed) + n(b.cost_feed) + n(b.cost_depreciation) + n(b.cost_transport) + n(b.cost_vet) + n(b.cost_probiotic);
        const out = b.revenue_output_kg && Number(b.revenue_output_kg) > 0 ? String(Math.round(total / Number(b.revenue_output_kg))) : null;
        push(CYCLES, c.ref, {
          ...c,
          ...meta(a.actor, c.version + 1),
          event: "closed",
          status_reason: null,
          accounting: {
            cost_seed: b.cost_seed ?? null,
            cost_feed: b.cost_feed ?? null,
            cost_depreciation: b.cost_depreciation ?? null,
            cost_transport: b.cost_transport ?? null,
            cost_vet: b.cost_vet ?? null,
            cost_probiotic: b.cost_probiotic ?? null,
            revenue_heads: b.revenue_heads ?? null,
            revenue_output_kg: b.revenue_output_kg ?? null,
            revenue_price: b.revenue_price ?? null,
            revenue_amount: b.revenue_amount,
            total_cost: String(total),
            profit: String(n(b.revenue_amount) - total),
            unit_cost: out,
          },
        });
      } else {
        if (!a.keeper && !a.reviewer) return problem(403, "forbidden", "chỉ người ghi nhật ký hoặc chủ nhiệm mở lại lứa");
        if (reasonBad(b.reason)) return validation([{ field: "reason", reason: "5–1000 ký tự" }]);
        push(CYCLES, c.ref, {
          ...c,
          ...meta(a.actor, c.version + 1),
          event: "reopened",
          status_reason: b.reason!.trim(),
        });
      }
      return json(cycleDetail(ctx, c.ref));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/diary-entries$/,
    handler: (ctx) => {
      seed(ctx);
      const q = ctx.url.searchParams;
      const cycle = q.get("cycle_ref"),
        hh = q.get("household_ref"),
        st = q.get("status"),
        review = q.get("review") === "true",
        fl = q.get("flagged") === "true";
      const items = [...ENTRIES.values()]
        .map((v) => v.at(-1)!)
        .filter((e) => (!cycle || e.cycle_ref === cycle) && (!hh || e.household_ref === hh))
        .map((e) => entryView(ctx, e))
        .filter(
          (e): e is S["CNTSDiaryEntry"] => !!e && (!st || e.status === st) && (!review || e.can_review) && (!fl || e.checks.some((c) => c.level === "warn")),
        )
        .sort((x, y) => y.entry_date.localeCompare(x.entry_date));
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-entries$/,
    handler: (ctx) => {
      seed(ctx);
      const b = (ctx.body ?? {}) as S["CNTSDiaryEntrySave"];
      const c = b.cycle_ref ? last(CYCLES, b.cycle_ref) : undefined;
      const a = c ? access(ctx, c.household_ref) : null;
      if (!c || !a) return cycleNF();
      if (!a.keeper) return problem(403, "forbidden", "chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ ghi nhật ký");
      if (c.event === "closed") return problem(409, "extension.cnts_diary_cycle_closed", "Lứa đã kết thúc (đã hạch toán) — mở lại trước khi ghi");
      const day = b.entry_date ?? (b.captured_at ?? now()).slice(0, 10);
      if (c.form !== "BM-19" && [...ENTRIES.values()].some((v) => v.at(-1)!.cycle_ref === c.ref && v.at(-1)!.entry_date === day))
        return problem(409, "extension.cnts_diary_entry_day_taken", "Lứa đã có lần ghi của ngày này");
      const fix = b.location ?? null;
      const d = fix && a.point ? Math.round(dist(fix, a.point) * 10) / 10 : null;
      const location: S["CNTSDiaryLocation"] = {
        has_fix: !!fix,
        accuracy_m: fix?.accuracy_m ?? null,
        distance_m: d === null ? null : String(d),
        no_target_point: !!fix && !a.point,
        far: d !== null && d > 200,
        weak_accuracy: !!fix?.accuracy_m && Number(fix.accuracy_m) > 50,
        mock_location: !!fix?.mock_location,
        mock_suspected: !!fix?.mock_location,
        fix_stored: !!fix,
      };
      const ref = b.id ?? uuidv7();
      const e: EntryCore = {
        ref,
        event: "created",
        status: b.submit ? "submitted" : "draft",
        cycle_ref: c.ref,
        household_ref: c.household_ref,
        site_ref: c.site_ref,
        project_ref: c.project_ref,
        commune_code: c.commune_code,
        form: c.form,
        entry_kind: b.entry_kind,
        entry_date: day,
        captured_at: b.captured_at ?? now(),
        metrics: b.metrics ?? {},
        tables: b.tables ?? {},
        photo_refs: b.photo_refs ?? [],
        checks: [],
        location,
        author_ref: a.actor,
        reviewer_ref: null,
        status_reason: null,
        ...meta(a.actor, 1),
      };
      e.checks = evaluate(c, e);
      if (b.submit) {
        const bad = blocked(e.checks);
        if (bad) return bad;
      }
      ENTRIES.set(ref, [e]);
      if (fix)
        FIXES.set(ref, {
          lat: fix.lat,
          lng: fix.lng,
          accuracy_m: fix.accuracy_m ?? null,
          captured_at: e.captured_at,
        });
      return json(entryDetail(ctx, ref), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/diary-entries\/([\w-]+)$/,
    handler: (ctx) => {
      seed(ctx);
      if (!ENTRIES.has(ctx.params[0]!)) return entryNF();
      const d = entryDetail(ctx, ctx.params[0]!);
      return d ? json(d) : entryNF();
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-entries\/([\w-]+)$/,
    handler: (ctx) => change(ctx, "update"),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-entries\/([\w-]+)\/submit$/,
    handler: (ctx) => change(ctx, "submit"),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-entries\/([\w-]+)\/verify$/,
    handler: (ctx) => change(ctx, "verify"),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-entries\/([\w-]+)\/return$/,
    handler: (ctx) => change(ctx, "return"),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-entries\/([\w-]+)\/correct$/,
    handler: (ctx) => change(ctx, "correct"),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/diary-photos$/,
    handler: (ctx) => {
      seed(ctx);
      const text = ctx.body instanceof Uint8Array ? new TextDecoder("latin1").decode(ctx.body) : "";
      const field = (name: string) => new RegExp(`name="${name}"\\r\\n\\r\\n([^\\r]*)`).exec(text)?.[1] ?? "";
      const hh = field("household_ref"),
        kind = (field("kind") || "other") as S["CNTSDiaryPhoto"]["kind"];
      const a = hh ? access(ctx, hh) : null;
      if (!a) return problem(404, "not_found", "Không có hộ mô hình");
      if (!a.keeper) return problem(403, "forbidden", "chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ tải ảnh");
      const size = ctx.body instanceof Uint8Array ? ctx.body.length : 0;
      if (size > 576 * 1024)
        return problem(422, "extension.cnts_diary_photo_invalid", "Ảnh không hợp lệ", { errors: [{ field: "file", reason: "quá 500 KB" }] });
      const ref = field("id") || uuidv7();
      const p: S["CNTSDiaryPhoto"] = {
        file_ref: ref,
        household_ref: hh,
        kind,
        content_type: "image/jpeg",
        size_bytes: size,
        captured_at: field("captured_at") || null,
        uploaded_by: a.actor,
        uploaded_at: now(),
      };
      PHOTOS.set(ref, p);
      return json(p, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/diary-photos\/([\w-]+)\/content$/,
    handler: (ctx) => {
      const p = PHOTOS.get(ctx.params[0]!);
      const a = p ? access(ctx, p.household_ref) : null;
      if (!p || !a || !(a.keeper || a.reviewer)) return problem(404, "extension.cnts_diary_photo_not_found", "Không có ảnh nhật ký");
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200"><rect width="320" height="200" fill="#e7efe3"/><text x="160" y="105" font-size="16" text-anchor="middle">Ảnh giả lập</text></svg>`;
      return new Response(svg, {
        status: 200,
        headers: {
          "Content-Type": "image/svg+xml",
          "Cache-Control": "no-store",
        },
      });
    },
  },
];
