// Mã đã dùng gần đây trong tab (lô, phiên cân, phiếu chuyển): api-internal chưa có route liệt kê các thực thể này
// (xem README — khoảng trống hợp đồng), nên giao diện nhớ mã người dùng vừa tạo/xem để chọn lại nhanh.
// Chỉ trong bộ nhớ tab — không lưu trình duyệt (không có dữ liệu cá nhân, nhưng cũng không cần bền).

export type RecentKind = "lot" | "session" | "transfer" | "htx" | "facility";
export interface RecentItem {
  kind: RecentKind;
  id: string;
  label: string;
}

const items: RecentItem[] = [];
const MAX = 30;

export function remember(kind: RecentKind, id: string, label?: string): void {
  if (!id) return;
  const i = items.findIndex((x) => x.kind === kind && x.id === id);
  const prev = i >= 0 ? items.splice(i, 1)[0] : undefined;
  items.unshift({ kind, id, label: label ?? prev?.label ?? id });
  const same = items.filter((x) => x.kind === kind);
  if (same.length > MAX) items.splice(items.lastIndexOf(same[same.length - 1]!), 1);
}

export function recent(kind: RecentKind): RecentItem[] {
  return items.filter((x) => x.kind === kind);
}

export function labelOf(kind: RecentKind, id: string): string | undefined {
  return items.find((x) => x.kind === kind && x.id === id)?.label;
}
