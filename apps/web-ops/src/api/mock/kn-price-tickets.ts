// ============================================================================
// BACKEND GIẢ LẬP — Phiếu báo giá + xác minh (W11.75, cap/price, BRD Báo giá nông sản v1.3 FN-XA-03.01…03, 03.05,
// FN-TI-02.02…04, 02.09). Đường dẫn, tham số, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml
// (/v1/price/tickets*, /v1/price/alerts*). Quy tắc mô phỏng (rút gọn từ internal/platform/price):
//   - Lịch T2–T6, khóa 12:00, hạn phản hồi 13:30 (giờ VN); phiếu Xã chỉ nhập hôm nay trước giờ khóa (409 price.ticket_locked).
//   - Giá tham chiếu = P của phiếu đã gửi gần nhất trong 7 ngày trước (giá hiệu chỉnh nếu có, bỏ dòng đã loại); ngưỡng 30 %.
//   - Persona Cấp Xã phụ trách XBG0001; Cấp Tỉnh xử lý mọi xã; Admin như Cấp Tỉnh.
//   - Có sẵn phiếu đã gửi 5 ngày báo giá trước cho các xã có cán bộ Xã.
// Dữ liệu là GIẢ LẬP. Trạng thái nằm trong bộ nhớ tab. Không float (Decimal BigInt).
// ============================================================================
import type { components } from "../generated/application";
import { Decimal } from "@/kn/grid/decimal";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { addDay, mockPointItems, vnToday } from "./kn-price-items";
import { MOCK_XA_OFFICER, mockPointsAt } from "./kn-price-points";

type S = components["schemas"];
type Role = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";
type AlertVer = S["PriceAlertVersion"];

interface LineVer {
  version: number;
  min: string | null;
  max: string | null;
  no_goods: boolean;
  note: string | null;
  adj_min: string | null;
  adj_max: string | null;
  adj_reason: string | null;
  actor_ref: string;
  at: string;
}
interface HeadVer {
  version: number;
  day: string;
  status: "draft" | "submitted" | "late" | "deleted";
  source: "xa" | "proxy";
  op: S["PriceTicketHead"]["op"];
  reason: string | null;
  submitted_at: string | null;
  officer_ref: string | null;
  actor_ref: string;
  at: string;
}
interface Ticket {
  id: string;
  point: string;
  heads: HeadVer[];
  lines: Map<string, LineVer[]>;
  alerts: Map<string, AlertVer[]>;
}

const ACTOR = "0190c0de-0000-7000-8000-00000000ad01";
let TICKETS: Ticket[] = [];
let seeded = false;
let nextId = 1;

const uuid = () => `0190c0de-7175-7000-8000-${String(nextId++).padStart(12, "0")}`;
const last = <T>(xs: T[]): T => xs[xs.length - 1]!;
const quote = (day: string) => {
  const w = new Date(`${day}T00:00:00Z`).getUTCDay();
  return w >= 1 && w <= 5;
};
const lockAt = (day: string) => `${day}T05:00:00Z`;
const respondAt = (day: string) => `${day}T06:30:00Z`;
const locked = (day: string) => quote(day) && Date.now() >= Date.parse(lockAt(day));
const D = (s: string) => Decimal.parse(s)!;
const mid = (lo: string, hi: string) => D(lo).add(D(hi)).divRound(Decimal.fromInt(2), 4).toString();

function seedOnce(): void {
  if (seeded) return;
  seeded = true;
  const today = vnToday();
  let d = today;
  let n = 0;
  while (n < 5) {
    d = addDay(d, -1);
    if (!quote(d)) continue;
    n++;
    for (const p of mockPointsAt(d)) {
      if (!p.officer_ref) continue;
      const t: Ticket = { id: uuid(), point: p.code, heads: [], lines: new Map(), alerts: new Map() };
      const at = `${d}T03:00:00Z`;
      t.heads.push({ version: 1, day: d, status: "submitted", source: "xa", op: "submit", reason: null, submitted_at: at, officer_ref: p.officer_ref, actor_ref: p.officer_ref, at });
      mockPointItems(p.code, d).forEach((it, i) => {
        const base = 8000 + i * 1500 + n * 100;
        t.lines.set(it.code, [
          { version: 1, min: String(base), max: String(base + 500), no_goods: false, note: null, adj_min: null, adj_max: null, adj_reason: null, actor_ref: p.officer_ref!, at },
        ]);
      });
      TICKETS.push(t);
    }
  }
}

