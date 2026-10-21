import { expect, test, type Page } from "@playwright/test";
import { accountMenu, logout, sidebar } from "./helpers";

// W11.29 — web-ops HCTH theo prototype `Khuyen_nong_so_HCTH.html`: Hồ sơ nhân viên (`hs`) → Thông tin làm việc (`tl`,
// `tl.d`, FN-08) và Dashboard nhân sự (`dash`, khối Cảnh báo). Dữ liệu giả lập: src/api/mock/kn-hrm-staff.ts,
// kn-hrm-promotion.ts (3 hồ sơ mẫu). Cấp Tỉnh – HCTH có quyền toàn Trung tâm theo ma trận FN-20.
const PASSWORD = "KhuyenNong@2026";
const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });

async function submitLogin(page: Page, identifier: string): Promise<void> {
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

/**
 * Phòng HCTH mặc định "Chưa mở" (kn-accounts.ts): Admin bật ở Quản trị › Phòng ban, đăng xuất, rồi Trưởng phòng HCTH
 * (Trần Quốc Hưng — 1 vai, 1 phòng) đăng nhập và mở `label` từ menu. Không tải lại trang → giữ trạng thái giả lập.
 */
async function enterHcth(page: Page, label: string): Promise<void> {
  await page.goto("/login");
  await submitLogin(page, "admin.mekong@mekonggreen.vn");
  await expect(sidebar(page)).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await submitLogin(page, "hung.tq@angiang.gov.vn");
  await expect(accountMenu(page)).toContainText("Trần Quốc Hưng");
  await menuNav(page).getByRole("link", { name: label, exact: true }).click();
}

test("hc-hs-tl: Hồ sơ nhân viên → Thông tin làm việc, cập nhật ngày nghỉ việc → NS01/NS02 ở chi tiết", async ({ page }) => {
  await enterHcth(page, "Hồ sơ nhân sự");
  await expect(page.getByRole("heading", { level: 1, name: "Hồ sơ nhân sự" })).toBeVisible();
  const staff = page.locator("table.dt").first();
  await expect(staff.getByRole("columnheader", { name: "Trình độ cao nhất" })).toBeVisible();
  await expect(staff.getByRole("link", { name: "VC-00001" })).toBeVisible();
  await expect(page.getByLabel("Ngạch/CDNN", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Thông tin làm việc", exact: true }).click();
  await expect(page).toHaveURL(/\/kn\/nhan-su\/ho-so\/thong-tin-lam-viec$/);
  await expect(page.getByRole("heading", { level: 1, name: "Thông tin làm việc" })).toBeVisible();
  const active = page.getByRole("button", { name: /^Đang làm việc/ });
  await expect(active).toContainText("3");

  await page.getByRole("button", { name: "Cập nhật VC-00001" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Ngày nghỉ việc").fill("2026-09-30");
  await dlg.getByLabel("Lý do nghỉ").selectOption("nghi_huu");
  await dlg.getByRole("button", { name: "Lưu" }).click();
  await expect(page.getByRole("status")).toContainText("Hồ sơ chuyển “Đã nghỉ việc”");
  await expect(active).toContainText("2");

  await page.getByRole("link", { name: "Xem thông tin làm việc VC-00001" }).click();
  await expect(page.getByRole("heading", { level: 1, name: /VC-00001/ })).toBeVisible();
  await expect(page.getByText("Có trong tháng 09/2026")).toBeVisible();
  await expect(page.getByText("Không còn từ tháng 10/2026")).toBeVisible();
  await expect(page.getByRole("link", { name: "Mở hồ sơ nhân viên" })).toBeVisible();
});

test("hc-dash: Dashboard nhân sự có thẻ chỉ số mở danh sách và khối Cảnh báo nâng bậc", async ({ page }) => {
  await enterHcth(page, "Dashboard nhân sự");
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard nhân sự" })).toBeVisible();
  const alerts = page.getByRole("region", { name: "Cảnh báo" });
  await expect(alerts).toContainText("đến hạn nâng bậc trong 3 tháng tới");
  await page.getByRole("button", { name: /^Viên chức \+ HĐLĐ: \d+ — xem danh sách/ }).click();
  await expect(page.getByRole("dialog", { name: "Viên chức + HĐLĐ" })).toBeVisible();
});
