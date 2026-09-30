// Đọc bảng số cân của khách (CSV UTF-8, phân tách dấu phẩy hoặc chấm phẩy) → dòng của
// POST /v1/measurement/customer-weight-imports. Kiểm trước trên trình duyệt; máy chủ vẫn kiểm lại
// (một dòng sai → cả lần bị từ chối).
import { isKg, isPct, normKg } from "./kg";

export interface WeightRow {
  lot_ref: string;
  mass_kg: string;
  weighed_at: string;
  route?: "HUB" | "DIRECT";
  moisture_pct?: string;
  customer_ticket_no?: string;
}

export interface ParsedWeights {
  rows: WeightRow[];
  errors: { line: number; reason: string }[];
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export const MAX_ROWS = 200;
const HEADERS = ["lot_ref", "mass_kg", "weighed_at", "route", "moisture_pct", "customer_ticket_no"] as const;

/** Số kiểu Việt Nam "96.800,5" hoặc "96800.5" → chuỗi kg chuẩn; null nếu không đọc được. */
export function parseKgCell(s: string): string | null {
  let t = s.trim().replace(/\s/g, "");
  if (/^\d{1,3}(\.\d{3})+(,\d{1,3})?$/.test(t)) t = t.replace(/\./g, "").replace(",", ".");
  else if (/^\d+,\d{1,3}$/.test(t)) t = t.replace(",", ".");
  return isKg(t) && !t.startsWith("-") ? normKg(t) : null;
}

export function parseWeightsCsv(text: string): ParsedWeights {
  const lines = text.replace(/^﻿/, "").split(/\r?\n/).map((l) => l.trim());
  const rows: WeightRow[] = [];
  const errors: ParsedWeights["errors"] = [];
  const first = lines.find((l) => l !== "") ?? "";
  const sep = first.includes(";") ? ";" : ",";
  let header: string[] = [...HEADERS];
  let start = 0;
  if (first.toLowerCase().includes("lot_ref")) {
    header = first.split(sep).map((h) => h.trim().toLowerCase());
    start = lines.indexOf(first) + 1;
    for (const need of ["lot_ref", "mass_kg", "weighed_at"]) if (!header.includes(need)) errors.push({ line: start, reason: `Thiếu cột ${need}` });
  }
  for (let i = start; i < lines.length; i++) {
    const line = lines[i]!;
    if (!line) continue;
    if (/^[=+\-@]/.test(line)) {
      errors.push({ line: i + 1, reason: "Ô bắt đầu bằng công thức — bị từ chối" });
      continue;
    }
    const cells = line.split(sep).map((c) => c.trim());
    const get = (k: string) => cells[header.indexOf(k)] ?? "";
    const lot = get("lot_ref");
    const mass = parseKgCell(get("mass_kg"));
    const at = get("weighed_at");
    const route = get("route").toUpperCase();
    const moisture = get("moisture_pct").replace(",", ".");
    if (!UUID.test(lot)) errors.push({ line: i + 1, reason: "lot_ref không phải UUID" });
    if (!mass || mass === "0.000") errors.push({ line: i + 1, reason: "mass_kg không hợp lệ" });
    if (!at || !Number.isFinite(Date.parse(at))) errors.push({ line: i + 1, reason: "weighed_at không phải thời điểm ISO 8601" });
    if (route && route !== "HUB" && route !== "DIRECT") errors.push({ line: i + 1, reason: "route phải là HUB hoặc DIRECT" });
    if (moisture && !isPct(moisture)) errors.push({ line: i + 1, reason: "moisture_pct 0..100" });
    rows.push({
      lot_ref: lot,
      mass_kg: mass ?? "",
      weighed_at: at && Number.isFinite(Date.parse(at)) ? new Date(at).toISOString() : at,
      ...(route ? { route: route as "HUB" | "DIRECT" } : {}),
      ...(moisture ? { moisture_pct: moisture } : {}),
      ...(get("customer_ticket_no") ? { customer_ticket_no: get("customer_ticket_no") } : {}),
    });
  }
  if (rows.length > MAX_ROWS) errors.push({ line: 0, reason: `Tối đa ${MAX_ROWS} dòng mỗi lần nhập` });
  if (rows.length === 0 && errors.length === 0) errors.push({ line: 0, reason: "Không có dòng dữ liệu" });
  return { rows, errors };
}

/** SHA-256 hex của tệp (ghi vào lần nhập để truy vết). */
export async function sha256Hex(data: ArrayBuffer): Promise<string> {
  const d = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(d), (b) => b.toString(16).padStart(2, "0")).join("");
}
