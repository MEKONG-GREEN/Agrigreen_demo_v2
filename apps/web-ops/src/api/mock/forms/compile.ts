// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của reports/form/compile.go: kiểm cấu trúc định nghĩa lưới.
import { Decimal } from "@/kn/grid/decimal";
import { columnIndex, columnLetter, parseRect, rectsOverlap, rectString, type Rect } from "@/kn/grid/letters";
import type { FormColumn, FormDefinition, FormIndicator, FormRule } from "@/kn/forms/types";
import { parse, refs, usesXRef, type Node } from "./formula";

export const CODE_INVALID = "reports.form_invalid";
export const CODE_FORMULA_INVALID = "reports.formula_invalid";
export const CODE_FORMULA_INVALID_REF = "reports.formula_invalid_ref";
export const CODE_FORMULA_CYCLE = "reports.formula_cycle";

export const MAX_COLUMNS = 2000;
export const MAX_HEADER_ROWS = 20;
const MAX_INDICATORS = 500;
const MAX_RULES = 200;
const MAX_TEXT = 4000;

export interface Issue {
  field: string;
  code: string;
  msg: string;
}

export class ValidationError extends Error {
  constructor(readonly issues: Issue[]) {
    super(`định nghĩa mẫu không hợp lệ: ${issues.map((i) => `${i.field}: ${i.msg}`).join("; ")}`);
  }
  get code(): string {
    for (const want of [CODE_FORMULA_INVALID_REF, CODE_FORMULA_CYCLE, CODE_FORMULA_INVALID]) {
      if (this.issues.some((i) => i.code === want)) return want;
    }
    return CODE_INVALID;
  }
}

const CODE_RE = /^[A-Z0-9][A-Z0-9_.-]{1,31}$/;
export const validCode = (c: string) => CODE_RE.test(c);

const runeLen = (s: string) => Array.from(s).length;

export interface CompiledRule {
  r: FormRule;
  expr: Node;
  when: Node | null;
}

export interface Compiled {
  def: FormDefinition;
  idx: Map<string, number>;
  progs: Map<number, Node>;
  order: number[];
  rules: CompiledRule[];
  indicators: Map<string, FormIndicator>;
  slotOf: Map<number, [number, number]>;
}

