// Tải tệp chứng từ vùng mkg (G4): biên bản chênh lệch phiếu nhận, chứng từ giao nhận (orders), ePOD / e-bill /
// biên bản chuyến (logistics). Trình duyệt tính SHA-256 của nội dung (Web Crypto) và gửi multipart kèm
// Idempotency-Key; server nhận diện loại tệp theo chữ ký nội dung và từ chối nếu băm lệch.
import { idem, unwrap, type ApiClient } from "@/api/client";
import { parseProblemResponse } from "@/api/problem";
import type { MkgFile } from "@/api/types";
import { sha256Hex } from "./sha256";

/** Giới hạn của server: 15 MB. */
export const MAX_FILE_BYTES = 15 * 1024 * 1024;

export const FILE_ACCEPT = "application/pdf,image/jpeg,image/png,image/webp,.pdf,.jpg,.jpeg,.png,.webp";

export type FileContentType = "application/pdf" | "image/jpeg" | "image/png" | "image/webp";

/** Nhận diện loại tệp theo chữ ký nội dung (magic bytes) — như server, không tin tên / Content-Type client khai. */
export function detectFileType(b: Uint8Array): FileContentType | null {
  const at = (i: number, ...xs: number[]) => xs.every((x, j) => b[i + j] === x);
  if (at(0, 0x25, 0x50, 0x44, 0x46, 0x2d)) return "application/pdf"; // %PDF-
  if (at(0, 0xff, 0xd8, 0xff)) return "image/jpeg";
  if (at(0, 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a)) return "image/png";
  if (at(0, 0x52, 0x49, 0x46, 0x46) && at(8, 0x57, 0x45, 0x42, 0x50)) return "image/webp"; // RIFF....WEBP
  return null;
}

/** SHA-256 hex thường của nội dung: Web Crypto (crypto.subtle) khi có, dự phòng bản đồng bộ (ngữ cảnh không bảo mật). */
export async function sha256HexAsync(data: Uint8Array): Promise<string> {
  const subtle = typeof crypto !== "undefined" ? crypto.subtle : undefined;
  if (subtle) {
    const copy = new Uint8Array(data.byteLength);
    copy.set(data);
    const d = await subtle.digest("SHA-256", copy.buffer);
    return Array.from(new Uint8Array(d), (x) => x.toString(16).padStart(2, "0")).join("");
  }
  return sha256Hex(data);
}

/** Kích thước tệp dạng vi-VN (B / KB / MB). */
export function formatBytes(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toLocaleString("vi-VN", { maximumFractionDigits: 1 })} KB`;
  return `${(n / 1024 / 1024).toLocaleString("vi-VN", { maximumFractionDigits: 1 })} MB`;
}

export type FileModule = "orders" | "logistics";
export type OrdersFilePurpose = "receipt_variance_report" | "delivery_evidence";

async function bytesOf(file: Blob): Promise<Uint8Array> {
  return new Uint8Array(await file.arrayBuffer());
}

/** Dựng multipart (file, sha256[, purpose]) — tách riêng để kiểm thử. */
export async function buildUploadForm(file: File, purpose?: OrdersFilePurpose): Promise<{ form: FormData; sha256: string }> {
  const sha256 = await sha256HexAsync(await bytesOf(file));
  const form = new FormData();
  form.append("file", file, file.name);
  form.append("sha256", sha256);
  if (purpose) form.append("purpose", purpose);
  return { form, sha256 };
}

/**
 * Tải một tệp lên `POST /v1/{orders|logistics}/files` → siêu dữ liệu tệp (dùng `file.id` làm file_ref).
 * Idempotency-Key sinh mới mỗi lần gọi; truyền `key` để thử lại đúng thao tác sau lỗi mạng.
 */
export async function uploadMkgFile(api: ApiClient, module: FileModule, file: File, purpose?: OrdersFilePurpose, key?: string): Promise<MkgFile> {
  const { form } = await buildUploadForm(file, module === "orders" ? purpose : undefined);
  const header = idem(key);
  const bodySerializer = (b: unknown) => b as FormData;
  if (module === "orders") {
    const r = await unwrap(api.POST("/v1/orders/files", { params: { header }, body: form as unknown as { file: string; sha256: string; purpose: OrdersFilePurpose }, bodySerializer }));
    return r.file;
  }
  const r = await unwrap(api.POST("/v1/logistics/files", { params: { header }, body: form as unknown as { file: string; sha256: string }, bodySerializer }));
  return r.file;
}

/** Tải nội dung tệp qua API (có Bearer) → Blob + băm server khai (X-Content-SHA256). */
export async function fetchMkgFileContent(api: ApiClient, module: FileModule, id: string): Promise<{ blob: Blob; sha256: string | null }> {
  const res =
    module === "orders"
      ? await api.GET("/v1/orders/files/{id}/content", { params: { path: { id } }, parseAs: "blob" })
      : await api.GET("/v1/logistics/files/{id}/content", { params: { path: { id } }, parseAs: "blob" });
  if (!res.response.ok) throw await parseProblemResponse(res.response);
  return { blob: res.data as unknown as Blob, sha256: res.response.headers.get("X-Content-SHA256") };
}

/** Lưu Blob thành tệp tải về trên trình duyệt. */
export function saveBlob(name: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
