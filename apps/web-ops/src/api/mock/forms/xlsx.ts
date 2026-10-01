// Tệp .xlsx tối giản (ZIP không nén + SpreadsheetML) cho backend giả lập: "Tải mẫu Excel" dựng đúng bố cục
// tiêu đề gộp, hàng chữ cái, dòng loại cột, dòng dữ liệu có công thức. Máy chủ thật dùng excelize
// (reports/formimport.Export). Bộ đọc lại (readStoredXlsx) chỉ hiểu tệp ZIP không nén do chính nó sinh.
import { columnIndex, columnLetter, parseRect } from "@/kn/grid/letters";
import type { FormDefinition } from "@/kn/forms/types";
import { KIND_SHORT } from "@/kn/forms/types";

const enc = new TextEncoder();
const dec = new TextDecoder();

let CRC_TABLE: Uint32Array | null = null;
function crc32(b: Uint8Array): number {
  if (!CRC_TABLE) {
    CRC_TABLE = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      CRC_TABLE[n] = c >>> 0;
    }
  }
  let c = 0xffffffff;
  for (let i = 0; i < b.length; i++) c = CRC_TABLE[(c ^ b[i]!) & 0xff]! ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function zipStored(files: { name: string; data: Uint8Array }[]): Uint8Array {
  const chunks: Uint8Array[] = [];
  const central: Uint8Array[] = [];
  let offset = 0;
  for (const f of files) {
    const name = enc.encode(f.name);
    const crc = crc32(f.data);
    const local = new Uint8Array(30 + name.length);
    const v = new DataView(local.buffer);
    v.setUint32(0, 0x04034b50, true);
    v.setUint16(4, 20, true);
    v.setUint16(6, 0x0800, true); // UTF-8
    v.setUint16(8, 0, true); // stored
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
    chunks.push(local, f.data);
    central.push(cd);
    offset += local.length + f.data.length;
  }
  const cdSize = central.reduce((s, c) => s + c.length, 0);
  const end = new Uint8Array(22);
  const e = new DataView(end.buffer);
  e.setUint32(0, 0x06054b50, true);
  e.setUint16(8, files.length, true);
  e.setUint16(10, files.length, true);
  e.setUint32(12, cdSize, true);
  e.setUint32(16, offset, true);
  const all = [...chunks, ...central, end];
  const out = new Uint8Array(all.reduce((s, c) => s + c.length, 0));
  let p = 0;
  for (const c of all) {
    out.set(c, p);
    p += c.length;
  }
  return out;
}

const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** Sheet XML: ô chữ inline, ô công thức <f>. */
function sheetXml(rows: (string | { f: string } | null)[][], merges: string[]): string {
  const body = rows
    .map((cells, ri) => {
      const r = ri + 1;
      const cs = cells
        .map((v, ci) => {
          if (v === null || v === "") return "";
          const ref = `${columnLetter(ci + 1)}${r}`;
          if (typeof v === "object") return `<c r="${ref}"><f>${esc(v.f)}</f></c>`;
          return `<c r="${ref}" t="inlineStr"><is><t xml:space="preserve">${esc(v)}</t></is></c>`;
        })
        .join("");
      return `<row r="${r}">${cs}</row>`;
    })
    .join("");
  const mg = merges.length ? `<mergeCells count="${merges.length}">${merges.map((m) => `<mergeCell ref="${m}"/>`).join("")}</mergeCells>` : "";
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>${body}</sheetData>${mg}</worksheet>`;
}

/** Mẫu Excel đúng bố cục: tiêu đề gộp (dòng 1…n), hàng chữ cái, dòng loại cột, một dòng dữ liệu mẫu. */
export function exportXlsx(def: FormDefinition): Uint8Array {
  const n = def.columns.length;
  const H = def.header.rows;
  const rows: (string | { f: string } | null)[][] = [];
  for (let r = 1; r <= H; r++) rows.push(new Array(n).fill(null));
  for (const c of def.header.cells) {
    const ci = columnIndex(c.letter);
    if (ci >= 1 && ci <= n && c.row >= 1 && c.row <= H) rows[c.row - 1]![ci - 1] = c.text;
  }
  rows.push(def.columns.map((c) => c.letter));
  rows.push(def.columns.map((c) => KIND_SHORT[c.kind]));
  const dataRow = H + 3;
  rows.push(def.columns.map((c) => (c.kind === "formula" && c.formula ? { f: c.formula.replaceAll("#", String(dataRow)) } : null)));
  const merges = def.header.merges.filter((m) => parseRect(m) !== null);
  const sheetName = esc((def.sheet_name || def.code).slice(0, 31).replace(/[\\/?*[\]:]/g, " "));
  const files = [
    {
      name: "[Content_Types].xml",
      data: enc.encode(
        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
      ),
    },
    {
      name: "_rels/.rels",
      data: enc.encode(
        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
      ),
    },
    {
      name: "xl/workbook.xml",
      data: enc.encode(
        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="${sheetName}" sheetId="1" r:id="rId1"/></sheets></workbook>`,
      ),
    },
    {
      name: "xl/_rels/workbook.xml.rels",
      data: enc.encode(
        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
      ),
    },
    { name: "xl/worksheets/sheet1.xml", data: enc.encode(sheetXml(rows, merges)) },
  ];
  return zipStored(files);
}

/** Đọc các mục ZIP không nén (phương thức 0). Mục nén → bỏ qua. */
function readStoredZip(b: Uint8Array): Map<string, Uint8Array> {
  const out = new Map<string, Uint8Array>();
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  let p = 0;
  while (p + 30 <= b.length && v.getUint32(p, true) === 0x04034b50) {
    const method = v.getUint16(p + 8, true);
    const size = v.getUint32(p + 18, true);
    const nameLen = v.getUint16(p + 26, true);
    const extra = v.getUint16(p + 28, true);
    const name = dec.decode(b.subarray(p + 30, p + 30 + nameLen));
    const start = p + 30 + nameLen + extra;
    if (method === 0) out.set(name, b.subarray(start, start + size));
    p = start + size;
  }
  return out;
}

export interface ParsedSheet {
  sheet: string;
  cells: Map<string, { text?: string; formula?: string }>;
  merges: string[];
}

/** Đọc lại tệp do exportXlsx sinh (hoặc tệp ZIP không nén tương tự). null = tệp nén/không đọc được. */
export function readStoredXlsx(b: Uint8Array): ParsedSheet | null {
  const files = readStoredZip(b);
  const sheetXmlBytes = files.get("xl/worksheets/sheet1.xml");
  if (!sheetXmlBytes) return null;
  const xml = dec.decode(sheetXmlBytes);
  const wb = files.get("xl/workbook.xml");
  const sheet = wb ? (/<sheet [^>]*name="([^"]*)"/.exec(dec.decode(wb))?.[1] ?? "Sheet1") : "Sheet1";
  const unesc = (s: string) => s.replace(/&quot;/g, '"').replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
  const cells = new Map<string, { text?: string; formula?: string }>();
  for (const m of xml.matchAll(/<c r="([A-Z]+\d+)"[^>]*>(.*?)<\/c>/g)) {
    const inner = m[2] ?? "";
    const f = /<f>(.*?)<\/f>/.exec(inner)?.[1];
    const t = /<t[^>]*>(.*?)<\/t>/.exec(inner)?.[1];
    cells.set(m[1]!, f !== undefined ? { formula: unesc(f) } : { text: unesc(t ?? "") });
  }
  const merges = [...xml.matchAll(/<mergeCell ref="([^"]+)"\/>/g)].map((m) => m[1]!);
  return { sheet: unesc(sheet), cells, merges };
}
