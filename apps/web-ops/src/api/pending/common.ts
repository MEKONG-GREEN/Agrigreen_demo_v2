// ĐANG CHỜ HỢP ĐỒNG: kiểu dữ liệu cho các endpoint CHƯA có trong
// contracts/openapi/application.yaml. Khi backend thêm route vào OpenAPI, chạy
// `npm run gen:api` và chuyển nơi dùng sang kiểu sinh tự động, rồi xoá tệp tương ứng.
//
// Quy ước (docs/dev/g1-conventions.md §3–4):
// - diện tích (ha), tỷ lệ % là decimal → truyền dạng CHUỖI thập phân, không float (R-10);
// - định danh UUIDv7 (R-11); thời điểm ISO 8601 có múi giờ;
// - phân trang cursor {items, next_cursor} (R-53);
// - dữ liệu cá nhân nông hộ không bao giờ ra khỏi vault: web chỉ nhận nhãn đã che (R-04, R-14).

/** Số thập phân dạng chuỗi, vd "312480.5000". */
export type Decimal = string;
/** Ngày giờ ISO 8601, vd "2026-09-27T09:20:00+07:00". */
export type DateTime = string;
/** Ngày, vd "2026-10-01". */
export type DateOnly = string;
export type Uuid = string;

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface PageQuery {
  cursor?: string;
  limit?: number;
}

/** Cấp phạm vi theo cây TW → Tỉnh → Xã (ADR-0009) + Cục KTHT (xem toàn vùng). */
export type ScopeLevel = "tw" | "tinh" | "xa" | "cuc";

export type OrgKind =
  | "ttkn_national"
  | "ttkn_province"
  | "extension_team"
  | "dcrd"
  | "cooperative"
  | "mkg"
  | "partner";

export type AdminUnitLevel = "province" | "commune";
