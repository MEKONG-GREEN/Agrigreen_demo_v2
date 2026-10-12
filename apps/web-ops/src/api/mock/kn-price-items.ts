// ============================================================================
// BACKEND GIẢ LẬP — Mặt hàng theo thời kỳ (W11.73, cap/price, BRD Báo giá nông sản v1.3 FN-AD-04.03, FN-XA-03.01).
// Đường dẫn, tham số, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/price/items*, groups*,
// assignments*, point-items, item-sets*, proposals*). Quy tắc mô phỏng (rút gọn từ internal/platform/price):
//   - Mỗi thay đổi là một phiên bản mới có valid_from (vắng → ngày mai = ngày báo giá kế tiếp); ngày đã qua → 422
//     price.item_date_invalid. Mã mặt hàng MH##### tự sinh, không tái sử dụng.
//   - Cơ bản áp dụng mọi xã; Địa phương gán theo xã; bộ theo vụ tắt → mặt hàng của bộ không có ở xã.
//   - Vai: Cấp Tỉnh + Admin ghi; Cấp Xã đề xuất (chỉ thấy đề xuất của mình); Cấp Quốc gia chỉ xem.
//   - Mặt hàng "đã có giá" mô phỏng bằng cờ priced (MH00001, MH00002) — sửa ĐVT/quy cách/nhóm → 409 price.item_locked.
// Dữ liệu là GIẢ LẬP. Trạng thái nằm trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Item = S["PriceItem"];
type Version = S["PriceItemVersion"];

interface Ver<T> {
  seq: number;
  from: string;
  status: "active" | "retired";
  v: T;
}
interface ItemBody {
  name: string;
  variety: string;
  group_code: string;
  unit_code: string;
  spec: string | null;
  sale_place: string | null;
  kind: "basic" | "local";
  sort_order: number;
  replaces_code: string | null;
  note: string | null;
  proposal_id: string | null;
  at: string;
  actor: string;
}
interface Group {
  code: string;
  name: string;
  sort_order: number;
  from: string;
  status: "active" | "retired";
  seq: number;
}
interface SetBody {
  name: string;
  season: string;
  item_codes: string[];
  enabled: boolean;
}

interface Store {
  items: Map<string, Ver<ItemBody>[]>;
  groups: Map<string, Group[]>;
  assigns: Map<string, Ver<{ point: string; item: string }>[]>;
  sets: Map<string, Ver<SetBody>[]>;
  proposals: S["PriceProposal"][];
  priced: Set<string>;
  next: number;
}

const pad = (n: number) => String(n).padStart(2, "0");
/** Hôm nay theo giờ Việt Nam (UTC+7). */
export function vnToday(now = Date.now()): string {
  const d = new Date(now + 7 * 3_600_000);
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}
export function addDay(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}
const isDay = (s: unknown): s is string =>
  typeof s === "string" && /^\d{4}-\d{2}-\d{2}$/.test(s);

const SEED_ACTOR = "0190c0de-0000-7000-8000-000000000001";
let clock: () => number = () => Date.now();
let STORE: Store = seed();

function seed(): Store {
  const s: Store = {
    items: new Map(),
    groups: new Map(),
    assigns: new Map(),
    sets: new Map(),
    proposals: [],
    priced: new Set(["MH00001", "MH00002"]),
    next: 1,
  };
  const from = addDay(vnToday(clock()), -30);
  [
    ["lua_gao", "Lúa gạo"],
    ["rau_mau", "Rau màu"],
    ["chan_nuoi", "Chăn nuôi"],
    ["thuy_hai_san", "Thủy hải sản"],
    ["vat_tu_nong_nghiep", "Vật tư nông nghiệp"],
  ].forEach(([code = "", name = ""], i) =>
    s.groups.set(code, [
      { code, name, sort_order: (i + 1) * 10, from, status: "active", seq: 1 },
    ]),
  );
  const add = (
    name: string,
    variety: string,
    group: string,
    kind: "basic" | "local",
    spec: string | null = null,
  ) => {
    const code = `MH${String(s.next++).padStart(5, "0")}`;
    const sort = (s.items.size + 1) * 10;
    s.items.set(code, [
      {
        seq: 1,
        from,
        status: "active",
        v: {
          name,
          variety,
          group_code: group,
          unit_code: "kg",
          spec,
          sale_place: null,
          kind,
          sort_order: sort,
          replaces_code: null,
          note: null,
          proposal_id: null,
          at: new Date(clock()).toISOString(),
          actor: SEED_ACTOR,
        },
      },
    ]);
    return code;
  };
  add("Lúa", "OM 18 (tươi)", "lua_gao", "basic");
  add("Lúa", "Jasmine 85 (tươi)", "lua_gao", "basic");
  add("Gạo", "thường", "lua_gao", "basic");
  add("Dưa leo", "loại 1", "rau_mau", "basic");
  const xoai = add("Xoài", "cát Hòa Lộc loại 1", "rau_mau", "local");
  add("Heo hơi", "loại 1", "chan_nuoi", "basic");
  add("Cá tra", "thịt", "thuy_hai_san", "basic");
  add("Phân bón", "Urê Phú Mỹ", "vat_tu_nong_nghiep", "basic", "bao 50 kg");
  s.assigns.set(`91301:${xoai}`, [
    { seq: 1, from, status: "active", v: { point: "91301", item: xoai } },
  ]);
  s.proposals.push({
    id: uuidv7(),
    version: 1,
    point_code: "91302",
    name: "Sầu riêng",
    variety: "Ri6",
    group_code: "rau_mau",
    unit_code: "kg",
    spec: null,
    reason: "Vùng trồng mới, chợ xã có bán hằng ngày",
    status: "pending",
    item_code: null,
    decision_note: null,
    proposer_ref: SEED_ACTOR,
    created_at: new Date(clock()).toISOString(),
    decided_at: null,
  });
  return s;
}

