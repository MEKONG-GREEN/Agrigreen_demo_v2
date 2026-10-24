import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetWorkSchedules } from "@/api/mock/kn-schedules";
import { createWorkScheduleApi } from "./api";
import { ApproveSchedules } from "./ApproveSchedules";
import { conflictIndexes, diffItems, itemInterval, looksLikePersonalNumber, placeByDay, placeBySession, statusLabel, timeError, type WsItemInput } from "./model";
import { RegisterSchedule } from "./RegisterSchedule";
import { ScheduleDetail } from "./ScheduleDetail";
import {
  addDays,
  currentWeekStart,
  dayColumnLabel,
  isMonday,
  isWeekLocked,
  mondayOf,
  nextWeekStart,
  submitDueAt,
  vnDate,
  vnDeadlineLabel,
  weekDays,
  weekRangeLabel,
} from "./week";

const push = vi.fn();
vi.mock("next/navigation", () => ({ usePathname: () => "/kn/nhiem-vu/lich-cong-tac", useRouter: () => ({ replace: vi.fn(), push }) }));

describe("tuần công tác (Thứ Hai, giờ Việt Nam)", () => {
  it("ngày Việt Nam lệch UTC+7: 23:30Z Chủ Nhật là sáng Thứ Hai ở Việt Nam", () => {
    expect(vnDate(Date.parse("2026-10-04T16:59:00Z"))).toBe("2026-10-04");
    expect(vnDate(Date.parse("2026-10-04T17:00:00Z"))).toBe("2026-10-05");
    expect(currentWeekStart(Date.parse("2026-10-04T16:59:00Z"))).toBe("2026-09-28");
    expect(currentWeekStart(Date.parse("2026-10-04T17:00:00Z"))).toBe("2026-10-05");
    expect(nextWeekStart(Date.parse("2026-10-02T03:00:00Z"))).toBe("2026-10-05");
  });

  it("Thứ Hai đầu tuần, qua tháng/năm, 7 ngày, nhãn cột", () => {
    expect(mondayOf("2026-01-01")).toBe("2025-12-29");
    expect(isMonday("2026-10-05")).toBe(true);
    expect(isMonday("2026-10-06")).toBe(false);
    expect(isMonday("2026-02-30")).toBe(false);
    expect(weekDays("2025-12-29").at(-1)).toBe("2026-01-04");
    expect(addDays("2026-02-28", 1)).toBe("2026-03-01");
    expect(dayColumnLabel("2026-10-05")).toBe("T2 05/10");
    expect(dayColumnLabel("2026-10-11")).toBe("CN 11/10");
    expect(weekRangeLabel("2026-09-08")).toBe("08/09 – 14/09/2026");
  });

  it("hạn đăng ký mặc định 17:00 Thứ Sáu tuần trước; tuần khóa sau 24:00 Chủ Nhật", () => {
    expect(submitDueAt("2026-10-05")).toBe("2026-10-02T10:00:00.000Z");
    expect(vnDeadlineLabel(submitDueAt("2026-09-07"))).toBe("Thứ Sáu 04/09 17:00");
    expect(isWeekLocked("2026-09-28", Date.parse("2026-10-04T16:59:59Z"))).toBe(false);
    expect(isWeekLocked("2026-09-28", Date.parse("2026-10-04T17:00:00Z"))).toBe(true);
  });
});

const it_ = (day: string, session: "morning" | "afternoon", start: string | null, end: string | null, content = "Mục", id?: string): WsItemInput => ({ id, day, session, start_time: start, end_time: end, content });

