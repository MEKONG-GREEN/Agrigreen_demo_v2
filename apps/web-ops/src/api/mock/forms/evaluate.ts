// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của reports/form/eval.go: tính lưới bài nộp + dòng tổng.
import { Decimal } from "@/kn/grid/decimal";
import type { FormCellIssue, FormColumn, FormDefinition, FormEvalInput, FormEvalResult, FormEvalRow, FormEvalRowResult, FormIndicator, FormTotalDef, FormTotalRow } from "@/kn/forms/types";
import { compile, numericDType, type Compiled } from "./compile";
import { BLANK, evaluate as run, num, text, valueString, type Env, type Value } from "./formula";

const RESULT_PLACES = 10;
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const DEC_RE = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/;

function excelSerial(iso: string): Decimal | null {
  if (!DATE_RE.test(iso)) return null;
  const [y, m, d] = iso.split("-").map((x) => Number.parseInt(x, 10)) as [number, number, number];
  const t = Date.UTC(y, m - 1, d);
  const dt = new Date(t);
  if (dt.getUTCFullYear() !== y || dt.getUTCMonth() !== m - 1 || dt.getUTCDate() !== d) return null;
  return Decimal.fromInt(Math.round((t - Date.UTC(1899, 11, 30)) / 86_400_000));
}

function parseDec(s: string): Decimal | null {
  return DEC_RE.test(s) ? Decimal.parse(s) : null;
}

function parseLoose(s: string): Value {
  const d = parseDec(s.trim());
  if (d) return num(d);
  const serial = excelSerial(s);
  if (serial) return num(serial);
  return text(s);
}

function parseTyped(col: FormColumn, raw: string): [Value, string, string] {
  const s = raw.trim();
  if (s === "") return [BLANK, "", ""];
  switch (col.dtype) {
    case "number":
    case "integer": {
      const d = parseDec(s.replace(/,/g, "."));
      if (!d || /[eE]/.test(s)) return [BLANK, "", "phải là số (dùng dấu chấm thập phân)"];
      if (col.dtype === "integer" && !d.isInteger()) return [BLANK, "", "phải là số nguyên"];
      return [num(d), d.toString(), ""];
    }
    case "date": {
      const serial = excelSerial(s);
      if (!serial) return [BLANK, "", "ngày dạng YYYY-MM-DD"];
      return [num(serial), s, ""];
    }
  }
  if (Array.from(s).length > 500) return [BLANK, "", "tối đa 500 ký tự"];
  return [text(s), s, ""];
}

function outString(v: Value): string {
  if (v.k === "num") return v.n.round(RESULT_PLACES).toString();
  if (v.k === "blank") return "0";
  return valueString(v);
}

class RowEnv implements Env {
  vals = new Map<number, Value>();
  constructor(private readonly xrefs?: Record<string, Record<string, string>>) {}
  cell(col: number): Value {
    return this.vals.get(col) ?? BLANK;
  }
  xref(form: string, letter: string): Value {
    const s = this.xrefs?.[form]?.[letter];
    return s ? parseLoose(s) : BLANK;
  }
}

function truthy(v: Value): [boolean, boolean] {
  switch (v.k) {
    case "err":
      return [false, true];
    case "bool":
      return [v.b, false];
    case "num":
      return [!v.n.isZero(), false];
    case "blank":
      return [false, false];
  }
  return [false, true];
}

function rangeMsg(col: FormColumn): string {
  if (col.min != null && col.max != null) return `trong khoảng ${col.min}–${col.max}`;
  if (col.min != null) return `không nhỏ hơn ${col.min}`;
  return `không lớn hơn ${col.max}`;
}

function fixedDefault(col: FormColumn, row: FormEvalRow, ind: FormIndicator | undefined, no: number): string {
  const src = col.fixed_source ?? "";
  if (src === "row_no") return String(no);
  if (src === "indicator_label") return ind?.label ?? "";
  if (src === "commune_name") return row.commune;
  if (src === "region_name") return row.region ?? "";
  if (src.startsWith("xref:")) {
    const parts = src.slice(5).split(":");
    if (parts.length === 2) return row.xrefs?.[parts[0]!]?.[parts[1]!.toUpperCase()] ?? "";
  }
  return "";
}

export function evaluateGrid(def: FormDefinition, input: FormEvalInput): FormEvalResult {
  let c: Compiled;
  try {
    c = compile(def);
  } catch (e) {
    return { valid: false, rows: [], totals: [], issues: [{ row_key: "", code: "reports.form_invalid", message: e instanceof Error ? e.message : String(e), severity: "error" }] };
  }
  const r = evaluateCompiled(c, input);
  return { ...r, totals: totalsCompiled(c, r.rows) };
}

