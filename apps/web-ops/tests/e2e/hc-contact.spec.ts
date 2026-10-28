import { expect, test, type Page } from "@playwright/test";
import { accountMenu, sidebar } from "./helpers";

// W11.29 — Contact cho mọi vai (prototype HCTH `ct`/`ctu`, FN-13): bổ sung Cấp Xã và Cấp Tỉnh cho kn-contact.spec.ts
// (Cấp TW, Admin). Cột theo vai do máy chủ lọc (src/api/mock/kn-contact.ts). Tài khoản: src/api/mock/kn-accounts.ts.
const PASSWORD = "KhuyenNong@2026";
const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });

async function enterAs(page: Page, identifier: string, role: RegExp, account: string): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(page).toHaveURL(/\/chon-vai-tro/);
  await page.getByRole("button", { name: role }).click();
  await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
  await expect(accountMenu(page)).toContainText(account);
  await menuNav(page).getByRole("link", { name: "Contact" }).click();
  await expect(page).toHaveURL(/\/kn\/dung-chung\/contact$/);
}

test("hc-contact Cấp Xã: chỉ Họ tên, Năm sinh, Số điện thoại; tab Đơn vị có số điện thoại", async ({ page }) => {
  await enterAs(page, "an.nv@angiang.gov.vn", /^Cấp Xã/, "Cấp Xã · Trồng trọt");
  const table = page.getByRole("table", { name: "Danh bạ cán bộ" });
  await expect(table.getByRole("columnheader")).toHaveText(["STT", "Họ tên", "Năm sinh", "Số điện thoại"]);
  await expect(page.getByRole("button", { name: "Thêm liên hệ ngoài tỉnh" })).toHaveCount(0);
  await page.getByRole("tab", { name: "Đơn vị" }).click();
  await expect(page.getByRole("columnheader", { name: "Số điện thoại" }).first()).toBeVisible();
});

test("hc-contact Cấp Tỉnh: đủ cột liên hệ (Chức danh, Đơn vị công tác, Email)", async ({ page }) => {
  await enterAs(page, "binh.tt@angiang.gov.vn", /^Cấp Tỉnh/, "Cấp Tỉnh · Trồng trọt");
  const table = page.getByRole("table", { name: "Danh bạ cán bộ" });
  for (const h of ["Họ tên", "Chức danh", "Đơn vị công tác", "Email"]) {
    await expect(table.getByRole("columnheader", { name: h, exact: true })).toBeVisible();
  }
});
