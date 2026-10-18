// ============================================================================
// BACKEND GIẢ LẬP — Duyệt / mở duyệt bảng giá ngày (W11.76, cap/price, BRD Báo giá nông sản v1.3 FN-TI-02.05, 02.06).
// Đường dẫn, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/price/boards*). Quy tắc mô phỏng (rút
// gọn từ internal/platform/price/app/board.go):
//   - Duyệt khi đã qua giờ khóa, hết cảnh báo chưa xử lý, có dòng giá; khóa "Đang duyệt" một người 10 phút.
//   - Duyệt → phiên bản mới (ảnh chụp dòng CT-04: min / max toàn tỉnh, trung bình P các xã); mở duyệt có lý do, trong 30
//     ngày; hủy mở duyệt (giả lập không kiểm "đã sửa").
//   - Cấp Tỉnh duyệt; Admin chỉ xem (vận hành chính thức); Cấp TW / Xã chỉ thấy phiên bản đã duyệt.
// Dữ liệu là GIẢ LẬP. Trạng thái nằm trong bộ nhớ tab. Không float (Decimal BigInt).
// ============================================================================
import type { components } from "../generated/application";
import { Decimal } from "@/kn/grid/decimal";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { addDay, vnToday } from "./kn-price-items";
import { mockDayBoardSource, priceQuoteDay } from "./kn-price-tickets";

type S = components["schemas"];
type Role = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";
type State = S["PriceBoardState"];

interface BoardVer {
  seq: number;
  action: S["PriceBoardLog"]["action"];
  status: State;
  base: State | null;
  lock_until: string | null;
  version: number;
  approved_at: string | null;
  reason: string | null;
  actor_ref: string;
  at: string;
  lines: S["PriceBoardLine"][] | null; // chỉ thao tác approve
}

const ME = "0190c0de-0000-7000-8000-0000000071a1";
const LOCK_MS = 10 * 60_000;
let BOARDS = new Map<string, BoardVer[]>();

/** Đặt lại trạng thái giả lập (kiểm thử). */
export function resetPriceBoards(): void {
  BOARDS = new Map();
}

/** Trạng thái nhìn của bảng giá ngày (phiếu giả lập dùng để chặn sửa / nhãn Đã duyệt). */
export function mockBoardState(day: string): State {
  const vs = BOARDS.get(day);
  if (!vs?.length) return "open";
  const b = vs[vs.length - 1]!;
  if (b.status === "approving" && b.lock_until && Date.now() >= Date.parse(b.lock_until)) return b.base ?? "open";
  return b.status;
}

function roleOf(ctx: Ctx): Role | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (kc) return kc.role as Role;
  const r = ctx.persona.me.roles;
  if (r.includes("platform_admin")) return "admin";
  if (r.includes("kn_province_admin")) return "kn_tinh";
  if (r.includes("kn_officer")) return "kn_xa";
  return problem(403, "forbidden", "vai của người gọi không thuộc cấu hình giá kn");
}

const D = (s: string) => Decimal.parse(s)!;
const lockAt = (day: string) => `${day}T05:00:00Z`;

function preview(day: string): { lines: S["PriceBoardLine"][]; summary: S["PriceBoardSummary"] } {
  const src = mockDayBoardSource(day);
  const by = new Map<string, { line: S["PriceBoardLine"]; sum: Decimal; n: number }>();
  for (const l of src.lines) {
    const cur = by.get(l.item.code);
    if (!cur) {
      by.set(l.item.code, {
        line: {
          item_code: l.item.code,
          item_name: `${l.item.name} ${l.item.variety}`.trim(),
          group_code: l.item.group_code,
          group_name: l.item.group_name,
          unit_code: l.item.unit_code,
          unit_name: l.item.unit_name,
          min: l.min,
          max: l.max,
          avg: "0",
          points: 0,
        },
        sum: D(l.mid),
        n: 1,
      });
      continue;
    }
    if (D(l.min).lt(D(cur.line.min))) cur.line.min = l.min;
    if (D(l.max).gt(D(cur.line.max))) cur.line.max = l.max;
    cur.sum = cur.sum.add(D(l.mid));
    cur.n++;
  }
  const lines = [...by.values()].map((x) => ({ ...x.line, avg: x.sum.divRound(Decimal.fromInt(x.n), 4).toString(), points: x.n }));
  return { lines, summary: { ...src.summary, price_lines: src.lines.length } };
}

