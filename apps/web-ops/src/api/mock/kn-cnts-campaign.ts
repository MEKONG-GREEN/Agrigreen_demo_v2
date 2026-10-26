// GIẢ LẬP Chiến dịch điều tra mở rộng CNTS (W11.50, BRD CNTS v3.7 FN-21, UR-TINH-26) theo hợp đồng
// /v1/extension/cnts/campaigns*: Nháp (sửa = phiên bản mới) → Phát hành (Đang thu thập, kỳ adhoc "DT-…") → nhắc, gia hạn có lý
// do → Đóng đợt (xã chưa gửi = Không nộp) → Mở lại (lý do + thời hạn). Tiến độ/ kết quả là số giả, KHÔNG nối giả lập công
// việc /v1/forms/tasks (máy chủ thật sinh việc cho xã). Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Campaign = S["CNTSCampaign"];
type Input = S["CNTSCampaignInput"];

const CAMPAIGNS = new Map<string, Campaign>();

/** Xóa trạng thái (kiểm thử). */
export function resetCntsCampaigns() {
  CAMPAIGNS.clear();
}

interface Who { role: string; readOnly: boolean; dept: string | null; actor: string }
function who(ctx: Ctx): Who {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  const role = kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh");
  return { role, readOnly: !!kc?.read_only, dept: kc?.department_code ?? null, actor: ctx.persona.me.actor_ref };
}

function reader(w: Who): Response | null {
  if (w.role === "admin") return null;
  if ((w.role === "kn_tinh" || w.role === "kn_tw") && (w.dept === null || w.dept === "chan_nuoi_thuy_san")) return null;
  return problem(403, "forbidden", "Chiến dịch điều tra thuộc Phòng Chăn nuôi – Thủy sản");
}
function writer(w: Who): Response | null {
  const r = reader(w);
  if (r) return r;
  if (w.role !== "kn_tinh") return problem(403, "forbidden", "chỉ Cấp Tỉnh Phòng CNTS tạo, phát hành, đóng đợt");
  return w.readOnly ? problem(403, "extension.read_only", "Ngữ cảnh chỉ xem") : null;
}

const body = <T>(ctx: Ctx) => (ctx.body ?? {}) as T;
const now = () => new Date().toISOString();
const stateErr = () => problem(409, "extension.cnts_campaign_state", "Thao tác không hợp lệ ở trạng thái hiện tại của đợt");

function check(i: Input): { field: string; reason: string }[] {
  const e: { field: string; reason: string }[] = [];
  if (!i.name?.trim()) e.push({ field: "name", reason: "bắt buộc" });
  if (!i.template_code) e.push({ field: "template_code", reason: "bắt buộc" });
  if (i.scope_mode === "communes" && !(i.communes ?? []).length) e.push({ field: "communes", reason: "phạm vi không có xã nào" });
  if (!i.from_date || !i.to_date || i.to_date < i.from_date) e.push({ field: "to_date", reason: "đến ngày ≥ từ ngày" });
  if (!i.due_at) e.push({ field: "due_at", reason: "bắt buộc" });
  return e;
}

function communesOf(i: Input): string[] {
  if (i.scope_mode === "communes") return [...new Set(i.communes ?? [])];
  return ["91307", "91310", "91313", "91316"]; // tỉnh/khu vực: danh sách xã giả của An Giang
}

function fromInput(i: Input, w: Who, prev?: Campaign): Campaign {
  const communes = communesOf(i);
  return {
    id: prev?.id ?? uuidv7(), version: (prev?.version ?? 0) + 1, name: i.name.trim(), goal: i.goal ?? null, field: i.field,
    template_code: i.template_code, scope_mode: i.scope_mode, regions: i.regions ?? [], communes, tasks: communes.length,
    from_date: i.from_date, to_date: i.to_date, due_at: i.due_at, remind_before_hours: i.remind_before_hours ?? 0, state: "draft",
    province_code: "91", updated_by: { ref: w.actor, display: "Cán bộ tỉnh (giả lập)" }, updated_at: now(), owner: null,
    forms_period_id: null, period_key: null, template_version: null, progress: null, events: [],
  };
}

function progressOf(c: Campaign, closed: boolean): NonNullable<Campaign["progress"]> {
  const units = c.communes.map((code, i) => {
    const submitted = i % 2 === 0;
    return { commune: code, commune_name: `Xã ${code}`, region_code: "", region_name: "Chưa gán khu vực",
      status: submitted ? "submitted" : closed ? "missed" : "not_submitted", versions: submitted ? 1 : 0, late: false,
      last_submitted_at: submitted ? now() : null };
  });
  const sub = units.filter((u) => u.versions > 0).length;
  const missed = units.filter((u) => u.status === "missed").length;
  return { period_status: closed ? "closed" : "open", total: units.length, submitted: sub, pending: units.length - sub - missed, overdue: 0,
    missed, returned: 0, regions: [{ code: "", name: "Chưa gán khu vực", total: units.length, submitted: sub, pending: units.length - sub - missed,
      overdue: 0, missed }], units };
}

