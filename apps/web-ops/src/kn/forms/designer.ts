// Soạn lô thao tác của trình thiết kế (16 op của POST …/versions/{v}/ops) — thuần, kiểm bằng Vitest.
// Máy chủ áp lô (cả lô hoặc không gì), dời công thức/vùng gộp như Excel và kiểm cấu trúc; web chỉ soạn lô.
import { columnIndex, columnLetter, parseRect, type Rect } from "@/kn/grid/letters";
import type { FormColumn, FormDefinition, FormHeaderCell, FormOp, FormPeriodBlock } from "./types";

/** Cột mới mặc định khi "+ Cột trước/sau". */
export function newColumn(title = "Cột mới"): FormColumn {
  return { letter: "", title, kind: "input", dtype: "number" };
}

export function insertColumnOps(at: string, side: "before" | "after", col: FormColumn = newColumn()): FormOp[] {
  return [{ op: "insert_column", at, side, column: col }];
}

/** Vùng gộp chứa ô (dòng, cột) của khung tiêu đề. */
export function mergeAt(def: FormDefinition, row: number, col: number): string | null {
  for (const m of def.header.merges) {
    const r = parseRect(m);
    if (r && row >= r.r1 && row <= r.r2 && col >= r.c1 && col <= r.c2) return m;
  }
  return null;
}

/** Vùng chọn tiêu đề mở rộng (nhấp Shift) — chuẩn hóa góc. */
export function extendRect(anchor: { row: number; col: number }, to: { row: number; col: number }): Rect {
  return { c1: Math.min(anchor.col, to.col), r1: Math.min(anchor.row, to.row), c2: Math.max(anchor.col, to.col), r2: Math.max(anchor.row, to.row) };
}

/** Dời tham chiếu "F#" ≥ at thêm delta (chèn cột) — như reports/formula.Shift khi chèn. */
export function shiftRefsText(formula: string, at: number, delta: number): string {
  return formula.replace(/(\$?)([A-Z]{1,3})#/g, (m, dollar: string, letters: string) => {
    const i = columnIndex(letters);
    return i >= at ? `${dollar}${columnLetter(i + delta)}#` : m;
  });
}

/** Đổi chỗ tham chiếu cột a ↔ b trong công thức (dải chứa cả hai giữ nguyên; dải có đầu mút ở a/b mà không chứa cả hai → null). */
function swapRefsText(formula: string, a: number, b: number): string | null {
  for (const m of formula.matchAll(/([A-Z]{1,3})#:([A-Z]{1,3})#/g)) {
    const f = columnIndex(m[1] ?? "");
    const t = columnIndex(m[2] ?? "");
    const lo = Math.min(f, t);
    const hi = Math.max(f, t);
    const hasA = a >= lo && a <= hi;
    const hasB = b >= lo && b <= hi;
    if (hasA !== hasB) return null;
  }
  const ranges: string[] = [];
  const masked = formula.replace(/([A-Z]{1,3})#:([A-Z]{1,3})#/g, (m) => {
    ranges.push(m);
    return `\u0000${ranges.length - 1}\u0000`;
  });
  const swapped = masked.replace(/(\$?)([A-Z]{1,3})#/g, (m, dollar: string, letters: string) => {
    const i = columnIndex(letters);
    return i === a ? `${dollar}${columnLetter(b)}#` : i === b ? `${dollar}${columnLetter(a)}#` : m;
  });
  return swapped.replace(/\u0000(\d+)\u0000/g, (_m, k: string) => ranges[Number(k)] ?? "");
}

export type MoveResult = { ops: FormOp[] } | { error: string };

/**
 * Chuyển cột sang trái/phải một vị trí (đổi chỗ với cột bên cạnh) bằng các op có sẵn: hai update_column, sửa
 * công thức/ràng buộc tham chiếu hai cột, đổi chữ ô tiêu đề. Từ chối khi cột thuộc khối kỳ, vùng gộp tiêu đề
 * của hai cột khác nhau, hoặc một dải công thức có đầu mút ở cột chuyển (như Excel sẽ đổi nghĩa dải).
 */
