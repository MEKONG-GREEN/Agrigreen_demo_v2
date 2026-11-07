import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { stayLabel, type WsCheckin, type WsListItem } from "./model";
import { CheckinVisits, VisitMonitor, visitRows } from "./Visits";

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
  no_checkout: false,
};

const legacy: WsCheckin = { ...base, id: "c2", item_id: "i2", captured_at: "2026-10-05T01:00:00Z", photo: null, legacy_no_photo: true, checkout: null, no_checkout: false };

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

// CNTS-V4-CHECKIN-MONITOR (#250, FN-12 BR-07, Hình 49): nhãn "Chưa check-out" (máy chủ tính lúc 23:59 giờ Việt Nam) và màn
// theo dõi check-in, check-out của cán bộ xã.
const open: WsCheckin = {
  ...base,
  id: "c3",
  item_id: "i3",
  captured_at: "2026-10-06T01:00:00Z",
  photo: { content_type: "image/jpeg", size_bytes: 1200 },
  legacy_no_photo: false,
  checkout: null,
  no_checkout: true,
};

const item = (id: string, day: string, content: string) =>
  ({ id, day, session: "morning", content, commune_code: "30001", changed: false, conflict: false }) as WsListItem["items"][number];

const schedule = (over: Partial<WsListItem>): WsListItem =>
  ({
    id: "s1",
    owner_level: "commune",
    owner_display: "Nguyễn Văn A",
    commune_codes: ["30001"],
    checkin_enabled: true,
    items: [],
    checkins: [],
    ...over,
  }) as WsListItem;

describe("nhãn Chưa check-out (BR-07)", () => {
  it("mục mang no_checkout hiện nhãn cảnh báo; chưa tới 23:59 chỉ ghi mờ", () => {
    render(<CheckinVisits checkins={[open, { ...open, id: "c4", item_id: "i4", no_checkout: false }]} itemLabel={(id) => id} loadPhoto={vi.fn()} />);
    const [, flagged, pending] = within(screen.getByRole("table")).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement];
    expect(within(flagged).getByTitle("Đã check-in, đến 23:59 cùng ngày chưa check-out")).toHaveTextContent("Chưa check-out");
    expect(within(pending).queryByTitle("Đã check-in, đến 23:59 cùng ngày chưa check-out")).toBeNull();
    expect(pending).toHaveTextContent("Chưa check-out");
  });
});

describe("<VisitMonitor> (Hình 49)", () => {
  const schedules = [
    schedule({
      id: "s1",
      items: [item("i1", "2026-10-06", "Kiểm tra lò mổ"), item("i3", "2026-10-06", "Thăm hộ"), item("i5", "2026-10-07", "Tiêm phòng")],
      checkins: [withCheckout, open],
    }),
    schedule({ id: "s2", owner_level: "province", owner_display: "Cán bộ tỉnh", items: [item("i9", "2026-10-06", "Họp")] }),
    schedule({ id: "s3", owner_display: "Trần Thị B", checkin_enabled: false, items: [item("i8", "2026-10-06", "Họp xã")] }),
  ];

  it("chỉ mục của lịch Cấp Xã có check-in; sắp theo ngày", () => {
    expect(visitRows(schedules).map((r) => r.itemId)).toEqual(["i1", "i3", "i5"]);
  });

  it("check-in, check-out, thời gian ở địa điểm, Đồng bộ trễ, nhãn Chưa check-out; lọc mục Chưa check-out; xem ảnh", async () => {
    const user = userEvent.setup();
    const loadPhoto = vi.fn(async () => new Blob([new Uint8Array([0xff, 0xd8, 0xff])], { type: "image/jpeg" }));
    URL.createObjectURL = vi.fn(() => "blob:anh");
    URL.revokeObjectURL = vi.fn();
    render(<VisitMonitor schedules={schedules} communeName={(c) => (c === "30001" ? "Xã A" : c)} loadPhoto={loadPhoto} />);
    const [done, flagged, none] = screen.getAllByTestId("visit-row") as [HTMLElement, HTMLElement, HTMLElement];
    expect(done).toHaveTextContent("8 giờ 30 phút");
    expect(done).toHaveTextContent("Đồng bộ trễ");
    expect(done).toHaveTextContent("Xã A");
    expect(within(flagged).getByTitle("Đã check-in, đến 23:59 cùng ngày chưa check-out")).toBeInTheDocument();
    expect(none).toHaveTextContent("Chưa check-in");

    await user.click(screen.getByRole("checkbox", { name: /Chỉ mục Chưa check-out \(1\)/ }));
    expect(screen.getAllByTestId("visit-row")).toHaveLength(1);
    await user.click(within(screen.getByTestId("visit-row")).getByRole("button", { name: "Ảnh check-in" }));
    expect(loadPhoto).toHaveBeenCalledWith("s1", "i3", "checkin");
    expect(await screen.findByRole("img", { name: /Ảnh check-in — Nguyễn Văn A, Sáng · Thăm hộ/ })).toHaveAttribute("src", "blob:anh");
  });
});
