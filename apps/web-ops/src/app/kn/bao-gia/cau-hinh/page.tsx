"use client";

import { PriceCalendarScreen } from "@/kn/price/CalendarConfig";

/**
 * Cấp Tỉnh có cờ Quản trị nghiệp vụ báo giá (W11.72): cùng màn lịch báo giá W11.74 của Admin
 * (/kn/phong-ban/[dept]/bao-gia-lich) — FN-AD-04.04 Kỳ báo giá & giờ khóa, FN-AD-04.05 Nhắc, FN-AD-04.06 Ngưỡng.
 */
export default function Page() {
  return <PriceCalendarScreen />;
}
