// Dữ liệu test (#162): hồ sơ KN loại HTX và GET /v1/kn/producer-records giả lập theo trang/tìm như máy chủ.
import { vi } from "vitest";
import type { KnProducer, KnProducerPageQuery } from "../api";

export function knHtxRecord(ref: string, name: string, htxRef: string | null, extra: Partial<KnProducer> = {}): KnProducer {
  return {
    ref,
    kind: "cooperative",
    kind_label: "HTX",
    commune_code: "91301",
    name,
    display_name: name,
    representative: { label: "Nguyễn V. B.", masked_phone: "0913•••101" },
    holder: null,
    parent_ref: null,
    parent_name: null,
    parent_kind: null,
    htx_ref: htxRef,
    htx_name: htxRef ? name : null,
    area_ha: null,
    tax_code: null,
    address: null,
    member_count: 0,
    status: "active",
    status_reason: null,
    version: 1,
    can_edit: true,
    updated_at: "2026-10-01T00:00:00Z",
    ...extra,
  } as KnProducer;
}

/** records(q) giả lập: lọc kind/commune/status/q/htx_ref, phân trang theo con trỏ (cursor = ref cuối trang trước). */
export function recordsMock(list: KnProducer[]) {
  return vi.fn(async (q: KnProducerPageQuery) => {
    const st = q.status ?? "active";
    const items = list.filter(
      (r) =>
        r.kind === q.kind &&
        (!q.commune || r.commune_code === q.commune) &&
        (st === "all" || r.status === st) &&
        (!q.q || (r.name ?? "").toLowerCase().includes(q.q.toLowerCase())) &&
        (!q.htx_ref || r.htx_ref === q.htx_ref),
    );
    const lim = q.limit ?? 50;
    const start = q.cursor ? items.findIndex((r) => r.ref === q.cursor) + 1 : 0;
    const page = items.slice(start, start + lim);
    return { items: page, next_cursor: start + lim < items.length ? page.at(-1)!.ref : null };
  });
}
