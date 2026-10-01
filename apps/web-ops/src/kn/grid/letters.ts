// Chữ cái cột Excel ↔ số thứ tự (từ 1), như reports/formula.ColumnIndex/ColumnLetter của máy chủ.

export const MAX_COLUMN = 16384; // XFD

/** "A" → 1, "HR" → 226; sai → 0. Bỏ "$" đầu, không phân biệt hoa thường. */
export function columnIndex(letter: string): number {
  let s = letter.trim().toUpperCase();
  if (s.startsWith("$")) s = s.slice(1);
  if (s === "" || s.length > 3) return 0;
  let n = 0;
  for (const ch of s) {
    const c = ch.charCodeAt(0);
    if (c < 65 || c > 90) return 0;
    n = n * 26 + (c - 64);
  }
  return n > MAX_COLUMN ? 0 : n;
}

/** 1 → "A", 226 → "HR"; ngoài khoảng → "". */
export function columnLetter(n: number): string {
  if (n < 1 || n > MAX_COLUMN) return "";
  let out = "";
  let x = n;
  while (x > 0) {
    x--;
    out = String.fromCharCode(65 + (x % 26)) + out;
    x = Math.floor(x / 26);
  }
  return out;
}

export interface Rect {
  c1: number;
  r1: number;
  c2: number;
  r2: number;
}

const CELL_RE = /^([A-Z]{1,3})([1-9][0-9]*)$/;

function parseCell(s: string): [number, number] | null {
  const m = CELL_RE.exec(s.trim().toUpperCase());
  if (!m) return null;
  const col = columnIndex(m[1] ?? "");
  const row = Number.parseInt(m[2] ?? "", 10);
  return col === 0 || !Number.isFinite(row) ? null : [col, row];
}

/** "F1:L1" → vùng (cột, dòng từ 1, bao gồm), tự đảo nếu ngược. Sai dạng → null. */
export function parseRect(s: string): Rect | null {
  const i = s.indexOf(":");
  if (i < 0) return null;
  const a = parseCell(s.slice(0, i));
  const b = parseCell(s.slice(i + 1));
  if (!a || !b) return null;
  return { c1: Math.min(a[0], b[0]), r1: Math.min(a[1], b[1]), c2: Math.max(a[0], b[0]), r2: Math.max(a[1], b[1]) };
}

export function rectString(r: Rect): string {
  return `${columnLetter(r.c1)}${r.r1}:${columnLetter(r.c2)}${r.r2}`;
}

export function rectsOverlap(a: Rect, b: Rect): boolean {
  return a.c1 <= b.c2 && b.c1 <= a.c2 && a.r1 <= b.r2 && b.r1 <= a.r2;
}
