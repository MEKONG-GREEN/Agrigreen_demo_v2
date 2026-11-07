import { expect, test, type Page } from "@playwright/test";
import { accountMenu, loginAs, sidebar } from "./helpers";

async function loginKn(page: Page, identifier: string, returnTo = "/"): Promise<void> {
  await page.goto(`/login?returnTo=${encodeURIComponent(returnTo)}`);
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

for (const path of ["/", "/htx", "/chi-dao"]) {
  test(`KN-STANDALONE-2: Admin KN gõ ${path} và tải lại đều bị từ chối`, async ({ page }) => {
    await loginKn(page, "admin.mekong@mekonggreen.vn");
    await expect(page).toHaveURL(/\/kn\/tong-quan$/);
    await page.goto(path);
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${path === "/" ? "/" : path}$`));
    await expect(sidebar(page).getByText(/^Chức năng khác/)).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    await expect(accountMenu(page)).toContainText("Admin");
    if (path === "/") {
      await page.screenshot({ path: test.info().outputPath("kn-gate-desktop.png"), animations: "disabled" });
      await page.setViewportSize({ width: 390, height: 844 });
      await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
      await page.screenshot({ path: test.info().outputPath("kn-gate-mobile.png"), animations: "disabled" });
    }
  });
}

test("KN-STANDALONE-2: prefix giả không bypass khung KN", async ({ page }) => {
  await loginKn(page, "0912345603");
  await expect(page).toHaveURL(/\/kn\/dashboard$/);
  for (const path of ["/login-evil", "/chon-vai-tro-evil", "/kn-evil"]) {
    await page.goto(path);
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  }
});

test("KN-STANDALONE-2: returnTo legacy không vượt bước chọn; đổi active vai vẫn từ chối URL legacy", async ({ page }) => {
  await loginKn(page, "binh.tt@angiang.gov.vn", "/htx");
  await expect(page).toHaveURL(/\/chon-vai-tro/);
  await page.getByRole("button", { name: /^Cấp Tỉnh/ }).click();
  await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
  await expect(page).toHaveURL(/\/kn\/bao-cao-khtc$/);
  await page.goto("/chi-dao");
  await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  await accountMenu(page).click();
  await page.getByRole("menuitem", { name: "Cấp Xã · Phòng Trồng trọt" }).click();
  await expect(page).toHaveURL(/\/kn\/nhiem-vu\/cong-viec$/);
  await expect(accountMenu(page)).toContainText("Cấp Xã · Trồng trọt");
  await page.goto("/htx");
  await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
});

test("KN-STANDALONE-2: deep link thông báo legacy không mở component ngoài KN", async ({ page }) => {
  await loginKn(page, "0912345603");
  await expect(page).toHaveURL(/\/kn\/dashboard$/);
  await page.getByTestId("bell").click();
  await page.getByRole("dialog", { name: "Hộp thư thông báo" }).getByRole("button", { name: /Chỉ đạo/ }).first().click();
  await expect(page).toHaveURL(/\/chi-dao\?id=/);
  await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  await expect(page.getByTestId("directive-detail")).toHaveCount(0);
});

test("KN-STANDALONE-2: phiên HTX giữ nguyên màn sản xuất và quyền", async ({ page }) => {
  await loginAs(page, "htx", "/htx/dashboard");
  await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành sản xuất" })).toBeVisible();
  await expect(sidebar(page).getByRole("link", { name: "Mùa vụ", exact: true })).toBeVisible();
  await expect(sidebar(page).getByRole("link", { name: "Phê duyệt nhật ký", exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành sản xuất" })).toBeVisible();
});

test("KN-STANDALONE-2: phiên quản trị nền tảng giữ nguyên màn legacy", async ({ page }) => {
  await loginAs(page, "setter", "/chi-dao");
  await expect(page.getByRole("heading", { level: 1, name: "Chỉ đạo khẩn" })).toBeVisible();
  await expect(sidebar(page).getByRole("link", { name: "Chỉ đạo khẩn", exact: true })).toHaveAttribute("aria-current", "page");
  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: "Chỉ đạo khẩn" })).toBeVisible();
});