describe("trùng giờ, xếp lưới, so bản đã duyệt", () => {
  it("trùng khi cùng ngày và khoảng giờ giao nhau; thiếu giờ = cả buổi", () => {
    const items = [
      it_("2026-10-06", "morning", "07:30", "10:00"),
      it_("2026-10-06", "morning", "09:00", "11:00"),
      it_("2026-10-06", "afternoon", "13:00", "15:00"),
      it_("2026-10-07", "morning", "07:30", "10:00"),
      it_("2026-10-08", "afternoon", null, null),
      it_("2026-10-08", "afternoon", "14:00", "15:00"),
      it_("2026-10-09", "morning", "07:00", "08:00"),
      it_("2026-10-09", "morning", "08:00", "09:00"),
    ];
    expect([...conflictIndexes(items)].sort()).toEqual([0, 1, 4, 5]);
    expect(itemInterval(it_("d", "afternoon", null, null))).toEqual([13 * 60, 17 * 60]);
    expect(timeError({ start_time: "10:00", end_time: "09:00" })).toMatch(/sau giờ bắt đầu/);
    expect(timeError({ start_time: "10:00", end_time: null })).toBeNull();
  });

  it("lưới Sáng/Chiều × 7 ngày và lưới theo ngày (sáng trước chiều, theo giờ)", () => {
    const days = weekDays("2026-10-05");
    const items = [it_("2026-10-06", "afternoon", "13:00", null, "C"), it_("2026-10-06", "morning", "09:00", null, "B"), it_("2026-10-06", "morning", "07:00", null, "A"), it_("2026-10-20", "morning", null, null, "ngoài tuần")];
    const g = placeBySession(items, days);
    expect(g.morning[1]!.map((x) => x.content)).toEqual(["A", "B"]);
    expect(g.afternoon[1]!.map((x) => x.content)).toEqual(["C"]);
    expect(g.morning.flat()).toHaveLength(2);
    expect(placeByDay(items, days)[1]!.map((x) => x.content)).toEqual(["A", "B", "C"]);
  });

  it("so theo mã mục: thêm, sửa (trường nào), bỏ", () => {
    const before = [it_("2026-10-05", "morning", "07:30", null, "Thăm đồng", "a"), it_("2026-10-06", "morning", null, null, "Họp", "b")];
    const after = [{ ...before[0]!, start_time: "08:00", content: "Thăm đồng lúa" }, it_("2026-10-07", "afternoon", null, null, "Mới", "c")];
    const d = diffItems(before, after);
    expect(d.added.map((x) => x.id)).toEqual(["c"]);
    expect(d.removed.map((x) => x.id)).toEqual(["b"]);
    expect(d.modified[0]!.fields).toEqual(["start_time", "content"]);
  });

  it("RB-3: phát hiện SĐT/CCCD trong thành phần phối hợp; nhãn trạng thái điều chỉnh", () => {
    expect(looksLikePersonalNumber("Anh Tư 0912 345 678")).toBe(true);
    expect(looksLikePersonalNumber("CCCD 089123456789")).toBe(true);
    expect(looksLikePersonalNumber("Cán bộ HTX, BVTV, tổ 12")).toBe(false);
    expect(statusLabel({ status: "pending", amendment: true })).toBe("Chờ duyệt điều chỉnh");
    expect(statusLabel({ status: "changes_requested", amendment: false })).toBe("Yêu cầu chỉnh sửa");
  });
});

// ---------------------------------------------------------------------------
// Màn hình trên backend giả lập (không độ trễ)
// ---------------------------------------------------------------------------
async function clientFor(personaId: string, assignmentId: string) {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${personaId}-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
  return { api, ws: createWorkScheduleApi(api) };
}

const AN_COMMUNES = [
  { code: "91301", name: "Mỹ Hòa Hưng" },
  { code: "91302", name: "Bình Đức" },
];

beforeEach(() => {
  resetWorkSchedules();
  push.mockReset();
});

