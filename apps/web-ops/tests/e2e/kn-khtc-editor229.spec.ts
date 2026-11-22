import { expect, test } from "@playwright/test";
import { MOCK_KN_PASSWORD } from "../../src/api/mock/kn-accounts";
import { logout, sidebar } from "./helpers";

test("229 editor: lỗi ô, Lưu nháp và xác nhận Gửi trên phiếu thường của phòng", async ({ page }, testInfo) => {
  await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
  const login = async (identifier: string) => {
    await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
    await page.getByLabel("Mật khẩu", { exact: true }).fill(MOCK_KN_PASSWORD);
    await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  };
  await page.goto("/login"); await login("admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Chăn nuôi - Thủy sản", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  await logout(page); await login("cnts.thu@demo.agrigreen.test");
  await page.getByRole("button", { name: /^Cấp Tỉnh/ }).click();
  await page.getByRole("button", { name: /Phòng Chăn nuôi - Thủy sản — Vào hệ thống/ }).click();
  const folder = sidebar(page).getByRole("button", { name: "Báo cáo cho KHTC", exact: true });
  if (await folder.getAttribute("aria-expanded") !== "true") await folder.click();
  await sidebar(page).getByRole("link", { name: "Phiếu kỳ này", exact: true }).click();
  await page.getByRole("link", { name: "Sửa, gửi lại", exact: true }).first().click();
  await expect(page.getByRole("heading", { level: 1, name: /^Phiếu / })).toBeVisible();
  await expect(page.getByRole("link", { name: "Báo cáo cho KHTC", exact: true }).filter({ has: page.locator("svg") })).toBeVisible();
  const number = page.getByRole("group", { name: "Nội dung phiếu" }).locator('input[inputmode="decimal"],input[inputmode="numeric"]').first();
  for (const hostile of ["1e100000", "1e-100000", "9".repeat(10000)]) {
    await number.fill(hostile); await expect(number).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("button", { name: "Gửi KHTC", exact: true })).toBeDisabled();
  }
  await page.screenshot({ path: testInfo.outputPath("229-editor-field-errors.png"), fullPage: true });
  await number.fill("7");
  for (const textarea of await page.getByRole("group", { name: "Nội dung phiếu" }).locator("textarea").all()) await textarea.fill("Số liệu tổng hợp đã kiểm tra cho kỳ báo cáo.");
  await expect(page.getByText("Có thay đổi chưa lưu.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Lưu nháp", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã lưu nháp." })).toBeVisible();
  await expect(page.getByText("Có thay đổi chưa lưu.", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1 })).toHaveCSS("font-family", /Noto Serif/);
  const sendBox = await page.getByRole("button", { name: "Gửi KHTC", exact: true }).boundingBox();
  expect(sendBox?.x).toBeGreaterThan(page.viewportSize()!.width * 0.65);
  await expect(page.getByRole("button", { name: "Gửi KHTC", exact: true })).toHaveClass(/btn-p(?:\s|$)/);
  await page.screenshot({ path: testInfo.outputPath("229-editor-saved.png"), fullPage: true });
  await page.getByRole("button", { name: "Gửi KHTC", exact: true }).click();
  const confirmation = page.getByRole("dialog", { name: "Xác nhận gửi phiếu" });
  await expect(confirmation).toContainText("lần 3/3");
  await confirmation.getByRole("button", { name: "Xác nhận gửi lần 3", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã gửi KHTC lần 3." })).toBeVisible();
  await expect(page.getByRole("group", { name: "Nội dung phiếu" }).locator("input,textarea,select")).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("229-editor-submitted.png"), fullPage: true });
});
