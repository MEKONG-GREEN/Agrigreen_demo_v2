/** Số điện thoại → URI tel:/sms: cho "gọi/nhắn 1 chạm" (KN US-DIR-02). Bỏ khoảng trắng, dấu chấm, gạch. */
export function dialUri(scheme: "tel" | "sms", phone: string): string {
  return `${scheme}:${phone.replace(/[^\d+]/g, "")}`;
}