/** Đặt lại dữ liệu giả lập (kiểm thử). */
export function resetPriceItems(now?: () => number): void {
  if (now) clock = now;
  STORE = seed();
}

// ---- phép chiếu hiệu lực ----
function effective<T>(
  vs: Ver<T>[] | undefined,
  at: string,
): Ver<T> | undefined {
  let best: Ver<T> | undefined;
  for (const v of vs ?? [])
    if (
      v.from <= at &&
      (!best ||
        v.from > best.from ||
        (v.from === best.from && v.seq > best.seq))
    )
      best = v;
  return best;
}
function upcoming<T>(vs: Ver<T>[] | undefined, at: string): Ver<T> | undefined {
  let best: Ver<T> | undefined;
  for (const v of vs ?? [])
    if (
      v.from > at &&
      (!best ||
        v.from < best.from ||
        (v.from === best.from && v.seq > best.seq))
    )
      best = v;
  return best;
}
function groupEff(code: string, at: string): Group | undefined {
  const vs = STORE.groups.get(code) ?? [];
  let best: Group | undefined;
  for (const g of vs)
    if (
      g.from <= at &&
      (!best ||
        g.from > best.from ||
        (g.from === best.from && g.seq > best.seq))
    )
      best = g;
  return best?.status === "active" ? best : undefined;
}
function gated(at: string): Set<string> {
  const member = new Set<string>();
  const on = new Set<string>();
  for (const vs of STORE.sets.values()) {
    const s = effective(vs, at);
    if (!s || s.status !== "active") continue;
    for (const c of s.v.item_codes) {
      member.add(c);
      if (s.v.enabled) on.add(c);
    }
  }
  return new Set([...member].filter((c) => !on.has(c)));
}
function assignedCount(code: string, at: string): number {
  let n = 0;
  for (const vs of STORE.assigns.values()) {
    const a = effective(vs, at);
    if (a && a.status === "active" && a.v.item === code) n++;
  }
  return n;
}

function view(code: string, at: string): Item | null {
  const vs = STORE.items.get(code) ?? [];
  const eff = effective(vs, at);
  const up = upcoming(vs, at);
  const shown = eff ?? up;
  if (!shown) return null;
  let state: Item["state"] = eff
    ? eff.status === "active"
      ? "active"
      : "retired"
    : "pending";
  let validTo: string | null = null;
  if (state === "retired" && eff) validTo = addDay(eff.from, -1);
  else {
    const r = vs
      .filter((v) => v.status === "retired" && v.from > at)
      .sort((a, b) => a.from.localeCompare(b.from))[0];
    if (r) validTo = addDay(r.from, -1);
  }
  if (!eff) state = "pending";
  const since =
    vs
      .filter((v) => v.status === "active")
      .map((v) => v.from)
      .sort()[0] ?? null;
  const b = shown.v;
  const priced = STORE.priced.has(code);
  return {
    code,
    name: b.name,
    variety: b.variety,
    group_code: b.group_code,
    unit_code: b.unit_code,
    spec: b.spec,
    sale_place: b.sale_place,
    kind: b.kind,
    sort_order: b.sort_order,
    version: shown.seq,
    valid_from: shown.from,
    valid_to: validTo,
    since,
    state,
    replaces_code: b.replaces_code,
    note: b.note,
    point_count: b.kind === "basic" ? null : assignedCount(code, at),
    has_price: priced,
    new_price: !!b.replaces_code && !priced,
    gated: gated(at).has(code),
    proposal_id: b.proposal_id,
  };
}

