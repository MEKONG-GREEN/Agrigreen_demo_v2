import { validateForm, type NumberRule } from "@/components/form";
import type { KhtcPlanEntry } from "../api";
import { calendarDate } from "./version-form";

export const ENTRY_NUMBER_RULES = {
  sort_order: { required: true, integer: true, max: "100000", maxInputLength: 16 },
  points_count: { integer: true, max: "100000", maxInputLength: 16 },
  planned_qty: { max: "9999999999999999.9999", maxDecimals: 4, maxInputLength: 32 },
  budget: { max: "999999999999999999.99", maxDecimals: 2, maxInputLength: 32 },
} satisfies Record<string, NumberRule>;
export interface EntryDraft {
  entry: KhtcPlanEntry; sort_order: string; points_count: string;
  planned_qty: string; budget: string; communes: string;
}
export function entryDraft(entry: KhtcPlanEntry): EntryDraft {
  return { entry, sort_order: String(entry.sort_order), points_count: entry.points_count === null ? "" : String(entry.points_count), planned_qty: entry.planned_qty ?? "", budget: entry.budget ?? "", communes: entry.communes.join(", ") };
}
export function checkEntry(draft: EntryDraft, year: number) {
  const e = draft.entry;
  const checked = validateForm({ name: e.name, code: e.code ?? "", sort_order: draft.sort_order, points_count: draft.points_count, planned_qty: draft.planned_qty, budget: draft.budget }, {
    name: { kind: "text", required: true }, code: { kind: "text" },
    sort_order: { kind: "number", ...ENTRY_NUMBER_RULES.sort_order }, points_count: { kind: "number", ...ENTRY_NUMBER_RULES.points_count },
    planned_qty: { kind: "number", ...ENTRY_NUMBER_RULES.planned_qty }, budget: { kind: "number", ...ENTRY_NUMBER_RULES.budget },
  });
  const errors: Record<string, string> = { ...checked.errors };
  for (const [field,max] of [["name",1000],["code",40],["scale_note",1000],["stop_decision",100],["note",2000]] as const) {
    if (Array.from(String(e[field]??"")).length>max) errors[field]=`Tối đa ${max} ký tự.`;
  }
  if (e.tracking!=="tracking"&&e.tracking!=="stopped") errors.tracking="Bắt buộc chọn trạng thái theo dõi.";
  if (e.kind === "item" && !e.department_code) errors.department_code = "Bắt buộc chọn phòng phụ trách.";
  if (e.kind === "item" && e.data_source === "module" && !/^[a-z][a-z0-9_.:-]{1,99}$/.test(e.source_key ?? "")) errors.source_key = "Nhập khóa nguồn module hợp lệ.";
  if (e.tracking === "stopped" && (!calendarDate(e.stopped_from ?? "") || !e.stopped_from?.startsWith(`${year}-`))) errors.stopped_from = `Nhập ngày ngừng hợp lệ trong năm ${year}.`;
  const communes = draft.communes.split(/[,;\s]+/).filter(Boolean);
  if (new Set(communes).size>500) errors.communes="Tối đa 500 xã/điểm.";
  if (communes.some(c => !/^[0-9]{2,10}$/.test(c))) errors.communes = "Mã xã/điểm không hợp lệ.";
  // Conversion is exclusively after bounded lexical, scale and resource checks.
  if (Object.keys(errors).length) return { valid: false as const, errors, entry: null };
  return { valid: true as const, errors, entry: { ...e, name: e.name.trim(), sort_order: Number(checked.values.sort_order), points_count: checked.values.points_count === "" ? null : Number(checked.values.points_count), planned_qty: checked.values.planned_qty || null, budget: checked.values.budget || null, communes: [...new Set(communes)] } };
}
