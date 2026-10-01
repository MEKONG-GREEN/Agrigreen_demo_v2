// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của reports/form/ops.go: 16 thao tác trình thiết kế, cả lô hoặc
// không gì; chèn/xóa cột dời công thức, vùng gộp, nhóm như Excel. Lưới thật gọi POST …/ops của máy chủ.
import { columnIndex, columnLetter, parseRect, rectsOverlap, rectString, type Rect } from "@/kn/grid/letters";
import type { FormColumn, FormDefinition, FormHeaderCell, FormOp } from "@/kn/forms/types";
import { CODE_FORMULA_INVALID_REF, CODE_INVALID, MAX_COLUMNS, MAX_HEADER_ROWS, validate } from "./compile";
import { RefDeletedError, shiftString } from "./formula";

export class OpError extends Error {
  constructor(
    public index: number,
    readonly code: string,
    readonly msg: string,
  ) {
    super(`thao tác ${index}: ${msg}`);
  }
}

class Fail extends Error {}

export function cloneDef<T>(d: T): T {
  return JSON.parse(JSON.stringify(d)) as T;
}

function colIndex(d: FormDefinition, letter: string | undefined): number {
  const i = columnIndex(letter ?? "");
  if (i === 0 || i > d.columns.length) throw new Fail(`cột ${JSON.stringify(letter ?? "")} không tồn tại`);
  return i;
}

function rect(s: string): Rect {
  const r = parseRect(s);
  if (!r) throw new Fail("vùng gộp dạng A1:B2");
  return r;
}

function shiftLetter(l: string, at: number, delta: number): string | null {
  let c = columnIndex(l);
  if (delta < 0) {
    if (c === at) return null;
    if (c > at) c--;
    return columnLetter(c);
  }
  if (c >= at) c += delta;
  return columnLetter(c);
}

function shiftSpan(from: number, to: number, at: number, delta: number): [number, number] {
  if (delta < 0) {
    if (from > at) from--;
    if (to >= at) to--;
    return [from, to];
  }
  if (from >= at) from += delta;
  if (to >= at) to += delta;
  return [from, to];
}

function wrapShift(src: string, at: number, delta: number, what: string): string {
  try {
    return shiftString(src, at, delta);
  } catch (e) {
    if (e instanceof RefDeletedError) throw new RefFail(`${what}: ${e.message}`);
    throw new Fail(`${what}: ${e instanceof Error ? e.message : String(e)}`);
  }
}

class RefFail extends Fail {}

function shiftAll(d: FormDefinition, at: number, delta: number): void {
  for (const c of d.columns) {
    if (c.formula) c.formula = wrapShift(c.formula, at, delta, `công thức cột ${c.letter}`);
  }
  for (const r of d.rules) {
    if (r.expr) r.expr = wrapShift(r.expr, at, delta, `ràng buộc ${r.id}`);
    if (r.when) r.when = wrapShift(r.when, at, delta, `ràng buộc ${r.id}`);
    r.letters = (r.letters ?? []).map((l) => shiftLetter(l, at, delta)).filter((x): x is string => x !== null);
  }
  const cells: FormHeaderCell[] = [];
  for (const hc of d.header.cells) {
    const l = shiftLetter(hc.letter, at, delta);
    if (l !== null) cells.push({ ...hc, letter: l });
  }
  d.header.cells = cells;
  const merges: string[] = [];
  for (const m of d.header.merges) {
    const r = rect(m);
    [r.c1, r.c2] = shiftSpan(r.c1, r.c2, at, delta);
    if (r.c1 > r.c2 || (r.c1 === r.c2 && r.r1 === r.r2)) continue;
    merges.push(rectString(r));
  }
  d.header.merges = merges;
  const groups = [];
  for (const g of d.groups) {
    const [f, t] = shiftSpan(columnIndex(g.from), columnIndex(g.to), at, delta);
    if (f > t) continue;
    groups.push({ ...g, from: columnLetter(f), to: columnLetter(t) });
  }
  d.groups = groups;
  for (const b of d.period_blocks) {
    const f = columnIndex(b.first_letter);
    if (f >= at && (delta > 0 || f > at)) b.first_letter = columnLetter(f + delta);
  }
  for (const t of d.totals) {
    const l = shiftLetter(t.label_letter, at, delta);
    if (l !== null) t.label_letter = l;
  }
  if (d.freeze_through) {
    let f = columnIndex(d.freeze_through);
    if ((delta > 0 && f >= at) || (delta < 0 && f > at)) f += delta;
    else if (delta < 0 && f === at) f--;
    d.freeze_through = columnLetter(f);
  }
}

function relabel(d: FormDefinition): void {
  d.columns.forEach((c, i) => {
    c.letter = columnLetter(i + 1);
  });
}