function sortItems(xs: Item[], at: string): Item[] {
  const rank = (g: string) => groupEff(g, at)?.sort_order ?? 1 << 30;
  return xs.sort(
    (a, b) =>
      rank(a.group_code) - rank(b.group_code) ||
      a.group_code.localeCompare(b.group_code) ||
      a.sort_order - b.sort_order ||
      a.code.localeCompare(b.code),
  );
}

// ---- người gọi ----
type Role = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";
interface Caller {
  role: Role;
  actor: string;
}
function callerOf(ctx: Ctx): Caller | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const actor = `0190c0de-0000-7000-8000-${ctx.persona.id
    .replace(/[^0-9a-f]/gi, "")
    .padEnd(12, "0")
    .slice(0, 12)}`;
  if (kc) return { role: kc.role, actor };
  const r = ctx.persona.me.roles;
  if (r.includes("platform_admin")) return { role: "admin", actor };
  if (r.includes("kn_province_admin")) return { role: "kn_tinh", actor };
  if (r.includes("kn_officer")) return { role: "kn_xa", actor };
  return problem(
    403,
    "forbidden",
    "vai của người gọi không thuộc cấu hình giá kn",
  );
}
const canEdit = (c: Caller) => c.role === "kn_tinh" || c.role === "admin";
const forbidden = () =>
  problem(403, "forbidden", "Không có quyền thao tác này");
const written = (code: string, version: number, from: string, n = 1) =>
  json({ code, version, valid_from: from, written: n }, 201);

function fromDay(raw: unknown, today: string): string | Response {
  if (raw === undefined || raw === null || raw === "") return addDay(today, 1);
  if (!isDay(raw))
    return problem(
      422,
      "validation_failed",
      "valid_from phải là ngày YYYY-MM-DD",
    );
  if (raw < today)
    return problem(
      422,
      "price.item_date_invalid",
      "Ngày hiệu lực không hợp lệ — thay đổi danh mục áp dụng từ ngày báo giá kế tiếp",
    );
  return raw;
}
function atOf(url: URL): string {
  const at = url.searchParams.get("at");
  return isDay(at) ? at : vnToday(clock());
}
const norm = (s: string | null | undefined) =>
  (s ?? "").toLowerCase().split(/\s+/).filter(Boolean).join(" ");
const ident = (b: {
  group_code: string;
  name: string;
  variety: string;
  spec: string | null;
}) => `${b.group_code}|${norm(b.name)}|${norm(b.variety)}|${norm(b.spec)}`;

type ItemIn = S["PriceItemInput"];
function validItem(b: ItemIn): Response | null {
  if (!b.name?.trim())
    return problem(422, "validation_failed", "Nhập tên mặt hàng");
  if (!b.variety?.trim())
    return problem(
      422,
      "validation_failed",
      "Nhập giống hoặc phẩm cấp (mỗi mặt hàng = loại + giống/phẩm cấp + đơn vị tính)",
    );
  if (!b.group_code || !b.unit_code)
    return problem(422, "validation_failed", "Chọn nhóm hàng và đơn vị tính");
  if (b.kind !== "basic" && b.kind !== "local")
    return problem(422, "validation_failed", "Chọn loại Cơ bản / Địa phương");
  return null;
}