describe("<RegisterSchedule> Cấp Xã (Hình 21)", { timeout: 30_000 }, () => {
  it("mặc định tuần sau, chưa đăng ký; thêm mục → Lưu nháp → Gửi duyệt; Log ghi sự kiện", async () => {
    const user = userEvent.setup();
    const { api, ws } = await clientFor("kn-an", "asg-an-xa");
    render(<RegisterSchedule ws={ws} api={api} communes={AN_COMMUNES} />);
    expect(screen.getByRole("heading", { level: 1, name: "Lịch làm việc hằng tuần" })).toBeInTheDocument();
    const next = nextWeekStart();
    expect(await screen.findByText(/Hạn đăng ký:/)).toBeInTheDocument();
    expect(screen.getByLabelText("Tuần")).toHaveValue(next);
    expect(screen.getByText("Chưa đăng ký")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Gửi duyệt" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: `Thêm mục ${dayColumnLabel(addDays(next, 1))} Sáng` }));
    const form = screen.getByRole("region", { name: "Mục công tác" });
    await user.type(within(form).getByLabelText("Nội dung công việc"), "Kiểm tra rầy nâu");
    // Log bug TT-05/CN-04: Xã/phường và Địa điểm bắt buộc.
    await user.click(within(form).getByRole("button", { name: /Lưu mục/ }));
    expect(within(form).getByRole("alert")).toHaveTextContent("Xã/phường bắt buộc.");
    await user.selectOptions(within(form).getByLabelText("Địa điểm: xã/phường"), "91302");
    await user.click(within(form).getByRole("button", { name: /Lưu mục/ }));
    expect(within(form).getByRole("alert")).toHaveTextContent("Địa điểm bắt buộc");
    await user.type(within(form).getByLabelText("Địa điểm cụ thể"), "Cánh đồng ấp 2");
    await within(form).findByRole("option", { name: "HTX Bình Đức" });
    await user.selectOptions(within(form).getByLabelText("Địa điểm: HTX"), "HTX Bình Đức");
    await user.type(within(form).getByLabelText("Thành phần phối hợp"), "0912345678");
    expect(within(form).getByRole("alert")).toHaveTextContent("Không ghi số điện thoại");
    await user.clear(within(form).getByLabelText("Thành phần phối hợp"));
    await user.type(within(form).getByLabelText("Thành phần phối hợp"), "Cán bộ HTX, BVTV");
    await user.click(within(form).getByRole("button", { name: /Lưu mục/ }));

    const card = await screen.findByText("Kiểm tra rầy nâu");
    expect(card.closest("[data-testid=ws-item]")).toHaveTextContent("Bình Đức · HTX Bình Đức");
    expect(screen.getByText("Có thay đổi chưa lưu")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    expect(await screen.findByText("Đã lưu nháp.")).toBeInTheDocument();
    expect(screen.getByText("Nháp", { selector: ".pill" })).toBeInTheDocument();
    const log = screen.getByRole("complementary", { name: "Log" });
    await waitFor(() => expect(log).toHaveTextContent("Lưu nháp"));
    expect(log).toHaveTextContent("Nguyễn Văn An");

    await user.click(screen.getByRole("button", { name: "Gửi duyệt" }));
    expect(await screen.findByText(/Đã gửi duyệt/)).toBeInTheDocument();
    expect(screen.getByText("Chờ duyệt", { selector: ".pill" })).toBeInTheDocument();
    await waitFor(() => expect(log).toHaveTextContent("Gửi duyệt"));
    expect(log).toHaveTextContent("Trạng thái: Nháp → Chờ duyệt");
  });

  it("Sao chép tuần trước; lịch đã duyệt tuần này: báo điều chỉnh phải duyệt lại; tuần đã kết thúc: chỉ xem", async () => {
    const user = userEvent.setup();
    const { api, ws } = await clientFor("kn-an", "asg-an-xa");
    render(<RegisterSchedule ws={ws} api={api} communes={AN_COMMUNES} />);
    await screen.findByText(/Hạn đăng ký:/);
    await user.click(screen.getByRole("button", { name: /Sao chép tuần trước/ }));
    expect(await screen.findByText(/Đã sao chép 5 mục từ tuần trước/)).toBeInTheDocument();
    expect(screen.getAllByTestId("ws-item")).toHaveLength(5);

    await user.click(screen.getByRole("button", { name: "Tuần trước" }));
    expect(await screen.findByText(/Lịch đã duyệt\./)).toBeInTheDocument();
    await user.click(screen.getByText("Họp tổ khuyến nông"));
    const form = screen.getByRole("region", { name: "Mục công tác" });
    await user.clear(within(form).getByLabelText("Nội dung công việc"));
    await user.type(within(form).getByLabelText("Nội dung công việc"), "Họp tổ khuyến nông (dời giờ)");
    await user.click(within(form).getByRole("button", { name: /Lưu mục/ }));
    expect(screen.getByText(/1 mục đang điều chỉnh/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lưu nháp" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Gửi duyệt" }));
    expect(await screen.findByText(/Đã gửi điều chỉnh/)).toBeInTheDocument();
    expect(screen.getByText("Phần điều chỉnh đang chờ duyệt lại.")).toBeInTheDocument();
    expect(screen.getByText("Chờ duyệt điều chỉnh", { selector: ".pill" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Tuần trước" }));
    expect(await screen.findByText("Tuần đã kết thúc — lịch bị khóa, chỉ xem.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Gửi duyệt" })).toBeNull();
    expect(screen.queryByRole("region", { name: "Mục công tác" })).toBeNull();
  });
});

describe("<ApproveSchedules> Cấp Tỉnh (Hình 12)", { timeout: 30_000 }, () => {
  it("tab có số đếm, lưới cán bộ × 7 ngày, trùng giờ tô vàng; yêu cầu chỉnh sửa bắt buộc lý do; Chưa đăng ký", async () => {
    const user = userEvent.setup();
    const { api, ws } = await clientFor("kn-binh", "asg-binh-tinh");
    render(<ApproveSchedules ws={ws} api={api} />);
    expect(screen.getByRole("heading", { level: 1, name: "Lịch công tác · duyệt lịch Cấp Xã" })).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "Chờ duyệt (2)" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Chưa đăng ký (4)" })).toBeInTheDocument();
    const rows = screen.getAllByTestId("ws-row");
    expect(rows.map((r) => within(r).getByRole("link").textContent)).toEqual(["Lý Thị Hoa", "Lê Thị Mai"]);
    await waitFor(() => expect(rows[0]).toHaveTextContent("Vĩnh Tế"));
    expect(rows[0]).toHaveTextContent("2 mục trùng giờ");
    expect(within(rows[0]!).getAllByTestId("ws-item").filter((x) => x.className.includes("is-conflict"))).toHaveLength(2);

    expect(screen.getByRole("button", { name: /Duyệt lịch đã chọn/ })).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Chọn lịch của Lý Thị Hoa" }));
    await user.click(screen.getByRole("button", { name: /Yêu cầu chỉnh sửa/ }));
    const dlg = screen.getByRole("dialog", { name: "Yêu cầu chỉnh sửa lịch công tác" });
    await user.type(within(dlg).getByRole("textbox"), "Ngắn");
    await user.click(within(dlg).getByRole("button", { name: "Gửi yêu cầu" }));
    expect(within(dlg).getByText(/cần ít nhất 10 ký tự/)).toBeInTheDocument();
    await user.type(within(dlg).getByRole("textbox"), " — tách hai mục trùng giờ sáng Thứ Ba");
    await user.click(within(dlg).getByRole("button", { name: "Gửi yêu cầu" }));
    expect(await screen.findByText("Đã yêu cầu chỉnh sửa 1 lịch: Lý Thị Hoa.")).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "Chờ duyệt (1)" })).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /Chưa đăng ký/ }));
    const un = await screen.findAllByTestId("ws-unregistered");
    expect(un.map((r) => r.querySelector("b")?.textContent)).toEqual(["Nguyễn Văn An", "Huỳnh Văn Phúc", "Đặng Thị Thu", "Trần Thị Bình"]);
    await waitFor(() => expect(un[0]).toHaveTextContent("Mỹ Hòa Hưng, Bình Đức"));
    expect(un[2]).toHaveTextContent("Mới lưu nháp");
  });

  it("Duyệt hàng loạt: duyệt mọi lịch Chờ duyệt theo bộ lọc; Lịch của tôi dùng màn đăng ký", async () => {
    const user = userEvent.setup();
    const { api, ws } = await clientFor("kn-binh", "asg-binh-tinh");
    render(<ApproveSchedules ws={ws} api={api} />);
    await screen.findByRole("tab", { name: "Chờ duyệt (2)" });
    await user.click(screen.getByRole("button", { name: /Duyệt hàng loạt/ }));
    await user.click(within(screen.getByRole("dialog", { name: "Duyệt hàng loạt" })).getByRole("button", { name: "Duyệt 2 lịch" }));
    expect(await screen.findByText("Đã duyệt hàng loạt 2 lịch.")).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "Đã duyệt (2)" })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Lịch của tôi" }));
    expect(await screen.findByRole("button", { name: /Sao chép tuần trước/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Duyệt hàng loạt/ })).toBeNull();
  });

  it("BF-SCHED R-B1 (PO 25/10/2026): Lịch của tôi Cấp Tỉnh bắt buộc Xã/phường, chọn trong mọi xã của tỉnh (tuần mới)", async () => {
    const user = userEvent.setup();
    const { api, ws } = await clientFor("kn-binh", "asg-binh-tinh");
    render(<RegisterSchedule ws={ws} api={api} communes={[]} embedded />);
    await screen.findByText(/Hạn đăng ký:/);
    const next = nextWeekStart();
    await user.click(screen.getByRole("button", { name: `Thêm mục ${dayColumnLabel(addDays(next, 2))} Chiều` }));
    const form = screen.getByRole("region", { name: "Mục công tác" });
    const select = within(form).getByLabelText("Địa điểm: xã/phường");
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["— Chọn xã/phường —", "Mỹ Hòa Hưng", "Bình Đức", "Long Xuyên", "Vĩnh Tế", "An Cư", "Núi Sam", "Châu Phong"]);
    await user.type(within(form).getByLabelText("Nội dung công việc"), "Kiểm tra mô hình lúa");
    await user.type(within(form).getByLabelText("Địa điểm cụ thể"), "Cánh đồng lớn");
    await user.click(within(form).getByRole("button", { name: /Lưu mục/ }));
    expect(within(form).getByRole("alert")).toHaveTextContent("Xã/phường bắt buộc.");
    await user.selectOptions(select, "91306");
    await user.click(within(form).getByRole("button", { name: /Lưu mục/ }));
    const card = await screen.findByText("Kiểm tra mô hình lúa");
    expect(card.closest("[data-testid=ws-item]")).toHaveTextContent("Núi Sam");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    expect(await screen.findByText("Đã lưu nháp.")).toBeInTheDocument();
  });

  it("Admin chỉ xem: không có nút duyệt, không có tab Lịch của tôi", async () => {
    const { api, ws } = await clientFor("kn-admin", "asg-admin");
    render(<ApproveSchedules ws={ws} api={api} readOnly />);
    expect(await screen.findByRole("tab", { name: "Chờ duyệt (2)" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Duyệt lịch đã chọn/ })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Lịch của tôi" })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getByRole("button", { name: /Xuất Excel/ })).toBeInTheDocument();
  });
});

