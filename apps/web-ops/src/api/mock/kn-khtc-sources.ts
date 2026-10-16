// GIẢ LẬP bộ đọc nguồn module của Kỳ KHTC (W11.64, BRD KHTC v2.2 BR-DL-02…04) theo hợp đồng
// /v1/extension/khtc/periods/{id}/sources*: "Cập nhật số liệu" chụp số của nguồn (ca golden G03 — lũy kế lúa Thu Đông của
// Trồng trọt, G02B — giá dưa leo), lịch sử cũ → mới, "Nguồn đã thay đổi" khi nguồn chốt lại sau lần chụp (chỉ cảnh báo).
// Chỉ Cấp Tỉnh Phòng KHTC (không chỉ xem) bấm. Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { khtcMockStore } from "./kn-khtc-periods";

type S = components["schemas"];
type Status = S["KHTCSourceStatus"];
type Value = S["KHTCSourceValue"];
type Snapshot = S["KHTCSourceSnapshot"];
type HistoryItem = S["KHTCSourceHistoryItem"];

const OWNER = "01920000-0000-7000-8000-00000000c0de";
const KEY_LUA = "season:trong_trot:pl01:lua_td:luy_ke";
const KEY_GIA = "thong_tin_truyen_thong:price-board:mh00001";
const ENTRY_LUA = { ref: "01920000-0000-7000-8000-0000000000a1", code: "I.1", name: "Diện tích xuống giống lúa Thu Đông" };
const ENTRY_GIA = { ref: "01920000-0000-7000-8000-0000000000a2", code: "III", name: "Giá dưa leo (MH00001)" };

interface Live {
  cumulative: string; // lũy kế nguồn của kỳ (đã chốt)
  closedAt: string;
  snapshotNo: number;
}
interface Stored {
  live: Live;
  snaps: { snap: Snapshot; values: Value[]; live: Live; changes: HistoryItem["changes"] }[];
}
const STORE = new Map<string, Stored>();
const BASE = "3050"; // lũy kế đã đóng tuần trước (golden G03)

function stored(id: string): Stored {
  let s = STORE.get(id);
  if (!s) {
    s = { live: { cumulative: "3230", closedAt: "2026-12-09T03:00:00Z", snapshotNo: 1 }, snaps: [] };
    STORE.set(id, s);
  }
  return s;
}

function values(live: Live): Value[] {
  const inc = (Number(live.cumulative) - Number(BASE)).toString();
  const common = { no_source: false, frozen: false, source_changed: false, fallback: false, unavailable: false, unconfirmed: null };
  return [
    {
      ...common,
      source_key: KEY_LUA,
      entries: [ENTRY_LUA],
      mode: "season",
      invalid: false,
      cumulative: live.cumulative,
      base: BASE,
      base_ref: "frozen:2026-W48",
      increment: inc,
      year_to_date: inc,
      sum: null,
      price_min: null,
      price_max: null,
      price_day: null,
      price_days: [],
      closed: true,
      provisional: false,
      source_keys: ["trong_trot:v2:2026-W49"],
    },
    {
      ...common,
      source_key: KEY_GIA,
      entries: [ENTRY_GIA],
      mode: "price",
      invalid: false,
      cumulative: null,
      base: null,
      increment: null,
      year_to_date: null,
      sum: null,
      price_min: "8000",
      price_max: "14000",
      price_day: "2026-11-27",
      price_days: [
        { day: "2026-11-26", min: "8000", max: "13500" },
        { day: "2026-11-27", min: "8000", max: "14000" },
      ],
      closed: false,
      provisional: true,
      source_keys: ["thong_tin_truyen_thong:v2:2026-11-26", "thong_tin_truyen_thong:v2:2026-11-27"],
    },
  ];
}

function asOf(live: Live): Status["as_of"] {
  return [
    {
      department: "trong_trot",
      source_module: "pf/extension",
      form_code: "PL01",
      native_keys: ["2026-W49"],
      closed_at: live.closedAt,
      snapshot_no: live.snapshotNo,
      closed: true,
      missing: false,
      unavailable: false,
      fallback: false,
      confirmation: "confirmed",
    },
    {
      department: "thong_tin_truyen_thong",
      source_module: "pf/price",
      form_code: "PRICE-BOARD",
      native_keys: ["2026-11-26", "2026-11-27"],
      closed_at: "2026-11-27T09:00:00Z",
      snapshot_no: 1,
      closed: false,
      missing: false,
      unavailable: false,
      fallback: false,
      confirmation: "confirmed",
    },
  ];
}

