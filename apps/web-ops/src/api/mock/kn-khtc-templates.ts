// GIẢ LẬP W11.65. Definitions are tenant-scoped. All source reads happen after caller checks.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { TENANT } from "./personas";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcTemplateNarrativeUsage, khtcTemplatePreviewContent } from "./kn-khtc-reports";
import { templateContext } from "./kn-khtc-template-context";
import { canonical, invalidDefinition, isFixedKey, mockDigest, renderTemplate, seedTemplate, type Definition } from "./kn-khtc-template-render";
type S = components["schemas"];
type Key = S["KHTCTemplateKey"];
type Version = S["KHTCTemplateVersion"];
type Publication = S["KHTCTemplatePublication"];
type State = {
    versions: Version[];
    publications: Publication[];
};
const STORE = new Map<string, State>();
const REPLAYS = new Map<string, {
    hash: string;
    body: unknown;
}>();
const RULE = "khtc-template/v2.3-1";
const TYPES: Record<Key, string> = { mau_1: "week", mau_2: "month", mau_3: "quarter", mau_4: "year", mau_5: "program" };
// Bound by a coordinated report-history seam; never infer saved history from preview's default narrative.
function savedNarrativeUsage(ctx: Ctx, key: Key, slots: string[]): boolean {
 return khtcMockStore().filter(period => period.p.report_type === TYPES[key]).some(period => {
  const usage = khtcTemplateNarrativeUsage(ctx, period.p.id);
  return usage?.revisions.some(revision => revision.sections.some(section => slots.includes(section))) ?? false;
 });
}
let narrativeUsage: ((ctx: Ctx, key: Key, slots: string[]) => boolean) | null = savedNarrativeUsage;
export function useTemplateMockNarrativeUsage(read: typeof narrativeUsage | undefined) { narrativeUsage = read === undefined ? savedNarrativeUsage : read; }
export function resetKhtcTemplates() { STORE.clear(); REPLAYS.clear(); }
const code = (name: string, title: string, status = 409) => problem(status, `extension.khtc_template_${name}`, title);
const state = (tenant: string, key: Key) => { const id = `${tenant}:${key}`; let s = STORE.get(id); if (!s) {
    s = { versions: [], publications: [] };
    STORE.set(id, s);
} return s; };
function caller(ctx: Ctx, write: boolean): Response | null {
    const c = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    if (!c || c.role !== "admin" || !ctx.persona.me.tenant_id || !ctx.persona.me.actor_ref || Date.parse(c.expires_at) <= Date.now())
        return problem(403, "forbidden", "Chỉ ngữ cảnh Admin được cấu hình mục lục mẫu");
    if (write && (c.read_only || c.support_session_id))
        return problem(403, "forbidden", "Phiên chỉ xem/hỗ trợ không cấu hình mẫu");
    return null;
}
function detail(ctx: Ctx, key: Key): S["KHTCTemplateDetail"] {
    const d = seedTemplate(key)!;
    d.hash = mockDigest(canonical(d.definition));
    const s = state(ctx.persona.me.tenant_id, key), v = s.versions.at(-1), c = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
    return { ...d, ...(v ? { version_id: v.id, version: v.version, definition: structuredClone(v.definition), hash: v.hash } : {}), history: structuredClone([...s.versions].reverse().slice(0, 100)), publications: structuredClone([...s.publications].reverse().slice(0, 100)), editable: !c?.read_only && !c?.support_session_id };
}
function bodyObject(ctx: Ctx, allowed: string[]): Record<string, unknown> | Response {
    const b = ctx.body;
    if (!b || typeof b !== "object" || Array.isArray(b) || Object.keys(b).some(k => !allowed.includes(k)))
        return code("invalid", "JSON không hợp lệ hoặc có trường không hỗ trợ", 422);
    return b as Record<string, unknown>;
}
function versionInput(b: Record<string, unknown>, min = 0) { return typeof b.base_version === "number" && Number.isSafeInteger(b.base_version) && b.base_version >= min && b.base_version <= 1000000000; }
function preview(ctx: Ctx, key: Key, s: State): Response {
    const b = bodyObject(ctx, ["definition", "version_id", "period_id", "scenario"]);
    if (b instanceof Response)
        return b;
    const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (b.definition != null && b.version_id != null || b.version_id != null && (typeof b.version_id !== "string" || !uuid.test(b.version_id)) || b.period_id != null && (typeof b.period_id !== "string" || !uuid.test(b.period_id)) || b.scenario != null && typeof b.scenario !== "string") return code("invalid", "Dữ liệu xem trước không hợp lệ", 422);
    let d: Definition | undefined;
    if (b.definition != null)
        d = b.definition as Definition;
    else if (b.version_id != null)
        d = s.versions.find(v => v.id === b.version_id)?.definition;
    else
        d = s.versions.at(-1)?.definition ?? seedTemplate(key)!.definition;
    if (!d)
        return code("not_found", "Không tìm thấy phiên bản mẫu", 404);
    const invalid = invalidDefinition(d);
    if (d.key !== key || invalid)
        return code("invalid", invalid ?? "Mẫu không khớp", 422);
    // The shared mock report store is seeded for one tenant. Do not expose it to another tenant's Admin.
    if (ctx.persona.me.tenant_id !== TENANT)
        return code("not_found", "Không có kỳ báo cáo trong tenant", 404);
    const periods = khtcMockStore().filter(p => p.p.report_type === TYPES[key]);
    const id = typeof b.period_id === "string" && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(b.period_id) ? b.period_id : null;
    const period = id ? khtcMockStore().find(p => p.p.id === id) : periods.sort((a, b) => b.p.from_date.localeCompare(a.p.from_date) || b.p.id.localeCompare(a.p.id))[0];
    if (!period)
        return code("not_found", "Chưa có kỳ phù hợp để xem trước", 404);
    if (period.p.report_type !== TYPES[key]) return code("invalid", "Kỳ không thuộc mẫu đã chọn", 422);
    const content = khtcTemplatePreviewContent(ctx, period.p.id);
    if (!content || content.data.template !== key)
        return code("invalid", "Kỳ không thuộc mẫu đã chọn", 422);
    try {
        const context = templateContext(content, content.template_terminal ?? period.terminal), scenario = typeof b.scenario === "string" ? b.scenario : "actual";
        return json({ context, document: renderTemplate(d, context, scenario), definition_hash: mockDigest(canonical(d)), scenario });
    }
    catch (e) {
        return code("invalid", e instanceof Error ? e.message : "Không dựng được mẫu", 422);
    }
}
function handle(ctx: Ctx): Response {
    const [raw, operation] = ctx.params;
    const write = ctx.request.method === "POST" && operation !== "preview";
    const deny = caller(ctx, write);
    if (deny)
        return deny;
    if (!raw)
        return json({ items: ["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"].map(k => detail(ctx, k as Key)) });
    if (!isFixedKey(raw))
        return code("not_found", "Chỉ cấu hình năm mẫu", 404);
    const key = raw, s = state(ctx.persona.me.tenant_id, key);
    if (ctx.request.method === "GET")
        return json(detail(ctx, key));
    if (operation === "preview")
        return preview(ctx, key, s);
    const allowed = operation === "versions" ? ["base_version", "definition", "acknowledge_text_loss"] : operation === "restore" ? ["base_version", "version_id", "acknowledge_text_loss"] : ["base_version", "version_id", "applies_from"];
    const b = bodyObject(ctx, allowed);
    if (b instanceof Response)
        return b;
    const idem = ctx.request.headers.get("Idempotency-Key");
    if (!idem)
        return problem(400, "idempotency_required", "Thiếu khóa thao tác");
    if (idem.length > 128 || idem.trim() !== idem || /[\u0000\r\n]/.test(idem))
        return problem(400, "bad_request", "Khóa thao tác không hợp lệ");
    if (b.acknowledge_text_loss != null && typeof b.acknowledge_text_loss !== "boolean")
        return code("invalid", "Xác nhận phải là đúng/sai", 422);
    const replayKey = `${ctx.persona.me.tenant_id}:${ctx.persona.me.actor_ref}:${key}:${operation}:${idem}`, hash = mockDigest(b), replay = REPLAYS.get(replayKey);
    if (replay)
        return replay.hash === hash ? json(replay.body) : problem(409, "idempotency_conflict", "Khóa đã dùng cho nội dung khác");
    const latest = s.versions.at(-1);
    if (!versionInput(b, operation === "versions" ? 0 : 1))
        return code("invalid", "Phiên bản không hợp lệ", 422);
    if (b.base_version !== (latest?.version ?? 0))
        return code("version_conflict", "Mẫu đã có phiên bản mới hơn");
    const now = new Date().toISOString(), actor = ctx.persona.me.actor_ref, actor_display = ctx.persona.displayName;
    let result: Version | Publication;
    if (operation === "publish") {
        if (!latest || latest.id !== b.version_id)
            return code("version_conflict", "Chọn bản nháp mới nhất");
        const day = b.applies_from;
        if (typeof day !== "string" || !/^(20\d\d|2100)-\d\d-\d\d$/.test(day) || !Number.isFinite(Date.parse(day)) || new Date(day).toISOString().slice(0, 10) !== day)
            return code("invalid", "Ngày áp dụng từ năm 2000–2100", 422);
        result = { id: uuidv7(), key, sequence: s.publications.length + 1, version_id: latest.id, version: latest.version, hash: latest.hash, applies_from: `${day}T00:00:00Z`, actor, actor_display, at: now };
        s.publications.push(result);
    }
    else {
        const source = operation === "restore" ? s.versions.find(v => v.id === b.version_id) : null;
        if (operation === "restore" && !source)
            return code("not_found", "Không có phiên bản cần khôi phục", 404);
        const definition = (source?.definition ?? b.definition) as Definition;
        const invalid = invalidDefinition(definition, latest?.definition ?? seedTemplate(key)!.definition);
        if (definition?.key !== key || invalid)
            return code("invalid", invalid ?? "Mẫu không khớp", 422);
        const previous = latest?.definition ?? seedTemplate(key)!.definition;
        const removed = previous.nodes.filter(n => n.text_slot && !definition.nodes.find(x => x.id === n.id)?.text_slot).map(n => n.id);
        if (!b.acknowledge_text_loss && removed.length) {
            if (!narrativeUsage)
                return problem(503, "extension.dependency_unavailable", "Chưa kiểm tra được nội dung bản soạn, vui lòng thử lại");
            if (narrativeUsage(ctx, key, removed))
                return code("text_in_use", "Vùng lời văn đã có nội dung, cần xác nhận");
        }
        result = { id: uuidv7(), key, version: (latest?.version ?? 0) + 1, action: operation === "restore" ? "restore" : "save", definition: canonical(definition), hash: mockDigest(canonical(definition)), ...(source ? { restored_from: source.id } : {}), actor, actor_display, at: now, rule: RULE };
        s.versions.push(result);
    }
    REPLAYS.set(replayKey, { hash, body: structuredClone(result) });
    return json(result, 201);
}
export const khtcTemplateRoutes: Route[] = [{ method: "GET", pattern: /^\/v1\/extension\/khtc\/templates(?:\/([^/]+))?$/, handler: handle }, { method: "POST", pattern: /^\/v1\/extension\/khtc\/templates\/([^/]+)\/(versions|restore|publish|preview)$/, handler: handle }];