/** Đặt lại trạng thái giả lập (kiểm thử). */
export function resetPriceTickets(): void {
  TICKETS = [];
  seeded = false;
  nextId = 1;
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

const isDay = (s: unknown): s is string => typeof s === "string" && /^\d{4}-\d{2}-\d{2}$/.test(s);
const dayQ = (ctx: Ctx, name: string, def: string) => {
  const v = ctx.url.searchParams.get(name);
  return isDay(v) ? v : def;
};

function liveTicket(point: string, day: string): Ticket | undefined {
  return TICKETS.find((t) => t.point === point && last(t.heads).day === day && last(t.heads).status !== "deleted");
}

function sent(t: Ticket) {
  const s = last(t.heads).status;
  return s === "submitted" || s === "late";
}

function refPrices(point: string, day: string): Map<string, { min: string; max: string; mid: string; day: string }> {
  const out = new Map<string, { min: string; max: string; mid: string; day: string }>();
  const cand = TICKETS.filter((t) => t.point === point && sent(t) && last(t.heads).day < day && last(t.heads).day >= addDay(day, -10)).sort((a, b) =>
    last(a.heads).day < last(b.heads).day ? 1 : -1,
  );
  for (const t of cand) {
    for (const [code, vs] of t.lines) {
      if (out.has(code)) continue;
      const l = last(vs);
      if (t.alerts.get(code) && last(t.alerts.get(code)!).status === "excluded") continue;
      const lo = l.adj_min ?? l.min;
      const hi = l.adj_max ?? l.max;
      if (l.no_goods || !lo || !hi) continue;
      out.set(code, { min: lo, max: hi, mid: mid(lo, hi), day: last(t.heads).day });
    }
  }
  return out;
}

function evaluate(lo: string, hi: string, ref: string): { direction: "" | "high" | "low"; pct: string; price: string } {
  const p = D(mid(lo, hi));
  const r = D(ref);
  const pct = p.sub(r).mul(Decimal.fromInt(100)).divRound(r, 4);
  const t = Decimal.fromInt(30);
  return { direction: !pct.lt(t) ? "high" : !pct.gt(t.neg()) ? "low" : "", pct: pct.round(2).toString(), price: p.toString() };
}

function alertView(a: AlertVer, day: string): S["PriceAlertView"] {
  if (a.status === "provisional" && locked(day)) return "pending";
  if (a.status === "reopened") return "pending";
  if (a.status === "requested" && a.respond_by && Date.now() >= Date.parse(a.respond_by)) return "overdue";
  return a.status;
}

function alertDto(t: Ticket, code: string): S["PriceAlert"] | null {
  const hs = t.alerts.get(code);
  if (!hs?.length) return null;
  const a = last(hs);
  const req = [...hs].reverse().find((h) => h.status === "requested") ?? null;
  const rep = [...hs].reverse().find((h) => h.status === "responded") ?? null;
  return { ...a, ticket_id: t.id, item_code: code, view: alertView(a, last(t.heads).day), request: req, reply: rep, history: hs };
}

function headDto(t: Ticket | undefined): S["PriceTicketHead"] | null {
  if (!t) return null;
  const h = last(t.heads);
  return { id: t.id, version: h.version, day: h.day, point_code: t.point, status: h.status, source: h.source, op: h.op, reason: h.reason, submitted_at: h.submitted_at, officer_ref: h.officer_ref, actor_ref: h.actor_ref, actor_name: null, updated_at: h.at };
}

function view(t: Ticket | undefined, day: string): S["PriceTicketView"] {
  const s = t ? last(t.heads).status : "";
  if (s === "deleted") return "deleted";
  if (s === "late") return "late";
  if (s === "submitted") return locked(day) ? "locked" : "submitted";
  if (locked(day)) return "missed";
  return s === "draft" ? "draft" : "not_entered";
}

function pointOf(code: string, day: string) {
  return mockPointsAt(day).find((p) => p.code === code);
}

function pointDto(p: ReturnType<typeof pointOf> & object, role: Role): S["PriceTicketPoint"] {
  const names = role === "kn_xa" ? { officer_name: null, owner_name: null } : {};
  return { active: true, ...p, ...names };
}

const handles = (role: Role) => role === "admin" || role === "kn_tinh";
const mineXa = (p: { officer_ref: string | null }) => p.officer_ref === MOCK_XA_OFFICER;

function sheet(role: Role, code: string, day: string, tk?: Ticket): S["PriceSheet"] | Response {
  const p = pointOf(code, day);
  if (!p) return problem(404, "price.point_not_found", `xã báo giá ${code}`);
  if (role === "kn_tw") return problem(403, "forbidden", "Cấp Quốc gia không xem phiếu báo giá");
  if (role === "kn_xa" && !mineXa(p)) return problem(403, "price.point_not_assigned", "Cán bộ Xã chỉ thao tác cho xã báo giá mình phụ trách");
  const t = tk ?? liveTicket(code, day);
  const refs = refPrices(code, day);
  const items = mockPointItems(code, day);
  const lk = locked(day);
  const lines: S["PriceSheetLine"][] = items.map((it) => {
    const l = t?.lines.get(it.code) ? last(t.lines.get(it.code)!) : null;
    const ref = refs.get(it.code) ?? null;
    const ev = l && ref && !l.no_goods && l.min && l.max ? evaluate(l.min, l.max, ref.mid) : null;
    return {
      item_code: it.code,
      name: it.name,
      variety: it.variety,
      group_code: it.group_code,
      group_name: it.group_name,
      unit_code: it.unit_code,
      unit_name: it.unit_name,
      kind: it.kind,
      ref,
      threshold_pct: "30.00",
      min: l?.min ?? null,
      max: l?.max ?? null,
      no_goods: l?.no_goods ?? false,
      note: l?.note ?? null,
      entered: !!l,
      adj_min: l?.adj_min ?? null,
      adj_max: l?.adj_max ?? null,
      adj_reason: l?.adj_reason ?? null,
      eval: ev ? { direction: ev.direction, change_pct: ev.pct } : null,
      alert: t ? alertDto(t, it.code) : null,
    };
  });
  const st = t ? last(t.heads).status : null;
  const today = vnToday();
  const review = role === "admin" || role === "kn_tinh";
  return {
    point: pointDto(p, role),
    day,
    quote: quote(day),
    lock_at: quote(day) ? lockAt(day) : null,
    respond_by: quote(day) ? respondAt(day) : null,
    locked: lk,
    board: "open",
    now: new Date().toISOString(),
    view: view(t, day),
    ticket: headDto(t),
    can: {
      enter: role === "kn_xa" && day === today && quote(day) && !lk && (!t || (last(t.heads).source === "xa" && st !== "deleted")),
      proxy: review && lk && (!t || st === "draft"),
      correct: review && lk && !!t && (st === "submitted" || st === "late"),
      redate: review && lk && !!t && (st === "submitted" || st === "late"),
      delete: review && lk && !!t && st !== "deleted",
      review,
      respond: role === "kn_xa",
    },
    lines,
  };
}

function writeEntry(ctx: Ctx, role: Role, proxy: boolean): Response {
  const b = ctx.body as S["PriceEntryInput"];
  const day = proxy ? b.day : (b.day ?? vnToday());
  if (!isDay(day)) return problem(422, "validation_failed", "day phải là ngày YYYY-MM-DD");
  const p = pointOf(b.point, day);
  if (!p) return problem(404, "price.point_not_found", `xã báo giá ${b.point}`);
  if (proxy) {
    if (role !== "admin" && role !== "kn_tinh") return problem(403, "forbidden", "chỉ Cấp Tỉnh nhập thay");
    if (!(b.reason ?? "").trim()) return problem(422, "price.reason_required", "lý do nộp muộn");
    if (!locked(day)) return problem(409, "price.ticket_not_locked", day);
  } else {
    if (role !== "kn_xa") return problem(403, "forbidden", "chỉ cán bộ Cấp Xã nhập báo giá");
    if (!mineXa(p)) return problem(403, "price.point_not_assigned", "Cán bộ Xã chỉ thao tác cho xã báo giá mình phụ trách");
    if (day !== vnToday() || !quote(day)) return problem(422, "price.not_quote_day", day);
    if (locked(day)) return problem(409, "price.ticket_locked", `khóa lúc ${lockAt(day)}`);
  }
  let t = liveTicket(b.point, day);
  if (proxy && t && sent(t)) return problem(409, "price.ticket_exists", "xã đã có phiếu đã gửi");
  if ((t ? last(t.heads).version : 0) !== b.version) return problem(409, "price.ticket_stale", "Phiếu đã được người khác cập nhật");
  const items = mockPointItems(b.point, day);
  const at = new Date().toISOString();
  const actor = proxy ? ACTOR : MOCK_XA_OFFICER;
  const merged = new Map<string, LineVer>();
  for (const [k, vs] of t?.lines ?? []) merged.set(k, last(vs));
  const bad: string[] = [];
  for (const l of b.lines) {
    if (!items.some((it) => it.code === l.item_code)) bad.push(`${l.item_code}: không thuộc danh sách mặt hàng của xã`);
    else if ((l.min == null) !== (l.max == null)) bad.push(`${l.item_code}: nhập đủ hai giá`);
    else if (l.min != null && l.max != null && (D(l.min).sign() <= 0 || D(l.min).gt(D(l.max)))) bad.push(`${l.item_code}: giá không hợp lệ`);
    else merged.set(l.item_code, { version: 0, min: l.no_goods ? null : (l.min ?? null), max: l.no_goods ? null : (l.max ?? null), no_goods: !!l.no_goods, note: l.note?.trim() || null, adj_min: null, adj_max: null, adj_reason: null, actor_ref: actor, at });
  }
  if (bad.length) return problem(422, "price.price_invalid", bad.join("; "));
  const refs = refPrices(b.point, day);
  if (b.submit) {
    const miss = items.filter((it) => {
      const l = merged.get(it.code);
      if (!l || (!l.no_goods && !l.min)) return true;
      const r = refs.get(it.code);
      return !!(r && l.min && l.max && evaluate(l.min, l.max, r.mid).direction && !l.note);
    });
    if (miss.length) return problem(422, "price.ticket_incomplete", miss.map((m) => m.name).join(", "));
  }
  if (!t) {
    t = { id: uuid(), point: b.point, heads: [], lines: new Map(), alerts: new Map() };
    TICKETS.push(t);
  }
  const prev = t.heads.length ? last(t.heads) : null;
  t.heads.push({
    version: (prev?.version ?? 0) + 1,
    day,
    status: b.submit ? (proxy ? "late" : "submitted") : "draft",
    source: proxy ? "proxy" : "xa",
    op: proxy ? (b.submit ? "proxy_submit" : "proxy_save") : b.submit ? "submit" : "save",
    reason: proxy ? b.reason!.trim() : null,
    submitted_at: b.submit ? at : null,
    officer_ref: p.officer_ref,
    actor_ref: actor,
    at,
  });
  for (const [k, l] of merged) {
    const vs = t.lines.get(k) ?? [];
    vs.push({ ...l, version: vs.length + 1 });
    t.lines.set(k, vs);
  }
  if (b.submit) {
    for (const it of items) {
      const l = merged.get(it.code);
      const r = refs.get(it.code);
      const hs = t.alerts.get(it.code) ?? [];
      const cur = hs.length ? last(hs) : null;
      const ev = l && r && l.min && l.max && !l.no_goods ? evaluate(l.min, l.max, r.mid) : null;
      const base = { version: hs.length + 1, actor_ref: actor, at, message: null, respond_by: null, response: null, proposed_min: null, proposed_max: null, explanation: null };
      if (!ev?.direction) {
        if (cur && ["provisional", "requested", "responded"].includes(cur.status)) hs.push({ ...cur, ...base, status: "cancelled" });
      } else if (!cur || cur.status === "cancelled") {
        hs.push({ ...base, status: "provisional", direction: ev.direction, ref_price: r!.mid, price: ev.price, change_pct: ev.pct, threshold_pct: "30.00" });
      } else if (["provisional", "requested", "responded"].includes(cur.status)) {
        hs.push({ ...cur, version: hs.length + 1, at, direction: ev.direction, price: ev.price, change_pct: ev.pct });
      }
      if (hs.length) t.alerts.set(it.code, hs);
    }
  }
  return json(sheet(role, b.point, day), 201);
}

function ticketById(id: string | undefined): Ticket | undefined {
  return TICKETS.find((t) => t.id === id);
}

function opTicket(ctx: Ctx, role: Role, op: "correct" | "redate" | "delete"): Response {
  if (role !== "admin" && role !== "kn_tinh") return problem(403, "forbidden", "chỉ Cấp Tỉnh hoặc vai quản trị");
  const t = ticketById(ctx.params[0]);
  if (!t) return problem(404, "price.ticket_not_found", "Không có phiếu báo giá này");
  const h = last(t.heads);
  const b = ctx.body as { version: number; reason: string; day?: string; lines?: S["PriceCorrectInput"]["lines"] };
  if (!(b.reason ?? "").trim()) return problem(422, "price.reason_required", "lý do");
  if (h.version !== b.version) return problem(409, "price.ticket_stale", "Phiếu đã được người khác cập nhật");
  if (!locked(h.day)) return problem(409, "price.ticket_not_locked", h.day);
  const at = new Date().toISOString();
  const nh: HeadVer = { ...h, version: h.version + 1, op, reason: b.reason.trim(), actor_ref: ACTOR, at };
  if (op === "delete") nh.status = "deleted";
  if (op === "redate") {
    if (!isDay(b.day) || !quote(b.day) || !locked(b.day)) return problem(422, "price.not_quote_day", String(b.day));
    if (liveTicket(t.point, b.day)) return problem(409, "price.ticket_exists", `xã đã có phiếu ngày ${b.day}`);
    nh.day = b.day;
  }
  if (op === "correct") {
    for (const a of b.lines ?? []) {
      const vs = t.lines.get(a.item_code);
      if (!vs || !last(vs).min) return problem(422, "price.price_invalid", `${a.item_code}: dòng không có giá`);
      if (D(a.min).sign() <= 0 || D(a.min).gt(D(a.max))) return problem(422, "price.price_invalid", `${a.item_code}: giá không hợp lệ`);
    }
    for (const a of b.lines ?? []) {
      const vs = t.lines.get(a.item_code)!;
      vs.push({ ...last(vs), version: vs.length + 1, adj_min: a.min, adj_max: a.max, adj_reason: b.reason.trim(), actor_ref: ACTOR, at });
      const hs = t.alerts.get(a.item_code);
      if (hs?.length && last(hs).status !== "cancelled")
        hs.push({ ...last(hs), version: hs.length + 1, status: "adjusted", message: b.reason.trim(), response: null, proposed_min: null, proposed_max: null, explanation: null, actor_ref: ACTOR, at });
    }
  }
  t.heads.push(nh);
  return json(sheet(role, t.point, nh.day, t), 201);
}

function alertRows(role: Role, from: string, to: string, onlyView: string | null, mineOnly: boolean): S["PriceAlertRow"][] {
  const out: S["PriceAlertRow"][] = [];
  for (const t of TICKETS) {
    const h = last(t.heads);
    if (h.status === "deleted" || h.day < from || h.day > to) continue;
    const p = pointOf(t.point, h.day);
    if (!p) continue;
    const mine = role === "kn_xa" ? mineXa(p) : role === "kn_tinh" && p.owner_ref !== null;
    if ((role === "kn_xa" && !mine) || (mineOnly && !mine && role !== "admin")) continue;
    const items = mockPointItems(t.point, h.day);
    for (const code of t.alerts.keys()) {
      const a = alertDto(t, code)!;
      if (a.status === "cancelled" || (onlyView && a.view !== onlyView)) continue;
      const it = items.find((x) => x.code === code);
      const l = last(t.lines.get(code) ?? [{ min: null, max: null, note: null, adj_min: null, adj_max: null } as LineVer]);
      out.push({
        ...a,
        day: h.day,
        point: pointDto(p, role),
        item_name: it?.name ?? code,
        variety: it?.variety ?? "",
        group_code: it?.group_code ?? "",
        group_name: it?.group_name ?? "",
        unit_name: it?.unit_name ?? "",
        min: l.min,
        max: l.max,
        note: l.note,
        adjusted: l.adj_min ? `${l.adj_min} – ${l.adj_max}` : null,
        ticket_version: h.version,
        ticket_status: h.status,
        mine,
        can_act: role === "kn_xa" ? a.view === "requested" : handles(role),
      });
    }
  }
  return out.sort((a, b) => (a.day === b.day ? (a.point.code + a.item_code < b.point.code + b.item_code ? -1 : 1) : a.day < b.day ? 1 : -1));
}

const FAKE_PDF = "%PDF-1.4\n% gia lap\n";

export const priceTicketRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (role === "kn_tw") return problem(403, "forbidden", "Cấp Quốc gia không xem phiếu báo giá");
      const day = dayQ(ctx, "day", vnToday());
      const mineOnly = ctx.url.searchParams.get("mine") === "true";
      const rows: S["PriceTicketRow"][] = [];
      for (const p of mockPointsAt(day)) {
        if (role === "kn_xa" && !mineXa(p)) continue;
        const mine = role === "kn_xa" ? true : role === "kn_tinh" && p.owner_ref !== null;
        if (mineOnly && role === "kn_tinh" && !mine) continue;
        const t = liveTicket(p.code, day);
        const ls = t ? [...t.lines.values()].map(last) : [];
        const as = t ? [...t.alerts.keys()].map((k) => alertDto(t, k)!).filter((a) => a.status !== "cancelled") : [];
        rows.push({
          point: pointDto(p, role),
          mine,
          view: view(t, day),
          ticket: headDto(t),
          items: mockPointItems(p.code, day).length,
          priced: ls.filter((l) => !l.no_goods && l.min).length,
          no_goods: ls.filter((l) => l.no_goods).length,
          adjusted: ls.filter((l) => l.adj_min).length,
          excluded: as.filter((a) => a.status === "excluded").length,
          high: as.filter((a) => a.direction === "high").length,
          low: as.filter((a) => a.direction === "low").length,
          open_alerts: as.filter((a) => !["adjusted", "kept", "excluded"].includes(a.status)).length,
        });
      }
      return json({ day, quote: quote(day), lock_at: quote(day) ? lockAt(day) : null, respond_by: quote(day) ? respondAt(day) : null, locked: locked(day), board: "open", now: new Date().toISOString(), rows } satisfies S["PriceTicketDay"]);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets\/sheet$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      const day = dayQ(ctx, "day", vnToday());
      let point = ctx.url.searchParams.get("point") ?? "";
      if (!point && role === "kn_xa") point = mockPointsAt(day).find(mineXa)?.code ?? "";
      const s = sheet(role, point, day);
      return s instanceof Response ? s : json(s);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/tickets\/sheet$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      return role instanceof Response ? role : writeEntry(ctx, role, false);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/tickets\/proxy$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      return role instanceof Response ? role : writeEntry(ctx, role, true);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets\/history$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      const to = dayQ(ctx, "to", vnToday());
      const from = dayQ(ctx, "from", addDay(to, -30));
      const today = vnToday();
      let point = ctx.url.searchParams.get("point") ?? "";
      if (!point) point = mockPointsAt(today).find(mineXa)?.code ?? "";
      const rows: S["PriceHistoryRow"][] = [];
      for (let d = to; d >= from; d = addDay(d, -1)) {
        const p = pointOf(point, d);
        if (!p) continue;
        const ts = TICKETS.filter((t) => t.point === point && last(t.heads).day === d);
        const live = ts.find((t) => last(t.heads).status !== "deleted");
        const add = (t: Ticket | undefined) => {
          const ls = t ? [...t.lines.values()].map(last) : [];
          const as = t ? [...t.alerts.values()].map(last).filter((a) => a.status !== "cancelled") : [];
          rows.push({ day: d, view: view(t, d), ticket: headDto(t), point: pointDto(p, role), priced: ls.filter((l) => l.min && !l.no_goods).length, no_goods: ls.filter((l) => l.no_goods).length, adjusted: ls.filter((l) => l.adj_min).length, excluded: as.filter((a) => a.status === "excluded").length, high: as.filter((a) => a.direction === "high").length, low: as.filter((a) => a.direction === "low").length });
        };
        if (live || quote(d)) add(live);
        ts.filter((t) => last(t.heads).status === "deleted").forEach(add);
      }
      return json({ rows });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets\/export$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (role !== "admin" && role !== "kn_tinh") return problem(403, "forbidden", "chỉ Cấp Tỉnh, Admin");
      return new Response(new Uint8Array([0x50, 0x4b, 0x03, 0x04]), {
        headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Content-Disposition": "attachment; filename*=UTF-8''chi-tiet-bao-gia.xlsx" },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets\/print$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      const xlsx = ctx.url.searchParams.get("format") === "xlsx";
      return new Response(xlsx ? new Uint8Array([0x50, 0x4b, 0x03, 0x04]) : FAKE_PDF, {
        headers: { "Content-Type": xlsx ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" : "application/pdf", "Content-Disposition": `attachment; filename*=UTF-8''phieu-bao-gia.${xlsx ? "xlsx" : "pdf"}` },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets\/([^/]+)$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      const t = ticketById(ctx.params[0]);
      if (!t) return problem(404, "price.ticket_not_found", "Không có phiếu báo giá này");
      const s = sheet(role, t.point, last(t.heads).day, t);
      return s instanceof Response ? s : json(s);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/price\/tickets\/([^/]+)\/log$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      const t = ticketById(ctx.params[0]);
      if (!t) return problem(404, "price.ticket_not_found", "Không có phiếu báo giá này");
      return json({
        ticket: t.heads.map((h) => ({ version: h.version, day: h.day, status: h.status, source: h.source, op: h.op, reason: h.reason, submitted_at: h.submitted_at, actor_ref: h.actor_ref, actor_name: null, at: h.at })),
        lines: [...t.lines.entries()].flatMap(([code, vs]) => vs.map((l) => ({ item_code: code, version: l.version, min: l.min, max: l.max, no_goods: l.no_goods, note: l.note, adj_min: l.adj_min, adj_max: l.adj_max, adj_reason: l.adj_reason, actor_ref: l.actor_ref, actor_name: null, at: l.at }))),
        alerts: [...t.alerts.entries()].flatMap(([code, hs]) => hs.map((h) => ({ item_code: code, entry: h, actor_name: null }))),
      } satisfies S["PriceTicketLog"]);
    },
  },
  ...(["correct", "redate", "delete"] as const).map(
    (op): Route => ({
      method: "POST",
      pattern: new RegExp(`^/v1/price/tickets/([^/]+)/${op}$`),
      handler: (ctx) => {
        const role = roleOf(ctx);
        return role instanceof Response ? role : opTicket(ctx, role, op);
      },
    }),
  ),
  {
    method: "GET",
    pattern: /^\/v1\/price\/alerts$/,
    handler: (ctx) => {
      seedOnce();
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (role === "kn_tw") return problem(403, "forbidden", "Cấp Quốc gia không xem phiếu báo giá");
      const from = dayQ(ctx, "from", vnToday());
      const to = dayQ(ctx, "to", from);
      return json({ alerts: alertRows(role, from, to, ctx.url.searchParams.get("view"), ctx.url.searchParams.get("mine") === "true") });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/alerts\/request$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (role !== "admin" && role !== "kn_tinh") return problem(403, "forbidden", "chỉ Cấp Tỉnh hoặc vai quản trị");
      const b = ctx.body as S["PriceVerificationRequest"];
      if (!(b.message ?? "").trim()) return problem(422, "price.reason_required", "nội dung yêu cầu xác minh");
      const at = new Date().toISOString();
      for (const k of b.alerts) {
        const t = ticketById(k.ticket_id);
        const hs = t?.alerts.get(k.item_code);
        if (!t || !hs?.length) return problem(404, "price.alert_not_found", k.item_code);
        const v = alertView(last(hs), last(t.heads).day);
        if (!["provisional", "pending", "responded"].includes(v)) return problem(409, "price.alert_state", v);
        const day = last(t.heads).day;
        const rb = Date.now() < Date.parse(respondAt(day)) ? respondAt(day) : new Date(Date.now() + 90 * 60_000).toISOString();
        hs.push({ ...last(hs), version: hs.length + 1, status: "requested", message: b.message.trim(), respond_by: rb, response: null, proposed_min: null, proposed_max: null, explanation: null, actor_ref: ACTOR, at });
      }
      return json({ written: b.alerts.length }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/alerts\/respond$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (role !== "kn_xa") return problem(403, "forbidden", "chỉ cán bộ Cấp Xã phản hồi");
      const b = ctx.body as S["PriceVerificationResponse"];
      if (!(b.explanation ?? "").trim()) return problem(422, "price.reason_required", "giải trình");
      const t = ticketById(b.ticket_id);
      const hs = t?.alerts.get(b.item_code);
      if (!t || !hs?.length) return problem(404, "price.alert_not_found", b.item_code);
      const cur = last(hs);
      if (cur.status !== "requested") return problem(409, "price.alert_state", cur.status);
      if (cur.respond_by && Date.now() >= Date.parse(cur.respond_by)) return problem(409, "price.respond_overdue", "Quá hạn phản hồi xác minh");
      if (b.choice === "propose" && (!b.min || !b.max || D(b.min).sign() <= 0 || D(b.min).gt(D(b.max)))) return problem(422, "price.price_invalid", "giá đề xuất");
      hs.push({ ...cur, version: hs.length + 1, status: "responded", response: b.choice, proposed_min: b.choice === "propose" ? b.min! : null, proposed_max: b.choice === "propose" ? b.max! : null, explanation: b.explanation.trim(), actor_ref: MOCK_XA_OFFICER, at: new Date().toISOString() });
      return json({ written: 1 }, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/price\/alerts\/decide$/,
    handler: (ctx) => {
      const role = roleOf(ctx);
      if (role instanceof Response) return role;
      if (role !== "admin" && role !== "kn_tinh") return problem(403, "forbidden", "chỉ Cấp Tỉnh hoặc vai quản trị");
      const b = ctx.body as S["PriceAlertDecision"];
      if (!(b.note ?? "").trim()) return problem(422, "price.reason_required", "ghi chú / lý do");
      const t = ticketById(b.ticket_id);
      const hs = t?.alerts.get(b.item_code);
      if (!t || !hs?.length) return problem(404, "price.alert_not_found", b.item_code);
      if (!locked(last(t.heads).day)) return problem(409, "price.ticket_not_locked", last(t.heads).day);
      const v = alertView(last(hs), last(t.heads).day);
      if (b.decision === "reopen" || !["pending", "responded", "overdue"].includes(v)) return problem(409, "price.alert_state", v);
      hs.push({ ...last(hs), version: hs.length + 1, status: b.decision === "keep" ? "kept" : "excluded", message: b.note.trim(), response: null, proposed_min: null, proposed_max: null, explanation: null, actor_ref: ACTOR, at: new Date().toISOString() });
      return json({ written: 1 }, 201);
    },
  },
];
