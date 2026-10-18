import { expect, test, type Page } from "@playwright/test";
import { sidebar } from "./helpers";

// W11.23 FN-13 Contact (pf/contact): menu "Dùng chung > Contact" cho mọi vai; cột theo vai (máy chủ lọc trường — bản giả lập
// src/api/mock/kn-contact.ts). Tài khoản thử: src/api/mock/kn-accounts.ts.
const PASSWORD = "KhuyenNong@2026";
const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });

async function openContact(page: Page, identifier: string): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await menuNav(page).getByRole("link", { name: "Contact" }).click();
  await expect(page).toHaveURL(/\/kn\/dung-chung\/contact$/);
}

test("Cấp TW: Contact chỉ Họ tên, Năm sinh, Số điện thoại; không nút Thêm, không chi tiết (AC-01)", async ({ page }) => {
  await openContact(page, "quan.lm@ttknqg.gov.vn");
  const table = page.getByRole("table", { name: "Danh bạ cán bộ" });
  await expect(table.getByRole("columnheader")).toHaveText(["STT", "Họ tên", "Năm sinh", "Số điện thoại"]);
  await expect(page.getByRole("button", { name: "Thêm liên hệ ngoài tỉnh" })).toHaveCount(0);
  await expect(table.getByRole("link", { name: "Nguyễn Thị Mai" })).toHaveCount(0);
});

test("Admin: Contact đủ cột, tìm kiếm, mở chi tiết liên hệ ngoài tỉnh", async ({ page }) => {
  await openContact(page, "admin.mekong@mekonggreen.vn");
  const table = page.getByRole("table", { name: "Danh bạ cán bộ" });
  await expect(table.getByRole("columnheader")).toHaveText([
    "STT",
    "Họ tên",
    "Năm sinh",
    "Số điện thoại",
    "Chức danh",
    "Đơn vị công tác",
    "Cấp",
    "Email",
  ]);
  await page.getByRole("searchbox").fill("hoang van");
  await table.getByRole("link", { name: "Hoàng Văn Trung" }).click();
  await expect(page.getByRole("heading", { level: 2, name: "Lịch sử thay đổi" })).toBeVisible();
});
