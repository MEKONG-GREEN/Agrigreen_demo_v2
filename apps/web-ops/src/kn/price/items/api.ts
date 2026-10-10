// Client có kiểu cho Mặt hàng theo thời kỳ (W11.73, cap/price — /v1/price/items*, groups*, assignments*, point-items,
// item-sets*, proposals*) trên openapi-fetch sinh từ hợp đồng. Bearer, X-AG-Context và Idempotency-Key do middleware của
// createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type PriceItem = S["PriceItem"];
export type PriceItemList = S["PriceItemList"];
export type PriceItemDetail = S["PriceItemDetail"];
export type PriceItemInput = S["PriceItemInput"];
export type PriceGroup = S["PriceGroup"];
export type PriceAssignment = S["PriceAssignment"];
export type PriceItemSet = S["PriceItemSet"];
export type PriceItemSetInput = S["PriceItemSetInput"];
export type PriceProposal = S["PriceProposal"];
export type PriceProposalInput = S["PriceProposalInput"];
export type PriceWriteResult = S["PriceWriteResult"];
export type PriceCatalog = S["PriceCatalog"];

export interface ItemQuery {
  at?: string;
  group?: string;
  kind?: "basic" | "local";
  state?: "active" | "pending" | "retired";
  q?: string;
}

export function createPriceItemApi(api: ApiClient) {
  const at = (d?: string) => (d ? { at: d } : {});
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  return {
    catalog: (day?: string): Promise<PriceCatalog> =>
      unwrap(api.GET("/v1/price/catalog", { params: { query: at(day) } })),
    items: (q: ItemQuery): Promise<PriceItemList> =>
      unwrap(api.GET("/v1/price/items", { params: { query: q } })),
    item: (code: string, day?: string): Promise<PriceItemDetail> =>
      unwrap(
        api.GET("/v1/price/items/{code}", {
          params: { path: { code }, query: at(day) },
        }),
      ),
    createItem: (body: PriceItemInput): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/items", { params: { header: header() }, body }),
      ),
    reviseItem: (
      code: string,
      body: PriceItemInput,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/items/{code}/revise", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    retireItem: (
      code: string,
      body: { valid_to?: string; reason: string },
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/items/{code}/retire", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    moveItem: (
      code: string,
      direction: "up" | "down",
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/items/{code}/move", {
          params: { header: header(), path: { code } },
          body: { direction },
        }),
      ),
    groups: (day?: string): Promise<{ groups: PriceGroup[] }> =>
      unwrap(api.GET("/v1/price/groups", { params: { query: at(day) } })),
    createGroup: (body: {
      code?: string;
      name: string;
      sort_order?: number;
    }): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/groups", { params: { header: header() }, body }),
      ),
    reviseGroup: (
      code: string,
      body: { name: string; sort_order?: number },
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/groups/{code}/revise", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    retireGroup: (code: string): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/groups/{code}/retire", {
          params: { header: header(), path: { code } },
        }),
      ),
    assignments: (
      point?: string,
      day?: string,
    ): Promise<{ assignments: PriceAssignment[] }> =>
      unwrap(
        api.GET("/v1/price/assignments", {
          params: { query: { ...at(day), ...(point ? { point } : {}) } },
        }),
      ),
    assign: (
      point_code: string,
      item_codes: string[],
      valid_from?: string,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/assignments", {
          params: { header: header() },
          body: {
            point_code,
            item_codes,
            ...(valid_from ? { valid_from } : {}),
          },
        }),
      ),
    unassign: (
      point_code: string,
      item_codes: string[],
      valid_from?: string,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/assignments/remove", {
          params: { header: header() },
          body: {
            point_code,
            item_codes,
            ...(valid_from ? { valid_from } : {}),
          },
        }),
      ),
    pointItems: (
      point: string,
      day?: string,
    ): Promise<{ day: string; point_code: string; items: PriceItem[] }> =>
      unwrap(
        api.GET("/v1/price/point-items", {
          params: { query: { point, ...at(day) } },
        }),
      ),
    sets: (day?: string): Promise<{ sets: PriceItemSet[] }> =>
      unwrap(api.GET("/v1/price/item-sets", { params: { query: at(day) } })),
    createSet: (body: PriceItemSetInput): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/item-sets", { params: { header: header() }, body }),
      ),
    reviseSet: (
      code: string,
      body: PriceItemSetInput,
    ): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/item-sets/{code}/revise", {
          params: { header: header(), path: { code } },
          body,
        }),
      ),
    proposals: (
      status?: "pending" | "approved" | "rejected",
    ): Promise<{ proposals: PriceProposal[] }> =>
      unwrap(
        api.GET("/v1/price/proposals", {
          params: { query: status ? { status } : {} },
        }),
      ),
    propose: (body: PriceProposalInput): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/proposals", { params: { header: header() }, body }),
      ),
    approve: (id: string, body: PriceItemInput): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/proposals/{id}/approve", {
          params: { header: header(), path: { id } },
          body,
        }),
      ),
    reject: (id: string, note: string): Promise<PriceWriteResult> =>
      unwrap(
        api.POST("/v1/price/proposals/{id}/reject", {
          params: { header: header(), path: { id } },
          body: { note },
        }),
      ),
  };
}

export type PriceItemApi = ReturnType<typeof createPriceItemApi>;

export const KIND_LABEL: Record<PriceItem["kind"], string> = {
  basic: "Cơ bản",
  local: "Địa phương",
};
export const STATE_LABEL: Record<PriceItem["state"], string> = {
  active: "Hoạt động",
  pending: "Chờ hiệu lực",
  retired: "Ngưng",
};
export const PROPOSAL_LABEL: Record<PriceProposal["status"], string> = {
  pending: "Chờ xử lý",
  approved: "Đã duyệt",
  rejected: "Từ chối",
};

/** dd/mm/yyyy từ yyyy-mm-dd. */
export function dmy(day: string | null | undefined): string {
  if (!day) return "—";
  const [y, m, d] = day.split("-");
  return `${d}/${m}/${y}`;
}

/** Tên hiển thị của mặt hàng: loại + giống/phẩm cấp (BR-07). */
export function itemLabel(i: { name: string; variety: string }): string {
  return `${i.name} ${i.variety}`;
}

/** Khoảng hiệu lực "từ – đến". */
export function validRange(i: PriceItem): string {
  return `${dmy(i.since ?? i.valid_from)} – ${i.valid_to ? dmy(i.valid_to) : "…"}`;
}