function createItem(
  c: Caller,
  b: ItemIn,
  proposal: string | null,
): Response | { code: string; from: string; n: number } {
  const today = vnToday(clock());
  const from = fromDay(b.valid_from, today);
  if (from instanceof Response) return from;
  const bad = validItem(b);
  if (bad) return bad;
  if (!groupEff(b.group_code, from))
    return problem(
      422,
      "validation_failed",
      `nhóm hàng ${b.group_code} không có hiệu lực`,
    );
  const key = ident({
    group_code: b.group_code,
    name: b.name,
    variety: b.variety,
    spec: b.spec || null,
  });
  for (const code of STORE.items.keys()) {
    const v = view(code, from);
    if (
      v &&
      v.state !== "retired" &&
      code !== b.replaces_code &&
      ident(v) === key
    )
      return problem(409, "price.item_duplicate", `trùng mặt hàng ${code}`);
  }
  if (b.kind === "basic" && b.points?.length)
    return problem(
      422,
      "price.item_basic_not_assignable",
      "Mặt hàng Cơ bản áp dụng mọi xã",
    );
  const code = `MH${String(STORE.next++).padStart(5, "0")}`;
  const sort =
    Math.max(
      0,
      ...[...STORE.items.values()].flatMap((vs) =>
        vs
          .filter((v) => v.v.group_code === b.group_code)
          .map((v) => v.v.sort_order),
      ),
    ) + 10;
  const at = new Date(clock()).toISOString();
  const body: ItemBody = {
    name: b.name.trim(),
    variety: b.variety.trim(),
    group_code: b.group_code,
    unit_code: b.unit_code,
    spec: b.spec?.trim() || null,
    sale_place: b.sale_place?.trim() || null,
    kind: b.kind ?? "local",
    sort_order: sort,
    replaces_code: b.replaces_code || null,
    note: null,
    proposal_id: proposal,
    at,
    actor: c.actor,
  };
  let points = [...(b.points ?? [])];
  if (b.replaces_code) {
    const old = STORE.items.get(b.replaces_code);
    if (!old)
      return problem(
        404,
        "price.item_not_found",
        "Không có mặt hàng được thay thế",
      );
    const eff = effective(old, from);
    if (eff?.status === "active")
      old.push({
        seq: old.length + 1,
        from,
        status: "retired",
        v: { ...eff.v, note: `Thay thế bởi ${code}`, at, actor: c.actor },
      });
    if (body.kind === "local" && !points.length)
      for (const vs of STORE.assigns.values()) {
        const a = effective(vs, from);
        if (a?.status === "active" && a.v.item === b.replaces_code)
          points.push(a.v.point);
      }
  }
  const vs: Ver<ItemBody>[] = [{ seq: 1, from, status: "active", v: body }];
  if (isDay(b.valid_to))
    vs.push({
      seq: 2,
      from: addDay(b.valid_to, 1),
      status: "retired",
      v: { ...body, note: "Hết hiệu lực theo kế hoạch" },
    });
  STORE.items.set(code, vs);
  points = [...new Set(points)];
  for (const p of points)
    STORE.assigns.set(`${p}:${code}`, [
      { seq: 1, from, status: "active", v: { point: p, item: code } },
    ]);
  return { code, from, n: vs.length + points.length };
}

const UNITS: [string, string][] = [
  ["kg", "kg"],
  ["ta", "tạ"],
  ["tan", "tấn"],
  ["lit", "lít"],
  ["con", "con"],
  ["trai", "trái"],
  ["bao", "bao"],
  ["chai", "chai"],
];

function itemRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/catalog$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const at = atOf(ctx.url);
        const from = addDay(vnToday(clock()), -30);
        const groups = [...STORE.groups.keys()]
          .map((k) => groupEff(k, at))
          .filter((g): g is Group => !!g)
          .sort((a, b) => a.sort_order - b.sort_order)
          .map((g) => ({
            code: g.code,
            name: g.name,
            sort_order: g.sort_order,
            version: g.seq,
            valid_from: g.from,
          }));
        const units = UNITS.map(([code, name], i) => ({
          code,
          name,
          sort_order: (i + 1) * 10,
          version: 1,
          valid_from: from,
        }));
        // W11.72: nhóm vùng mặc định (An Giang, Kiên Giang).
        const regions = (
          [
            ["an_giang", "An Giang"],
            ["kien_giang", "Kiên Giang"],
          ] as const
        ).map(([code, name], i) => ({
          code,
          name,
          sort_order: (i + 1) * 10,
          version: 1,
          valid_from: from,
        }));
        return json({
          day: at,
          units,
          groups,
          regions,
        } satisfies S["PriceCatalog"]);
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/items$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const at = atOf(ctx.url);
        const q = ctx.url.searchParams;
        const s = norm(q.get("q"));
        const items = sortItems(
          [...STORE.items.keys()]
            .map((k) => view(k, at))
            .filter((v): v is Item => !!v)
            .filter(
              (v) =>
                (!q.get("group") || v.group_code === q.get("group")) &&
                (!q.get("kind") || v.kind === q.get("kind")) &&
                (!q.get("state") || v.state === q.get("state")),
            )
            .filter(
              (v) => !s || norm(`${v.code} ${v.name} ${v.variety}`).includes(s),
            ),
          at,
        );
        const pending = STORE.proposals.filter(
          (p) =>
            p.status === "pending" &&
            (c.role !== "kn_xa" || p.proposer_ref === c.actor),
        ).length;
        return json({
          day: at,
          items,
          pending_proposals: pending,
          can_edit: canEdit(c),
          can_steward: canEdit(c),
        } satisfies S["PriceItemList"]);
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/items$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        if (!canEdit(c)) return forbidden();
        const r = createItem(c, ctx.body as ItemIn, null);
        return r instanceof Response ? r : written(r.code, 1, r.from, r.n);
      },
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/items\/(MH\d{5})$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const code = ctx.params[0] ?? "";
        const v = view(code, atOf(ctx.url));
        if (!v)
          return problem(404, "price.item_not_found", "Không có mặt hàng này");
        const history: Version[] = (STORE.items.get(code) ?? []).map((x) => ({
          version: x.seq,
          status: x.status,
          valid_from: x.from,
          name: x.v.name,
          variety: x.v.variety,
          group_code: x.v.group_code,
          unit_code: x.v.unit_code,
          spec: x.v.spec,
          sale_place: x.v.sale_place,
          kind: x.v.kind,
          sort_order: x.v.sort_order,
          note: x.v.note,
          event_time: x.v.at,
          actor_ref: x.v.actor,
          proposal_id: x.v.proposal_id,
        }));
        const replacedBy = [...STORE.items.entries()]
          .filter(([, vs]) => vs.some((x) => x.v.replaces_code === code))
          .map(([k]) => k);
        return json({
          ...v,
          history,
          replaced_by: replacedBy,
        } satisfies S["PriceItemDetail"]);
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/items\/(MH\d{5})\/(revise|retire|move)$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        if (!canEdit(c)) return forbidden();
        const [code = "", op = ""] = ctx.params;
        const vs = STORE.items.get(code);
        if (!vs)
          return problem(404, "price.item_not_found", "Không có mặt hàng này");
        const today = vnToday(clock());
        const at = new Date(clock()).toISOString();
        const b = (ctx.body ?? {}) as Record<string, unknown>;
        if (op === "move") {
          const from = addDay(today, 1);
          const cur = view(code, from);
          if (!cur || cur.state !== "active")
            return problem(
              409,
              "price.item_retired",
              "Mặt hàng đã ngưng tại ngày hiệu lực",
            );
          const peers = sortItems(
            [...STORE.items.keys()]
              .map((k) => view(k, from))
              .filter(
                (v): v is Item =>
                  !!v &&
                  v.state === "active" &&
                  v.group_code === cur.group_code,
              ),
            from,
          );
          const i = peers.findIndex((p) => p.code === code);
          const j = b.direction === "down" ? i + 1 : i - 1;
          if (j < 0 || j >= peers.length) return written(code, 0, from, 0);
          [peers[i], peers[j]] = [peers[j]!, peers[i]!];
          let n = 0;
          peers.forEach((p, k) => {
            if (p.sort_order === (k + 1) * 10) return;
            const pv = STORE.items.get(p.code)!;
            const e = effective(pv, from)!;
            pv.push({
              seq: pv.length + 1,
              from,
              status: "active",
              v: { ...e.v, sort_order: (k + 1) * 10, at, actor: c.actor },
            });
            n++;
          });
          return written(code, vs.length, from, n);
        }
        if (op === "retire") {
          if (!String(b.reason ?? "").trim())
            return problem(422, "validation_failed", "Nhập lý do ngưng");
          const from = fromDay(
            isDay(b.valid_to) ? addDay(b.valid_to, 1) : "",
            today,
          );
          if (from instanceof Response) return from;
          const e = effective(vs, from);
          if (!e || e.status !== "active")
            return problem(
              409,
              "price.item_retired",
              "Mặt hàng đã ngưng tại ngày hiệu lực",
            );
          vs.push({
            seq: vs.length + 1,
            from,
            status: "retired",
            v: { ...e.v, note: String(b.reason).trim(), at, actor: c.actor },
          });
          return written(code, vs.length, from);
        }
        const bi = b as ItemIn;
        const from = fromDay(bi.valid_from, today);
        if (from instanceof Response) return from;
        const bad = validItem(bi);
        if (bad) return bad;
        const e = effective(vs, from);
        if (!e)
          return problem(
            422,
            "price.item_date_invalid",
            "Mặt hàng chưa có hiệu lực tại ngày này",
          );
        if (e.status !== "active")
          return problem(
            409,
            "price.item_retired",
            "Mặt hàng đã ngưng tại ngày hiệu lực",
          );
        if (
          STORE.priced.has(code) &&
          (bi.variety.trim() !== e.v.variety ||
            bi.group_code !== e.v.group_code ||
            bi.unit_code !== e.v.unit_code ||
            (bi.spec?.trim() || null) !== e.v.spec ||
            bi.kind !== e.v.kind)
        )
          return problem(
            409,
            "price.item_locked",
            "Mặt hàng đã có giá: không sửa đơn vị tính, quy cách, nhóm hàng, loại — tạo mặt hàng thay thế",
          );
        vs.push({
          seq: vs.length + 1,
          from,
          status: "active",
          v: {
            ...e.v,
            name: bi.name.trim(),
            variety: bi.variety.trim(),
            group_code: bi.group_code,
            unit_code: bi.unit_code,
            spec: bi.spec?.trim() || null,
            sale_place: bi.sale_place?.trim() || null,
            kind: bi.kind ?? e.v.kind,
            at,
            actor: c.actor,
          },
        });
        return written(code, vs.length, from);
      },
    },
  ];
}

function groupRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/groups$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const at = atOf(ctx.url);
        const groups = [...STORE.groups.keys()]
          .map((k) => groupEff(k, at))
          .filter((g): g is Group => !!g)
          .sort((a, b) => a.sort_order - b.sort_order)
          .map((g) => ({
            code: g.code,
            name: g.name,
            sort_order: g.sort_order,
            version: g.seq,
            valid_from: g.from,
            items: [...STORE.items.keys()]
              .map((k) => view(k, at))
              .filter((v) => v?.state === "active" && v.group_code === g.code)
              .length,
          }));
        return json({ groups });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/groups$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        if (!canEdit(c)) return forbidden();
        const b = ctx.body as S["PriceGroupInput"];
        if (!b?.name?.trim())
          return problem(422, "validation_failed", "Nhập tên nhóm hàng");
        let code = b.code;
        if (!code)
          for (
            let n = STORE.groups.size + 1;
            STORE.groups.has((code = `nhom_${n}`));
            n++
          );
        else if (STORE.groups.has(code))
          return problem(409, "conflict", "Mã nhóm hàng đã có");
        const today = vnToday(clock());
        const max = Math.max(
          0,
          ...[...STORE.groups.values()].flat().map((g) => g.sort_order),
        );
        STORE.groups.set(code!, [
          {
            code: code!,
            name: b.name.trim(),
            sort_order: b.sort_order || max + 10,
            from: today,
            status: "active",
            seq: 1,
          },
        ]);
        return written(code!, 1, today);
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/groups\/([a-z][a-z0-9_]*)\/(revise|retire)$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        if (!canEdit(c)) return forbidden();
        const [code = "", op = ""] = ctx.params;
        const today = vnToday(clock());
        const cur = groupEff(code, today);
        if (!cur)
          return problem(
            404,
            "price.group_not_found",
            "Không có nhóm hàng này",
          );
        const vs = STORE.groups.get(code)!;
        if (op === "retire") {
          const busy = [...STORE.items.keys()].some((k) => {
            const all = STORE.items.get(k)!;
            const last = [...all]
              .sort((a, b) => a.from.localeCompare(b.from) || a.seq - b.seq)
              .at(-1)!;
            return last.status === "active" && last.v.group_code === code;
          });
          if (busy)
            return problem(
              409,
              "price.group_not_empty",
              "Nhóm hàng còn mặt hàng đang hiệu lực — không xóa được",
            );
          vs.push({
            ...cur,
            from: today,
            status: "retired",
            seq: vs.length + 1,
          });
        } else {
          const b = ctx.body as S["PriceGroupInput"];
          if (!b?.name?.trim())
            return problem(422, "validation_failed", "Nhập tên nhóm hàng");
          vs.push({
            ...cur,
            name: b.name.trim(),
            sort_order: b.sort_order || cur.sort_order,
            from: today,
            seq: vs.length + 1,
          });
        }
        return written(code, vs.length, today);
      },
    },
  ];
}

