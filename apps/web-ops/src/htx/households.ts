// Quy tắc phía client của màn "Quản lý nông hộ" (HTX US-HH-01/02/03). Chỉ để báo lỗi sớm — máy chủ
// kiểm lại mọi thứ (SĐT trùng tra chỉ mục mù ở kho định danh).
import { normalizePhone } from "./rules";

/** Một hồ sơ sao chép đang sửa trên màn: họ tên/SĐT của hồ sơ MỚI (không chép định danh hộ nguồn). */
export interface CopyDraft {
  sourceRef: string;
  sourceLabel: string;
  fullName: string;
  phone: string;
}

/** Lỗi theo hồ sơ trước khi gửi (AC-4 cùng nhãn với máy chủ). */
export function copyDraftErrors(rows: readonly CopyDraft[]): (string | null)[] {
  const seen = new Map<string, number>();
  return rows.map((r, i) => {
    if (!r.fullName.trim()) return "Thiếu họ tên";
    if (!r.phone.trim()) return "Thiếu SĐT";
    const p = normalizePhone(r.phone);
    if (!p) return "SĐT phải có dạng 0xxxxxxxxx (10 chữ số) hoặc +84…";
    const prev = seen.get(p);
    if (prev !== undefined) return `SĐT trùng với hồ sơ ${prev + 1} trong lần sao chép này`;
    seen.set(p, i);
    return null;
  });
}

/** Thân POST /v1/households/copy từ các hồ sơ hợp lệ (SĐT đã chuẩn hoá). */
export function copyPayload(rows: readonly CopyDraft[]): { source_ref: string; full_name: string; phone: string }[] {
  return rows.map((r) => ({ source_ref: r.sourceRef, full_name: r.fullName.trim(), phone: normalizePhone(r.phone) ?? r.phone.trim() }));
}

/**
 * Ô tìm nông hộ (AC-2): chỉ SĐT mới gửi lên máy chủ (tra chỉ mục mù). Họ tên chủ hộ nằm ở kho định danh
 * nên không tìm được theo tên — trả lý do để màn hình giải thích.
 */
export function householdQuery(raw: string): { phone: string } | { error: string } | null {
  const s = raw.trim();
  if (!s) return null;
  const p = normalizePhone(s);
  if (p) return { phone: p };
  if (/\d/.test(s)) return { error: "SĐT phải có dạng 0xxxxxxxxx (10 chữ số) hoặc +84…" };
  return { error: "Chỉ tìm được theo SĐT: họ tên chủ hộ được lưu mã hoá trong kho định danh, máy chủ không lọc theo tên." };
}

/** Thông báo AC-2 của HTX US-HH-03 khi thiết bị không gọi được. */
export const CANNOT_CALL = "Không thể khởi tạo cuộc gọi trên thiết bị này.";

/**
 * Thiết bị có khả năng gọi thoại? Trình duyệt không cho biết chắc chắn; dùng dấu hiệu màn hình cảm ứng/di động.
 * Máy tính bàn vẫn mở được `tel:` nếu có ứng dụng gọi (Skype, Phone Link), nên chỉ dùng để hiện gợi ý.
 */
export function likelyCanCall(nav: { maxTouchPoints?: number; userAgent?: string } | undefined): boolean {
  if (!nav) return false;
  return (nav.maxTouchPoints ?? 0) > 0 || /Android|iPhone|iPad|Mobile/i.test(nav.userAgent ?? "");
}
