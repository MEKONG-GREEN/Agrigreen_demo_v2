import { expect, test } from "@playwright/test";
import { loginAs, loginViaUi, sidebar } from "./helpers";

test.describe("đăng nhập giả lập và điều hướng theo vai/phạm vi", () => {
  test("đăng nhập qua /login bằng vai thử TW → bảng điều hành toàn quốc", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByText("dữ liệu giả lập", { exact: false }).first()).toBeVisible();
    await loginViaUi(page, /Cán bộ TTKN Quốc gia/);
    await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành toàn quốc" })).toBeVisible();
    const nav = sidebar(page);
    for (const label of ["Tổng quan", "Chỉ đạo khẩn", "Báo cáo & xuất dữ liệu", "Đồng bộ thiết bị", "Thư viện số"]) {
      await expect(nav.getByRole("link", { name: label })).toBeVisible();
    }
    await expect(nav.getByRole("link", { name: "Bản đồ CGH" })).toHaveCount(0);
    await expect(nav.getByRole("link", { name: "Tài khoản cán bộ" })).toHaveCount(0);
    await expect(page.getByTestId("bell")).toBeVisible();
  });

  test("TTKN tỉnh: phạm vi Đồng Tháp, có Đồng bộ thiết bị, không có quản trị", async ({ page }) => {
    await loginAs(page, "tinh");
    await expect(sidebar(page).getByText("Tỉnh · Đồng Tháp")).toBeVisible();
    await expect(sidebar(page).getByRole("link", { name: "Đồng bộ thiết bị" })).toBeVisible();
    await expect(sidebar(page).getByText("5. Quản trị")).toHaveCount(0);
  });

  test("KNV xã: không có Đồng bộ thiết bị; vào trang quản trị → không có quyền", async ({ page }) => {
    await loginAs(page, "xa");
    await expect(sidebar(page).getByText(/^Xã · /)).toBeVisible();
    await expect(sidebar(page).getByRole("link", { name: "Đồng bộ thiết bị" })).toHaveCount(0);
    await page.goto("/quan-tri/tai-khoan");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  });

  test("Cục KTHT: vào / được chuyển tới cổng CGH; chỉ CGH + báo cáo; khối vận hành bị chặn", async ({ page }) => {
    await loginAs(page, "cuc", "/");
    await expect(page).toHaveURL(/\/cgh$/);
    await expect(page.getByRole("heading", { level: 1, name: /Cổng Cục KTHT/ })).toBeVisible();
    await expect(sidebar(page).getByRole("link")).toHaveCount(2);
    await expect(page.getByTestId("forecast-provisional")).toContainText("Phương pháp dự báo tạm thời");
    await page.goto("/htx");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  });

  test("quản trị nền tảng: mọi màn hình mới mở được, không lỗi hệ thống", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await loginAs(page, "setter", "/");
    const screens: [string, string, string][] = [
      ["Đồng bộ thiết bị", "Đồng bộ thiết bị", "Theo thiết bị"],
      ["Mẫu & lịch báo cáo", "Mẫu & lịch báo cáo", "Tiến độ gieo sạ theo đơn vị (FR-01)"],
      ["Nhập ranh giới thửa", "Nhập ranh giới thửa", "Tệp ranh giới"],
      ["Cơ cấu tổ chức", "Cơ cấu tổ chức", "Trung tâm Khuyến nông Quốc gia"],
      ["Danh mục dùng chung", "Danh mục dùng chung", "Giống lúa"],
      ["Chỉ đạo khẩn", "Chỉ đạo khẩn", "Danh sách chỉ đạo"],
      ["Danh bạ trực", "Danh bạ trực", "Theo địa bàn"],
      ["Thư viện số", "Thư viện số", "Đã lưu"],
      ["Bản đồ CGH", "Bản đồ cơ giới hoá — Cổng Cục KTHT", "Dự báo nhu cầu máy (FN-09)"],
    ];
    for (const [link, heading, content] of screens) {
      await sidebar(page).getByRole("link", { name: link, exact: true }).click();
      await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
      await expect(page.getByText(content).first()).toBeVisible();
      await expect(page.getByText("Hệ thống gặp lỗi")).toHaveCount(0);
    }
    expect(errors).toEqual([]);
  });

  test("quản trị chưa qua MFA: màn hình Admin hướng dẫn đăng nhập lại có OTP (mfa_required)", async ({ page }) => {
    await loginAs(page, "admin-no-mfa", "/quan-tri/tham-so");
    await expect(page.getByTestId("mfa-notice")).toContainText("Cần đăng nhập lại có mã OTP");
    const alert = page.getByRole("alert").filter({ hasText: "mfa_required" });
    await expect(alert).toContainText("xác thực hai yếu tố");
    await expect(alert.getByTestId("trace-id")).not.toBeEmpty();
  });
});
