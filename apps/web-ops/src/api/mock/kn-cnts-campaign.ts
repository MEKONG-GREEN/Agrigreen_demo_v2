// GIẢ LẬP Chiến dịch điều tra mở rộng CNTS (W11.50, BRD CNTS v3.7 FN-21, UR-TINH-26) theo hợp đồng
// /v1/extension/cnts/campaigns*: Nháp (sửa = phiên bản mới) → Phát hành (Đang thu thập, kỳ adhoc "DT-…") → nhắc, gia hạn có lý
// do → Đóng đợt (xã chưa gửi = Không nộp) → Mở lại (lý do + thời hạn). Tiến độ/ kết quả là số giả, KHÔNG nối giả lập công
// việc theo xã; #277 nối công việc theo hộ vào /v1/forms/tasks. Dữ liệu tổng hợp, không phải thật.
// #236 (BRD CNTS v4.0 FN-21): người phụ trách bắt buộc (chọn từ /v1/extension/cnts/campaign-owners), sao chép đợt cũ
// (/copy → đợt Nháp mới, không mang số liệu). Giả lập không phân nhóm Cấp Tỉnh: người ghi được coi là cán bộ tổng hợp.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { livestockHouseholdRef } from "./kn-cnts-livestock";
import { cntsDesignTemplate } from "./kn-cnts-design";
import { projectHouseholdCampaignPeriod, resetHouseholdCampaignPeriods } from "./kn-cnts-periods";

type S = components["schemas"];
type Campaign = S["CNTSCampaign"];
type Input = S["CNTSCampaignInput"];
type CopyInput = S["CNTSCampaignCopyInput"];

/** Cán bộ Cấp Tỉnh CNTS giả lập (ngoài người đang đăng nhập) chọn được làm người phụ trách. */
const OTHER_OWNERS = [
  { ref: "0193a000-0000-7000-8000-00000000c236", display: "Trần Thị Hoa (giả lập)" },
  { ref: "0193a000-0000-7000-8000-00000000d236", display: "Lê Văn Nam (giả lập)" },
];
const ownerName = (ref: string, w: Who) =>
  ref === w.actor ? "Cán bộ tỉnh (giả lập)" : (OTHER_OWNERS.find((o) => o.ref === ref)?.display ?? "");

const CAMPAIGNS = new Map<string, Campaign>();

/** Xóa trạng thái (kiểm thử). */
export function resetCntsCampaigns() {
  CAMPAIGNS.clear();
  resetHouseholdCampaignPeriods();
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
  if (i.template_version != null && i.template_version < 1) e.push({ field: "template_version", reason: "phiên bản biểu từ 1 trở lên" });
  if (i.scope_mode === "communes" && !(i.communes ?? []).length) e.push({ field: "communes", reason: "phạm vi không có xã nào" });
  if (i.scope_mode === "households" && !(i.households ?? []).length) e.push({ field: "households", reason: "chọn ít nhất một hộ chăn nuôi" });
  if (!i.from_date || !i.to_date || i.to_date < i.from_date) e.push({ field: "to_date", reason: "đến ngày ≥ từ ngày" });
  if (!i.due_at) e.push({ field: "due_at", reason: "bắt buộc" });
  if (!i.owner_ref) e.push({ field: "owner_ref", reason: "người phụ trách đợt bắt buộc" });
  return e;
}

function communesOf(i: Input): string[] {
  if (i.scope_mode === "communes") return [...new Set(i.communes ?? [])];
  if (i.scope_mode === "households") return ["91307"]; // giả lập: xã của hộ không tra sổ hộ
  return ["91307", "91310", "91313", "91316"]; // tỉnh/khu vực: danh sách xã giả của An Giang
}

function checkOwner(i: Input, w: Who): Response | null {
  return ownerName(i.owner_ref, w) ? null : problem(422, "extension.cnts_campaign_owner", "Người phụ trách đợt phải là cán bộ Cấp Tỉnh Phòng CNTS của tỉnh");
}

function fromInput(i: Input, w: Who, prev?: Campaign): Campaign {
  const communes = communesOf(i);
  return {
    id: prev?.id ?? uuidv7(), version: (prev?.version ?? 0) + 1, name: i.name.trim(), goal: i.goal ?? null, field: i.field,
    template_code: i.template_code, scope_mode: i.scope_mode, regions: i.regions ?? [], communes,
    households: [...new Set(i.households ?? [])].map((ref) => ({ ref, code: "", commune: "91307", status: "active" })),
    tasks: i.scope_mode === "households" ? new Set(i.households ?? []).size : communes.length,
    from_date: i.from_date, to_date: i.to_date, due_at: i.due_at, remind_before_hours: i.remind_before_hours ?? 0, state: "draft",
    province_code: "91", updated_by: { ref: w.actor, display: "Cán bộ tỉnh (giả lập)" }, updated_at: now(), can_manage: true,
    owner: { ref: i.owner_ref, display: ownerName(i.owner_ref, w) },
    forms_period_id: null, period_key: null, template_version: i.template_version ?? null, progress: null, events: [],
  };
}

