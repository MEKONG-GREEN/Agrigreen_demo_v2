// Dữ liệu giả lập (tất định) của HTX thuộc persona "Quản lý HTX" — xem src/api/mock/cropcycle.ts:
// thửa đang dùng thứ i (theo thứ tự plotsOf): i % 5 = 0 → chỉ có vụ Đông Xuân đã hoàn thành (sao chép được);
// 1, 2 → vụ Hè Thu đang mở (chưa tới thu hoạch); 3 → vụ đang mở đã tới thu hoạch; 4 → chưa có vụ nào.
// Thửa i = 1 có nhật ký chờ duyệt do chính Quản lý HTX ghi hộ (thử quy tắc không tự duyệt).
import { householdRefOf, householdsOf, htxCoop, plotsOf, syntheticPhone } from "../../src/api/mock/data";

export const coop = htxCoop();
export const active = plotsOf(coop).filter((p) => p.properties.status === "active" && p.properties.cooperative_ref === coop.api.ref);
export const plot = (i: number) => active[i]!.properties;
export const byKind = (k: number) => active.map((p, i) => ({ p: p.properties, i })).filter(({ i }) => i % 5 === k).map(({ p }) => p);

/** Nông hộ i của HTX (nhãn đã che, SĐT tổng hợp 09990xxxxx, mã nông hộ). */
export const household = (i: number) => ({ label: householdsOf(coop)[i]!.label, phone: syntheticPhone(coop, i), ref: householdRefOf(coop, i) });

export function todayVN(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}