function event(c: Campaign, w: Who, action: Campaign["events"][number]["action"], extra: Partial<Campaign["events"][number]> = {}) {
  c.events.push({ action, at: now(), actor: { ref: w.actor, display: "Cán bộ tỉnh (giả lập)" }, due_at: null, reason: null, reminded: null, ...extra });
}

function withCampaign(ctx: Ctx, f: (c: Campaign, w: Who) => Response): Response {
  const w = who(ctx);
  const deny = ctx.request.method === "GET" ? reader(w) : writer(w);
  if (deny) return deny;
  const c = CAMPAIGNS.get(ctx.params[0] ?? "");
  if (!c) return problem(404, "extension.cnts_campaign_not_found", "Không tìm thấy đợt điều tra");
  return f(c, w);
}

const reasonOk = (r: string | undefined) => !!r && r.trim().length >= 10 && r.trim().length <= 500;

export const cntsCampaignRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/campaigns$/,
    handler: (ctx) => {
      const deny = reader(who(ctx));
      if (deny) return deny;
      return json({ items: [...CAMPAIGNS.values()].reverse() });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = writer(w);
      if (deny) return deny;
      const i = body<Input>(ctx);
      const errs = check(i);
      if (errs.length) return validation(errs);
      const c = fromInput(i, w);
      CAMPAIGNS.set(c.id, c);
      return json(c, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)$/,
    handler: (ctx) => withCampaign(ctx, (c) => json(c)),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/draft$/,
    handler: (ctx) =>
      withCampaign(ctx, (c, w) => {
        if (c.state !== "draft") return stateErr();
        const i = body<Input>(ctx);
        const errs = check(i);
        if (errs.length) return validation(errs);
        const next = fromInput(i, w, c);
        CAMPAIGNS.set(c.id, next);
        return json(next);
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/publish$/,
    handler: (ctx) =>
      withCampaign(ctx, (c, w) => {
        if (c.state !== "draft") return stateErr();
        c.state = "collecting";
        c.forms_period_id = uuidv7();
        c.period_key = `DT-${c.from_date.replaceAll("-", "")}-${c.id.replaceAll("-", "").slice(-6)}`;
        c.template_version = 1;
        c.progress = progressOf(c, false);
        event(c, w, "published", { due_at: c.due_at });
        return json(c);
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/extend$/,
    handler: (ctx) =>
      withCampaign(ctx, (c, w) => {
        if (c.state !== "collecting") return stateErr();
        const b = body<{ due_at: string; reason: string }>(ctx);
        if (!reasonOk(b.reason)) return validation([{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }]);
        if (!b.due_at || Date.parse(b.due_at) <= Date.parse(c.due_at)) return validation([{ field: "due_at", reason: "hạn mới phải sau hạn hiện hành" }]);
        c.due_at = b.due_at;
        event(c, w, "extended", { due_at: b.due_at, reason: b.reason });
        return json(c);
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/close$/,
    handler: (ctx) =>
      withCampaign(ctx, (c, w) => {
        if (c.state !== "collecting") return stateErr();
        c.state = "closed";
        c.progress = progressOf(c, true);
        event(c, w, "closed");
        return json(c);
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/reopen$/,
    handler: (ctx) =>
      withCampaign(ctx, (c, w) => {
        if (c.state !== "closed") return stateErr();
        const b = body<{ due_at: string; reason: string }>(ctx);
        if (!reasonOk(b.reason)) return validation([{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }]);
        c.state = "collecting";
        c.due_at = b.due_at;
        c.progress = { ...progressOf(c, false), period_status: "reopened" };
        event(c, w, "reopened", { due_at: b.due_at, reason: b.reason });
        return json(c);
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/remind$/,
    handler: (ctx) =>
      withCampaign(ctx, (c, w) => {
        if (c.state !== "collecting") return stateErr();
        const n = c.progress?.pending ?? 0;
        if (n === 0) return validation([{ field: "", reason: "mọi xã đã gửi — không có xã cần nhắc" }]);
        event(c, w, "reminded", { reminded: n });
        return json({ campaign: c, reminded: n });
      }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/results$/,
    handler: (ctx) =>
      withCampaign(ctx, (c) => {
        if (c.state === "draft") return stateErr();
        const rows = (c.progress?.units ?? []).map((u, i) => ({ commune: u.commune, commune_name: u.commune_name, region_name: u.region_name,
          values: u.versions > 0 ? { tong_dan: String(100 + i * 10) } : {}, nil_report: false }));
        const total = rows.reduce((s, r) => s + Number(r.values.tong_dan ?? 0), 0);
        return json({ campaign_id: c.id, basis: c.state === "closed" ? "closed" : "submitted", fields: [{ key: "tong_dan", label: "Tổng đàn", unit: "con" }],
          rows, total: { tong_dan: String(total) }, units: rows.length, reported: rows.filter((r) => r.values.tong_dan).length,
          separate_from_cumulative: true, period_kind: "adhoc" });
      }),
  },
];
