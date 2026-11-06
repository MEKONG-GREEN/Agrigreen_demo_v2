import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { stayLabel, type WsCheckin } from "./model";
import { CheckinVisits } from "./Visits";

// CNTS-V4-CHECKOUT-PHOTO (#249, BRD CNTS v4.0 FN-12): check-in/check-out kèm ảnh, thời gian ở địa điểm, Đồng bộ trễ.
const base = {
  day: "2026-10-06",
  recorded_at: "2026-10-06T01:00:05Z",
  accuracy_m: 10,
  distance_m: 111,
  far: false,
  weak_accuracy: false,
  mock_location: false,
  point_stored: true,
  location: null,
} as const;

const withCheckout: WsCheckin = {
  ...base,
  id: "c1",
  item_id: "i1",
  captured_at: "2026-10-06T01:00:00Z", // 08:00 giờ Việt Nam
  photo: { content_type: "image/jpeg", size_bytes: 1200 },
  legacy_no_photo: false,
  checkout: {
    ...base,
    id: "o1",
    checkin_id: "c1",
    item_id: "i1",
    captured_at: "2026-10-06T09:30:00Z", // 16:30
    distance_m: 900,
    far: true,
    photo: { content_type: "image/jpeg", size_bytes: 900 },
    stay_minutes: 510,
    synced_at: "2026-10-07T00:10:00Z", // 07:10 ngày 07/10
    synced_late: true,
  },
};

const legacy: WsCheckin = { ...base, id: "c2", item_id: "i2", captured_at: "2026-10-05T01:00:00Z", photo: null, legacy_no_photo: true, checkout: null };

describe("thời gian ở địa điểm (BR-07)", () => {
  it("AC-12: 08:00 → 11:30 hiện 3 giờ 30 phút", () => {
    expect(stayLabel(210)).toBe("3 giờ 30 phút");
    expect(stayLabel(120)).toBe("2 giờ");
    expect(stayLabel(45)).toBe("45 phút");
    expect(stayLabel(-3)).toBe("0 phút");
  });
});

describe("<CheckinVisits>", () => {
  it("check-out kèm Đồng bộ trễ, xa địa điểm, ảnh; check-in cũ ghi Dữ liệu cũ, không ảnh", async () => {
    const user = userEvent.setup();
    const loadPhoto = vi.fn(async () => new Blob([new Uint8Array([0xff, 0xd8, 0xff])], { type: "image/jpeg" }));
    const created = vi.fn(() => "blob:anh");
    URL.createObjectURL = created;
    URL.revokeObjectURL = vi.fn();
    render(<CheckinVisits checkins={[withCheckout, legacy]} itemLabel={(id) => (id === "i1" ? "T3 06/10 Kiểm tra lò mổ" : "T2 05/10 Họp")} loadPhoto={loadPhoto} />);
    const [, first, second] = within(screen.getByRole("table")).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Đồng bộ trễ");
    expect(first).toHaveTextContent("8 giờ 30 phút");
    expect(first).toHaveTextContent("Check-out: 900 m · xa");
    expect(second).toHaveTextContent("Dữ liệu cũ — không ảnh");
    expect(second).toHaveTextContent("Chưa check-out");
    expect(within(second).queryByRole("button")).toBeNull();
    await user.click(within(first).getByRole("button", { name: "Ảnh check-out" }));
    expect(loadPhoto).toHaveBeenCalledWith("i1", "checkout");
    expect(await screen.findByRole("img", { name: /Ảnh check-out — T3 06\/10 Kiểm tra lò mổ/ })).toHaveAttribute("src", "blob:anh");
  });
});