/** The composer selects once for a new revision; saved content never resolves latest. */
export function selectKhtcMockTemplate(ctx: Ctx, key: Key, start: string, sections: S["KHTCReportSection"][]): S["KHTCFixedTemplateSnapshot"] {
 const ledger=state(ctx.persona.me.tenant_id,key), day=start.slice(0,10);
 const first=[...ledger.publications].sort((a,b)=>a.applies_from.localeCompare(b.applies_from))[0];
 if(!first || day<first.applies_from.slice(0,10)) {
  const definition={sections:structuredClone(sections)};
  return {mode:"legacy",key,id:"00000000-0000-0000-0000-000000000000",version:1,rule:"khtc-report/g1-v1",hash:mockDigest(definition),definition,applicable_from:"0001-01-01T00:00:00Z",...(first?{legacy_until:first.applies_from}:{}),word_key:`khtc.${key}`,word_version:1,word_hash:mockDigest({key,sections}),excel_rule:"khtc-report/g1-v1"};
 }
 const publication=ledger.publications.filter(p=>p.applies_from.slice(0,10)<=day).sort((a,b)=>b.applies_from.localeCompare(a.applies_from)||b.sequence-a.sequence)[0];
 const version=ledger.versions.find(v=>v.id===publication?.version_id);
 if(!publication || !version || version.key!==key || version.version!==publication.version || version.hash!==publication.hash || version.rule!==RULE || version.hash!==mockDigest(canonical(version.definition)) || invalidDefinition(version.definition))throw new Error("Nguồn mẫu đã phát hành không khớp");
 return {mode:"published",key,id:version.id,version:version.version,rule:version.rule,hash:version.hash,definition:structuredClone(version.definition) as unknown as Record<string,unknown>,applicable_from:publication.applies_from,word_key:"khtc.fixed",word_version:1,word_hash:mockDigest("khtc.fixed/v1"),excel_rule:"khtc-xlsx/fixed-v1"};
}
