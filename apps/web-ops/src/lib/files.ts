// Tệp tải lên/tải xuống phía trình duyệt. Nội dung tệp (có thể chứa định danh) chỉ được đọc để gửi thẳng
// lên máy chủ — không lưu, không ghi log, không hiển thị lại (R-04).

export const XLSX_TYPE = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";

export type UploadKind = "csv" | "xlsx" | "zip" | "kml" | "geojson" | "xls" | "unknown";

export interface UploadFile {
  kind: UploadKind;
  name: string;
  size: number;
  /** Nội dung gửi đi: chuỗi (CSV/KML/GeoJSON) hoặc byte (.xlsx, .zip). */
  body: string | Uint8Array;
  contentType: string;
}

/** Nhận dạng theo chữ ký nội dung (ZIP "PK\x03\x04", OLE2 .xls cũ), rồi theo đuôi tệp. */
export function detectKind(name: string, head: Uint8Array): UploadKind {
  const ext = name.toLowerCase().split(".").pop() ?? "";
  if (head[0] === 0xd0 && head[1] === 0xcf && head[2] === 0x11 && head[3] === 0xe0) return "xls";
  if (head[0] === 0x50 && head[1] === 0x4b) return ext === "zip" ? "zip" : "xlsx";
  if (ext === "csv") return "csv";
  if (ext === "kml") return "kml";
  if (ext === "geojson" || ext === "json") return "geojson";
  return "unknown";
}

const CONTENT_TYPE: Record<UploadKind, string> = {
  csv: "text/csv; charset=utf-8",
  xlsx: XLSX_TYPE,
  zip: "application/zip",
  kml: "application/vnd.google-earth.kml+xml",
  geojson: "application/geo+json",
  xls: "application/vnd.ms-excel",
  unknown: "application/octet-stream",
};

export async function readUpload(f: File): Promise<UploadFile> {
  const bytes = new Uint8Array(await f.arrayBuffer());
  const kind = detectKind(f.name, bytes.subarray(0, 8));
  const text = kind === "csv" || kind === "kml" || kind === "geojson";
  return {
    kind,
    name: f.name,
    size: f.size,
    body: text ? new TextDecoder("utf-8").decode(bytes) : bytes,
    contentType: CONTENT_TYPE[kind],
  };
}

/** Tên tệp từ Content-Disposition (RFC 6266, gồm filename*=UTF-8''…). */
export function filenameFromDisposition(header: string | null | undefined, fallback: string): string {
  if (!header) return fallback;
  const star = /filename\*\s*=\s*UTF-8''([^;]+)/i.exec(header);
  if (star?.[1]) {
    try {
      return decodeURIComponent(star[1].trim().replace(/^"|"$/g, ""));
    } catch {
      // bỏ qua, dùng filename thường
    }
  }
  const plain = /filename\s*=\s*"?([^";]+)"?/i.exec(header);
  return plain?.[1]?.trim() || fallback;
}

/** Kích hoạt tải xuống một URL (blob: hoặc URL có chữ ký của kho đối tượng). */
export function triggerDownload(url: string, fileName: string): void {
  const a = document.createElement("a");
  a.href = url;
  a.download = fileName;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

export function saveBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob);
  triggerDownload(url, fileName);
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

export function formatBytes(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1).replace(".", ",")} KB`;
  return `${(n / 1024 / 1024).toFixed(1).replace(".", ",")} MB`;
}