export function evaluateCompiled(c: Compiled, input: FormEvalInput): Omit<FormEvalResult, "totals"> {
  const rows: FormEvalRowResult[] = [];
  const issues: FormCellIssue[] = [];
  const issue = (row_key: string, letter: string, code: FormCellIssue["code"], message: string, severity: FormCellIssue["severity"]) => {
    const it: FormCellIssue = { row_key, code, message, severity };
    if (letter) it.letter = letter;
    issues.push(it);
  };
  const open = input.open_slots == null ? null : new Set(input.open_slots);
  const seen = new Set<string>();
  const communeNo = new Map<string, number>();
  const cols = c.def.columns;
  for (const row of input.rows) {
    let key = row.key ?? "";
    if (!key) key = row.indicator ? `${row.commune}/${row.indicator}` : row.commune;
    if (!row.commune || seen.has(key)) {
      issue(key, "", "reports.row_invalid", "dòng thiếu mã xã hoặc trùng khóa", "error");
      continue;
    }
    seen.add(key);
    if (!communeNo.has(row.commune)) communeNo.set(row.commune, communeNo.size + 1);
    let ind: FormIndicator | undefined;
    if (c.indicators.size > 0) {
      ind = c.indicators.get(row.indicator ?? "");
      if (!ind) {
        issue(key, "", "reports.row_invalid", "chỉ tiêu không có trong mẫu", "error");
        continue;
      }
    } else if (row.indicator) {
      issue(key, "", "reports.row_invalid", "mẫu không có dòng chỉ tiêu", "error");
      continue;
    }
    const env = new RowEnv(row.xrefs);
    const cells: Record<string, string> = {};
    // 1. Ô hệ thống
    cols.forEach((col, i) => {
      let raw: string;
      if (col.kind === "fixed") {
        raw = row.system?.[col.letter] ?? "";
        if (raw === "") raw = fixedDefault(col, row, ind, communeNo.get(row.commune) ?? 0);
      } else if (col.kind === "provincial") {
        raw = row.system?.[col.letter] ?? "";
      } else return;
      if (raw === "") return;
      const parsed = parseTyped(col, raw);
      let v = parsed[0];
      let s = parsed[1];
      if (parsed[2]) {
        v = parseLoose(raw);
        s = raw;
      }
      env.vals.set(i + 1, v);
      cells[col.letter] = s;
    });
    // 2. Ô nhập
    const letters = Object.keys(row.input ?? {}).sort();
    for (const l of letters) {
      const raw = row.input[l] ?? "";
      const ci = c.idx.get(l.toUpperCase());
      if (!ci) {
        issue(key, l, "reports.cell_unknown_column", "cột không có trong mẫu", "error");
        continue;
      }
      const col = cols[ci - 1]!;
      if (col.kind === "per_period") {
        if (open && !open.has(c.slotOf.get(ci)?.[0] ?? -1)) {
          if (raw.trim() !== "") issue(key, col.letter, "reports.cell_period_closed", "chỉ cột của kỳ đang mở được nhập", "error");
          continue;
        }
      } else if (col.kind !== "input") {
        if (raw.trim() !== "") issue(key, col.letter, "reports.cell_not_editable", "cột cố định, Tỉnh giao hoặc công thức không nhập tay", "error");
        continue;
      }
      const [v, s, msg] = parseTyped(col, raw);
      if (msg) {
        issue(key, col.letter, "reports.cell_invalid_type", msg, "error");
        continue;
      }
      if (v.k === "num") {
        const min = col.min != null ? Decimal.parse(col.min) : null;
        const max = col.max != null ? Decimal.parse(col.max) : null;
        if ((min && v.n.lt(min)) || (max && v.n.gt(max))) issue(key, col.letter, "reports.cell_out_of_range", rangeMsg(col), "error");
      }
      if (s !== "") {
        env.vals.set(ci, v);
        cells[col.letter] = s;
      }
    }
    // 3. Bắt buộc khi gửi
    if (input.mode === "submit") {
      cols.forEach((col, i) => {
        if (!col.required || cells[col.letter]) return;
        if (col.kind === "input" || (col.kind === "per_period" && (!open || open.has(c.slotOf.get(i + 1)?.[0] ?? -1)))) {
          issue(key, col.letter, "reports.cell_required", "bắt buộc nhập", "error");
        }
      });
    }
    // 4. Công thức
    for (const ci of c.order) {
      let v = run(c.progs.get(ci)!, env);
      if (v.k === "blank") v = num(Decimal.ZERO);
      env.vals.set(ci, v);
      const col = cols[ci - 1]!;
      cells[col.letter] = outString(v);
      if (v.k === "err") issue(key, col.letter, "reports.formula_error", `công thức ${col.formula ?? ""} cho ${v.e}`, "warning");
    }
    // 5. Ràng buộc
    for (const r of c.rules) {
      if (r.when) {
        const [ok] = truthy(run(r.when, env));
        if (!ok) continue;
      }
      const v = run(r.expr, env);
      const [ok, isErr] = truthy(v);
      if (isErr) {
        const it: FormCellIssue = { row_key: key, code: "reports.rule_error", message: `${r.r.message} (không tính được: ${valueString(v)})`, severity: "warning", rule_id: r.r.id };
        const fl = r.r.letters?.[0];
        if (fl) it.letter = fl.toUpperCase();
        issues.push(it);
        continue;
      }
      if (!ok) {
        if (!r.r.letters?.length) issues.push({ row_key: key, code: "reports.rule_failed", message: r.r.message, severity: r.r.severity, rule_id: r.r.id });
        for (const l of r.r.letters ?? []) issues.push({ row_key: key, letter: l.toUpperCase(), code: "reports.rule_failed", message: r.r.message, severity: r.r.severity, rule_id: r.r.id });
      }
    }
    const out: FormEvalRowResult = { key, commune: row.commune, cells };
    if (row.region) out.region = row.region;
    if (row.indicator) out.indicator = row.indicator;
    rows.push(out);
  }
  return { valid: !issues.some((i) => i.severity === "error"), rows, issues };
}