function dto(role: Role, day: string): S["PriceBoard"] {
  const vs = BOARDS.get(day) ?? [];
  const cur = vs[vs.length - 1];
  const state = mockBoardState(day);
  const full = role === "kn_tinh" || role === "admin";
  const quote = priceQuoteDay(day);
  const locked = quote && Date.now() >= Date.parse(lockAt(day));
  const pv = full ? preview(day) : null;
  const blockers: string[] = [];
  if (full && state !== "approved") {
    if (!quote) blockers.push("Không phải ngày báo giá");
    if (!locked) blockers.push("Chưa qua giờ khóa nhập");
    if (pv!.summary.open_alerts > 0) blockers.push(`Còn ${pv!.summary.open_alerts} cảnh báo chưa xử lý`);
    if (pv!.lines.length === 0) blockers.push("Không có dòng giá nào để công bố");
  }
  const approve = [...vs].reverse().find((v) => v.action === "approve" && v.version === cur?.version);
  const lockedBy = state === "approving" ? cur!.actor_ref : null;
  const can = role === "kn_tinh";
  return {
    day,
    quote,
    lock_at: quote ? lockAt(day) : null,
    locked,
    now: new Date().toISOString(),
    state,
    seq: cur?.seq ?? 0,
    version: cur?.version ?? 0,
    locked_by: lockedBy,
    locked_by_name: lockedBy ? "Cán bộ Tỉnh (giả lập)" : null,
    lock_until: lockedBy ? cur!.lock_until : null,
    summary: pv ? pv.summary : null,
    preview: pv ? pv.lines : [],
    published:
      approve && cur
        ? {
            version: cur.version,
            approved_at: cur.approved_at!,
            reopened: state === "reopened" || (state === "approving" && cur.base === "reopened"),
            week_key: "",
            lines: approve.lines ?? [],
          }
        : null,
    log: full
      ? vs.map((v) => ({ seq: v.seq, action: v.action, status: v.status, version: v.version, reason: v.reason, actor_ref: v.actor_ref, actor_name: "Cán bộ Tỉnh (giả lập)", at: v.at }))
      : [],
    can: {
      approve: can && blockers.length === 0 && (state === "open" || state === "reopened" || (state === "approving" && lockedBy === ME)),
      unlock: can && state === "approving" && lockedBy === ME,
      reopen: can && state === "approved" && day >= addDay(vnToday(), -30),
      cancel_reopen: can && state === "reopened",
    },
    blockers,
    reopen_days: 30,
  };
}

