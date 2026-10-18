// ============================================================================
// BACKEND GIẢ LẬP — Báo cáo giá (W11.77, cap/price, BRD Báo giá nông sản v1.3 FN-TI-02.01, FN-TI-02.08, FN-TW-01.01…03).
// Đường dẫn, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/price/reports/*). Số liệu là ẢNH CỐ
// ĐỊNH của một ngày công bố (ba xã, hai nhóm vùng, hai mặt hàng) — đủ để xem bố cục màn; quy tắc tính thật ở
// internal/platform/price/app/report*.go. Quyền: Tiến độ / Thống kê chỉ Cấp Tỉnh, Admin; Dashboard không cho Cấp Xã;
// xuất Biến động chỉ Cấp Tỉnh, Admin. Không float (giá là chuỗi decimal).
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { vnToday } from "./kn-price-items";

type S = components["schemas"];
type Role = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";

function roleOf(ctx: Ctx): Role | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (kc) return kc.role as Role;
  const r = ctx.persona.me.roles;
  if (r.includes("platform_admin")) return "admin";
  if (r.includes("kn_province_admin")) return "kn_tinh";
  if (r.includes("kn_officer")) return "kn_xa";
  return problem(
    403,
    "forbidden",
    "vai của người gọi không thuộc cấu hình giá kn",
  );
}

function allow(ctx: Ctx, ...roles: Role[]): Role | Response {
  const role = roleOf(ctx);
  if (role instanceof Response) return role;
  if (role !== "admin" && !roles.includes(role))
    return problem(403, "forbidden", "vai của bạn không xem báo cáo này");
  return role;
}

const REGIONS: S["PriceRef"][] = [
  { code: "an_giang", name: "An Giang" },
  { code: "kien_giang", name: "Kiên Giang" },
];
const GROUPS: S["PriceRef"][] = [
  { code: "rau_mau", name: "Rau màu" },
  { code: "chan_nuoi", name: "Chăn nuôi" },
];
const ITEMS: S["PriceReportItem"][] = [
  {
    item_code: "MH00001",
    item_name: "Dưa leo Loại 1",
    group_code: "rau_mau",
    group_name: "Rau màu",
    unit_code: "kg",
    unit_name: "kg",
    basic: true,
  },
  {
    item_code: "MH00002",
    item_name: "Heo hơi Thịt",
    group_code: "chan_nuoi",
    group_name: "Chăn nuôi",
    unit_code: "kg",
    unit_name: "kg",
    basic: true,
  },
];
const POINTS = [
  {
    code: "XBG0001",
    name: "Mỹ Chánh",
    region: "an_giang",
    prices: [
      ["8000", "10000", "9000"],
      ["38000", "40000", "39000"],
    ],
  },
  {
    code: "XBG0002",
    name: "Núi Cấm",
    region: "an_giang",
    prices: [
      ["9000", "11000", "10000"],
      ["40000", "42000", "41000"],
    ],
  },
  {
    code: "XBG0003",
    name: "Hà Tiên",
    region: "kien_giang",
    prices: [
      ["12000", "14000", "13000"],
      ["41000", "43000", "42000"],
    ],
  },
] as const;

function pointPrices(region?: string, item?: string): S["PricePointPrice"][] {
  const out: S["PricePointPrice"][] = [];
  for (const p of POINTS) {
    if (region && p.region !== region) continue;
    ITEMS.forEach((it, i) => {
      if (item && it.item_code !== item) return;
      const [min, max, mid] = p.prices[i]!;
      out.push({
        ...it,
        point_code: p.code,
        point_name: p.name,
        region_code: p.region,
        region_name: REGIONS.find((r) => r.code === p.region)!.name,
        min,
        max,
        mid,
        late: p.code === "XBG0003",
      });
    });
  }
  return out;
}

const SUMMARY: Record<string, S["PriceSummaryLine"][]> = {
  "": [
    {
      ...ITEMS[0]!,
      min: "8000",
      max: "14000",
      avg: "10666.6667",
      points: 3,
      prev: "10500",
      change_pct: "1.59",
      trend: "up",
    },
    {
      ...ITEMS[1]!,
      min: "38000",
      max: "43000",
      avg: "40666.6667",
      points: 3,
      prev: "40666.6667",
      change_pct: "0.00",
      trend: "flat",
    },
  ],
  an_giang: [
    {
      ...ITEMS[0]!,
      min: "8000",
      max: "11000",
      avg: "9500",
      points: 2,
      prev: "9600",
      change_pct: "-1.04",
      trend: "down",
    },
    {
      ...ITEMS[1]!,
      min: "38000",
      max: "42000",
      avg: "40000",
      points: 2,
      prev: "40000",
      change_pct: "0.00",
      trend: "flat",
    },
  ],
  kien_giang: [
    {
      ...ITEMS[0]!,
      min: "12000",
      max: "14000",
      avg: "13000",
      points: 1,
      prev: "12300",
      change_pct: "5.69",
      trend: "up",
    },
    {
      ...ITEMS[1]!,
      min: "41000",
      max: "43000",
      avg: "42000",
      points: 1,
      prev: "42000",
      change_pct: "0.00",
      trend: "flat",
    },
  ],
};