export function totalsOf(def: FormDefinition, rows: FormEvalRowResult[]): FormTotalRow[] {
  try {
    return totalsCompiled(compile(def), rows);
  } catch {
    return [];
  }
}

function totalsCompiled(c: Compiled, rows: FormEvalRowResult[]): FormTotalRow[] {
  const out: FormTotalRow[] = [];
  const inds = c.def.rows.indicators.length ? c.def.rows.indicators.map((i) => i.code) : [""];
  const filter = (region: string, ind: string) => rows.filter((r) => (region === "" || r.region === region) && (r.indicator ?? "") === ind);
  for (const t of c.def.totals) {
    if (t.kind === "region") {
      const regions = [...new Set(rows.map((r) => r.region ?? "").filter((x) => x !== ""))].sort();
      for (const reg of regions) {
        for (const ind of inds) out.push(totalRow(c, t, `region:${reg}${ind ? `/${ind}` : ""}`, reg, ind, filter(reg, ind)));
      }
    } else if (t.kind === "province") {
      for (const ind of inds) out.push(totalRow(c, t, `province${ind ? `/${ind}` : ""}`, "", ind, filter("", ind)));
    }
  }
  return out;
}

function totalRow(c: Compiled, t: FormTotalDef, key: string, region: string, ind: string, members: FormEvalRowResult[]): FormTotalRow {
  const cells: Record<string, string> = {};
  const env = new RowEnv();
  c.def.columns.forEach((col, i) => {
    let mode = col.total ?? "";
    if (mode === "") mode = col.kind === "formula" ? "formula" : "sum";
    if (mode !== "sum" || (!numericDType(col.dtype) && col.kind !== "formula") || col.kind === "fixed") return;
    let sum = Decimal.ZERO;
    let any = false;
    for (const m of members) {
      const d = parseDec(m.cells[col.letter] ?? "");
      if (d) {
        sum = sum.add(d);
        any = true;
      }
    }
    if (any) {
      env.vals.set(i + 1, num(sum));
      cells[col.letter] = sum.toString();
    }
  });
  for (const ci of c.order) {
    const col = c.def.columns[ci - 1]!;
    if (col.total && col.total !== "formula") continue;
    const v = run(c.progs.get(ci)!, env);
    env.vals.set(ci, v);
    cells[col.letter] = outString(v);
  }
  let label = t.label.replaceAll("{region}", region);
  if (ind) {
    const it = c.indicators.get(ind);
    if (it) label += ` – ${it.label}`;
  }
  cells[t.label_letter.toUpperCase()] = label;
  const res: FormTotalRow = { key, kind: t.kind, members: members.length, cells };
  if (region) res.region = region;
  if (ind) res.indicator = ind;
  return res;
}
