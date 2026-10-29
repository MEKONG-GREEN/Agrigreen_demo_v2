// GIẢ LẬP W11.65: bounded outline/sentence rendering of the composer's captured projection; no source calculations.
import type { components } from "../generated/application";
import seeds from "./fixtures/khtc-template-seeds.json";
import { FIELDS } from "@/kn/khtc/templates/schema";
import { TEMPLATE_KEYS } from "@/kn/khtc/templates/model";
type S = components["schemas"];
export type Definition = S["KHTCTemplateDefinition"];
type Node = S["KHTCTemplateNode"];
type Context = S["KHTCTemplateContext"];
type Token = S["KHTCTemplateToken"];
const ALL = seeds as S["KHTCTemplateDetail"][];
export const seedTemplate = (key: string) => structuredClone(ALL.find(s => s.key === key) ?? null);
export function canonical(d: Definition): Definition {
    const out = structuredClone(d);
    out.nodes.sort((a, b) => a.id.localeCompare(b.id));
    for (const n of out.nodes) {
        n.bindings.sort((a, b) => a.variable.localeCompare(b.variable));
        n.locked_admin = [...(n.locked_admin ?? [])].sort();
    }
    return out;
}
// Demo identity digest, as in other mock providers; cryptographic hash assertions use the native PG implementation.
export function mockDigest(input: unknown): string {
    const raw = JSON.stringify(input);
    let result = "";
    for (let k = 0; k < 8; k++) {
        let h = 2166136261 ^ k;
        for (let i = 0; i < raw.length; i++)
            h = Math.imul(h ^ raw.charCodeAt(i), 16777619) >>> 0;
        result += h.toString(16).padStart(8, "0");
    }
    return result;
}
type Part = {
    text: string;
} | {
    variable: string;
};
const length = (value: string) => [...value].length;
function parts(text: string): Part[] {
    const result: Part[] = [];
    let literal = "";
    const flush = () => { if (literal) {
        result.push({ text: literal });
        literal = "";
    } };
    for (let i = 0; i < text.length;) {
        if (text.slice(i, i + 2) === "{{" || text.slice(i, i + 2) === "}}") {
            literal += text[i];
            i += 2;
            continue;
        }
        if (text[i] === "}")
            throw Error("Dấu ngoặc đóng lẻ");
        if (text[i] !== "{") {
            literal += text[i++];
            continue;
        }
        flush();
        const end = text.indexOf("}", i + 1);
        if (end < 0)
            throw Error("Thiếu dấu ngoặc đóng");
        const variable = text.slice(i + 1, end);
        if (!/^[a-z][a-z0-9_]{0,39}$/.test(variable))
            throw Error("Biến không hợp lệ");
        result.push({ variable });
        i = end + 1;
    }
    flush();
    return result;
}
export function invalidDefinition(d: Definition, previous?: Definition): string | null {
    if (!d || typeof d !== "object" || Object.keys(d).some(k => !["key", "nodes"].includes(k)))
        return "Thuộc tính mẫu không hợp lệ";
    const seed = seedTemplate(d.key);
    if (!seed || !Array.isArray(d.nodes) || !d.nodes.length || d.nodes.length > 256 || new TextEncoder().encode(JSON.stringify(d)).byteLength > 1048576)
        return "Chỉ năm mẫu, tối đa 256 mục và 1 MiB";
    const ids = new Map<string, Node>(), orders = new Set<string>();
    for (const n of d.nodes) {
        if (!n || typeof n !== "object" || typeof n.id !== "string" || typeof n.parent !== "string" || typeof n.text_slot !== "boolean" || Object.keys(n).some(k => !["id", "parent", "order", "title", "repeat", "tab", "department", "preamble", "body", "bindings", "text_slot", "locked_admin"].includes(k)))
            return "Thuộc tính mục không hợp lệ";
        if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$/.test(n.id) || ids.has(n.id) || typeof n.title !== "string" || !n.title.trim() || length(n.title) > 500 || n.title.includes("\u0000") || !Number.isInteger(n.order) || n.order < 1 || n.order > 10000)
            return "Mã, tên hoặc STT không hợp lệ";
        const order = n.parent + ":" + n.order;
        if (orders.has(order))
            return "STT trong cùng cha trùng";
        orders.add(order);
        ids.set(n.id, n);
        if (!["none", "plan_item", "program", "metric", "commodity", "form_bullet"].includes(n.repeat) || ![undefined, "", "all", "previous", "current"].includes(n.tab) || ![undefined, "", "trong_trot", "chan_nuoi_thuy_san", "hanh_chinh_tong_hop", "thong_tin_truyen_thong", "ke_hoach_tai_chinh"].includes(n.department))
            return "Nguồn lặp/phòng không hợp lệ";
        if (typeof n.body !== "string" || length(n.body) > 12000 || (n.preamble != null && typeof n.preamble !== "string") || length(n.preamble ?? "") > 1000 || (n.body + (n.preamble ?? "")).includes("\u0000") || n.repeat === "none" && n.preamble)
            return "Khuôn câu vượt giới hạn hoặc không hợp lệ";
        if (!Array.isArray(n.bindings) || n.bindings.length > 32)
            return "Tối đa 32 biến";
        const vars = new Set<string>();
        for (const b of n.bindings) {
            if (!b || typeof b !== "object" || Object.keys(b).some(k => !["variable", "source", "field", "ref"].includes(k)) || typeof b.variable !== "string" || !/^[a-z][a-z0-9_]{0,39}$/.test(b.variable) || vars.has(b.variable) || typeof b.source !== "string" || typeof b.field !== "string" || !Array.isArray(FIELDS[b.source]) || !FIELDS[b.source].includes(b.field) || b.source === "text" && !n.text_slot || (b.ref != null && (typeof b.ref !== "string" || new TextEncoder().encode(b.ref).byteLength > 200 || b.ref.includes("\u0000"))))
                return "Biến và nguồn không hợp lệ";
            vars.add(b.variable);
        }
        try {
            for (const part of [...parts(n.body), ...parts(n.preamble ?? "")])
                if ("variable" in part && !vars.has(part.variable))
                    return "Biến chưa gắn nguồn";
        }
        catch {
            return "Dấu ngoặc khuôn câu không hợp lệ";
        }
        const locks = n.locked_admin ?? [];
        if (!Array.isArray(locks) || new Set(locks).size !== locks.length || locks.some(x => !FIELDS.admin.includes(x)))
            return "Ô hành chính khóa không hợp lệ";
    }
    for (const n of d.nodes) {
        let at = n;
        const path = new Set([n.id]);
        for (let depth = 1; at.parent; depth++) {
            const parent = ids.get(at.parent);
            if (!parent || path.has(parent.id) || depth >= 6)
                return "Mục cha thiếu, lặp hoặc quá 6 cấp";
            path.add(parent.id);
            at = parent;
        }
        if (!n.parent && !seed.seeded_ids.includes(n.id))
            return "Chỉ thêm mục con";
        if (!d.nodes.some(x => x.parent === n.id) && !n.body.trim())
            return "Khuôn câu bắt buộc";
    }
    for (const old of seed.definition.nodes) {
        const n = ids.get(old.id);
        if (!n || n.parent !== old.parent || seed.protected_ids.includes(old.id) && (n.title !== old.title || n.order !== old.order))
            return "Giữ khung và tiêu đề/thứ tự cấp I/II chính thức";
    }
    if (previous && previous.key !== d.key)
        return "Không chuyển mẫu";
    for (const old of previous?.nodes ?? []) {
        const n = ids.get(old.id);
        if (n && n.parent !== old.parent)
            return "Chỉ sắp xếp trong cùng cha";
    }
    return null;
}
const NAMES: Record<string, string> = { trong_trot: "Trồng trọt", chan_nuoi_thuy_san: "Chăn nuôi – Thủy sản", hanh_chinh_tong_hop: "Hành chính – Tổng hợp", thong_tin_truyen_thong: "Thông tin – Tuyên truyền", ke_hoach_tai_chinh: "Kế hoạch – Tài chính" };
function statusMessage(s: string | undefined, dept: string | undefined, kind: string) { if (s === "no_send")
    return `Phòng ${NAMES[dept ?? ""] ?? dept ?? ""} chưa gửi số liệu.`; if (s === "unclosed")
    return `Phòng ${NAMES[dept ?? ""] ?? dept ?? ""} chưa chốt số liệu.`; if (s === "no_activity")
    return kind === "week" ? "Trong tuần không tổ chức." : "Trong tháng không tổ chức."; return ""; }