function progressOf(c: Campaign, closed: boolean): NonNullable<Campaign["progress"]> {
  const leaves = c.scope_mode === "households" ? c.households.map((h) => ({ unit: h.ref, household_code: h.code, commune: h.commune }))
    : c.communes.map((code) => ({ unit: code, household_code: "", commune: code }));
  const units = leaves.map(({ unit, household_code, commune: code }, i) => {
    const submitted = i % 2 === 0;
    return { unit, household_code, commune: code, commune_name: `Xã ${code}`, region_code: "", region_name: "Chưa gán khu vực",
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
      const bad = checkOwner(i, w);
      if (bad) return bad;
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
        const bad = checkOwner(i, w);
        if (bad) return bad;
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
        const template = c.scope_mode === "households" ? cntsDesignTemplate(c.template_code, c.template_version ?? undefined) : null;
        if (c.scope_mode === "households") {
          const current = c.households.map((h) => livestockHouseholdRef(h.ref));
          if (current.some((h) => !h || h.status !== "active")) return problem(422, "extension.cnts_campaign_household", "Hộ không còn Đang dùng");
          if (!template) return problem(422, "forms.template_not_found", "Không tìm thấy phiên bản biểu đã chọn");
          c.households = current.map((h) => ({ ref: h!.household_ref, code: h!.code, commune: h!.commune_code, status: h!.status }));
          c.communes = [...new Set(c.households.map((h) => h.commune))];
        }
        c.state = "collecting";
        c.forms_period_id = uuidv7();
        c.period_key = `DT-${c.from_date.replaceAll("-", "")}-${c.id.replaceAll("-", "").slice(-6)}`;
        c.template_version = c.template_version ?? 1; // #192: giữ phiên bản đã chọn ở Nháp
        if (template) {
          c.template_version = template.version;
          projectHouseholdCampaignPeriod({ id: c.forms_period_id, code: c.template_code, title: template.title, label: c.name,
            kind: "adhoc", key: c.period_key, from: c.from_date, to: c.to_date, due_at: c.due_at, rule: "cnts.campaign/v1",
            template_id: template.template_id, template_version: template.version, fields: template.fields, unit_kind: "household",
            units: c.households.map((h) => h.ref), parents: c.households.map((h) => h.commune) });
        }
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
        const rows = (c.progress?.units ?? []).map((u, i) => ({ unit: u.unit, household_code: u.household_code, commune: u.commune, commune_name: u.commune_name, region_name: u.region_name,
          values: u.versions > 0 ? { tong_dan: String(100 + i * 10) } : {}, nil_report: false }));
        const total = rows.reduce((s, r) => s + Number(r.values.tong_dan ?? 0), 0);
        return json({ campaign_id: c.id, basis: c.state === "closed" ? "closed" : "submitted", fields: [{ key: "tong_dan", label: "Tổng đàn", unit: "con" }],
          rows, total: { tong_dan: String(total) }, units: rows.length, reported: rows.filter((r) => r.values.tong_dan).length,
          separate_from_cumulative: true, period_kind: "adhoc" });
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/campaigns\/([^/]+)\/copy$/,
    handler: (ctx) =>
      withCampaign(ctx, (src, w) => {
        const b = body<CopyInput>(ctx);
        const i: Input = { name: b.name?.trim() || src.name, goal: src.goal ?? undefined, field: src.field as Input["field"],
          template_code: src.template_code, template_version: src.template_version, scope_mode: src.scope_mode, regions: src.regions, communes: src.communes,
          households: src.households.map((h) => h.ref),
          from_date: b.from_date, to_date: b.to_date, due_at: b.due_at, owner_ref: b.owner_ref ?? src.owner?.ref ?? "",
          remind_before_hours: src.remind_before_hours };
        const errs = check(i);
        if (errs.length) return validation(errs);
        const bad = checkOwner(i, w);
        if (bad) return bad;
        const c = fromInput(i, w);
        CAMPAIGNS.set(c.id, c);
        return json(c, 201);
      }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/campaign-owners$/,
    handler: (ctx) => {
      const w = who(ctx);
      const deny = writer(w);
      if (deny) return deny;
      return json({ items: [{ ref: w.actor, display: ownerName(w.actor, w) }, ...OTHER_OWNERS] });
    },
  },
];
