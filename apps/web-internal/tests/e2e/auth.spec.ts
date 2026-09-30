import { expect, test } from "@playwright/test";
import { loginAs, sidebar, TITLES } from "./helpers";

test.describe("Đăng nhập, menu theo vai, MFA bề mặt quản trị", () => {
  test("chọn vai ở màn hình đăng nhập → vào tổng quan", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
    await page.getByRole("button", { name: TITLES["dieu-phoi"] }).click();
    await expect(page.getByRole("heading", { name: "Tổng quan vận hành" })).toBeVisible();
    await expect(page.getByRole("group", { name: "Tranh chấp đang mở" })).toBeVisible();
    await expect(page.getByText("Dữ liệu giả lập")).toBeVisible();
  });

  test("tổ gom chỉ thấy khối thu gom; mở trực tiếp màn hình hub → không có quyền", async ({ page }) => {
    await loginAs(page, "hien-truong", "/hub/ton-kho");
    await expect(sidebar(page).getByRole("link", { name: "Đếm cuộn & xác nhận HTX" })).toBeVisible();
    await expect(sidebar(page).getByRole("link", { name: "Lô & tồn kho" })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  });

  test("phiên chưa qua OTP: màn hình quản trị hiện hướng dẫn, không gọi bề mặt quản trị", async ({ page }) => {
    await loginAs(page, "hub", "/hub/co-so");
    await expect(page.getByTestId("mfa-gate")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Cần xác thực hai yếu tố" })).toBeVisible();
    await page.goto("/hub/tham-so");
    await expect(page.getByTestId("mfa-gate")).toBeVisible();
    // Màn hình ứng dụng vẫn dùng được.
    await page.goto("/hub/ton-kho");
    await expect(page.getByRole("table", { name: "Lô theo rủi ro" })).toBeVisible();
  });

  test("phiên có OTP: màn hình quản trị mở được", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/tham-so");
    await expect(page.getByText("hub.risk.v2#1").first()).toBeVisible();
    await expect(page.getByText("Bề mặt quản trị · MFA")).toBeVisible();
  });
});
