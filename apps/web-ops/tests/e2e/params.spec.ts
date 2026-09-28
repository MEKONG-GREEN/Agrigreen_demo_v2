import { expect, test } from "@playwright/test";
import { loginViaUi, logout, navTo } from "./helpers";

test("tham số: người đặt đề xuất → không tự duyệt được (R-56) → người khác phê duyệt", async ({ page }) => {
  // Đăng nhập qua giao diện và chỉ điều hướng phía client để giữ trạng thái giả lập giữa hai người dùng.
  await page.goto("/login");
  await loginViaUi(page, /Quản trị nền tảng/);
  await navTo(page, "Tham số & phê duyệt");
  const section = page.getByRole("region", { name: "SLA xử lý nhiệm vụ hỗ trợ" });
  await expect(section).toContainText("extension.task_sla_hours");
  await section.getByRole("button", { name: "Đề xuất thay đổi" }).click();
  await section.getByLabel(/Giá trị mới/).fill("30");
  await section.getByLabel("Ngày hiệu lực", { exact: true }).fill("2027-01-15");
  await section.getByLabel("Văn bản căn cứ (số/ngày)").fill("QĐ thử nghiệm E2E số 01/2026");
  await section.getByRole("button", { name: "Tạo và gửi duyệt" }).click();

  const mine = section.getByRole("listitem").filter({ hasText: "phiên bản 3" });
  await expect(mine).toContainText("Chờ duyệt");
  await expect(mine).toContainText("Quản trị viên A (giả lập)");
  await expect(mine).toContainText("Người phê duyệt phải khác người đề xuất (R-56)");
  await expect(mine.getByRole("button", { name: "Phê duyệt" })).toBeDisabled();

  await logout(page);
  await loginViaUi(page, /người phê duyệt/);
  await navTo(page, "Tham số & phê duyệt");
  const section2 = page.getByRole("region", { name: "SLA xử lý nhiệm vụ hỗ trợ" });
  const pending = section2.getByRole("listitem").filter({ hasText: "phiên bản 3" });
  await expect(pending.getByRole("button", { name: "Phê duyệt" })).toBeEnabled();
  await pending.getByRole("button", { name: "Phê duyệt" }).click();
  await expect(section2.getByRole("listitem").filter({ hasText: "phiên bản 3" })).toHaveCount(0);
  await expect(section2).toContainText("Sắp hiệu lực từ 15/01/2027");
});