function insertColumn(d: FormDefinition, op: FormOp): void {
  if (!op.column) throw new Fail("thiếu column");
  let at: number;
  try {
    at = colIndex(d, op.at);
  } catch (e) {
    if (d.columns.length === 0) at = 1;
    else throw e;
  }
  if (op.side === "after") at++;
  else if (op.side && op.side !== "before") throw new Fail("side là before hoặc after");
  if (d.columns.length + 1 > MAX_COLUMNS) throw new Fail(`tối đa ${MAX_COLUMNS} cột`);
  shiftAll(d, at, 1);
  d.columns.splice(at - 1, 0, cloneDef(op.column) as FormColumn);
  relabel(d);
}

function deleteColumn(d: FormDefinition, letter: string | undefined): void {
  const at = colIndex(d, letter);
  if (d.columns.length === 1) throw new Fail("mẫu phải còn ít nhất một cột");
  const removed = d.columns[at - 1]!;
  d.columns.splice(at - 1, 1);
  shiftAll(d, at, -1);
  relabel(d);
  if (removed.kind === "per_period" && d.period_blocks.some((b) => b.id === removed.period_block)) {
    throw new Fail(`cột ${letter ?? ""} thuộc khối kỳ ${removed.period_block ?? ""}: đổi số kỳ bằng set_period_block thay vì xóa lẻ`);
  }
}

function insertHeaderRow(d: FormDefinition, op: FormOp): void {
  let r = op.row ?? 0;
  if (r < 1 || r > d.header.rows) {
    if (d.header.rows === 0 && r <= 1) r = 1;
    else throw new Fail(`dòng tiêu đề ${op.row ?? 0} không tồn tại`);
  }
  if (op.side === "after") r++;
  if (d.header.rows + 1 > MAX_HEADER_ROWS) throw new Fail(`tối đa ${MAX_HEADER_ROWS} dòng tiêu đề`);
  d.header.rows++;
  for (const c of d.header.cells) if (c.row >= r) c.row++;
  d.header.merges = d.header.merges.map((m) => {
    const rc = rect(m);
    [rc.r1, rc.r2] = shiftSpan(rc.r1, rc.r2, r, 1);
    return rectString(rc);
  });
}

function deleteHeaderRow(d: FormDefinition, r: number): void {
  if (r < 1 || r > d.header.rows) throw new Fail(`dòng tiêu đề ${r} không tồn tại`);
  if (d.header.rows === 1) throw new Fail("mẫu phải còn ít nhất một dòng tiêu đề");
  d.header.rows--;
  const cells: FormHeaderCell[] = [];
  for (const c of d.header.cells) {
    if (c.row === r) continue;
    cells.push(c.row > r ? { ...c, row: c.row - 1 } : c);
  }
  d.header.cells = cells;
  const merges: string[] = [];
  for (const m of d.header.merges) {
    const rc = rect(m);
    [rc.r1, rc.r2] = shiftSpan(rc.r1, rc.r2, r, -1);
    if (rc.r1 > rc.r2 || (rc.c1 === rc.c2 && rc.r1 === rc.r2)) continue;
    merges.push(rectString(rc));
  }
  d.header.merges = merges;
}

function setHeaderCell(d: FormDefinition, op: FormOp): void {
  colIndex(d, op.letter);
  const row = op.row ?? 0;
  if (row < 1 || row > d.header.rows) throw new Fail(`dòng tiêu đề ${row} không tồn tại`);
  const l = (op.letter ?? "").toUpperCase();
  const idx = d.header.cells.findIndex((c) => c.row === row && c.letter === l);
  if (!op.text) {
    if (idx >= 0) d.header.cells.splice(idx, 1);
    return;
  }
  if (idx >= 0) d.header.cells[idx]!.text = op.text;
  else d.header.cells.push({ row, letter: l, text: op.text });
}

function merge(d: FormDefinition, rng: string): void {
  const r = rect(rng);
  for (const m of d.header.merges) {
    const o = parseRect(m);
    if (o && rectsOverlap(r, o)) throw new Fail(`vùng ${rectString(r)} chồng lên vùng gộp ${m} — tách trước`);
  }
  d.header.merges.push(rectString(r));
  d.header.cells = d.header.cells.filter((c) => {
    const ci = columnIndex(c.letter);
    const inside = ci >= r.c1 && ci <= r.c2 && c.row >= r.r1 && c.row <= r.r2;
    return !inside || (ci === r.c1 && c.row === r.r1);
  });
}