export function moveColumnOps(def: FormDefinition, letter: string, dir: -1 | 1): MoveResult {
  const a = columnIndex(letter);
  const b = a + dir;
  if (a < 1 || b < 1 || b > def.columns.length) return { error: "Không còn cột bên cạnh để chuyển." };
  const ca = def.columns[a - 1]!;
  const cb = def.columns[b - 1]!;
  if (ca.period_block || cb.period_block) return { error: "Cột thuộc khối cột theo kỳ — đổi số kỳ bằng “Khối kỳ”, không chuyển lẻ." };
  const shape = (c: number) =>
    def.header.merges
      .map((m) => parseRect(m))
      .filter((r): r is Rect => !!r && c >= r.c1 && c <= r.c2 && !(Math.min(a, b) >= r.c1 && Math.max(a, b) <= r.c2))
      .map((r) => (r.c1 === r.c2 ? `${r.r1}-${r.r2}` : "wide"));
  const sa = shape(a);
  const sb = shape(b);
  if (sa.includes("wide") || sb.includes("wide") || sa.join() !== sb.join()) return { error: "Vùng gộp tiêu đề của hai cột khác nhau — tách ô trước khi chuyển cột." };
  const ops: FormOp[] = [];
  const fx = (c: FormColumn): FormColumn | null => {
    if (!c.formula) return c;
    const f = swapRefsText(c.formula, a, b);
    return f === null ? null : { ...c, formula: f };
  };
  const na = fx({ ...cb, letter: ca.letter });
  const nb = fx({ ...ca, letter: cb.letter });
  if (!na || !nb) return { error: "Công thức có dải ô bắt đầu hoặc kết thúc ở cột này — sửa công thức trước khi chuyển." };
  ops.push({ op: "update_column", letter: ca.letter, column: na }, { op: "update_column", letter: cb.letter, column: nb });
  for (const c of def.columns) {
    if (c.letter === ca.letter || c.letter === cb.letter || !c.formula) continue;
    const f = swapRefsText(c.formula, a, b);
    if (f === null) return { error: `Công thức cột ${c.letter} có dải ô bắt đầu hoặc kết thúc ở cột chuyển — sửa trước.` };
    if (f !== c.formula) ops.push({ op: "update_column", letter: c.letter, column: { ...c, formula: f } });
  }
  for (const r of def.rules) {
    const expr = swapRefsText(r.expr, a, b);
    const when = r.when ? swapRefsText(r.when, a, b) : r.when;
    if (expr === null || when === null) return { error: `Ràng buộc ${r.id} có dải ô ở biên cột chuyển — sửa trước.` };
    const letters = r.letters.map((l) => (columnIndex(l) === a ? cb.letter : columnIndex(l) === b ? ca.letter : l));
    if (expr !== r.expr || when !== r.when || letters.join() !== r.letters.join()) ops.push({ op: "set_rule", rule: { ...r, expr, ...(when ? { when } : {}), letters } });
  }
  for (let row = 1; row <= def.header.rows; row++) {
    const ta = def.header.cells.find((h) => h.row === row && h.letter === ca.letter)?.text ?? "";
    const tb = def.header.cells.find((h) => h.row === row && h.letter === cb.letter)?.text ?? "";
    if (ta !== tb) ops.push({ op: "set_header_cell", row, letter: ca.letter, text: tb }, { op: "set_header_cell", row, letter: cb.letter, text: ta });
  }
  return { ops };
}

/** Chuyển dòng chỉ tiêu lên/xuống một vị trí (delete_indicator + add_indicator sau dòng kia). */
export function moveIndicatorOps(def: FormDefinition, code: string, dir: -1 | 1): FormOp[] {
  const list = def.rows.indicators;
  const i = list.findIndex((x) => x.code === code);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= list.length) return [];
  // Đổi chỗ i và j = đưa dòng trên (min) xuống sau dòng dưới (max).
  const upper = list[Math.min(i, j)]!;
  const lower = list[Math.max(i, j)]!;
  return [
    { op: "delete_indicator", code: upper.code },
    { op: "add_indicator", indicator: { ...upper }, after: lower.code },
  ];
}

export interface SlotChange {
  ops: FormOp[];
  /** Sau khi máy chủ áp lô (tăng số kỳ): sửa khung tiêu đề cho khe mới (chép ô/vùng gộp của khe cũ). */
  fixHeader?: (def: FormDefinition) => FormDefinition | null;
}

