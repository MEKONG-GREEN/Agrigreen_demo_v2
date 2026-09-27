// Nhập nông hộ từ CSV — Admin API POST /v1/households/import (pf/party), thân text/csv.
// Tệp chứa định danh chủ hộ (họ tên, SĐT, CCCD): web chỉ đọc tệp để gửi thẳng lên máy chủ,
// không lưu, không ghi log, không hiển thị lại nội dung (R-04). Kết quả theo dòng không có PII.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { HouseholdImportResult } from "@/api/types";

export const IMPORT_MAX_ROWS = 200;
export const REQUIRED_COLUMNS: readonly (readonly string[])[] = [
  ["full_name", "ho_ten"],
  ["phone", "so_dien_thoai"],
  ["cooperative_code", "ma_htx"],
];

export interface CsvPrecheck {
  rows: number;
  missingColumns: string[];
  tooMany: boolean;
}

/** Kiểm sơ bộ phía client (chỉ đếm dòng và đọc dòng tiêu đề). Máy chủ vẫn kiểm đầy đủ. */
export function precheckCsv(text: string): CsvPrecheck {
  const lines = text.replace(/^﻿/, "").split(/\r?\n/).filter((l) => l.trim() !== "");
  const header = (lines[0] ?? "").split(",").map((h) => h.trim().replace(/^"|"$/g, "").toLowerCase());
  const missingColumns = REQUIRED_COLUMNS.filter((alts) => !alts.some((a) => header.includes(a))).map((alts) => alts.join(" | "));
  const rows = Math.max(0, lines.length - 1);
  return { rows, missingColumns, tooMany: rows > IMPORT_MAX_ROWS };
}

export function importHouseholds(admin: AdminApiClient, csv: string, idemKey: string): Promise<HouseholdImportResult> {
  return unwrap(
    admin.POST("/v1/households/import", {
      params: { header: { "Idempotency-Key": idemKey } },
      body: csv,
      bodySerializer: (b: string) => b,
      headers: { "Content-Type": "text/csv; charset=utf-8" },
    }),
  );
}
