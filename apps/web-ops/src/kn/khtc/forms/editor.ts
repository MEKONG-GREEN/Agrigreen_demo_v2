import type { KhtcFormColumn, KhtcFormContent, KhtcFormIssue, KhtcFormSheet } from "./api";

/** Same bounded decimal grammar as khtcform/domain/sheet.go, before any numeric conversion. */
export function formNumberError(raw: string, column: Pick<KhtcFormColumn, "dtype" | "non_negative">, required = false): string | undefined {
  if (raw.length > 24) return "Số quá dài (tối đa 15 chữ số và 6 chữ số thập phân).";
  const value = raw.trim().replace(",", ".");
  if (!value) return required ? "Bắt buộc nhập." : undefined;
  if (!(column.dtype === "integer" ? /^-?\d{1,9}$/ : /^-?\d{1,15}(\.\d{1,6})?$/).test(value))
    return column.dtype === "integer" ? "Chỉ nhập số nguyên, tối đa 9 chữ số." : "Nhập số thập phân, tối đa 15 chữ số và 6 chữ số lẻ; không dùng số mũ.";
  if (column.non_negative && /^-/.test(value) && (column.dtype === "integer" || /[1-9]/.test(value))) return "Không được nhập số âm. Số điều chỉnh nhập ở khối Điều chỉnh kỳ trước.";
}

/** Validation of editable inputs only; the server remains the authority for formula/source/version rules. */
export function editorIssues(form: KhtcFormSheet, content: KhtcFormContent, submit: boolean): KhtcFormIssue[] {
  if (form.supplement) return [];
  const issues: KhtcFormIssue[] = [];
  const add = (message: string, location: Partial<KhtcFormIssue>) => issues.push({ rule: "template", severity: "error", adjustment: -1, message, ...location });
  for (const section of form.template.sections) {
    const raw = content.texts[section.key] ?? "";
    if (raw.length > 16000 || [...raw.trim()].length > 8000) add("Tối đa 8000 ký tự.", { section: section.key });
    else if (submit && section.required && !raw.trim()) add("Bắt buộc nhập.", { section: section.key });
  }
  if (!form.grid) return issues;
  for (const row of form.tabs.flatMap(tab => tab.rows)) for (const column of form.template.columns) {
    if (column.kind !== "input" || !column.input || (row.module_sourced && column.input === "done")) continue;
    const raw = content.cells[row.entry_ref]?.[column.input] ?? "";
    const error = column.dtype === "text"
      ? raw.length > 4000 || [...raw.trim()].length > 2000 ? "Tối đa 2000 ký tự." : submit && column.required && !raw.trim() ? "Bắt buộc nhập." : undefined
      : formNumberError(raw, column, submit && column.required);
    if (error) add(error, { entry_ref: row.entry_ref, letter: column.letter });
  }
  content.adjustments.forEach((adjustment, index) => {
    // Blank rows are ignored by the server's Normalize, including while saving a draft.
    if (![adjustment.entry_ref, adjustment.period_id, adjustment.amount, adjustment.reason].some(value => value.trim())) return;
    const location = (field: string) => ({ adjustment: index, letter: field });
    if (!form.tabs.some(tab => tab.rows.some(row => row.entry_ref === adjustment.entry_ref && !row.module_sourced))) add("Chọn hạng mục của phiếu.", location("entry_ref"));
    if (!form.adj_periods.some(period => period.id === adjustment.period_id)) add("Chọn kỳ được điều chỉnh đã khóa.", location("period_id"));
    const error = formNumberError(adjustment.amount, { dtype: "number", non_negative: false }, true);
    if (error) add(error, location("amount"));
    else if (!/[1-9]/.test(adjustment.amount)) add("Số điều chỉnh phải khác 0.", location("amount"));
    if (!adjustment.reason.trim()) add("Bắt buộc nhập lý do điều chỉnh.", location("reason"));
    else if (adjustment.reason.length > 1000 || [...adjustment.reason.trim()].length > 500) add("Lý do tối đa 500 ký tự.", location("reason"));
  });
  if (content.adjustments.length > 200) add("Tối đa 200 dòng điều chỉnh.", {});
  return issues;
}