export function renderTemplate(d: Definition, c: Context, scenario = "actual"): S["KHTCTemplateRendered"] {
    const err = invalidDefinition(d);
    if (err)
        throw Error(err);
    if (!["actual", "normal", "no_send", "unclosed", "no_activity", "provisional"].includes(scenario))
        throw Error("Tình huống không hợp lệ");
    for (const value of [...Object.values(c.values), ...Object.values(c.lists).flatMap(rows => rows.flatMap(row => Object.values(row.values)))])
        if (length(value) > 20000)
            throw Error("Giá trị nguồn quá dài");
    for (const text of Object.values(c.texts)) if (length(text) > 12000) throw Error("Lời văn quá dài");
    const out: S["KHTCTemplateRendered"] = { key: d.key, nodes: [], input_hash: c.input_hash, provisional: [...c.provisional] };
    let budget = 0, count = 0;
    if (scenario === "provisional")
        out.provisional.push("Tình huống xem trước Tạm tính");
    const visit = (parent: string, prefix: string, depth: number) => {
        const children = d.nodes.filter(n => n.parent === parent).sort((a, b) => a.order - b.order);
        children.forEach((n, index) => {
            const seed = seedTemplate(d.key)!;
            const number = !parent || seed.protected_ids.includes(n.id) ? n.id : `${prefix}.${index + 1}`;
            const item: S["KHTCTemplateRenderedNode"] = { id: n.id, parent: n.parent, number, title: n.title, depth, processing: new Set(n.bindings.map(b => b.source)).size > 1 ? "mixed" : n.bindings[0]?.source ?? "fixed", protected: seed.protected_ids.includes(n.id), seeded: seed.seeded_ids.includes(n.id), text_slot: n.text_slot, rows: [] };
            const rows: S["KHTCTemplateSourceRow"][] = n.repeat === "none" ? [{ ref: "", values: {} }] : (c.lists[n.repeat] ?? []).filter(r => (!n.department || r.department === n.department) && !(n.tab === "previous" && r.values.tab !== "prev_year" || n.tab === "current" && r.values.tab === "prev_year") && n.bindings.every(b => !b.ref?.startsWith("section:") || r.values.section === b.ref.slice(8)));
            budget += length(n.title);
            if (budget > 1048576)
                throw Error("Nội dung vượt giới hạn");
            let lastProgram = "";
            for (const r of rows) {
                if (++count > 20000)
                    throw Error("Tổng số dòng sau lặp vượt giới hạn");
                // Native boundValues validates every binding, including unused variables and status-only rows.
                const bindings = new Map<string, Token>();
                for (const b of n.bindings) {
                    let selected = r.values;
                    if (b.source !== "admin" && b.source !== "text" && b.ref && !b.ref.startsWith("section:")) {
                        const found = Object.values(c.lists).flat().find(x => x.ref === b.ref);
                        if (!found)
                            throw Error("Không có tham chiếu nguồn");
                        selected = found.values;
                    }
                    const source = b.source === "auto" ? (selected.data_source === "module" ? "module" : "form") : b.source;
                    let value: string;
                    if (source === "text")
                        value = c.texts[n.id] ?? "";
                    else if (source === "admin")
                        value = c.values["admin." + b.field] ?? "";
                    else {
                        const captured = selected[source + "." + b.field] ?? c.values[source + "." + b.field];
                        if (captured === undefined)
                            throw Error("Nguồn không cung cấp ô đã chọn");
                        value = captured;
                    }
                    if (length(value) > 12000)
                        throw Error("Giá trị nguồn quá dài");
                    bindings.set(b.variable, { text: value, variable: b.variable, source, field: b.field, locked: !["admin", "text"].includes(source) || source === "admin" && (n.locked_admin ?? []).includes(b.field) });
                }
                const sentence = (text: string): Token[] => parts(text).map(part => { if ("text" in part)
                    return { text: part.text, locked: false }; const token = bindings.get(part.variable); if (!token)
                    throw Error("Biến chưa gắn nguồn"); return { ...token }; });
                const status = n.repeat !== "none" && !["actual", "provisional"].includes(scenario) ? scenario : ("status" in r ? r.status : undefined);
                const message = statusMessage(status, "department" in r ? r.department : undefined, c.period_type);
                const tokens = message ? [{ text: message, source: "status", locked: true }] : sentence(n.body);
                const row: S["KHTCTemplateRenderedRow"] = { ref: r.ref, status, tokens };
                const program = "program_ref" in r ? r.program_ref ?? "" : "";
                if (!message && n.preamble && (lastProgram !== program || !lastProgram)) {
                    row.preamble_tokens = sentence(n.preamble);
                    row.preamble = row.preamble_tokens.map(t => t.text).join("");
                    lastProgram = program;
                }
                budget += length(tokens.map(t => t.text).join("")) + length(row.preamble ?? "");
                if (budget > 1048576)
                    throw Error("Nội dung vượt giới hạn");
                if (message || n.body)
                    item.rows.push(row);
            }
            if (n.text_slot && !item.rows.some(row => [...row.tokens, ...(row.preamble_tokens ?? [])].some(token => token.source === "text")) && c.texts[n.id]) {
                if (++count > 20000) throw Error("Tổng số dòng sau lặp vượt giới hạn");
                const text = c.texts[n.id]!;
                budget += length(text);
                if (budget > 1048576)
                    throw Error("Nội dung vượt giới hạn");
                if (length(text) > 12000)
                    throw Error("Lời văn quá dài");
                item.rows.push({ ref: `text:${n.id}`, tokens: [{ text, source: "text", field: "narrative", locked: false }] });
            }
            out.nodes.push(item);
            visit(n.id, number, depth + 1);
        });
    };
    if (Object.values(c.lists).reduce((n, rs) => n + rs.length, 0) > 20000)
        throw Error("Nguồn quá nhiều dòng");
    visit("", "", 0);
    return out;
}
export function isFixedKey(s: string): s is S["KHTCTemplateKey"] { return (TEMPLATE_KEYS as readonly string[]).includes(s); }
