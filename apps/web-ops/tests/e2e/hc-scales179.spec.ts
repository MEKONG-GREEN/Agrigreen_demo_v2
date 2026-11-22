import { expect, test } from "@playwright/test";
import { loginAs, sidebar } from "./helpers";
test("hc-scales179: danh sách, phiên bản hiệu lực và Log giữ lịch sử", async ({ page }) => {
  await page.clock.setFixedTime(new Date("2026-10-12T03:00:00Z"));
  await loginAs(page, "kn-admin", "/kn/tong-quan");
  await expect(sidebar(page)).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  // Trạng thái phòng áp dụng khi chọn lại ngữ cảnh. Vào/thoát hỗ trợ để tải lại menu Admin,
  // giữ nguyên dữ liệu mock trong tab (reload toàn trang sẽ cài lại dữ liệu mẫu).
  await sidebar(page).getByRole("link", { name: "Menu các cấp", exact: true }).click();
  await page.getByLabel("Xem như vai trò*").selectOption("kn_tinh");
  await page.getByLabel(/^Lý do hỗ trợ/).fill("Kiểm tra danh mục nhân sự FN-19");
  await page.getByRole("button", { name: "Vào menu Cấp Tỉnh", exact: true }).click();
  await page.getByRole("button", { name: "Thoát chế độ hỗ trợ", exact: true }).click();
  await sidebar(page).getByRole("button", { name: "Phòng Hành chính - Tổng hợp", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Danh mục nhân sự", exact: true }).click();
  await page.getByRole("button", { name: /^Bảng hệ số theo bậc/ }).click();
  await expect(page).toHaveURL(/\/he-so\?at=2026-10-12$/);
  await expect(page.getByRole("table", { name: "Bảng hệ số các ngạch" })).toContainText("STT");
  await page.getByRole("link", { name: "Bảng hệ số mới 01.003", exact: true }).click();
  await expect(page).toHaveURL(/\/he-so\/01\.003\/them-moi\?at=2026-10-12$/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const hold = page.getByLabel(/^Thời gian giữ bậc/);
  for (const bad of ["1e100000", "1e-100000", "9".repeat(100000)]) {
    await hold.fill(bad);
    await page.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(hold).toHaveAttribute("aria-invalid", "true");
    await expect(page).toHaveURL(/\/them-moi\?/);
  }
  await hold.fill("24");
  await page.getByLabel(/^Ngày hiệu lực/).fill("2027-01-01");
  await page.getByRole("textbox", { name: /^Hệ số bậc 1$/ }).fill("2,40");
  await page.getByLabel(/^Ghi chú/).fill("Căn cứ bảng mới E2E");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/\/he-so\/01\.003\?at=2027-01-01$/);
  const history = page.getByRole("table", { name: "Lịch sử bảng hệ số" });
  await expect(history).toContainText("Căn cứ bảng mới E2E");
  await expect(history.getByRole("row")).toHaveCount(3);
  await expect(page.getByRole("complementary", { name: "Log", exact: true })).toContainText("Phiên bản 2");
  await page.getByRole("link", { name: "Danh sách bảng hệ số", exact: true }).click();
  await page.getByLabel("Hiệu lực tại ngày", { exact: true }).fill("2026-10-12");
  await page.getByRole("link", { name: "Chi tiết hệ số 01.003", exact: true }).click();
  await expect(page).toHaveURL(/\/he-so\/01\.003\?at=2026-10-12$/);
  await expect(page.getByText("2,34", { exact: true })).toBeVisible();
  await expect(page.getByRole("table", { name: "Lịch sử bảng hệ số" })).toContainText("Căn cứ bảng mới E2E");
});