// Client có kiểu cho Kho tài liệu nhân sự (W11.24, dom/hrm — /v1/hrm/docs*) trên openapi-fetch sinh từ hợp đồng. Nội dung
// tệp chỉ đi qua bộ nhớ (máy chủ giải mã qua pf/evidence, phản hồi no-store) — không lưu vào localStorage/sessionStorage.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type HrmDocTree = S["HrmDocTree"];
export type HrmFolder = S["HrmFolder"];
export type HrmFolderACL = S["HrmFolderACL"];
export type HrmFolderVersion = S["HrmFolderVersion"];
export type HrmDoc = S["HrmDoc"];
export type HrmDocDetail = S["HrmDocDetail"];
export type HrmDocInput = S["HrmDocInput"];
export type HrmDocType = S["HrmDocType"];

export const DOC_TYPE_LABEL: Record<HrmDocType, string> = {
  nang_bac: "Quyết định nâng bậc",
  dieu_dong: "Quyết định điều động",
  bo_nhiem: "Quyết định bổ nhiệm",
  khen_thuong: "Quyết định khen thưởng",
  ky_luat: "Quyết định kỷ luật",
  danh_gia: "Hồ sơ đánh giá",
  khac: "Tài liệu khác",
};
export const DOC_CHANGE_LABEL: Record<string, string> = {
  created: "Tải lên",
  updated: "Sửa phân loại",
  hidden: "Ẩn (xóa)",
  restored: "Hiện lại",
  renamed: "Đổi tên",
  acl: "Đổi quyền thư mục",
};
export const DOC_FIELD_LABEL: Record<string, string> = {
  title: "Tên tài liệu",
  doc_type: "Loại",
  year: "Năm",
  unit_code: "Phòng ban",
  staff_ids: "Nhân viên",
  doc_no: "Số quyết định",
  issued_on: "Ngày ban hành",
  note: "Ghi chú",
};

export interface DocFilter {
  folder?: string;
  recursive?: "0" | "1";
  q?: string;
  doc_type?: HrmDocType;
  year?: number;
  unit?: string;
  staff?: string;
  include_hidden?: "0" | "1";
  offset?: number;
  limit?: number;
}

export function fileSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1).replace(".", ",")} MB`;
}

export function createDocApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const clean = (f: DocFilter) =>
    Object.fromEntries(
      Object.entries(f).filter(([, v]) => v !== undefined && v !== ""),
    ) as DocFilter;
  return {
    tree: (includeHidden = false): Promise<HrmDocTree> =>
      unwrap(
        api.GET("/v1/hrm/docs/folders", {
          params: { query: includeHidden ? { include_hidden: "1" } : {} },
        }),
      ),
    createFolder: (body: {
      parent_id?: string | null;
      name: string;
      doc_type?: HrmDocType;
    }): Promise<{ folder_id: string; version: number }> =>
      unwrap(
        api.POST("/v1/hrm/docs/folders", {
          params: { header: header() },
          body,
        }),
      ),
    installDefaults: (): Promise<{ created: number }> =>
      unwrap(
        api.POST("/v1/hrm/docs/folders/defaults", {
          params: { header: header() },
        }),
      ),
    renameFolder: (id: string, name: string) =>
      unwrap(
        api.POST("/v1/hrm/docs/folders/{id}/rename", {
          params: { header: header(), path: { id } },
          body: { name },
        }),
      ),
    setACL: (id: string, acl: HrmFolderACL, note?: string) =>
      unwrap(
        api.POST("/v1/hrm/docs/folders/{id}/acl", {
          params: { header: header(), path: { id } },
          body: { acl, note },
        }),
      ),
    hideFolder: (id: string, hidden: boolean, reason: string) =>
      unwrap(
        hidden
          ? api.POST("/v1/hrm/docs/folders/{id}/hide", {
              params: { header: header(), path: { id } },
              body: { reason },
            })
          : api.POST("/v1/hrm/docs/folders/{id}/restore", {
              params: { header: header(), path: { id } },
              body: { reason },
            }),
      ),
    folderHistory: (id: string): Promise<{ items: HrmFolderVersion[] }> =>
      unwrap(
        api.GET("/v1/hrm/docs/folders/{id}/history", {
          params: { path: { id } },
        }),
      ),
    list: (f: DocFilter): Promise<{ total: number; items: HrmDoc[] }> =>
      unwrap(api.GET("/v1/hrm/docs", { params: { query: clean(f) } })),
    get: (id: string): Promise<HrmDocDetail> =>
      unwrap(api.GET("/v1/hrm/docs/{id}", { params: { path: { id } } })),
    /** Tải lên: phần "meta" (JSON phân loại) + phần "file". */
    upload: (
      meta: HrmDocInput,
      file: File,
    ): Promise<{ doc_id: string; version: number }> =>
      unwrap(
        api.POST("/v1/hrm/docs", {
          params: { header: header() },
          body: { meta: JSON.stringify(meta), file: file as unknown as string },
          bodySerializer: (b: { meta: string; file: string }) => {
            const fd = new FormData();
            fd.append("meta", b.meta);
            fd.append("file", b.file as unknown as Blob, file.name);
            return fd;
          },
        }),
      ),
    classify: (id: string, body: HrmDocInput) =>
      unwrap(
        api.POST("/v1/hrm/docs/{id}/classify", {
          params: { header: header(), path: { id } },
          body,
        }),
      ),
    hide: (id: string, hidden: boolean, reason: string) =>
      unwrap(
        hidden
          ? api.POST("/v1/hrm/docs/{id}/hide", {
              params: { header: header(), path: { id } },
              body: { reason },
            })
          : api.POST("/v1/hrm/docs/{id}/restore", {
              params: { header: header(), path: { id } },
              body: { reason },
            }),
      ),
    /** Xem trước (inline) hoặc tải xuống — máy chủ ghi nhật ký mỗi lần. */
    async content(
      id: string,
      download: boolean,
    ): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/hrm/docs/{id}/content", {
        params: { path: { id }, query: download ? { download: "1" } : {} },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      return {
        blob: res.data as Blob,
        name: filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          "tai-lieu",
        ),
      };
    },
  };
}

export type DocApi = ReturnType<typeof createDocApi>;
