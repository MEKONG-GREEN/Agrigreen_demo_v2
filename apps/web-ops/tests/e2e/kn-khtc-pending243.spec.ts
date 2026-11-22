import { expect, test } from "@playwright/test";
import { MOCK_KN_PASSWORD } from "../../src/api/mock/kn-accounts";
import { logout, sidebar } from "./helpers";

test("243 pending inbox: menu, phiếu native chờ, mobile và quyết định loại khỏi danh sách", async ({ page }, testInfo) => {
  await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
  const login = async (identifier: string) => {
    await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
    await page.getByLabel("Mật khẩu", { exact: true }).fill(MOCK_KN_PASSWORD);
    await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  };
  await page.goto("/login"); await login("admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  // Phân công thật qua Admin UI; không giả header, API ngầm hoặc sửa storage quyền.
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const main = page.getByRole("main");
  await expect(main.getByRole("button", { name: "Lưu", exact: true })).toBeEnabled();
  await main.getByLabel("Họ tên").fill("Cán bộ KHTC kiểm thử 243");
  await main.getByLabel("Email").fill("khtc.review243@example.test");
  await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345243");
  const assignment = main.getByRole("group", { name: "Vai trò và phạm vi" });
  await assignment.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tinh");
  await assignment.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await assignment.getByRole("checkbox", { name: "ke_hoach_tai_chinh", exact: true }).check();
  await expect(main.getByLabel("Họ tên")).toHaveValue("Cán bộ KHTC kiểm thử 243");
  await main.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Cán bộ KHTC kiểm thử 243" })).toBeVisible();
  await logout(page); await login("0912345243");
  await sidebar(page).getByRole("link", { name: "Phiếu số liệu các phòng", exact: true }).click();
  const heading = page.getByRole("heading", { level: 1, name: "Phiếu số liệu các phòng", exact: true });
  await expect(heading).toBeVisible();
  await expect(heading).toHaveCSS("font-family", /Noto Serif/);
  const inbox = page.getByRole("table", { name: "Phiếu chờ rà soát · Trang 1", exact: true });
  const pending = inbox.getByRole("row").filter({ hasText: "Phòng Trồng trọt" });
  await expect(pending).toHaveCount(1);
  await expect(pending.getByRole("link", { name: "Xem phiếu", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
  await page.screenshot({ path: testInfo.outputPath("243-pending-desktop.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  // Wait for the responsive drawer transition, then use the public mobile menu.
  await expect(sidebar(page)).not.toBeInViewport();
  await page.getByRole("button", { name: "Mở menu", exact: true }).click();
  await expect(sidebar(page)).toBeInViewport();
  await sidebar(page).getByRole("link", { name: "Phiếu số liệu các phòng", exact: true }).click();
  await expect(sidebar(page)).not.toBeInViewport();
  await expect(heading).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  const tableWidth = await inbox.evaluate(el => el.getBoundingClientRect().width);
  expect(tableWidth).toBeGreaterThan(300);
  await page.screenshot({ path: testInfo.outputPath("243-pending-mobile.png"), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await pending.getByRole("link", { name: "Xem phiếu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: /Rà soát phiếu Phòng Trồng trọt/ })).toBeVisible();
  await page.getByRole("textbox", { name: /Nội dung yêu cầu bổ sung/ }).fill("Bổ sung số liệu đã đối chiếu từ hộp phiếu.");
  await page.getByRole("button", { name: "Yêu cầu bổ sung", exact: true }).click();
  const confirmation = page.getByRole("dialog", { name: "Xác nhận yêu cầu bổ sung" });
  await expect(confirmation).toContainText("lần gửi 1");
  await confirmation.getByRole("button", { name: "Gửi yêu cầu bổ sung", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã gửi yêu cầu bổ sung cho lần gửi 1." })).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Phiếu số liệu các phòng", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Chưa có phiếu chờ rà soát trong nhóm kỳ này." })).toBeVisible();
  await expect(inbox.getByRole("row").filter({ hasText: "Phòng Trồng trọt" })).toHaveCount(0);
  // The offline mock recreates accounts/data on tab reload. Re-read through
  // the actual UI in this lifetime; durable native facts are verified on PG.
  await page.getByRole("button", { name: "Làm mới", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Chưa có phiếu chờ rà soát trong nhóm kỳ này." })).toBeVisible();
});
