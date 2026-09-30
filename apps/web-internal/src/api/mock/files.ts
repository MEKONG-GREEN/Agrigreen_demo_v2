// Kho tệp chứng từ giả lập của vùng mkg (orders, logistics) — như kho đối tượng trong lãnh thổ của backend:
// ghi một lần, ≤ 15 MB, PDF / JPEG / PNG / WebP nhận diện theo chữ ký nội dung, `sha256` khai phải khớp nội dung.
// Nội dung giữ trong bộ nhớ tab; tải về qua route `/content` (có kiểm vai, header X-Content-SHA256).
import type { MkgFile } from "../types";
import { detectFileType, MAX_FILE_BYTES } from "@/lib/upload";
import { sha256Hex } from "@/lib/sha256";
import { newId } from "./db";
import { created, forbidden, hasRole, json, multipartFile, nowIso, problem, route, str, traceId, validation, type Route } from "./http";

export type FileModule = "orders" | "logistics";
interface FileRec {
  module: FileModule;
  file: MkgFile;
  bytes: Uint8Array;
}
const files: FileRec[] = [];

export function findFile(module: FileModule, id: string): MkgFile | undefined {
  return files.find((f) => f.module === module && f.file.id === id)?.file;
}

/** Ghi sẵn một tệp (hạt giống). */
export function seedFile(module: FileModule, id: string, purpose: MkgFile["purpose"], name: string, bytes: Uint8Array, by: string, at: string): MkgFile {
  const file: MkgFile = { id, purpose, file_name: name, content_type: detectFileType(bytes) ?? "application/pdf", size: bytes.length, sha256: sha256Hex(bytes), uploaded_by: by, rule_version: `${module}.file.v1`, recorded_at: at };
  files.push({ module, file, bytes });
  return file;
}

/** PDF tối thiểu hợp lệ về chữ ký (%PDF-) cho hạt giống / kiểm thử. */
export function tinyPdf(label: string): Uint8Array {
  return new TextEncoder().encode(`%PDF-1.4\n% ${label}\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n`);
}

export function fileRoutes(module: FileModule, roles: string[], purposes: MkgFile["purpose"][]): Route[] {
  const prefix = `/v1/${module}/files`;
  const notFound = () => problem(404, `${module}.file_not_found`, "File not found");
  return [
    route("POST", prefix, ({ body, persona }) => {
      if (!hasRole(persona, ...roles)) return forbidden();
      const f = multipartFile(body, "file");
      const sha = str(body, "sha256");
      const errs: { field: string; reason: string }[] = [];
      if (!f) errs.push({ field: "file", reason: "Bắt buộc (multipart)" });
      if (!/^[0-9a-fA-F]{64}$/.test(sha)) errs.push({ field: "sha256", reason: "SHA-256 hex 64 ký tự" });
      let purpose = purposes[0]!;
      if (module === "orders") {
        const p = str(body, "purpose");
        if (!purposes.includes(p as MkgFile["purpose"])) errs.push({ field: "purpose", reason: purposes.join(" | ") });
        else purpose = p as MkgFile["purpose"];
      }
      if (errs.length) return validation(errs);
      const bytes = f!.bytes;
      if (bytes.length > MAX_FILE_BYTES) return problem(413, `${module}.file_too_large`, "File too large");
      const type = detectFileType(bytes);
      if (!type) return problem(415, `${module}.file_type`, "Unsupported file type");
      if (sha256Hex(bytes) !== sha.toLowerCase()) return problem(422, `${module}.file_checksum`, "Checksum mismatch");
      const file: MkgFile = { id: newId(0x3a), purpose, file_name: f!.name || "tep", content_type: type, size: bytes.length, sha256: sha.toLowerCase(), uploaded_by: persona.actor, rule_version: `${module}.file.v1`, recorded_at: nowIso() };
      files.push({ module, file, bytes });
      return created({ file });
    }),
    route("GET", `${prefix}/:id`, ({ params, persona }) => {
      if (!hasRole(persona, ...roles)) return forbidden();
      const f = findFile(module, params[0]!);
      return f ? json({ file: f }) : notFound();
    }),
    route("GET", `${prefix}/:id/content`, ({ params, persona }) => {
      if (!hasRole(persona, ...roles)) return forbidden();
      const r = files.find((x) => x.module === module && x.file.id === params[0]);
      if (!r) return notFound();
      const copy = new Uint8Array(r.bytes.length);
      copy.set(r.bytes);
      return new Response(copy.buffer, {
        status: 200,
        headers: { "Content-Type": r.file.content_type, "X-Content-SHA256": r.file.sha256, "Content-Disposition": `attachment; filename="${encodeURIComponent(r.file.file_name)}"`, "X-Trace-Id": traceId() },
      });
    }),
  ];
}