const DTYPES = ["number", "integer", "text", "date", "catalog"];
export const numericDType = (d: string) => d === "number" || d === "integer";

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function compile(def: FormDefinition): Compiled {
  const issues: Issue[] = [];
  const add = (field: string, code: string, msg: string) => issues.push({ field, code, msg });
  const c: Compiled = { def, idx: new Map(), progs: new Map(), order: [], rules: [], indicators: new Map(), slotOf: new Map() };
  if (def.schema !== "form-template.v1") add("schema", CODE_INVALID, `phải là "form-template.v1"`);
  if (!validCode(def.code)) add("code", CODE_INVALID, "mã mẫu chữ hoa, số, . _ -; 2–32 ký tự (vd T01)");
  const tl = runeLen((def.title ?? "").trim());
  if (tl < 1 || tl > 300) add("title", CODE_INVALID, "bắt buộc, tối đa 300 ký tự");
  if (runeLen(def.sheet_name ?? "") > 100) add("sheet_name", CODE_INVALID, "tối đa 100 ký tự");
  if (runeLen(def.requirements ?? "") > MAX_TEXT) add("requirements", CODE_INVALID, `tối đa ${MAX_TEXT} ký tự`);
  if (!["complete", "partial", "placeholder"].includes(def.completeness)) add("completeness", CODE_INVALID, "complete | partial | placeholder");
  const cols = def.columns ?? [];
  const n = cols.length;
  if (n < 1 || n > MAX_COLUMNS) add("columns", CODE_INVALID, `cần 1–${MAX_COLUMNS} cột`);
  cols.forEach((col, i) => {
    const want = columnLetter(i + 1);
    if (col.letter !== want) {
      add(`columns[${i}].letter`, CODE_INVALID, `cột phải liên tục từ A: muốn ${want}, nhận ${JSON.stringify(col.letter)}`);
      return;
    }
    c.idx.set(col.letter, i + 1);
  });
  if (issues.length) throw new ValidationError(issues);
  const letterOK = (l: string) => c.idx.has(l.toUpperCase());

  // --- cột
  const groups = new Set((def.groups ?? []).map((g) => g.id));
  cols.forEach((col: FormColumn, i) => {
    const f = `columns[${i}]`;
    if (runeLen(col.title ?? "") > 500) add(`${f}.title`, CODE_INVALID, "tối đa 500 ký tự");
    if (!DTYPES.includes(col.dtype)) add(`${f}.dtype`, CODE_INVALID, "number | integer | text | date | catalog");
    if (col.dtype === "catalog" && (col.catalog ?? "").trim() === "") add(`${f}.catalog`, CODE_INVALID, "cột danh mục cần mã danh mục");
    if (col.total && !["sum", "formula", "none"].includes(col.total)) add(`${f}.total`, CODE_INVALID, "sum | formula | none");
    for (const [name, p] of [
      ["min", col.min],
      ["max", col.max],
    ] as const) {
      if (p === undefined || p === null) continue;
      if (!Decimal.parse(p) || !numericDType(col.dtype)) add(`${f}.${name}`, CODE_INVALID, "giới hạn là số, chỉ cho cột số");
    }
    if (col.decimals !== undefined && col.decimals !== null && (col.decimals < 0 || col.decimals > 10)) add(`${f}.decimals`, CODE_INVALID, "0–10");
    if (col.group && !groups.has(col.group)) add(`${f}.group`, CODE_INVALID, `nhóm ${JSON.stringify(col.group)} không có trong groups`);
    if (col.kind !== "formula" && col.formula) add(`${f}.formula`, CODE_INVALID, "chỉ cột loại công thức có công thức");
    if (col.kind !== "fixed" && col.fixed_source) add(`${f}.fixed_source`, CODE_INVALID, "chỉ cột cố định có nguồn hệ thống");
    const inBlock = !!col.period_block || !!col.period_slot;
    if (inBlock && col.kind !== "per_period" && col.kind !== "formula") add(`${f}.period_block`, CODE_INVALID, "chỉ cột theo kỳ (hoặc công thức trong khối kỳ) thuộc khối kỳ");
    if (inBlock && (!col.period_block || (col.period_slot ?? 0) < 1)) add(`${f}.period_slot`, CODE_INVALID, "cột trong khối kỳ cần period_block và period_slot ≥ 1");
    switch (col.kind) {
      case "fixed": {
        const src = col.fixed_source ?? "";
        if (["row_no", "region_name", "commune_name", "indicator_label", "none"].includes(src)) break;
        if (src.startsWith("xref:")) {
          const parts = src.slice(5).split(":");
          if (parts.length !== 2 || !validCode(parts[0] ?? "") || columnIndex(parts[1] ?? "") === 0) add(`${f}.fixed_source`, CODE_INVALID, "dạng xref:<mã mẫu>:<cột>, vd xref:T01:M");
          break;
        }
        add(`${f}.fixed_source`, CODE_INVALID, "row_no | region_name | commune_name | indicator_label | none | xref:<mẫu>:<cột>");
        break;
      }
      case "provincial":
      case "input":
        break;
      case "per_period":
        if (!inBlock) add(`${f}.period_slot`, CODE_INVALID, "cột theo kỳ cần period_block và period_slot ≥ 1");
        break;
      case "formula": {
        let p: Node;
        try {
          p = parse(col.formula ?? "");
        } catch (e) {
          add(`${f}.formula`, CODE_FORMULA_INVALID, errMsg(e));
          break;
        }
        if (usesXRef(p)) {
          add(`${f}.formula`, CODE_FORMULA_INVALID, "XREF chỉ dùng trong ràng buộc của mẫu, không trong công thức cột");
          break;
        }
        const bad = refs(p).find((r) => r > n);
        if (bad !== undefined) add(`${f}.formula`, CODE_FORMULA_INVALID_REF, `tham chiếu ô không tồn tại: cột ${columnLetter(bad)}`);
        else c.progs.set(i + 1, p);
        break;
      }
      default:
        add(`${f}.kind`, CODE_INVALID, "fixed | provincial | input | per_period | formula");
    }
  });
  const gids = new Set<string>();
  (def.groups ?? []).forEach((g, i) => {
    const f = `groups[${i}]`;
    if (!g.id || gids.has(g.id)) add(`${f}.id`, CODE_INVALID, "id bắt buộc, không trùng");
    gids.add(g.id);
    if (!letterOK(g.from) || !letterOK(g.to) || columnIndex(g.from) > columnIndex(g.to)) add(f, CODE_INVALID, "from ≤ to là cột có trong mẫu");
  });
  if (def.freeze_through && !letterOK(def.freeze_through)) add("freeze_through", CODE_INVALID, "cột không tồn tại");

  // --- khối kỳ
  const ids = new Set<string>();
  (def.period_blocks ?? []).forEach((b, i) => {
    const f = `period_blocks[${i}]`;
    if (!b.id || ids.has(b.id)) {
      add(`${f}.id`, CODE_INVALID, "id bắt buộc, không trùng");
      return;
    }
    ids.add(b.id);
    const first = columnIndex(b.first_letter);
    if (first === 0 || b.width < 1 || b.slots < 1 || first + b.width * b.slots - 1 > n) {
      add(f, CODE_INVALID, "first_letter, width ≥ 1, slots ≥ 1 phải nằm trong các cột của mẫu");
      return;
    }
    for (let j = 0; j < b.width * b.slots; j++) {
      const col = cols[first - 1 + j]!;
      const slot = 1 + Math.floor(j / b.width);
      if ((col.kind !== "per_period" && col.kind !== "formula") || col.period_block !== b.id || col.period_slot !== slot) {
        add(f, CODE_INVALID, `cột ${col.letter} phải là cột theo kỳ (hoặc công thức) của khối ${b.id}, kỳ ${slot}`);
        break;
      }
      c.slotOf.set(first + j, [slot, j % b.width]);
    }
  });
  cols.forEach((col, i) => {
    if ((col.kind === "per_period" || col.period_block) && !c.slotOf.has(i + 1)) add(`columns[${i}].period_block`, CODE_INVALID, "cột theo kỳ không thuộc khối kỳ nào hợp lệ");
  });

  // --- tiêu đề
  const h = def.header;
  if (!h || h.rows < 1 || h.rows > MAX_HEADER_ROWS) {
    add("header.rows", CODE_INVALID, `1–${MAX_HEADER_ROWS} dòng tiêu đề`);
  } else {
    const seen = new Set<string>();
    (h.cells ?? []).forEach((cell, i) => {
      const f = `header.cells[${i}]`;
      const ci = columnIndex(cell.letter);
      if (cell.row < 1 || cell.row > h.rows || ci === 0 || ci > n) {
        add(f, CODE_INVALID, `ô tiêu đề ngoài khung (dòng 1–${h.rows}, cột A–${columnLetter(n)})`);
        return;
      }
      const k = `${ci}:${cell.row}`;
      if (seen.has(k)) add(f, CODE_INVALID, `trùng ô ${cell.letter}${cell.row}`);
      seen.add(k);
      if (runeLen(cell.text ?? "") > 500) add(`${f}.text`, CODE_INVALID, "tối đa 500 ký tự");
    });
    const rects: Rect[] = [];
    (h.merges ?? []).forEach((m, i) => {
      const f = `header.merges[${i}]`;
      const r = parseRect(m);
      if (!r) {
        add(f, CODE_INVALID, "vùng gộp dạng A1:B2");
        return;
      }
      if (r.r2 > h.rows || r.c2 > n) {
        add(f, CODE_INVALID, `vùng gộp ${m} ngoài khung tiêu đề`);
        return;
      }
      if (r.c1 === r.c2 && r.r1 === r.r2) {
        add(f, CODE_INVALID, `vùng gộp ${m} chỉ có một ô`);
        return;
      }
      for (const o of rects) if (rectsOverlap(r, o)) add(f, CODE_INVALID, `vùng gộp ${m} chồng lên ${rectString(o)}`);
      rects.push(r);
    });
  }

  // --- dòng
  if (def.rows?.kind !== "per_commune") add("rows.kind", CODE_INVALID, "per_commune");
  const inds = def.rows?.indicators ?? [];
  if (inds.length > MAX_INDICATORS) add("rows.indicators", CODE_INVALID, `tối đa ${MAX_INDICATORS} chỉ tiêu`);
  inds.forEach((ind, i) => {
    const f = `rows.indicators[${i}]`;
    if (!ind.code || ind.code.length > 64 || /[/ ]/.test(ind.code)) {
      add(`${f}.code`, CODE_INVALID, "mã bắt buộc, ≤ 64 ký tự, không có / hay khoảng trắng");
      return;
    }
    if (c.indicators.has(ind.code)) add(`${f}.code`, CODE_INVALID, `trùng mã ${ind.code}`);
    c.indicators.set(ind.code, ind);
  });
  (def.totals ?? []).forEach((t, i) => {
    const f = `totals[${i}]`;
    if (t.kind !== "region" && t.kind !== "province") add(`${f}.kind`, CODE_INVALID, "region | province");
    if (!letterOK(t.label_letter ?? "")) add(`${f}.label_letter`, CODE_INVALID, "cột không tồn tại");
  });

  // --- ràng buộc
  const rules = def.rules ?? [];
  if (rules.length > MAX_RULES) add("rules", CODE_INVALID, `tối đa ${MAX_RULES} ràng buộc`);
  const rids = new Set<string>();
  rules.forEach((r, i) => {
    const f = `rules[${i}]`;
    if (!r.id || rids.has(r.id)) add(`${f}.id`, CODE_INVALID, "id bắt buộc, không trùng");
    rids.add(r.id);
    if (r.severity !== "error" && r.severity !== "warning") add(`${f}.severity`, CODE_INVALID, "error | warning");
    if ((r.message ?? "").trim() === "") add(`${f}.message`, CODE_INVALID, "bắt buộc");
    let expr: Node | null = null;
    let when: Node | null = null;
    for (const [name, src] of [
      ["expr", r.expr ?? ""],
      ["when", r.when ?? ""],
    ] as const) {
      if (src === "" && name === "when") continue;
      let p: Node;
      try {
        p = parse(src);
      } catch (e) {
        add(`${f}.${name}`, CODE_FORMULA_INVALID, errMsg(e));
        continue;
      }
      for (const ref of refs(p)) if (ref > n) add(`${f}.${name}`, CODE_FORMULA_INVALID_REF, `tham chiếu ô không tồn tại: cột ${columnLetter(ref)}`);
      if (name === "expr") expr = p;
      else when = p;
    }
    for (const l of r.letters ?? []) if (!letterOK(l)) add(`${f}.letters`, CODE_INVALID, `cột ${JSON.stringify(l)} không tồn tại`);
    if (expr) c.rules.push({ r, expr, when });
  });

  // --- thứ tự tính công thức
  const state = new Map<number, number>();
  const order: number[] = [];
  const visit = (col: number): boolean => {
    const s = state.get(col) ?? 0;
    if (s === 1) return false;
    if (s === 2) return true;
    state.set(col, 1);
    for (const r of refs(c.progs.get(col)!)) {
      if (c.progs.has(r) && !visit(r)) return false;
    }
    state.set(col, 2);
    order.push(col);
    return true;
  };
  for (const col of [...c.progs.keys()].sort((a, b) => a - b)) {
    if (!visit(col)) {
      add(`columns[${col - 1}].formula`, CODE_FORMULA_CYCLE, `công thức cột ${columnLetter(col)} tham chiếu vòng`);
      break;
    }
  }
  c.order = order;
  if (issues.length) throw new ValidationError(issues);
  return c;
}

export function validate(def: FormDefinition): void {
  compile(def);
}