const base = () => {
  const day = vnToday();
  return {
    requested: day,
    day,
    version: 1,
    approved_at: `${day}T06:30:00Z`,
    not_today: false,
    updating: false,
    prev_day: null,
  };
};

export const priceReportRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/price\/reports\/progress$/,
    handler: (ctx) => {
      const role = allow(ctx, "kn_tinh");
      if (role instanceof Response) return role;
      const day = ctx.url.searchParams.get("day") || vnToday();
      const rows: S["PriceProgressRow"][] = POINTS.map((p, i) => ({
        point: {
          code: p.code,
          name: p.name,
          region_code: p.region,
          region_name: REGIONS.find((r) => r.code === p.region)!.name,
          active: true,
          officer_ref:
            i === 2 ? null : `0190c0de-0000-7000-8000-00000000000${i}`,
          officer_name: i === 2 ? null : `Cán bộ Xã ${p.name}`,
          owner_ref: null,
          owner_name: null,
        },
        mine: false,
        view: (["submitted", "draft", "not_entered"] as const)[i]!,
        ticket: null,
        items: 2,
        priced: i === 0 ? 2 : i === 1 ? 1 : 0,
        no_goods: 0,
        adjusted: 0,
        excluded: 0,
        high: i === 0 ? 1 : 0,
        low: 0,
        open_alerts: i === 0 ? 1 : 0,
        updated_at: i === 2 ? null : `${day}T02:30:00Z`,
        reminders: i === 2 ? [] : [`${day}T00:30:00Z`],
      })) as S["PriceProgressRow"][];
      const status = ctx.url.searchParams.get("status") ?? "";
      const shown = rows.filter(
        (r) =>
          !status ||
          (status === "incomplete"
            ? r.view !== "submitted"
            : r.view === status),
      );
      const count = (v: string) => shown.filter((r) => r.view === v).length;
      return json({
        requested: day,
        day,
        no_quote: false,
        quote: true,
        lock_at: `${day}T05:00:00Z`,
        locked: false,
        board: "open",
        now: new Date().toISOString(),
        cards: {
          points: shown.length,
          submitted: count("submitted"),
          draft: count("draft"),
          not_entered: count("not_entered"),
          missed: count("missed"),
          late: count("late"),
          high: shown.reduce((s, r) => s + r.high, 0),
          low: 0,
          unassigned: shown.length,
          no_officer: shown.filter((r) => !r.point.officer_ref).length,
        },
        rows: shown,
        regions: REGIONS,
      } satisfies S["PriceProgress"]);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/reports\/summary$/,
    handler: (ctx) => {
      const role = allow(ctx, "kn_tw", "kn_tinh", "kn_xa");
      if (role instanceof Response) return role;
      const region = ctx.url.searchParams.get("region") ?? "";
      const group = ctx.url.searchParams.get("group") ?? "";
      const item = ctx.url.searchParams.get("item") ?? "";
      const view = ctx.url.searchParams.get("view") ?? "item";
      return json({
        ...base(),
        flat_pct: "1.00",
        region,
        read_only: role !== "kn_tinh" && role !== "admin",
        lines: (SUMMARY[region] ?? SUMMARY[""]!).filter(
          (l) => !group || l.group_code === group,
        ),
        points:
          view === "point" || item
            ? pointPrices(region, item || undefined).filter(
                (p) => !group || p.group_code === group,
              )
            : [],
        regions: REGIONS,
        groups: GROUPS,
      } satisfies S["PriceSummary"]);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/reports\/movement$/,
    handler: (ctx) => {
      const role = allow(ctx, "kn_tw", "kn_tinh", "kn_xa");
      if (role instanceof Response) return role;
      const day = vnToday();
      const period = (ctx.url.searchParams.get("period") ??
        "day") as S["PriceMovement"]["period"];
      const lines: S["PriceMovementLine"][] = SUMMARY[""]!.map((l) => ({
        ...ITEMS.find((i) => i.item_code === l.item_code)!,
        cur: l.avg,
        prev: l.prev,
        change_pct: l.change_pct,
        trend: l.trend,
      }));
      const item = ctx.url.searchParams.get("item") || "MH00001";
      return json({
        period,
        cur: { from: day, to: day, label: "Kỳ này", days: 1 },
        prev: { from: day, to: day, label: "Kỳ trước", days: 1 },
        insufficient: false,
        flat_pct: "1.00",
        cards: { up: 1, down: 0, flat: 1, exceeded: 2 },
        lines,
        item,
        trend: [
          {
            period: { from: day, to: day, label: "Kỳ trước", days: 1 },
            avg: "10500",
          },
          {
            period: { from: day, to: day, label: "Kỳ này", days: 1 },
            avg: "10666.6667",
          },
        ],
        points: POINTS.map((p) => ({
          point_code: p.code,
          point_name: p.name,
          region_name: REGIONS.find((r) => r.code === p.region)!.name,
          cur: p.prices[0][2],
          prev: p.prices[0][2],
          change_pct: "0.00",
          trend: "flat" as const,
        })),
        regions: REGIONS,
        groups: GROUPS,
        can_export: role === "kn_tinh" || role === "admin",
      } satisfies S["PriceMovement"]);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/reports\/dashboard$/,
    handler: (ctx) => {
      const role = allow(ctx, "kn_tw", "kn_tinh");
      if (role instanceof Response) return role;
      const region = ctx.url.searchParams.get("region") ?? "";
      const item = ctx.url.searchParams.get("item") || "MH00001";
      const avgOf = (r: string, code: string) =>
        SUMMARY[r]!.find((l) => l.item_code === code)!.avg;
      return json({
        ...base(),
        cards: { up: 1, down: 0, flat: 1, points: 3 },
        regions: [
          { code: "an_giang", name: "An Giang", points: 2 },
          { code: "kien_giang", name: "Kiên Giang", points: 1 },
        ],
        lines: SUMMARY[""]!.map((l) => ({
          item: ITEMS.find((i) => i.item_code === l.item_code)!,
          avg: l.avg,
          prev: l.prev,
          change_pct: l.change_pct,
          trend: l.trend,
          regions: REGIONS.map((r) => ({
            region: r.code,
            avg: avgOf(r.code, l.item_code),
            points: r.code === "an_giang" ? 2 : 1,
          })),
        })),
        groups: [
          { code: "rau_mau", name: "Rau màu", up: 1, down: 0, flat: 0 },
          { code: "chan_nuoi", name: "Chăn nuôi", up: 0, down: 0, flat: 1 },
        ],
        item,
        trend: [
          {
            day: vnToday(),
            avg: avgOf("", item),
            regions: REGIONS.map((r) => ({
              region: r.code,
              avg: avgOf(r.code, item),
              points: 1,
            })),
          },
        ],
        region,
        drill: region ? pointPrices(region) : [],
      } satisfies S["PriceDashboard"]);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/reports\/stats$/,
    handler: (ctx) => {
      const role = allow(ctx, "kn_tinh");
      if (role instanceof Response) return role;
      const month = ctx.url.searchParams.get("month") || vnToday().slice(0, 7);
      const rows: S["PriceStatRow"][] = POINTS.map((p, i) => ({
        officer_ref: `0190c0de-0000-7000-8000-00000000000${i}`,
        officer_name: `Cán bộ Xã ${p.name}`,
        point_code: p.code,
        point_name: p.name,
        region_code: p.region,
        region_name: REGIONS.find((r) => r.code === p.region)!.name,
        required: 22,
        on_time: 19,
        late: 1,
        missed: 2,
        deleted: 0,
        adjusted_lines: i,
      }));
      return json({
        from: `${month}-01`,
        to: `${month}-28`,
        monthly: true,
        month: { seq: 0, status: "none", version: 0, reason: null },
        snapshot: false,
        pending: 0,
        cards: {
          quote_days: 22,
          required: 66,
          on_time: rows.reduce((s, r) => s + r.on_time, 0),
          late: 3,
          missed: rows.reduce((s, r) => s + r.missed, 0),
        },
        rows,
        can_close: false,
        can_reopen: false,
        regions: REGIONS,
      } satisfies S["PriceStats"]);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/reports\/stats\/detail$/,
    handler: (ctx) => {
      const role = allow(ctx, "kn_tinh");
      if (role instanceof Response) return role;
      const day = vnToday();
      return json({
        days: [
          {
            day,
            point_code: ctx.url.searchParams.get("point") ?? "XBG0001",
            result: "on_time",
            view: "approved",
            ticket_id: null,
            deleted: 0,
            adjusted_lines: 0,
          },
        ],
      });
    },
  },
  {
    method: "GET",
    pattern:
      /^\/v1\/price\/reports\/(progress|summary|movement|dashboard|stats)\/export$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (
        ctx.params[0] === "movement" &&
        role !== "kn_tinh" &&
        role !== "admin"
      )
        return problem(403, "forbidden", "chỉ Cấp Tỉnh, Admin xuất");
      return new Response(new Uint8Array([80, 75, 3, 4]), {
        headers: {
          "Content-Type":
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
          "Content-Disposition": `attachment; filename="${ctx.params[0]}.xlsx"`,
        },
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/reports\/stats\/(close|reopen)$/,
    handler: () =>
      problem(409, "price.stat_month_open", "Giả lập: tháng chưa kết thúc"),
  },
];
