// Client có kiểu cho API lịch công tác tuần (P5, /v1/extension/work-schedules*) trên openapi-fetch sinh từ hợp đồng.
// Bearer, X-AG-Context (ngữ cảnh KN P1) và Idempotency-Key do middleware của createApiClient gắn;
// nơi gọi có thể truyền khóa riêng để thử lại ĐÚNG một thao tác sau lỗi mạng (R-51).
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { KnLogPage, WsCounts, WsDetail, WsListItem, WsMine, WsSave, WsSettings, WsStatus, WsUnregistered } from "./model";

const idem = (key?: string) => ({ header: { "Idempotency-Key": key ?? newIdempotencyKey() } });

export interface WsListQuery {
  week_start?: string;
  status?: WsStatus;
  commune_code?: string;
  owner_level?: "commune" | "province";
  cursor?: string;
  limit?: number;
}

export interface WsListPage {
  week_start: string;
  items: WsListItem[];
  next_cursor: string | null;
}

export interface BulkApproveResult {
  approved: string[];
  skipped: { id: string; code: string }[];
}

export function createWorkScheduleApi(api: ApiClient) {
  return {
    mine: (weekStart?: string): Promise<WsMine> =>
      unwrap(api.GET("/v1/extension/work-schedules/mine", { params: { query: weekStart ? { week_start: weekStart } : {} } })),

    save: (body: WsSave, key?: string): Promise<WsDetail> =>
      unwrap(api.POST("/v1/extension/work-schedules", { params: idem(key), body })),

    copyPrevious: (weekStart: string, key?: string): Promise<WsDetail> =>
      unwrap(api.POST("/v1/extension/work-schedules/copy-previous", { params: idem(key), body: { week_start: weekStart } })),

    list: (q: WsListQuery): Promise<WsListPage> => unwrap(api.GET("/v1/extension/work-schedules", { params: { query: q } })),

    /** Mọi trang của danh sách (lưới tuần cần đủ dòng). */
    async listAll(q: Omit<WsListQuery, "cursor">): Promise<WsListPage> {
      const first = await this.list({ ...q, limit: q.limit ?? 200 });
      const items = [...first.items];
      let cursor = first.next_cursor;
      while (cursor) {
        const next = await this.list({ ...q, limit: q.limit ?? 200, cursor });
        items.push(...next.items);
        cursor = next.next_cursor;
      }
      return { week_start: first.week_start, items, next_cursor: null };
    },

    counts: (weekStart?: string): Promise<WsCounts> =>
      unwrap(api.GET("/v1/extension/work-schedules/counts", { params: { query: weekStart ? { week_start: weekStart } : {} } })),

    unregistered: (weekStart: string, communeCode?: string): Promise<{ week_start: string; items: WsUnregistered[] }> =>
      unwrap(
        api.GET("/v1/extension/work-schedules/unregistered", {
          params: { query: { week_start: weekStart, ...(communeCode ? { commune_code: communeCode } : {}) } },
        }),
      ),

    settings: (provinceCode?: string): Promise<WsSettings> =>
      unwrap(api.GET("/v1/extension/work-schedules/settings", { params: { query: provinceCode ? { province_code: provinceCode } : {} } })),

    get: (id: string, version?: number): Promise<WsDetail> =>
      unwrap(api.GET("/v1/extension/work-schedules/{id}", { params: { path: { id }, query: version ? { version } : {} } })),

    log: (id: string, cursor?: number, limit = 50): Promise<KnLogPage> =>
      unwrap(api.GET("/v1/extension/work-schedules/{id}/log", { params: { path: { id }, query: { limit, ...(cursor ? { cursor } : {}) } } })),

    approve: (id: string, version: number, note?: string, key?: string): Promise<WsDetail> =>
      unwrap(
        api.POST("/v1/extension/work-schedules/{id}/approve", {
          params: { path: { id }, ...idem(key) },
          body: { version, ...(note ? { note } : {}) },
        }),
      ),

    requestChanges: (id: string, version: number, reason: string, key?: string): Promise<WsDetail> =>
      unwrap(api.POST("/v1/extension/work-schedules/{id}/request-changes", { params: { path: { id }, ...idem(key) }, body: { version, reason } })),

    bulkApprove: (ids: string[], key?: string): Promise<BulkApproveResult> =>
      unwrap(api.POST("/v1/extension/work-schedules/bulk-approve", { params: idem(key), body: { ids } })),

    /** Xuất Excel/PDF (GET …/export?format=xlsx|pdf) — trả tệp và tên tệp từ Content-Disposition. */
    async exportFile(q: { week_start: string; status?: WsStatus; commune_code?: string; format: "xlsx" | "pdf" }): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/extension/work-schedules/export", { params: { query: q }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      const fallback = `lich-cong-tac-${q.week_start}.${q.format}`;
      return { blob: res.data as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), fallback) };
    },
  };
}

export type WorkScheduleApi = ReturnType<typeof createWorkScheduleApi>;