function write(ctx: Ctx, action: BoardVer["action"]): Response {
  const role = roleOf(ctx);
  if (role instanceof Response) return role;
  if (role !== "kn_tinh") return problem(403, "forbidden", role === "admin" ? "Admin không duyệt bảng giá khi vận hành chính thức" : "chỉ cán bộ Cấp Tỉnh");
  const day = ctx.params[0]!;
  const b = (ctx.body ?? {}) as S["PriceBoardOp"];
  const vs = BOARDS.get(day) ?? [];
  const cur = vs[vs.length - 1];
  if ((cur?.seq ?? 0) !== b.seq) return problem(409, "price.board_stale", "tải lại bảng giá");
  const state = mockBoardState(day);
  const v = dto("kn_tinh", day);
  const now = new Date().toISOString();
  const base: Omit<BoardVer, "action" | "status"> = {
    seq: vs.length + 1,
    base: null,
    lock_until: null,
    version: cur?.version ?? 0,
    approved_at: cur?.approved_at ?? null,
    reason: null,
    actor_ref: ME,
    at: now,
    lines: null,
  };
  const ready = (): Response | null => {
    if (!v.quote) return problem(422, "price.not_quote_day", day);
    if (!v.locked) return problem(409, "price.board_not_locked", day);
    if (v.summary!.open_alerts > 0) return problem(409, "price.board_open_alerts", `còn ${v.summary!.open_alerts} cảnh báo`);
    if (v.preview.length === 0) return problem(422, "price.board_empty", day);
    return null;
  };
  switch (action) {
    case "lock": {
      if (state !== "open" && state !== "reopened") return problem(409, "price.board_state", state);
      const bad = ready();
      if (bad) return bad;
      vs.push({ ...base, action, status: "approving", base: state, lock_until: new Date(Date.now() + LOCK_MS).toISOString() });
      break;
    }
    case "unlock":
      if (state !== "approving") return problem(409, "price.board_state", state);
      vs.push({ ...base, action, status: cur!.base ?? "open" });
      break;
    case "approve": {
      if (state === "approved") return problem(409, "price.board_state", state);
      const bad = ready();
      if (bad) return bad;
      vs.push({ ...base, action, status: "approved", version: base.version + 1, approved_at: now, lines: v.preview });
      break;
    }
    case "reopen":
      if (!(b.reason ?? "").trim()) return problem(422, "price.reason_required", "lý do mở duyệt");
      if (state !== "approved") return problem(409, "price.board_state", state);
      if (day < addDay(vnToday(), -30)) return problem(422, "price.reopen_expired", "30 ngày");
      vs.push({ ...base, action, status: "reopened", reason: b.reason!.trim() });
      break;
    case "cancel_reopen":
      if (state !== "reopened") return problem(409, "price.board_state", state);
      vs.push({ ...base, action, status: "approved", reason: (b.reason ?? "").trim() || null });
      break;
  }
  BOARDS.set(day, vs);
  return json(dto("kn_tinh", day), 201);
}

export const priceBoardRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/price\/boards$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      const to = ctx.url.searchParams.get("to") ?? vnToday();
      const from = ctx.url.searchParams.get("from") ?? addDay(to, -13);
      const boards: S["PriceBoardDay"][] = [];
      for (let d = from; d <= to; d = addDay(d, 1)) {
        if (!priceQuoteDay(d) && !BOARDS.has(d)) continue;
        const vs = BOARDS.get(d) ?? [];
        const cur = vs[vs.length - 1];
        boards.push({ day: d, quote: priceQuoteDay(d), state: mockBoardState(d), version: cur?.version ?? 0, approved_at: cur?.approved_at ?? null });
      }
      return json({ boards });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/boards\/(\d{4}-\d{2}-\d{2})$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      return json(dto(role, ctx.params[0]!));
    },
  },
  { method: "POST", pattern: /^\/v1\/price\/boards\/(\d{4}-\d{2}-\d{2})\/lock$/, handler: (ctx) => write(ctx, "lock") },
  { method: "POST", pattern: /^\/v1\/price\/boards\/(\d{4}-\d{2}-\d{2})\/unlock$/, handler: (ctx) => write(ctx, "unlock") },
  { method: "POST", pattern: /^\/v1\/price\/boards\/(\d{4}-\d{2}-\d{2})\/approve$/, handler: (ctx) => write(ctx, "approve") },
  { method: "POST", pattern: /^\/v1\/price\/boards\/(\d{4}-\d{2}-\d{2})\/reopen$/, handler: (ctx) => write(ctx, "reopen") },
  { method: "POST", pattern: /^\/v1\/price\/boards\/(\d{4}-\d{2}-\d{2})\/cancel-reopen$/, handler: (ctx) => write(ctx, "cancel_reopen") },
];
