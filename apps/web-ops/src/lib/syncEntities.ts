// Nhóm dữ liệu cho "Đồng bộ ngay" (KN US-SYNC-02): mỗi nhóm là các entity chiều xuống của pf/sync
// (docs/dev/sync-contract.md). Không chọn nhóm nào = đồng bộ tất cả (entities rỗng).

export interface SyncGroup {
  id: string;
  label: string;
  entities: readonly string[];
}

export const SYNC_GROUPS: readonly SyncGroup[] = [
  { id: "catalog", label: "Danh mục & đơn vị hành chính", entities: ["catalog_item", "admin_unit"] },
  { id: "registry", label: "HTX & nông hộ", entities: ["cooperative", "household", "pubkey"] },
  { id: "plots", label: "Thửa ruộng & ranh giới", entities: ["plot"] },
  { id: "seasons", label: "Mùa vụ, nhật ký & SOP", entities: ["season", "farm_log", "sop_version"] },
  { id: "tasks", label: "Nhiệm vụ, chỉ đạo & yêu cầu hỗ trợ", entities: ["task", "directive", "support_request"] },
  { id: "content", label: "Tài liệu & thư viện", entities: ["document", "library_item"] },
  { id: "mrv", label: "Danh mục minh chứng MRV", entities: ["mrv_catalog"] },
  { id: "notify", label: "Thông báo", entities: ["notification"] },
];

/** entities[] gửi kèm sync_now: gộp các nhóm đã chọn, không trùng, giữ thứ tự nhóm. Rỗng = tất cả. */
export function entitiesOf(groupIds: Iterable<string>): string[] {
  const picked = new Set(groupIds);
  const out: string[] = [];
  for (const g of SYNC_GROUPS) {
    if (!picked.has(g.id)) continue;
    for (const e of g.entities) if (!out.includes(e)) out.push(e);
  }
  return out;
}

/** Tên nhóm hiển thị cho danh sách entity máy chủ trả về (NudgeResult.entities). */
export function describeEntities(entities: readonly string[] | null | undefined): string {
  if (!entities || entities.length === 0) return "tất cả dữ liệu";
  const labels = SYNC_GROUPS.filter((g) => g.entities.some((e) => entities.includes(e))).map((g) => g.label);
  return labels.length > 0 ? labels.join(", ") : entities.join(", ");
}

/** Mọi entity đã biết (mock kiểm entities[] như pf/notify). */
export const KNOWN_ENTITIES: ReadonlySet<string> = new Set(SYNC_GROUPS.flatMap((g) => g.entities));
