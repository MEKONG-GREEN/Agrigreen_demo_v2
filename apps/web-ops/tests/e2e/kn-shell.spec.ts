import { expect, test, type Page } from "@playwright/test";
import { accountMenu, sidebar } from "./helpers";

// Khung P6 KN v7.7 (BRD §4.0, FN-CH-00.01): đăng nhập → chọn vai trò/phòng ban → menu theo vai trò →
// thu gọn sidebar → đổi vai trò trong menu tài khoản. Tài khoản & mật khẩu thử: src/api/mock/kn-accounts.ts.
const PASSWORD = "KhuyenNong@2026";

async function submitLogin(page: Page, identifier: string, password: string): Promise<void> {
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

async function loginKn(page: Page, identifier: string): Promise<void> {
  await page.goto("/login");
  await submitLogin(page, identifier, PASSWORD);
}

// Next.js có sẵn một role="alert" (route announcer) → luôn giới hạn trong thẻ đăng nhập / màn chọn.
const loginAlert = (page: Page) => page.getByRole("form", { name: "Đăng nhập" }).getByRole("alert");
const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });
const groupLabels = (page: Page) => menuNav(page).locator(".nav-lbl");

test.describe("khung KN v7.7: đăng nhập, chọn vai trò & phòng ban, menu theo vai trò", () => {
  test("FN-CH-00.01: sai mật khẩu → banner còn n lần thử; 3 vai trò → chọn Cấp Xã · Trồng trọt → Công việc cần làm", async ({ page }) => {
    await page.goto("/kn/dashboard");
    await expect(page).toHaveURL(/\/login\?returnTo=/);
    await expect(page.getByRole("heading", { name: "Module Phòng Trồng trọt" })).toBeVisible();

    await submitLogin(page, "an.nv@angiang.gov.vn", "sai-mat-khau");
    await expect(loginAlert(page)).toContainText("Sai tài khoản hoặc mật khẩu.");
    await expect(loginAlert(page)).toContainText("Còn 4 lần thử.");

    // Hiện / Ẩn mật khẩu.
    const pw = page.getByLabel("Mật khẩu", { exact: true });
    await page.getByRole("button", { name: "Hiện" }).click();
    await expect(pw).toHaveAttribute("type", "text");

    await submitLogin(page, "an.nv@angiang.gov.vn", PASSWORD);
    await expect(page).toHaveURL(/\/chon-vai-tro/);
    await expect(page.getByRole("heading", { level: 1, name: "Chọn vai trò và phòng ban" })).toBeVisible();
    await expect(page.getByText("3 vai trò")).toBeVisible();
    await expect(page.getByRole("button", { name: /Cấp Xã.*Mỹ Hòa Hưng, Bình Đức/ })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("button", { name: /Phòng Chăn nuôi - Thủy sản — Chưa mở/ })).toBeDisabled();
    await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();

    await expect(page).toHaveURL(/\/kn\/nhiem-vu\/cong-viec$/);
    await expect(page.getByRole("heading", { level: 1, name: "Công việc cần làm" })).toBeVisible();
    await expect(groupLabels(page)).toHaveText(["Điều hành", "Báo cáo", "Dữ liệu", "Dùng chung"]);
    const active = menuNav(page).locator('[aria-current="page"]');
    await expect(active).toHaveCount(1);
    await expect(active).toContainText("Công việc cần làm");
    await expect(active).toContainText(/\d/); // số đếm từ sổ giả lập vòng đời báo cáo (P4)
    await expect(menuNav(page).getByRole("link", { name: "Dashboard" })).toHaveCount(0);
    await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toHaveText(/Nhiệm vụ\s*\/?\s*Công việc cần làm/);
    await expect(accountMenu(page)).toContainText("Nguyễn Văn An");
    await expect(accountMenu(page)).toContainText("Cấp Xã · Trồng trọt");
    await expect(sidebar(page)).toContainText("© 2026 TTKN Quốc gia");
  });

  test("Cấp Quốc gia (1 vai trò): vào thẳng Dashboard, chỉ xem, menu không có Nhiệm vụ", async ({ page }) => {
    await loginKn(page, "0912345603");
    await expect(page).toHaveURL(/\/kn\/dashboard$/);
    await expect(page.getByRole("heading", { level: 1, name: /Xin chào, Lê Minh Quân/ })).toBeVisible();
    await expect(groupLabels(page)).toHaveText(["Điều hành", "Báo cáo", "Dùng chung"]);
    for (const label of ["Dashboard", "Báo cáo tuần", "Báo cáo tháng", "Báo cáo hàng vụ", "Báo cáo năm"]) {
      await expect(menuNav(page).getByRole("link", { name: label })).toBeVisible();
    }
    await expect(page.getByText("Chỉ số nóng")).toBeVisible();
    await expect(page.getByText("Chỉ số so sánh")).toBeVisible();
    await expect(page.getByRole("region", { name: "Cảnh báo" })).toContainText("Thông tin");
    await accountMenu(page).click();
    await expect(page.getByRole("menu")).toContainText("Cấp Quốc gia · Phòng Trồng trọt");
    await expect(page.getByRole("menu")).toContainText("Tài khoản chỉ có 1 vai trò, 1 phòng ban");
    await page.keyboard.press("Escape");
    // Báo cáo năm: chưa có mẫu năm (Q-07) → "Chưa có dữ liệu", chỉ xem.
    await menuNav(page).getByRole("link", { name: "Báo cáo năm" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Báo cáo năm" })).toBeVisible();
    await expect(menuNav(page).getByRole("link", { name: "Báo cáo năm" })).toHaveAttribute("aria-current", "page");
  });

  test("Cấp Tỉnh: Nhiệm vụ có số đếm việc cần xử lý; đổi sang Cấp Xã trong menu tài khoản không cần đăng nhập lại", async ({ page }) => {
    await loginKn(page, "binh.tt@angiang.gov.vn");
    await expect(page).toHaveURL(/\/chon-vai-tro/);
    await page.getByRole("button", { name: /^Cấp Tỉnh/ }).click();
    await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
    // Trang vào Cấp Tỉnh phòng chuyên môn = "Báo cáo cho KHTC" (#44, internal/kn/extension/menus/navigation.json).
    await expect(page).toHaveURL(/\/kn\/bao-cao-khtc$/);
    const nav = menuNav(page);
    await expect(nav.getByRole("button", { name: /Nhiệm vụ/ })).toHaveAttribute("aria-expanded", "false");
    await nav.getByRole("button", { name: /Nhiệm vụ/ }).click();
    await expect(nav.getByRole("link", { name: /Trạng thái nộp/ })).toContainText(/\d/);
    await expect(nav.getByRole("link", { name: /Rà soát & chốt kỳ/ })).toContainText(/\d/);
    await expect(nav.getByRole("link", { name: /Lịch công tác/ })).toContainText("2");
    await expect(nav.getByRole("link", { name: "Phát hành biểu mẫu" })).not.toContainText(/\d/);
    await nav.getByRole("link", { name: "Phát hành biểu mẫu" }).click();
    await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toHaveText(/Nhiệm vụ\s*\/?\s*Phát hành biểu mẫu/);

    await accountMenu(page).click();
    await expect(page.getByRole("menu")).toContainText("Cấp Tỉnh · Phòng Trồng trọt");
    await page.getByRole("menuitem", { name: "Cấp Xã · Phòng Trồng trọt" }).click();
    await expect(page).toHaveURL(/\/kn\/nhiem-vu\/cong-viec$/);
    await expect(accountMenu(page)).toContainText("Cấp Xã · Trồng trọt");
    await expect(groupLabels(page)).toHaveText(["Điều hành", "Báo cáo", "Dữ liệu", "Dùng chung"]);
    await expect(menuNav(page).getByRole("link", { name: "Phát hành biểu mẫu" })).toHaveCount(0);
    // Trang của vai trò cũ không còn trong menu → không có quyền.
    await page.goto("/kn/nhiem-vu/phat-hanh");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    // Tải lại trang: giữ vai trò đã chọn.
    await expect(accountMenu(page)).toContainText("Cấp Xã · Trồng trọt");
  });

  test("Admin: vào thẳng Tổng quan hệ thống; nhóm Phòng ban (chưa mở hiện mờ) và Quản trị hệ thống mở màn KN", async ({ page }) => {
    await loginKn(page, "admin.mekong@mekonggreen.vn");
    await expect(page).toHaveURL(/\/kn\/tong-quan$/);
    await expect(page.getByRole("heading", { level: 1, name: /Xin chào, admin\.mekong/ })).toBeVisible();
    await expect(groupLabels(page)).toHaveText(["Điều hành", "Phòng ban", "Quản trị hệ thống", "Dùng chung"]);
    // W11.23: "Dùng chung > Contact" (pf/contact) là liên kết thường của mọi vai.
    await expect(menuNav(page).getByRole("link", { name: /Contact/ })).toHaveAttribute("href", "/kn/dung-chung/contact");
    const closed = menuNav(page).locator(".nav-item.is-disabled", { hasText: "Chăn nuôi - TS" });
    await expect(closed).toContainText("Chưa mở");
    await expect(menuNav(page).getByRole("link", { name: /Chăn nuôi/ })).toHaveCount(0);
    await menuNav(page).getByRole("button", { name: /Phòng Trồng trọt/ }).click();
    await expect(menuNav(page).getByRole("link", { name: "Mẫu báo cáo" })).toBeVisible();
    // BF-ACC-KN-MENU (#120, TT-13): Công cụ nền tảng không còn liên kết màn tài khoản native cũ.
    await menuNav(page).getByRole("button", { name: /Công cụ nền tảng/ }).click();
    await expect(menuNav(page).getByRole("link", { name: "Tham số & phê duyệt" })).toBeVisible();
    await expect(menuNav(page).getByRole("link", { name: /Tài khoản cán bộ/ })).toHaveCount(0);
    await expect(menuNav(page).locator('a[href="/quan-tri/tai-khoan"]')).toHaveCount(0);
    await menuNav(page).getByRole("link", { name: "Người dùng" }).click();
    await expect(page).toHaveURL(/\/kn\/quan-tri\/nguoi-dung$/);
    await expect(page.getByRole("heading", { level: 1, name: "Người dùng" })).toBeVisible();
    await expect(menuNav(page).getByRole("link", { name: "Người dùng" })).toHaveAttribute("aria-current", "page");
    await expect(page.getByText("Hệ thống gặp lỗi")).toHaveCount(0);
  });

  test("thu gọn sidebar: chỉ icon, flyout trắng chứa menu con; trạng thái giữ khi tải lại", async ({ page }) => {
    await loginKn(page, "an.nv@angiang.gov.vn");
    await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
    await expect(page).toHaveURL(/\/kn\/nhiem-vu\/cong-viec$/);
    const side = sidebar(page);
    await side.getByRole("button", { name: "Thu gọn menu" }).click();
    await expect(side).toHaveAttribute("data-collapsed", "true");
    await expect.poll(async () => (await side.boundingBox())?.width ?? 999).toBeLessThanOrEqual(73); // 72 px sau hiệu ứng

    await side.getByRole("button", { name: /Thu thập dữ liệu/ }).hover();
    const fly = side.getByRole("group", { name: "Thu thập dữ liệu" });
    await expect(fly).toBeVisible();
    await fly.getByRole("link", { name: "Danh sách HTX" }).click();
    await expect(page).toHaveURL(/\/kn\/du-lieu\/htx$/);
    await expect(page.getByRole("heading", { level: 1, name: "Danh sách HTX" })).toBeVisible();

    await page.reload();
    await expect(sidebar(page)).toHaveAttribute("data-collapsed", "true");
    await expect(accountMenu(page)).toContainText("Cấp Xã · Trồng trọt");
    await sidebar(page).getByRole("button", { name: "Mở rộng menu" }).click();
    await expect(sidebar(page)).not.toHaveAttribute("data-collapsed", "true");
    await expect(groupLabels(page)).toHaveText(["Điều hành", "Báo cáo", "Dữ liệu", "Dùng chung"]);
  });

  test("mẫu List → Detail: STT, nhãn trạng thái, xem/sửa/xóa, phân trang; Detail có quay lại, Log, breadcrumb", async ({ page }) => {
    await loginKn(page, "an.nv@angiang.gov.vn");
    await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
    await expect(page).toHaveURL(/\/kn\/nhiem-vu\/cong-viec$/);
    await menuNav(page).getByRole("button", { name: /Thu thập dữ liệu/ }).click();
    await menuNav(page).getByRole("link", { name: "Danh sách HTX" }).click();
    // Hồ sơ KN (#67, #102): GET /v1/kn/producer-records theo xã phụ trách, phân trang máy chủ theo con trỏ — không có tổng số.
    // Kiểm thêm STT, nhãn "Hiển thị 1–3" và nút Trang trước/sau (10 dòng/trang).
    const table = page.getByRole("table", { name: "Danh sách HTX" });
    const rows = table.getByRole("row").filter({ has: page.getByRole("cell") });
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0).getByRole("cell").first()).toHaveText("1");
    await expect(rows.nth(2).getByRole("cell").first()).toHaveText("3");
    await expect(rows.first().getByText("Hoạt động", { exact: true })).toBeVisible();
    const pager = page.getByRole("navigation", { name: "Phân trang" });
    await expect(page.locator(".pager [aria-live=polite]")).toHaveText("Hiển thị 1–3");
    await expect(pager.getByRole("button", { name: "Trang 1" })).toHaveAttribute("aria-current", "page");
    await expect(pager.getByRole("button", { name: "Trang trước" })).toBeDisabled();
    await expect(pager.getByRole("button", { name: "Trang sau" })).toBeDisabled();
    await page.getByRole("button", { name: "Đặt lại" }).click();
    await page.getByRole("link", { name: /^Xem HTX/ }).first().click();
    await expect(page).toHaveURL(/\/kn\/du-lieu\/htx\/[0-9a-f-]{36}$/);
    await expect(page.getByRole("complementary", { name: "Log" })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toContainText("Danh sách HTX");
    await page.getByRole("link", { name: "Danh sách HTX" }).first().click();
    await expect(page).toHaveURL(/\/kn\/du-lieu\/htx$/);
  });

  test("tài khoản không có phòng ban đang áp dụng → thông báo liên hệ quản trị viên", async ({ page }) => {
    await loginKn(page, "loc.pv@angiang.gov.vn");
    await expect(page).toHaveURL(/\/chon-vai-tro/);
    await expect(page.locator(".choose-msg[role=alert]")).toContainText("liên hệ quản trị viên");
  });

  test("sai 5 lần → khóa tạm 15 phút; Quên mật khẩu dẫn tới liên hệ quản trị viên", async ({ page }) => {
    await page.goto("/login");
    for (let i = 0; i < 5; i++) await submitLogin(page, "quan.lm@ttknqg.gov.vn", `sai-${i}`);
    await expect(loginAlert(page)).toContainText("tạm khóa 15 phút");
    await submitLogin(page, "quan.lm@ttknqg.gov.vn", PASSWORD);
    await expect(loginAlert(page)).toContainText("tạm khóa 15 phút");
    await page.getByRole("link", { name: "Quên mật khẩu?" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Quên mật khẩu" })).toBeVisible();
  });
});
