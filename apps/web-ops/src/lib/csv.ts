// CSV theo RFC 4180 (như encoding/csv của máy chủ): ô chứa dấu phẩy, dấu nháy kép hoặc xuống dòng được bọc trong "…",
// dấu nháy bên trong nhân đôi. Dùng cho tệp mẫu tải xuống và bộ đọc CSV của giả lập để hai phía hiểu cùng một tệp.

/** Một ô CSV: bọc "…" khi chứa , " CR LF; nhân đôi dấu nháy bên trong. */
export function csvCell(v: string | number | null | undefined): string {
  const s = v === null || v === undefined ? "" : String(v);
  return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** Các dòng → CSV phân cách dấu phẩy, xuống dòng CRLF (có CRLF cuối). Không thêm BOM. */
export function toCsvText(rows: readonly (readonly (string | number | null | undefined)[])[]): string {
  return rows.map((r) => r.map(csvCell).join(",")).join("\r\n") + "\r\n";
}

/** Đọc CSV (bỏ BOM, ô trong "…" được chứa dấu phẩy/xuống dòng, "" = "). Bỏ dòng trống; cắt khoảng trắng hai đầu ô. */
export function parseCsvText(text: string): string[][] {
  const src = text.replace(/^﻿/, "");
  const rows: string[][] = [];
  let row: string[] = [];
  let cur = "";
  let quoted = false;
  const endCell = () => {
    row.push(cur.trim());
    cur = "";
  };
  const endRow = () => {
    endCell();
    if (row.some((c) => c !== "")) rows.push(row);
    row = [];
  };
  for (let i = 0; i < src.length; i++) {
    const ch = src[i];
    if (quoted) {
      if (ch === '"' && src[i + 1] === '"') {
        cur += '"';
        i++;
      } else if (ch === '"') quoted = false;
      else cur += ch;
    } else if (ch === '"') quoted = true;
    else if (ch === ",") endCell();
    else if (ch === "\r" && src[i + 1] === "\n") {
      endRow();
      i++;
    } else if (ch === "\n" || ch === "\r") endRow();
    else cur += ch;
  }
  if (cur !== "" || row.length) endRow();
  return rows;
}