function unmerge(d: FormDefinition, rng: string): void {
  const r = rect(rng);
  const idx = d.header.merges.findIndex((m) => {
    const o = parseRect(m);
    return !!o && o.c1 === r.c1 && o.c2 === r.c2 && o.r1 === r.r1 && o.r2 === r.r2;
  });
  if (idx < 0) throw new Fail(`không có vùng gộp ${rectString(r)}`);
  d.header.merges.splice(idx, 1);
}

function applyOne(d: FormDefinition, op: FormOp): void {
  switch (op.op) {
    case "insert_column":
      return insertColumn(d, op);
    case "delete_column":
      return deleteColumn(d, op.letter);
    case "update_column": {
      const at = colIndex(d, op.letter);
      if (!op.column) throw new Fail("thiếu column");
      d.columns[at - 1] = { ...(cloneDef(op.column) as FormColumn), letter: d.columns[at - 1]!.letter };
      return;
    }
    case "insert_header_row":
      return insertHeaderRow(d, op);
    case "delete_header_row":
      return deleteHeaderRow(d, op.row ?? 0);
    case "set_header_cell":
      return setHeaderCell(d, op);
    case "merge":
      return merge(d, op.range ?? "");
    case "unmerge":
      return unmerge(d, op.range ?? "");
    case "add_indicator": {
      if (!op.indicator) throw new Fail("thiếu indicator");
      let pos = d.rows.indicators.length;
      if (op.after) {
        const idx = d.rows.indicators.findIndex((x) => x.code === op.after);
        if (idx < 0) throw new Fail(`không có chỉ tiêu ${op.after}`);
        pos = idx + 1;
      }
      d.rows.indicators.splice(pos, 0, { code: op.indicator.code ?? "", label: op.indicator.label ?? "", ...(op.indicator.catalog ? { catalog: op.indicator.catalog } : {}), ...(op.indicator.placeholder ? { placeholder: true } : {}) });
      return;
    }
    case "delete_indicator": {
      const idx = d.rows.indicators.findIndex((x) => x.code === op.code);
      if (idx < 0) throw new Fail(`không có chỉ tiêu ${op.code ?? ""}`);
      d.rows.indicators.splice(idx, 1);
      return;
    }
    case "set_rule": {
      if (!op.rule) throw new Fail("thiếu rule");
      const idx = d.rules.findIndex((r) => r.id === op.rule!.id);
      if (idx >= 0) d.rules[idx] = cloneDef(op.rule);
      else d.rules.push(cloneDef(op.rule));
      return;
    }
    case "delete_rule": {
      const idx = d.rules.findIndex((r) => r.id === op.id);
      if (idx < 0) throw new Fail(`không có ràng buộc ${op.id ?? ""}`);
      d.rules.splice(idx, 1);
      return;
    }
    case "set_meta": {
      const m = op.meta;
      if (!m) throw new Fail("thiếu meta");
      if (m.title !== undefined) d.title = m.title;
      if (m.sheet_name !== undefined) d.sheet_name = m.sheet_name;
      if (m.requirements !== undefined) d.requirements = m.requirements;
      if (m.freeze_through !== undefined) d.freeze_through = m.freeze_through.toUpperCase();
      if (m.completeness !== undefined) d.completeness = m.completeness;
      if (m.pending !== undefined) d.pending = m.pending.map((p) => ({ part: p.part ?? "", detail: p.detail ?? "", question: p.question ?? "" }));
      return;
    }
    case "set_groups":
      d.groups = (op.groups ?? []).map((g) => ({ id: g.id ?? "", title: g.title ?? "", from: g.from ?? "", to: g.to ?? "" }));
      return;
    case "set_totals":
      d.totals = (op.totals ?? []).map((t) => ({ kind: (t.kind ?? "") as "region" | "province", label: t.label ?? "", label_letter: t.label_letter ?? "" }));
      return;
    case "set_period_block": {
      if (!op.block) throw new Fail("thiếu block");
      const idx = d.period_blocks.findIndex((b) => b.id === op.block!.id);
      if (idx >= 0) d.period_blocks[idx] = { ...op.block };
      else d.period_blocks.push({ ...op.block });
      return;
    }
    default:
      throw new Fail(`thao tác không hỗ trợ ${String((op as { op: string }).op)}`);
  }
}

/** Áp lô thao tác rồi kiểm cấu trúc; lỗi → OpError (ở thao tác i) hoặc ValidationError. */
export function applyOps(def: FormDefinition, ops: FormOp[]): FormDefinition {
  const d = cloneDef(def);
  ops.forEach((op, i) => {
    try {
      applyOne(d, op);
    } catch (e) {
      if (e instanceof Fail) throw new OpError(i, e instanceof RefFail ? CODE_FORMULA_INVALID_REF : CODE_INVALID, e.message);
      throw e;
    }
  });
  validate(d);
  return d;
}
