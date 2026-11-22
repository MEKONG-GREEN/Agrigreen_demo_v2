// Thân multipart/form-data TẤT ĐỊNH cho lệnh ghi có Idempotency-Key (#29 W11.53 P2, docs/conventions/idempotency.md).
// FormData của trình duyệt sinh boundary ngẫu nhiên mỗi lần gửi → cùng tệp + cùng trường vẫn ra thân khác: client tính
// dấu vân tay khác (khóa mới) và máy chủ băm thân khác (cùng khóa → idempotency_key_reused). Bấm lại sau mất phản hồi
// vì thế không được phát lại kết quả đã ghi. Ở đây boundary suy ra từ chính các phần (tên trường, giá trị, tên/kiểu/cỡ/
// thời điểm sửa của tệp) → cùng thao tác ⇒ cùng từng byte ⇒ cùng khóa và máy chủ phát lại; đổi tệp/trường ⇒ thân khác.

export type MultipartPart = readonly [name: string, value: string | File];

export interface StableMultipart {
  body: Blob;
  /** "multipart/form-data; boundary=…" — đặt vào header Content-Type của request. */
  contentType: string;
}

/** FNV-1a 32 bit trên chuỗi, hai làn khác hạt giống → 64 bit cho boundary (không phải băm bảo mật). */
function fnvHex(s: string): string {
  let a = 0x811c9dc5;
  let b = 0x01000193 ^ 0x9e3779b9;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    a = Math.imul(a ^ c, 0x01000193) >>> 0;
    b = Math.imul(b ^ c, 0x01000193) >>> 0;
  }
  return a.toString(16).padStart(8, "0") + b.toString(16).padStart(8, "0");
}

/** Thoát tên trường/tên tệp trong Content-Disposition như WHATWG (", CR, LF). */
function quote(s: string): string {
  return s.replace(/"/g, "%22").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
}

/** Dựng thân multipart/form-data tất định từ các phần theo thứ tự. */
export function stableMultipart(parts: readonly MultipartPart[]): StableMultipart {
  const id = parts
    .map(([n, v]) => (typeof v === "string" ? `${n}=${v}` : `${n}=file:${v.name}:${v.type}:${v.size}:${v.lastModified}`))
    .join("\u0000");
  const boundary = `----AgFormBoundary${fnvHex(id)}${fnvHex(`${id.length}|${id}`)}`;
  const chunks: BlobPart[] = [];
  for (const [name, v] of parts) {
    if (typeof v === "string") {
      chunks.push(`--${boundary}\r\nContent-Disposition: form-data; name="${quote(name)}"\r\n\r\n${v}\r\n`);
    } else {
      chunks.push(
        `--${boundary}\r\nContent-Disposition: form-data; name="${quote(name)}"; filename="${quote(v.name)}"\r\n` +
          `Content-Type: ${v.type || "application/octet-stream"}\r\n\r\n`,
        v,
        "\r\n",
      );
    }
  }
  chunks.push(`--${boundary}--\r\n`);
  const contentType = `multipart/form-data; boundary=${boundary}`;
  return { body: new Blob(chunks, { type: contentType }), contentType };
}
