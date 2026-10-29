// GIẢ LẬP nạp/xuất Excel lịch trực KSGM BM-03 (BF-CN-DUTY #171). Máy chủ thật dùng excelize (cntscal/app/roster_excel.go,
// cntscal/domain/bm03.go); ở đây chỉ dựng .xlsx tối giản (ZIP không nén, ô chữ inline) đúng bố cục mẫu và đọc lại tệp do
// chính nó sinh (tệp Excel thật nén deflate → giả lập báo không đọc được). Dữ liệu hư cấu.
import { columnLetter } from "@/kn/grid/letters";
import { readStoredXlsx } from "./forms/xlsx";

const enc = new TextEncoder();

let CRC: Uint32Array | null = null;
function crc32(b: Uint8Array): number {
  if (!CRC) {
    CRC = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      CRC[n] = c >>> 0;
    }
  }
  let c = 0xffffffff;
  for (let i = 0; i < b.length; i++) c = CRC[(c ^ b[i]!) & 0xff]! ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function zipStored(files: { name: string; data: Uint8Array }[]): Uint8Array {
  const parts: Uint8Array[] = [];
  const central: Uint8Array[] = [];
  let offset = 0;
  for (const f of files) {
    const name = enc.encode(f.name);
    const crc = crc32(f.data);
    const local = new Uint8Array(30 + name.length);
    const v = new DataView(local.buffer);
    v.setUint32(0, 0x04034b50, true);
    v.setUint16(4, 20, true);
    v.setUint16(6, 0x0800, true);
    v.setUint32(14, crc, true);
    v.setUint32(18, f.data.length, true);
    v.setUint32(22, f.data.length, true);
    v.setUint16(26, name.length, true);
    local.set(name, 30);
    const cd = new Uint8Array(46 + name.length);
    const w = new DataView(cd.buffer);
    w.setUint32(0, 0x02014b50, true);
    w.setUint16(4, 20, true);
    w.setUint16(6, 20, true);
    w.setUint16(8, 0x0800, true);
    w.setUint32(16, crc, true);
    w.setUint32(20, f.data.length, true);
    w.setUint32(24, f.data.length, true);
    w.setUint16(28, name.length, true);
    w.setUint32(42, offset, true);
    cd.set(name, 46);
    parts.push(local, f.data);
    central.push(cd);
    offset += local.length + f.data.length;
  }
  const size = central.reduce((s, c) => s + c.length, 0);
  const end = new Uint8Array(22);
  const e = new DataView(end.buffer);
  e.setUint32(0, 0x06054b50, true);
  e.setUint16(8, files.length, true);
  e.setUint16(10, files.length, true);
  e.setUint32(12, size, true);
  e.setUint32(16, offset, true);
  const all = [...parts, ...central, end];
  const out = new Uint8Array(all.reduce((s, c) => s + c.length, 0));
  let p = 0;
  for (const c of all) {
    out.set(c, p);
    p += c.length;
  }
  return out;
}

const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** .xlsx một sheet: mọi ô là chữ inline. */
export function xlsxOf(sheetName: string, rows: (string | null)[][], merges: string[]): Uint8Array {
  const body = rows
    .map((cells, ri) => `<row r="${ri + 1}">${cells.map((v, ci) => (v ? `<c r="${columnLetter(ci + 1)}${ri + 1}" t="inlineStr"><is><t xml:space="preserve">${esc(v)}</t></is></c>` : "")).join("")}</row>`)
    .join("");
  const mg = merges.length ? `<mergeCells count="${merges.length}">${merges.map((m) => `<mergeCell ref="${m}"/>`).join("")}</mergeCells>` : "";
  const x = (s: string) => enc.encode(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>${s}`);
  return zipStored([
    { name: "[Content_Types].xml", data: x(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`) },
    { name: "_rels/.rels", data: x(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`) },
    { name: "xl/workbook.xml", data: x(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="${esc(sheetName.slice(0, 31))}" sheetId="1" r:id="rId1"/></sheets></workbook>`) },
    { name: "xl/_rels/workbook.xml.rels", data: x(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`) },
    { name: "xl/worksheets/sheet1.xml", data: x(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>${body}</sheetData>${mg}</worksheet>`) },
  ]);
}

/** Đọc lại tệp do xlsxOf sinh thành bảng chuỗi (dòng × cột). null = không đọc được (tệp nén/không phải .xlsx). */
export function rowsOfXlsx(b: Uint8Array): { sheet: string; rows: string[][] } | null {
  const p = readStoredXlsx(b);
  if (!p) return null;
  const rows: string[][] = [];
  for (const [ref, v] of p.cells) {
    const m = /^([A-Z]+)(\d+)$/.exec(ref);
    if (!m) continue;
    let col = 0;
    for (const ch of m[1]!) col = col * 26 + (ch.charCodeAt(0) - 64);
    const r = Number(m[2]) - 1;
    if (r > 6000 || col > 40) continue;
    while (rows.length <= r) rows.push([]);
    const row = rows[r]!;
    while (row.length < col) row.push("");
    row[col - 1] = v.text ?? "";
  }
  return { sheet: p.sheet, rows };
}
