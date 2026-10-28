import { expect, test, type Page } from "@playwright/test";
import { addDays, dayColumnLabel, nextWeekStart, weekRangeLabel } from "../../src/kn/schedule/week";
import { accountMenu, logout, sidebar } from "./helpers";

// KN v7.7 P5 — Lịch công tác tuần (FN-XA-03.08 đăng ký, FN-TI-02.07 duyệt) trên backend giả lập.
// Trạng thái giả lập nằm trong bộ nhớ tab: đổi người dùng bằng Đăng xuất → Đăng nhập (điều hướng phía client).
// Tài khoản thử: Nguyễn Văn An (Cấp Xã · Mỹ Hòa Hưng, Bình Đức), Trần Thị Bình (Cấp Tỉnh · Trưởng phòng).
const PASSWORD = "KhuyenNong@2026";
const AN = "an.nv@angiang.gov.vn";
const BINH = "binh.tt@angiang.gov.vn";

const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });

async function submitLogin(page: Page, identifier: string): Promise<void> {
  await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

/** Đăng nhập (không tải lại trang) và vào Lịch công tác với vai trò đã chọn. */
async function enterAs(page: Page, who: "xa" | "tinh"): Promise<void> {
  await submitLogin(page, who === "xa" ? AN : BINH);
  await expect(page).toHaveURL(/\/chon-vai-tro/);
  await page.getByRole("button", { name: who === "xa" ? /^Cấp Xã/ : /^Cấp Tỉnh/ }).click();
  await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
  await expect(accountMenu(page)).toContainText(who === "xa" ? "Cấp Xã · Trồng trọt" : "Cấp Tỉnh · Trồng trọt");
  const nav = menuNav(page);
  if (who === "tinh") await nav.getByRole("button", { name: /Nhiệm vụ/ }).click();
  await nav.getByRole("link", { name: /Lịch công tác/ }).click();
  await expect(page).toHaveURL(/\/kn\/nhiem-vu\/lich-cong-tac$/);
}

const statusBar = (page: Page) => page.getByRole("group", { name: "Tuần và trạng thái" });
const form = (page: Page) => page.getByRole("region", { name: "Mục công tác" });
const log = (page: Page) => page.getByRole("complementary", { name: "Log" });

// BF-SCHED (#99): mỗi mục bắt buộc Xã/phường và Địa điểm (địa điểm cụ thể hoặc tọa độ).
async function addItem(page: Page, day: string, session: "Sáng" | "Chiều", content: string, opts: { start?: string; end?: string; commune: string; location: string }) {
  await page.getByRole("button", { name: `Thêm mục ${dayColumnLabel(day)} ${session}` }).click();
  const f = form(page);
  if (opts.start) await f.getByLabel("Giờ bắt đầu").fill(opts.start);
  if (opts.end) await f.getByLabel("Giờ kết thúc").fill(opts.end);
  await f.getByLabel("Nội dung công việc").fill(content);
  await f.getByLabel("Địa điểm: xã/phường").selectOption({ label: opts.commune });
  await f.getByLabel("Địa điểm cụ thể").fill(opts.location);
  await f.getByRole("button", { name: "Lưu mục" }).click();
  await expect(page.getByTestId("ws-item").filter({ hasText: content })).toBeVisible();
}

test.describe("Lịch công tác tuần: Cấp Xã đăng ký → Cấp Tỉnh duyệt", () => {
  test("đăng ký tuần sau, lưu nháp, gửi duyệt → Tỉnh yêu cầu chỉnh sửa → Xã sửa, gửi lại → Tỉnh duyệt; duyệt hàng loạt; Chưa đăng ký; Log", async ({ page }) => {
    test.setTimeout(180_000);
    const next = nextWeekStart();
    const tue = addDays(next, 1);
    const thu = addDays(next, 3);

    // --- 1. Cấp Xã: mặc định tuần sau, chưa đăng ký ---
    await page.goto("/login");
    await enterAs(page, "xa");
    await expect(page.getByRole("heading", { level: 1, name: "Lịch làm việc hằng tuần" })).toBeVisible();
    await expect(statusBar(page)).toContainText(weekRangeLabel(next));
    await expect(statusBar(page)).toContainText("Chưa đăng ký");
    await expect(statusBar(page)).toContainText("Hạn đăng ký:");
    await expect(page.getByRole("button", { name: "Gửi duyệt" })).toBeDisabled();

    // BF-SCHED (#99): thiếu Xã/phường hoặc Địa điểm thì không lưu được mục.
    await page.getByRole("button", { name: `Thêm mục ${dayColumnLabel(tue)} Sáng` }).click();
    await form(page).getByLabel("Nội dung công việc").fill("Mục thiếu địa điểm");
    await form(page).getByRole("button", { name: "Lưu mục" }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Xã/phường bắt buộc." })).toBeVisible();
    await form(page).getByLabel("Địa điểm: xã/phường").selectOption({ label: "Mỹ Hòa Hưng" });
    await form(page).getByRole("button", { name: "Lưu mục" }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Địa điểm bắt buộc — nhập địa điểm cụ thể hoặc tọa độ." })).toBeVisible();
    await expect(page.getByTestId("ws-item").filter({ hasText: "Mục thiếu địa điểm" })).toHaveCount(0);

    await addItem(page, tue, "Sáng", "Kiểm tra rầy nâu HTX Mỹ Hòa", { start: "07:30", end: "10:00", commune: "Mỹ Hòa Hưng", location: "Ruộng HTX Mỹ Hòa" });
    await addItem(page, tue, "Sáng", "Họp tổ khuyến nông", { start: "09:00", end: "11:00", commune: "Mỹ Hòa Hưng", location: "Trụ sở UBND xã" });
    // Trùng giờ: tô vàng + cảnh báo, vẫn lưu được.
    await expect(page.getByTestId("ws-item").filter({ hasText: "trùng giờ" })).toHaveCount(2);
    await expect(page.getByText(/Có 2 mục trùng thời gian/)).toBeVisible();
    await addItem(page, thu, "Chiều", "Hướng dẫn kỹ thuật 1P5G", { commune: "Bình Đức", location: "Cánh đồng ấp Bình Hòa" });

    await page.getByRole("button", { name: "Lưu nháp" }).click();
    await expect(page.getByText("Đã lưu nháp.")).toBeVisible();
    await expect(statusBar(page)).toContainText("Nháp");
    await expect(log(page)).toContainText("Lưu nháp");

    await page.getByRole("button", { name: "Gửi duyệt" }).click();
    await expect(page.getByText(/Đã gửi duyệt/)).toBeVisible();
    await expect(statusBar(page)).toContainText("Chờ duyệt");
    await expect(log(page)).toContainText("Trạng thái: Nháp → Chờ duyệt");

    // --- 2. Cấp Tỉnh (Trưởng phòng): thấy ngay ở Chờ duyệt, yêu cầu chỉnh sửa có lý do ---
    await logout(page);
    await enterAs(page, "tinh");
    await expect(page.getByRole("heading", { level: 1, name: "Lịch công tác · duyệt lịch Cấp Xã" })).toBeVisible();
    await expect(menuNav(page).getByRole("link", { name: /Lịch công tác/ })).toContainText("3");
    await expect(page.getByRole("tab", { name: "Chờ duyệt (3)" })).toHaveAttribute("aria-selected", "true");
    const anRow = page.getByTestId("ws-row").filter({ hasText: "Nguyễn Văn An" });
    await expect(anRow).toContainText("Mỹ Hòa Hưng, Bình Đức");
    await expect(anRow).toContainText("2 mục trùng giờ");
    await expect(anRow.locator(".ws-card.is-conflict")).toHaveCount(2);

    await page.getByRole("checkbox", { name: "Chọn lịch của Nguyễn Văn An" }).check();
    await page.getByRole("button", { name: "Yêu cầu chỉnh sửa" }).click();
    const dlg = page.getByRole("dialog", { name: "Yêu cầu chỉnh sửa lịch công tác" });
    await dlg.getByRole("textbox").fill("Ngắn");
    await dlg.getByRole("button", { name: "Gửi yêu cầu" }).click();
    await expect(dlg).toContainText("cần ít nhất 10 ký tự");
    const reason = "Tách hai mục trùng giờ sáng Thứ Ba, ghi rõ địa điểm họp";
    await dlg.getByRole("textbox").fill(reason);
    await dlg.getByRole("button", { name: "Gửi yêu cầu" }).click();
    await expect(page.getByText("Đã yêu cầu chỉnh sửa 1 lịch: Nguyễn Văn An.")).toBeVisible();
    await expect(page.getByRole("tab", { name: "Chờ duyệt (2)" })).toBeVisible();

    // --- 3. Cấp Xã: thấy lý do, sửa, gửi lại ---
    await logout(page);
    await enterAs(page, "xa");
    await expect(statusBar(page)).toContainText("Yêu cầu chỉnh sửa");
    const note = page.getByRole("note", { name: "Yêu cầu chỉnh sửa" });
    await expect(note).toContainText("Trần Thị Bình");
    await expect(note).toContainText(reason);
    await page.getByTestId("ws-item").filter({ hasText: "Họp tổ khuyến nông" }).click();
    await form(page).getByLabel("Buổi").selectOption({ label: "Chiều" });
    await form(page).getByLabel("Giờ bắt đầu").fill("13:30");
    await form(page).getByLabel("Giờ kết thúc").fill("16:00");
    await form(page).getByLabel("Địa điểm cụ thể").fill("Hội trường ấp Mỹ Hòa");
    await form(page).getByRole("button", { name: "Lưu mục" }).click();
    await expect(page.getByTestId("ws-item").filter({ hasText: "trùng giờ" })).toHaveCount(0);
    await page.getByRole("button", { name: "Gửi duyệt" }).click();
    await expect(page.getByText(/Đã gửi duyệt/)).toBeVisible();
    await expect(statusBar(page)).toContainText("Chờ duyệt");
    await expect(log(page)).toContainText("Yêu cầu chỉnh sửa");
    await expect(log(page)).toContainText(`Lý do: ${reason}`);

    // --- 4. Cấp Tỉnh: mở Detail, duyệt; Log đủ sự kiện; duyệt hàng loạt phần còn lại; Chưa đăng ký ---
    await logout(page);
    await enterAs(page, "tinh");
    await page.getByTestId("ws-row").filter({ hasText: "Nguyễn Văn An" }).getByRole("link", { name: "Nguyễn Văn An" }).click();
    await expect(page).toHaveURL(/\/kn\/nhiem-vu\/lich-cong-tac\/[0-9a-f-]{36}$/);
    await expect(page.getByRole("heading", { level: 1, name: /Nguyễn Văn An/ })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toContainText("Lịch công tác");
    await expect(log(page)).toContainText("Lưu nháp");
    await expect(log(page)).toContainText("Yêu cầu chỉnh sửa");
    await page.getByRole("toolbar", { name: "Thao tác" }).getByRole("button", { name: "Duyệt" }).click();
    await expect(page.getByText("Đã duyệt lịch.")).toBeVisible();
    await expect(log(page)).toContainText("Duyệt lịch");
    await expect(log(page)).toContainText("Trạng thái: Chờ duyệt → Đã duyệt");
    await expect(page.getByRole("toolbar", { name: "Thao tác" })).toHaveCount(0);

    await page.locator("a.back-link", { hasText: "Lịch công tác" }).click();
    await expect(page.getByRole("tab", { name: "Chờ duyệt (2)" })).toBeVisible();
    await page.getByRole("button", { name: "Duyệt hàng loạt" }).click();
    await page.getByRole("dialog", { name: "Duyệt hàng loạt" }).getByRole("button", { name: "Duyệt 2 lịch" }).click();
    await expect(page.getByText("Đã duyệt hàng loạt 2 lịch.")).toBeVisible();
    await expect(page.getByRole("tab", { name: "Chờ duyệt (0)" })).toBeVisible();
    await page.getByRole("tab", { name: /^Đã duyệt/ }).click();
    await expect(page.getByTestId("ws-row")).toHaveCount(3);

    await page.getByRole("tab", { name: /^Chưa đăng ký/ }).click();
    const un = page.getByTestId("ws-unregistered");
    await expect(un.filter({ hasText: "Huỳnh Văn Phúc" })).toContainText("An Cư");
    await expect(un.filter({ hasText: "Đặng Thị Thu" })).toContainText("Mới lưu nháp");
    await expect(un.filter({ hasText: "Nguyễn Văn An" })).toHaveCount(0);

    // Xuất Excel/PDF: tải tệp theo tuần.
    const dl = page.waitForEvent("download");
    await page.getByRole("button", { name: "Xuất Excel" }).click();
    expect((await dl).suggestedFilename()).toBe(`lich-cong-tac-${next}-gia-lap.csv`);
  });

  test("Cấp Tỉnh: Lịch của tôi dùng màn đăng ký; không tự duyệt lịch của mình", async ({ page }) => {
    await page.goto("/login");
    await enterAs(page, "tinh");
    await page.getByRole("tab", { name: "Lịch của tôi" }).click();
    await expect(page.getByRole("button", { name: /Sao chép tuần trước/ })).toBeVisible();
    await page.getByRole("button", { name: "Tuần trước", exact: true }).click();
    // Tuần này: lịch của Trưởng phòng đang chờ người duyệt khác.
    await expect(statusBar(page)).toContainText("Chờ duyệt");
    await page.getByRole("tab", { name: /^Chờ duyệt/ }).click();
    const own = page.getByTestId("ws-row").filter({ hasText: "Trần Thị Bình" });
    await expect(own).toBeVisible();
    await own.getByRole("link", { name: "Trần Thị Bình" }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Trần Thị Bình/ })).toBeVisible();
    await expect(page.getByRole("toolbar", { name: "Thao tác" })).toHaveCount(0);
  });
});