describe("<ScheduleDetail>", { timeout: 30_000 }, () => {
  it("Detail: khối thông tin, cảnh báo trùng giờ, Duyệt → Log có sự kiện duyệt", async () => {
    const user = userEvent.setup();
    const { api, ws } = await clientFor("kn-binh", "asg-binh-tinh");
    const list = await ws.list({ week_start: nextWeekStart(), status: "pending" });
    const hoa = list.items.find((s) => s.owner_display === "Lý Thị Hoa")!;
    render(<ScheduleDetail ws={ws} api={api} id={hoa.id} backHref="/kn/nhiem-vu/lich-cong-tac" />);
    expect(await screen.findByRole("heading", { level: 1, name: /Lý Thị Hoa/ })).toBeInTheDocument();
    expect(screen.getByText("2 mục trùng thời gian (tô vàng).")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Lịch công tác" })).toHaveAttribute("href", "/kn/nhiem-vu/lich-cong-tac");
    const log = screen.getByRole("complementary", { name: "Log" });
    await waitFor(() => expect(log).toHaveTextContent("Gửi duyệt"));
    await user.click(within(screen.getByRole("toolbar", { name: "Thao tác" })).getByRole("button", { name: /Duyệt/ }));
    expect(await screen.findByText("Đã duyệt lịch.")).toBeInTheDocument();
    await waitFor(() => expect(log).toHaveTextContent("Duyệt lịch"));
    expect(log).toHaveTextContent("Trạng thái: Chờ duyệt → Đã duyệt");
    expect(screen.queryByRole("toolbar", { name: "Thao tác" })).toBeNull();
  });

  it("Trưởng phòng mở lịch của chính mình: không có nút Duyệt (R-56)", async () => {
    const { api, ws } = await clientFor("kn-binh", "asg-binh-tinh");
    const mine = await ws.mine(currentWeekStart());
    render(<ScheduleDetail ws={ws} api={api} id={mine.schedule!.schedule.id} backHref="/kn/nhiem-vu/lich-cong-tac" />);
    expect(await screen.findByRole("heading", { level: 1, name: /Trần Thị Bình/ })).toBeInTheDocument();
    expect(screen.queryByRole("toolbar", { name: "Thao tác" })).toBeNull();
  });
});
