// Nhập nông hộ từ CSV UTF-8 hoặc Excel .xlsx — Admin API POST /v1/households/import (pf/party).
// Tệp chứa định danh chủ hộ (họ tên, SĐT, CCCD): web chỉ đọc tệp để gửi thẳng lên máy chủ,
// không lưu, không ghi log, không hiển thị lại nội dung (R-04). Kết quả theo dòng không có PII.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { HouseholdImportResult } from "@/api/types";
import type { UploadFile } from "@/lib/files";

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

/** Gửi tệp CSV (chuỗi) hoặc Excel .xlsx (byte) nguyên văn — máy chủ nhận dạng theo chữ ký tệp. */
export function importHouseholds(admin: AdminApiClient, file: string | UploadFile, idemKey: string): Promise<HouseholdImportResult> {
  const body = typeof file === "string" ? file : file.body;
  const contentType = typeof file === "string" ? "text/csv; charset=utf-8" : file.contentType;
  return unwrap(
    admin.POST("/v1/households/import", {
      params: { header: { "Idempotency-Key": idemKey } },
      body: body as unknown as string,
      bodySerializer: (b: unknown) => b as BodyInit,
      headers: { "Content-Type": contentType },
    }),
  );
}