/**
 * Đổi số kỳ của một khối cột theo kỳ (FN-TI-02.02: số cột = số kỳ đã chia).
 * Tăng n → n+1: chèn width cột TRƯỚC khe cuối (dải SUM, vùng gộp nở ra như Excel), cột mới là khe n, khe cũ
 * dời sang phải thành khe n+1. Giảm n → n−1: đổi các cột khe cuối thành cột nhập rồi xóa (máy chủ báo lỗi nếu
 * công thức khác còn tham chiếu — reports.formula_invalid_ref).
 */
export function periodSlotChange(def: FormDefinition, blockId: string, slots: number, titlePattern: string): SlotChange {
  const b = def.period_blocks.find((x) => x.id === blockId);
  if (!b) return { ops: [] };
  const block: FormPeriodBlock = { ...b, slots, title_pattern: titlePattern };
  const w = b.width;
  const f = columnIndex(b.first_letter);
  const n = b.slots;
  if (slots === n) return { ops: [{ op: "set_period_block", block }] };
  const s = f + (n - 1) * w; // cột đầu của khe cuối
  const lastCols = def.columns.slice(s - 1, s - 1 + w);
  const ops: FormOp[] = [{ op: "set_period_block", block }];
  if (slots === n + 1) {
    for (let k = 0; k < w; k++) ops.push({ op: "insert_column", at: columnLetter(s + k), side: "before", column: newColumn() });
    lastCols.forEach((c, k) => {
      ops.push({ op: "update_column", letter: columnLetter(s + k), column: { ...c, letter: "", period_slot: n } });
    });
    lastCols.forEach((c, k) => {
      const moved: FormColumn = { ...c, letter: "", period_slot: n + 1, key: c.key ? c.key.replace(/(\d+)$/, String(n + 1)) : undefined };
      if (!moved.key) delete moved.key;
      if (moved.formula) moved.formula = shiftRefsText(moved.formula, s, w);
      if (w === 1) moved.title = titlePattern.replaceAll("{n}", String(n + 1));
      ops.push({ op: "update_column", letter: columnLetter(s + w + k), column: moved });
    });
    const fixHeader = (d: FormDefinition): FormDefinition | null => {
      const from = s + w;
      const to = s + 2 * w - 1;
      const cells: FormHeaderCell[] = [];
      let changed = false;
      for (const hc of d.header.cells) {
        const ci = columnIndex(hc.letter);
        if (ci >= from && ci <= to) {
          const copy: FormHeaderCell = { ...hc, letter: columnLetter(ci - w) };
          if (hc.period_block === blockId) {
            copy.period_slot = n;
            const label = titlePattern.replaceAll("{n}", String(n + 1));
            cells.push({ ...hc, period_slot: n + 1, text: hc.period_slot === n ? label : hc.text });
          } else cells.push(hc);
          cells.push(copy);
          changed = true;
        } else cells.push(hc);
      }
      const merges = [...d.header.merges];
      for (const m of d.header.merges) {
        const r = parseRect(m);
        if (r && r.c1 >= from && r.c2 <= to) {
          merges.push(`${columnLetter(r.c1 - w)}${r.r1}:${columnLetter(r.c2 - w)}${r.r2}`);
          changed = true;
        }
      }
      return changed ? { ...d, header: { ...d.header, cells, merges } } : null;
    };
    return { ops, fixHeader };
  }
  if (slots === n - 1 && n > 1) {
    lastCols.forEach((c) => ops.push({ op: "update_column", letter: c.letter, column: { letter: c.letter, title: c.title, kind: "input", dtype: c.dtype } }));
    for (let k = w - 1; k >= 0; k--) ops.push({ op: "delete_column", letter: columnLetter(s + k) });
    return { ops };
  }
  return { ops: [] };
}

/** Thay {season} cho xem trước (nhãn hiển thị; máy chủ làm việc này ở /layout). */
export function withSeason(def: FormDefinition, season: string): FormDefinition {
  const rep = (t: string) => t.replaceAll("{season}", season);
  return {
    ...def,
    title: rep(def.title),
    columns: def.columns.map((c) => ({ ...c, title: rep(c.title) })),
    header: { ...def.header, cells: def.header.cells.map((c) => ({ ...c, text: rep(c.text) })) },
  };
}
