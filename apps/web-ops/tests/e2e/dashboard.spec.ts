import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("bảng điều hành tỉnh: huy hiệu Số liệu tạm tính khi còn xã chưa báo cáo; 4 KPI gộp (G-02)", async ({ page }) => {
  await loginAs(page, "tinh", "/");
  await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành tỉnh Đồng Tháp" })).toBeVisible();
  const badge = page.locator(".pg-head").getByRole("status");
  await expect(badge).toContainText("Số liệu tạm tính");
  await expect(badge).toContainText("còn 1 trên 4 đơn vị");
  for (const kpi of ["Diện tích đã gieo sạ", "HTX đang hoạt động", "Thửa đã số hoá", "Nhiệm vụ quá hạn"]) {
    await expect(page.getByRole("group", { name: kpi })).toBeVisible();
  }
  // Dòng xã chưa báo cáo mang nhãn tạm tính; tên xã do backend trả.
  const row = page.getByRole("row").filter({ hasText: "Số liệu tạm tính" });
  await expect(row).toHaveCount(1);
  await expect(row).toContainText("Xã ");
});

test("bảng điều hành toàn quốc: theo tỉnh, có tiến độ so với mục tiêu", async ({ page }) => {
  await loginAs(page, "tw", "/");
  await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành toàn quốc" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Tiến độ so với mục tiêu" })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "Đồng Tháp" }).first()).toBeVisible();
  await expect(page.getByRole("img", { name: /% mục tiêu/ }).first()).toBeVisible();
});