function assignRoutes(): Route[] {
  const change = (ctx: Ctx, status: "active" | "retired"): Response => {
    const c = callerOf(ctx);
    if (c instanceof Response) return c;
    if (!canEdit(c)) return forbidden();
    const b = ctx.body as S["PriceAssignInput"];
    if (
      !b?.point_code ||
      !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(b.point_code)
    )
      return problem(422, "validation_failed", "Mã xã báo giá không hợp lệ");
    if (!b.item_codes?.length)
      return problem(422, "validation_failed", "Chọn mặt hàng");
    const from = fromDay(b.valid_from, vnToday(clock()));
    if (from instanceof Response) return from;
    let n = 0;
    for (const code of b.item_codes) {
      const v = view(code, from);
      if (!v)
        return problem(
          404,
          "price.item_not_found",
          `Không có mặt hàng ${code}`,
        );
      if (v.kind === "basic")
        return problem(
          422,
          "price.item_basic_not_assignable",
          "Mặt hàng Cơ bản áp dụng mọi xã, không gán hay bỏ gán riêng từng xã",
        );
      if (status === "active" && v.state !== "active")
        return problem(
          409,
          "price.item_retired",
          `Mặt hàng ${code} không có hiệu lực`,
        );
      const key = `${b.point_code}:${code}`;
      const vs = STORE.assigns.get(key) ?? [];
      const cur = effective(vs, from);
      if ((cur && cur.status === status) || (!cur && status === "retired"))
        continue;
      vs.push({
        seq: vs.length + 1,
        from,
        status,
        v: { point: b.point_code, item: code },
      });
      STORE.assigns.set(key, vs);
      n++;
    }
    return written(b.point_code, 0, from, n);
  };
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/assignments$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const at = atOf(ctx.url);
        const point = ctx.url.searchParams.get("point");
        const assignments = [...STORE.assigns.values()]
          .map((vs) => effective(vs, at))
          .filter(
            (a): a is Ver<{ point: string; item: string }> =>
              !!a && a.status === "active" && (!point || a.v.point === point),
          )
          .map((a) => ({
            point_code: a.v.point,
            item_code: a.v.item,
            version: a.seq,
            valid_from: a.from,
          }))
          .sort(
            (x, y) =>
              x.point_code.localeCompare(y.point_code) ||
              x.item_code.localeCompare(y.item_code),
          );
        return json({ assignments });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/assignments$/,
      handler: (ctx) => change(ctx, "active"),
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/assignments\/remove$/,
      handler: (ctx) => change(ctx, "retired"),
    },
    {
      method: "GET",
      pattern: /^\/v1\/price\/point-items$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const at = atOf(ctx.url);
        const point = ctx.url.searchParams.get("point") ?? "";
        if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(point))
          return problem(
            422,
            "validation_failed",
            "Mã xã báo giá không hợp lệ",
          );
        const gate = gated(at);
        const items = sortItems(
          [...STORE.items.keys()]
            .map((k) => view(k, at))
            .filter(
              (v): v is Item =>
                !!v && v.state === "active" && !gate.has(v.code),
            )
            .filter(
              (v) =>
                v.kind === "basic" ||
                effective(STORE.assigns.get(`${point}:${v.code}`), at)
                  ?.status === "active",
            ),
          at,
        );
        return json({ day: at, point_code: point, items });
      },
    },
  ];
}

function setRoutes(): Route[] {
  const save = (ctx: Ctx, code: string, create: boolean): Response => {
    const c = callerOf(ctx);
    if (c instanceof Response) return c;
    if (!canEdit(c)) return forbidden();
    const b = ctx.body as S["PriceItemSetInput"];
    if (!/^[a-z][a-z0-9_]{0,39}$/.test(code))
      return problem(
        422,
        "validation_failed",
        "Mã bộ: chữ thường, số, gạch dưới",
      );
    if (!b?.name?.trim() || !b.season?.trim() || !b.item_codes?.length)
      return problem(
        422,
        "validation_failed",
        "Nhập tên bộ, vụ và chọn mặt hàng",
      );
    if (create && STORE.sets.has(code))
      return problem(409, "price.item_set_exists", "Mã bộ mặt hàng đã có");
    if (!create && !STORE.sets.has(code))
      return problem(
        404,
        "price.item_set_not_found",
        "Không có bộ mặt hàng này",
      );
    for (const ic of b.item_codes)
      if (!STORE.items.has(ic))
        return problem(404, "price.item_not_found", `Không có mặt hàng ${ic}`);
    const from = fromDay(b.valid_from, vnToday(clock()));
    if (from instanceof Response) return from;
    const vs = STORE.sets.get(code) ?? [];
    vs.push({
      seq: vs.length + 1,
      from,
      status: b.retire ? "retired" : "active",
      v: {
        name: b.name.trim(),
        season: b.season.trim(),
        item_codes: [...new Set(b.item_codes)].sort(),
        enabled: b.enabled,
      },
    });
    STORE.sets.set(code, vs);
    return written(code, vs.length, from);
  };
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/item-sets$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const at = atOf(ctx.url);
        const sets: S["PriceItemSet"][] = [];
        for (const [code, vs] of STORE.sets) {
          const e = effective(vs, at);
          const s = e ?? upcoming(vs, at);
          if (!s) continue;
          sets.push({
            code,
            ...s.v,
            version: s.seq,
            valid_from: s.from,
            state: !e
              ? "pending"
              : e.status === "active"
                ? "active"
                : "retired",
          });
        }
        return json({
          sets: sets.sort((a, b) => a.code.localeCompare(b.code)),
        });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/item-sets$/,
      handler: (ctx) =>
        save(ctx, String((ctx.body as { code?: string })?.code ?? ""), true),
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/item-sets\/([a-z][a-z0-9_]*)\/revise$/,
      handler: (ctx) => save(ctx, ctx.params[0] ?? "", false),
    },
  ];
}

