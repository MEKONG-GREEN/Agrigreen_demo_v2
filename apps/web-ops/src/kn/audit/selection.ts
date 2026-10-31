// AUDIT-LOG-KN (#73): dòng nhật ký của trang vừa xem, giữ TRONG BỘ NHỚ (không localStorage/sessionStorage) để trang
// Chi tiết hiện lại mà không gọi máy chủ lần nữa — dòng này đã được máy chủ ghi lượt xem khi tải danh sách. Tải lại
// trình duyệt thì mất: trang Chi tiết yêu cầu mở lại từ danh sách.
import type { AuditItem } from "../cycle/api";

let rows: AuditItem[] = [];
let names = new Map<string, string>();

export function rememberAuditPage(items: AuditItem[], actorNames: Map<string, string>): void {
  rows = items.slice();
  names = new Map(actorNames);
}

export function findAuditItem(eventRef: string): { item: AuditItem; names: Map<string, string> } | null {
  const item = rows.find((r) => r.event_ref === eventRef);
  return item ? { item, names } : null;
}

/** Chỉ dùng trong kiểm thử. */
export function resetAuditSelection(): void {
  rows = [];
  names = new Map();
}
