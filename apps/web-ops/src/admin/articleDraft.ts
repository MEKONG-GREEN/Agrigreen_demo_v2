// Lưu nháp bài viết khuyến nông (HTX US-CMS-01, Admin API /v1/documents*) — tách khỏi màn quan-tri/bai-viet để kiểm thử
// với client thật. #172: cả lần "Lưu nháp" (tạo tài liệu/thêm phiên bản → tải ảnh bìa → gắn ảnh bằng phiên bản mới) là MỘT
// lượt thao tác; mỗi lệnh ghi khởi phát đồng bộ trong withIdempotentOperation(lượt, …) nên mang khóa theo dấu vân tay. Ảnh
// bìa gửi bằng thân multipart tất định (src/api/multipart.ts): mất phản hồi rồi bấm lại cùng nội dung + cùng ảnh → cùng
// khóa ở mọi bước → máy chủ phát lại tài liệu/ảnh/phiên bản đã ghi, không tạo bản thứ hai. Đổi ảnh/nội dung → khóa mới.
import { unwrap, type AdminApiClient } from "@/api/client";
import { newIdempotencyKey, withIdempotentOperation, type OperationRun } from "@/api/idempotency";
import { stableMultipart } from "@/api/multipart";
import type { ContentCategory, ContentVersion } from "@/api/types";

export interface ArticleInput {
  title: string;
  summary: string;
  body: string;
  body_format: "markdown";
  category?: ContentCategory;
}

async function sha256Hex(bytes: ArrayBuffer): Promise<string> {
  const d = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(d), (b) => b.toString(16).padStart(2, "0")).join("");
}

const key = () => ({ "Idempotency-Key": newIdempotencyKey() });

/** Tải ảnh bìa (thân multipart tất định). Gọi trong phạm vi một lượt thao tác để giữ khóa khi bấm lại. */
export function uploadCover(api: AdminApiClient, ref: string, cover: File, sha: string) {
  const mp = stableMultipart([
    ["file", cover],
    ["sha256", sha],
  ]);
  return unwrap(
    api.POST("/v1/documents/{ref}/attachments", {
      params: { path: { ref }, header: key() },
      headers: { "Content-Type": mp.contentType },
      body: { file: cover as unknown as string, sha256: sha },
      bodySerializer: () => mp.body,
    }),
  );
}

/**
 * Lưu nháp: tạo tài liệu (phiên bản 1) khi chưa có ref, không thì thêm phiên bản; có ảnh bìa thì tải lên rồi gắn bằng
 * phiên bản mới. Mọi lệnh ghi chạy trong `run` (lượt thao tác của màn); nơi gọi complete() lượt khi hàm này thành công.
 */
export async function saveArticleDraft(
  api: AdminApiClient,
  run: OperationRun,
  args: { ref: string | null; input: ArticleInput; cover: File | null },
): Promise<{ ref: string; version: ContentVersion }> {
  const { input, cover } = args;
  const scoped = <T,>(g: () => Promise<T>) => withIdempotentOperation(run, g);
  let ref = args.ref;
  let version: ContentVersion;
  if (!ref) {
    const r = await scoped(() =>
      unwrap(api.POST("/v1/documents", { params: { header: key() }, body: { ...input, kind: "news", audience: { level: "national" } } })),
    );
    ref = r.document.ref;
    version = r.version;
  } else {
    const at = ref;
    const r = await scoped(() => unwrap(api.POST("/v1/documents/{ref}/versions", { params: { path: { ref: at }, header: key() }, body: input })));
    version = r.version;
  }
  if (cover) {
    const sha = await sha256Hex(await cover.arrayBuffer());
    const at = ref;
    const att = await scoped(() => uploadCover(api, at, cover, sha));
    const r = await scoped(() =>
      unwrap(
        api.POST("/v1/documents/{ref}/versions", {
          params: { path: { ref: at }, header: key() },
          body: { ...input, attachments: [{ id: att.id, role: "cover" }] },
        }),
      ),
    );
    version = r.version;
  }
  return { ref, version };
}