function proposalRoutes(): Route[] {
  return [
    {
      method: "GET",
      pattern: /^\/v1\/price\/proposals$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        const st = ctx.url.searchParams.get("status");
        const proposals = STORE.proposals
          .filter(
            (p) =>
              (!st || p.status === st) &&
              (c.role !== "kn_xa" || p.proposer_ref === c.actor),
          )
          .sort((a, b) => b.created_at.localeCompare(a.created_at));
        return json({ proposals });
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/proposals$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        if (c.role !== "kn_xa" && c.role !== "kn_tinh")
          return problem(
            403,
            "forbidden",
            "chỉ Cấp Xã, Cấp Tỉnh đề xuất mặt hàng",
          );
        const b = ctx.body as S["PriceProposalInput"];
        if (
          !b?.point_code ||
          !b.name?.trim() ||
          !b.variety?.trim() ||
          !b.group_code ||
          !b.unit_code ||
          !b.reason?.trim()
        )
          return problem(
            422,
            "validation_failed",
            "Nhập đủ xã, tên, giống/phẩm cấp, nhóm hàng, đơn vị tính, lý do",
          );
        const id = uuidv7();
        STORE.proposals.push({
          id,
          version: 1,
          point_code: b.point_code,
          name: b.name.trim(),
          variety: b.variety.trim(),
          group_code: b.group_code,
          unit_code: b.unit_code,
          spec: b.spec?.trim() || null,
          reason: b.reason.trim(),
          status: "pending",
          item_code: null,
          decision_note: null,
          proposer_ref: c.actor,
          created_at: new Date(clock()).toISOString(),
          decided_at: null,
        });
        return written(id, 1, vnToday(clock()));
      },
    },
    {
      method: "POST",
      pattern: /^\/v1\/price\/proposals\/([0-9a-f-]{36})\/(approve|reject)$/,
      handler: (ctx) => {
        const c = callerOf(ctx);
        if (c instanceof Response) return c;
        if (!canEdit(c)) return forbidden();
        const [id = "", op = ""] = ctx.params;
        const p = STORE.proposals.find((x) => x.id === id);
        if (!p)
          return problem(
            404,
            "price.proposal_not_found",
            "Không có đề xuất mặt hàng này",
          );
        if (p.status !== "pending")
          return problem(
            409,
            "price.proposal_decided",
            "Đề xuất đã được xử lý",
          );
        const now = new Date(clock()).toISOString();
        if (op === "reject") {
          const note = String(
            (ctx.body as { note?: string })?.note ?? "",
          ).trim();
          if (!note)
            return problem(422, "validation_failed", "Nhập lý do từ chối");
          Object.assign(p, {
            status: "rejected",
            decision_note: note,
            decided_at: now,
            version: 2,
          });
          return written(id, 2, vnToday(clock()));
        }
        const b = { kind: "local", ...(ctx.body as ItemIn) } as ItemIn;
        if (b.kind === "local" && !(b.points ?? []).includes(p.point_code))
          b.points = [...(b.points ?? []), p.point_code];
        const r = createItem(c, b, id);
        if (r instanceof Response) return r;
        Object.assign(p, {
          status: "approved",
          item_code: r.code,
          decided_at: now,
          version: 2,
        });
        return written(r.code, 1, r.from, r.n + 1);
      },
    },
  ];
}

export const priceItemRoutes: Route[] = [
  ...itemRoutes(),
  ...groupRoutes(),
  ...assignRoutes(),
  ...setRoutes(),
  ...proposalRoutes(),
];
