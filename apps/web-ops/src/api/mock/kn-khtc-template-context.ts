// GIẢ LẬP W11.65: projection only. Capture, sealed inputs and calculations remain in kn-khtc-reports.
import type { components } from "../generated/application";
import { FIELDS } from "@/kn/khtc/templates/schema";
type S = components["schemas"];
type Row = S["KHTCTemplateSourceRow"];
export function templateContext(content: S["KHTCReportContent"], terminal: string): S["KHTCTemplateContext"] {
    const d = content.data;
    const out: S["KHTCTemplateContext"] = { period_id: d.period.id, period_name: d.period.name, period_type: d.period.type, period_start: new Date(d.period.from).toISOString(), plan_version_id: d.plan.VersionID, values: {}, lists: {}, texts: structuredClone(content.texts), provisional: [...d.provisional], input_hash: d.input_hash };
    for (const [key, field] of Object.entries(content.admin))
        out.values[`admin.${key}`] = field.override ?? field.system;
    const empty = () => Object.fromEntries(Object.entries(FIELDS).filter(([s]) => !["auto", "admin", "text"].includes(s)).flatMap(([s, fs]) => fs.map(f => [`${s}.${f}`, ""])));
    const names = new Map<string, string>();
    const field = (e: Record<string, unknown>, k: string) => typeof e[k] === "string" ? e[k] as string : "";
    const parents = new Map((d.plan.Entries ?? []).filter(e => field(e, "Ref")).map(e => [field(e, "Ref"), e]));
    for (const [ref, e] of parents)
        if (e.Kind === "program") {
            names.set(ref, field(e, "Name"));
            if (field(e, "ProgramRef"))
                names.set(field(e, "ProgramRef"), field(e, "Name"));
        }
    for (const row of d.rows)
        if (row.kind === "program") {
            names.set(row.ref, row.name);
            if (row.program_ref)
                names.set(row.program_ref, row.name);
        }
    const push = (list: string, row: Row) => (out.lists[list] ??= []).push(row);
    for (const row of d.rows) {
        let program = row.program_ref ?? "";
        if (!program) {
            let at = row.ref;
            for (let depth = 0; depth < 8; depth++) {
                const entry = parents.get(at);
                if (!entry)
                    break;
                if (entry.Kind === "program") {
                    program = field(entry, "ProgramRef") || field(entry, "Ref");
                    break;
                }
                at = field(entry, "ParentRef");
                if (!at)
                    break;
            }
        }
        const values = empty();
        Object.assign(values, { tab: row.tab, "plan.item_name": row.name, "plan.program_name": names.get(program) ?? "", "plan.roman": row.roman, "plan.unit": row.unit, "plan.department": row.department, "plan.planned": row.planned ?? "", "calculated.year_to_date": row.year_to_date, "calculated.percent": row.percent ?? "" });
        const source = row.source_key ? "module" : "form";
        values.data_source = source;
        for (const [field, value] of Object.entries({ value: row.in_period, content: row.content, unit: row.unit, label: row.name, department: row.department, status: row.status, people: row.people, budget: row.budget }))
            values[`${source}.${field}`] = value;
        const status = row.missing ? (source === "module" ? (terminal === "closed" ? "unclosed" : "provisional") : "no_send") : row.in_period === "0" && !row.content ? "no_activity" : "normal";
        if (row.kind === "item" || row.kind === "program")
            push(row.kind === "item" ? "plan_item" : "program", { ref: row.ref, parent_ref: row.parent_ref ?? "", program_ref: program, department: row.department, status, values });
    }
    const hasTotal = d.metrics.some(m => ["cnts-bm18", "bm-18"].includes(m.form));
    for (const m of d.metrics) {
        const v = m.value, values = empty();
        const section = m.department === "chan_nuoi_thuy_san" ? (m.stock ? "A.I.2" : "A.I.3") : "A.I.1";
        Object.assign(values, { data_source: "module", section, "module.label": m.label, "module.unit": m.unit, "module.department": m.department, "module.value": m.stock ? (v.Cumulative ?? "") : (v.Sum ?? v.Increment ?? ""), "calculated.year_to_date": m.stock ? "" : v.YearToDate ?? "" });
        const status = (!v.Closed || v.Unavailable) ? (terminal === "closed" ? "unclosed" : "provisional") : v.NoSource ? "no_activity" : "normal";
        const row: Row = { ref: m.key, department: m.department, status, values };
        if (v.Mode === "price") {
            const last = (v.PriceDays ?? []).at(-1);
            values["module.from_price"] = last?.Min ?? v.Min ?? "";
            values["module.to_price"] = last?.Max ?? v.Max ?? "";
            push("commodity", row);
        }
        else if (!(section === "A.I.3" && hasTotal && ["cnts-bm17", "bm-17"].includes(m.form)))
            push("metric", row);
    }
    const submitted = new Set(d.submissions.map(s => s.department));
    for (const dept of [...d.period.departments].sort()) {
        const sections = dept === "hanh_chinh_tong_hop" ? ["A.IV", "II.1", "II.2"] : ["A.II.3.2", "I.3.2"];
        const fields = d.form_texts[dept] ?? {};
        const keys = Object.keys(fields).filter(k => !["kho_khan", "nhiem_vu_ky_toi", "nhiem_vu_cchc"].includes(k)).sort();
        for (const section of sections) {
            let added = false;
            for (const key of keys) {
                if (!fields[key]?.trim())
                    continue;
                const values = empty();
                Object.assign(values, { section, "form.department": dept, "form.content": fields[key] });
                push("form_bullet", { ref: `${dept}:${section}:${key}`, department: dept, status: "normal", values });
                added = true;
            }
            if (!added) {
                const values = empty();
                Object.assign(values, { section, "form.department": dept });
                push("form_bullet", { ref: `${dept}:${section}`, department: dept, status: submitted.has(dept) ? "no_activity" : "no_send", values });
            }
        }
    }
    return out;
}