function canRefresh(ctx: Ctx): boolean {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  return !kc || (kc.role === "kn_tinh" && kc.department_code === "ke_hoach_tai_chinh");
}

function status(ctx: Ctx, id: string): Status {
  const s = stored(id);
  const last = s.snaps[s.snaps.length - 1];
  const changed = !!last && last.live.cumulative !== s.live.cumulative;
  return {
    period_id: id,
    owner_ref: OWNER,
    owner_name: "Cán bộ KHTC phụ trách",
    terminal: null,
    can_refresh: canRefresh(ctx) && !khtcMockStore().find(p => p.p.id === id)?.reportFinal,
    source_changed: changed,
    live: !last,
    snapshot: last ? last.snap : null,
    as_of: asOf(last ? last.live : s.live),
    values: last ? last.values : [],
    changes: changed
      ? [
          {
            kind: "figures",
            target_key: "2026-W49",
            department: "trong_trot",
            source_module: "pf/extension",
            form_code: "PL01",
            native_key: "2026-W49",
            closed_at: s.live.closedAt,
            was_closed_at: last.live.closedAt,
          },
        ]
      : [],
  };
}

export const khtcSourceRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/sources$/,
    handler: (ctx) => json(status(ctx, ctx.params[0]!)),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/sources\/refresh$/,
    handler: (ctx) => {
      if (!canRefresh(ctx))
        return problem(403, "extension.khtc_refresh_not_owner", "Chỉ cán bộ KHTC phụ trách kỳ được \"Cập nhật số liệu\"");
      const id = ctx.params[0]!;
      if (khtcMockStore().find(p => p.p.id === id)?.reportFinal) return problem(409, "extension.khtc_report_final", "Báo cáo đã có Bản cuối");
      const s = stored(id);
      const prev = s.snaps[s.snaps.length - 1];
      const vals = values(s.live);
      const changes: HistoryItem["changes"] = [];
      if (prev) {
        const a = prev.values[0]!;
        const b = vals[0]!;
        if (a.cumulative !== b.cumulative)
          changes.push({ source_key: KEY_LUA, entries: [ENTRY_LUA], field: "cumulative", old: a.cumulative, new: b.cumulative });
        if (a.increment !== b.increment)
          changes.push({ source_key: KEY_LUA, entries: [ENTRY_LUA], field: "increment", old: a.increment, new: b.increment });
      }
      const snap: Snapshot = {
        id: uuidv7(),
        no: s.snaps.length + 1,
        taken_at: new Date().toISOString(),
        actor_ref: OWNER,
        actor_name: "Cán bộ KHTC phụ trách",
        closed: false,
        target_calendar: "ke_hoach_tai_chinh:v2",
        crosswalk_version: "kn.crosswalk/v1;attribution=source_end",
        plan_version_id: null,
        figures_hash: "0".repeat(64),
        status_hash: "0".repeat(64),
        row_count: 3,
        value_count: vals.length,
        change_count: changes.length,
      };
      s.snaps.push({ snap, values: vals, live: { ...s.live }, changes });
      // Giả lập G03: sau lần chụp đầu, Trồng trọt mở lại tuần 49 và chốt lại 3.190 → "Nguồn đã thay đổi".
      if (s.snaps.length === 1) s.live = { cumulative: "3190", closedAt: "2026-12-17T08:00:00Z", snapshotNo: 2 };
      return json(status(ctx, id));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/periods\/([^/]+)\/sources\/history$/,
    handler: (ctx) => {
      const s = stored(ctx.params[0]!);
      return json({ items: [...s.snaps].reverse().map((x) => ({ snapshot: x.snap, changes: x.changes })) });
    },
  },
];

export function khtcReportSource(ctx: Ctx, id: string, refresh = false): Status {
 if (refresh) { const response = khtcSourceRoutes[1]!.handler({ ...ctx, params: [id] }); if (!response.ok) throw new Error("Không cập nhật được nguồn giả lập"); }
 return structuredClone(status(ctx, id));
}
