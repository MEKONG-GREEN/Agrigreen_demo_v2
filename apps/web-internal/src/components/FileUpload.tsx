"use client";

// Tải tệp chứng từ vùng mkg (G4): chọn tệp → trình duyệt tính SHA-256 (Web Crypto) → multipart + Idempotency-Key
// → dùng `file.id` trả về làm file_ref. Tải về qua `/content` (có Bearer), kiểm lại băm X-Content-SHA256.
import { useState } from "react";
import { unwrap } from "@/api/client";
import type { MkgFile } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { shortHash } from "@/lib/money";
import { fetchMkgFileContent, FILE_ACCEPT, formatBytes, MAX_FILE_BYTES, saveBlob, uploadMkgFile, type FileModule, type OrdersFilePurpose } from "@/lib/upload";
import { useAsync } from "@/lib/useAsync";
import { useAction, Field, Ref } from "./kit";
import { ProblemAlert } from "./ProblemAlert";

/** Nút tải nội dung tệp (qua API có kiểm vai) + kiểm băm. */
export function FileDownload({ module, file }: { module: FileModule; file: Pick<MkgFile, "id" | "file_name"> }) {
  const { api } = useAuth();
  const act = useAction();
  return (
    <>
      <button
        type="button"
        className="btn btn-o btn-sm"
        disabled={act.busy}
        onClick={() =>
          void act.run(async () => {
            const c = await fetchMkgFileContent(api, module, file.id);
            saveBlob(file.file_name || `${file.id}`, c.blob);
          })
        }
      >
        Tải tệp {file.file_name}
      </button>
      {act.error ? <ProblemAlert problem={act.error} /> : null}
    </>
  );
}

/** Tên · cỡ · băm rút gọn · nút tải về của một tệp đã tải lên. */
export function FileInfo({ module, file }: { module: FileModule; file: MkgFile }) {
  return (
    <span className="row" style={{ gap: 6, flexWrap: "wrap" }} aria-label={`Tệp ${file.file_name}`}>
      <b>{file.file_name}</b>
      <span className="small">{formatBytes(file.size)}</span>
      <span className="mono small" title={file.sha256}>
        SHA-256 {shortHash(file.sha256)}
      </span>
      <FileDownload module={module} file={file} />
    </span>
  );
}

/** Tệp theo mã (file_ref): tra siêu dữ liệu; mã chưa đăng ký (bản ghi trước G4) → chỉ hiện mã. */
export function FileRef({ module, id }: { module: FileModule; id: string | null | undefined }) {
  const { api } = useAuth();
  const meta = useAsync(async () => {
    if (!id) return null;
    try {
      return module === "orders" ? (await unwrap(api.GET("/v1/orders/files/{id}", { params: { path: { id } } }))).file : (await unwrap(api.GET("/v1/logistics/files/{id}", { params: { path: { id } } }))).file;
    } catch {
      return null;
    }
  }, [api, module, id]);
  if (!id) return <span className="mute">—</span>;
  if (meta.data) return <FileInfo module={module} file={meta.data} />;
  return <Ref id={id} />;
}

/**
 * Ô chọn tệp: tải lên ngay khi chọn; `onUploaded` nhận siêu dữ liệu (dùng file.id làm file_ref).
 * Kiểm cỡ ≤ 15 MB ở trình duyệt; loại tệp / băm do máy chủ kiểm (thông báo lỗi tiếng Việt).
 */
export function FileUploadField({
  label,
  module,
  purpose,
  value,
  onUploaded,
  hint,
}: {
  label: string;
  module: FileModule;
  purpose?: OrdersFilePurpose;
  value: MkgFile | null;
  onUploaded: (f: MkgFile | null) => void;
  hint?: string;
}) {
  const { api } = useAuth();
  const act = useAction();
  const [tooBig, setTooBig] = useState<string | null>(null);
  return (
    <Field label={label} hint={hint ?? "PDF, JPEG, PNG hoặc WebP, tối đa 15 MB — băm SHA-256 tính ở trình duyệt"}>
      {(id) => (
        <div className="stack" style={{ gap: 6 }}>
          <input
            id={id}
            className="input"
            type="file"
            accept={FILE_ACCEPT}
            disabled={act.busy}
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              setTooBig(null);
              if (!f) return;
              if (f.size > MAX_FILE_BYTES) {
                setTooBig(`Tệp ${f.name} (${formatBytes(f.size)}) vượt 15 MB.`);
                return;
              }
              onUploaded(null);
              void act.run(() => uploadMkgFile(api, module, f, purpose), (x) => `Đã tải lên ${x.file_name}`).then((x) => {
                if (x) onUploaded(x);
              });
            }}
          />
          {act.busy ? <span className="small mute">Đang tính SHA-256 và tải lên…</span> : null}
          {tooBig ? (
            <p className="notice" role="alert">
              {tooBig}
            </p>
          ) : null}
          {act.error ? <ProblemAlert problem={act.error} /> : null}
          {value ? (
            <div role="status" aria-label={`Tệp đã tải ${label}`}>
              <FileInfo module={module} file={value} />
            </div>
          ) : null}
        </div>
      )}
    </Field>
  );
}
