"use client";

// X-02 (RB-3, W11.02): cảnh báo khi người dùng gõ dữ liệu cá nhân vào ô tự do (ghi chú, lý do, nội dung). Dữ liệu cá nhân
// (SĐT, email, số CCCD/CMND) phải nằm ở kho định danh — ô tự do được lưu nguyên văn trong sổ, nhật ký và tệp gửi Chi cục.
// Chỉ cảnh báo, không chặn: máy chủ không đọc nội dung để đoán PII. Mẫu nhận dạng là tham số tạm (chờ khách, Q-066/X-02).
import { Icon } from "@/components/Icon";

export type PiiKind = "phone" | "email" | "national_id";

const PATTERNS: { kind: PiiKind; re: RegExp }[] = [
  { kind: "email", re: /[^\s@,;]+@[^\s@,;]+\.[a-z]{2,}/i },
  // SĐT Việt Nam: 0xxxxxxxxx hoặc +84xxxxxxxxx, cho phép dấu cách/chấm/gạch giữa các nhóm số.
  { kind: "phone", re: /(?:\+?84|0)(?:[\s.-]?\d){9}(?!\d)/ },
  // CCCD 12 số / CMND 9 số đứng riêng (không phải một phần của số dài hơn).
  { kind: "national_id", re: /(?<!\d)(?:\d{12}|\d{9})(?!\d)/ },
];

const LABEL: Record<PiiKind, string> = { phone: "số điện thoại", email: "email", national_id: "số CCCD/CMND" };

/** Trả các loại dữ liệu cá nhân nhận ra trong chuỗi (không trả chính giá trị). */
export function detectPii(text: string | null | undefined): PiiKind[] {
  if (!text) return [];
  const out: PiiKind[] = [];
  for (const p of PATTERNS) {
    if (p.re.test(text) && !out.includes(p.kind)) {
      // Số 9–12 chữ số đã khớp là SĐT thì không báo thêm CCCD.
      if (p.kind === "national_id" && out.includes("phone") && !/(?<!\d)\d{12}(?!\d)/.test(text)) continue;
      out.push(p.kind);
    }
  }
  return out;
}

/** Dòng cảnh báo dưới ô tự do khi phát hiện dữ liệu cá nhân (role="status", không chặn lưu). */
export function PiiHint({ text }: { text: string | null | undefined }) {
  const kinds = detectPii(text);
  if (!kinds.length) return null;
  return (
    <p className="small" role="status" data-testid="pii-hint" style={{ color: "var(--warn, #b45309)", marginTop: 4, display: "flex", gap: 6, alignItems: "center" }}>
      <Icon name="alert" size={14} />
      <span>
        Ô này có vẻ chứa {kinds.map((k) => LABEL[k]).join(", ")}. Không ghi dữ liệu cá nhân vào ô tự do — nội dung được lưu nguyên văn trong nhật ký và báo cáo.
      </span>
    </p>
  );
}
