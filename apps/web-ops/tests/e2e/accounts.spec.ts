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

test("thao tác loạt (US-ADM-03): chọn nhiều tài khoản → khóa theo đợt có lý do; gửi lại liên kết cho tài khoản đã khóa báo lỗi từng tài khoản", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/tai-khoan");
  const rows = page.getByRole("table", { name: "Tài khoản trong phạm vi" }).locator("tbody tr");
  await expect(rows.first()).toBeVisible();
  await expect(page.getByTestId("bulk-bar")).toHaveCount(0);
  const boxes = rows.getByRole("checkbox");
  await boxes.nth(0).check();
  await boxes.nth(1).check();
  const bar = page.getByTestId("bulk-bar");
  await expect(bar).toContainText("Đã chọn 2 tài khoản");
  const run = bar.getByRole("button", { name: "Khóa 2 tài khoản" });
  await bar.getByLabel("Lý do (tối thiểu 20 ký tự)").fill("Nghỉ phép");
  await expect(run).toBeDisabled();
  await bar.getByLabel("Lý do (tối thiểu 20 ký tự)").fill("Sáp nhập đơn vị, tạm khóa theo đợt từ 01/10");
  await run.click();
  await expect(page.getByTestId("bulk-result")).toContainText("Thành công 2 · Lỗi 0");
  await expect(rows.nth(0).locator(".chip")).toHaveText("Đã khóa");
  await expect(rows.nth(1).locator(".chip")).toHaveText("Đã khóa");

  await bar.getByLabel("Thao tác").selectOption("resend");
  await bar.getByRole("button", { name: "Gửi lại liên kết kích hoạt 2 tài khoản" }).click();
  const result = page.getByTestId("bulk-result");
  await expect(result).toContainText("Thành công 0 · Lỗi 2");
  await expect(result.getByText("accounts.state_conflict")).toHaveCount(2);
});

test("quản trị chưa MFA: màn hình tài khoản báo mfa_required", async ({ page }) => {
  await loginAs(page, "admin-no-mfa", "/quan-tri/tai-khoan");
  await expect(page.getByTestId("mfa-notice")).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "mfa_required" }).first()).toBeVisible();
});
