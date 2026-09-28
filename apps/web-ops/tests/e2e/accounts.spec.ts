import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("khóa tài khoản: lý do dưới 20 ký tự bị chặn, đủ lý do thì khóa và ghi nhật ký", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/tai-khoan");
  await page.getByRole("button", { name: /^Chi tiết / }).first().click();
  const panel = page.getByTestId("user-panel");
  await expect(panel).toContainText("Đang hoạt động");
  const form = panel.getByTestId("lock-form");
  await form.getByLabel(/Lý do khóa tài khoản/).fill("Nghỉ phép");
  await form.getByRole("button", { name: "Khóa tài khoản" }).click();
  await expect(form.getByRole("alert")).toHaveText("Lý do tối thiểu 20 ký tự — còn thiếu 11 ký tự.");
  await expect(panel).toContainText("Đang hoạt động");

  await form.getByLabel(/Lý do khóa tài khoản/).fill("Cán bộ chuyển công tác sang đơn vị khác từ 01/10");
  await form.getByRole("button", { name: "Khóa tài khoản" }).click();
  await expect(panel.locator(".chip").filter({ hasText: "Đã khóa" })).toBeVisible();
  await expect(panel.getByRole("listitem").filter({ hasText: /^Khóa/ })).toContainText("Cán bộ chuyển công tác");
  await expect(panel.getByRole("button", { name: "Mở khóa tài khoản" })).toBeVisible();
});

test("quản trị tài khoản tỉnh: vai theo ma trận cấp vai, lỗi máy chủ hiện nguyên mã", async ({ page }) => {
  await loginAs(page, "account-tinh", "/quan-tri/tai-khoan");
  await expect(page.getByText("Bạn được cấp: Khuyến nông viên xã, Quản lý HTX.")).toBeVisible();
  await page.getByRole("tab", { name: "Tạo tài khoản" }).click();
  await page.getByLabel("Email công vụ").fill("canbo05@example.test");
  await page.getByLabel("Họ tên").fill("Trần Văn Thử");
  const org = page.getByLabel("Đơn vị công tác");
  const value = await org.locator("option", { hasText: "Tổ khuyến nông cộng đồng" }).first().getAttribute("value");
  await org.selectOption(value!);
  // Tổ khuyến nông chỉ nhận vai kn_officer (coop_manager cần HTX) — ô vai do backend quyết định.
  await expect(page.getByLabel(/Quản lý HTX/)).toHaveCount(0);
  await page.getByLabel(/Khuyến nông viên xã/).check();
  await page.getByRole("button", { name: "Tạo tài khoản và gửi liên kết kích hoạt" }).click();
  const alert = page.getByRole("alert").filter({ hasText: "accounts.user_exists" });
  await expect(alert).toContainText("Tên đăng nhập hoặc email đã có.");
  await expect(alert).toContainText("accounts.user_exists");
});

test("quản trị chưa MFA: màn hình tài khoản báo mfa_required", async ({ page }) => {
  await loginAs(page, "admin-no-mfa", "/quan-tri/tai-khoan");
  await expect(page.getByTestId("mfa-notice")).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "mfa_required" }).first()).toBeVisible();
});
