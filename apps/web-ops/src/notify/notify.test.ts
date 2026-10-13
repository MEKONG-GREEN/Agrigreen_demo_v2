import { describe, expect, it } from "vitest";

import { KIND_LABEL } from "./notify";

// Gói NOTIF: chuông thông báo hiện nhãn tiếng Việt cho các loại thông báo Báo giá nông sản (pf 000159).
describe("KIND_LABEL — Báo giá nông sản", () => {
  it("có nhãn tiếng Việt cho mọi loại price_*", () => {
    const kinds = [
      "price_reminder",
      "price_alert",
      "price_verify_request",
      "price_verify_reply",
      "price_verify_decision",
      "price_lock_summary",
      "price_board_approved",
      "price_board_reopened",
    ] as const;
    for (const k of kinds) {
      expect(KIND_LABEL[k]).toBeTruthy();
      expect(KIND_LABEL[k]).not.toBe(k);
    }
    expect(KIND_LABEL.price_verify_request).toBe("Yêu cầu xác minh giá");
    expect(KIND_LABEL.price_lock_summary).toBe("Tổng hợp khóa nhập giá");
  });
});
