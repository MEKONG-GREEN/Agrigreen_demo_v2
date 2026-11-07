import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("KN không mở bảng điều hành legacy hoặc tải các KPI HTX", async ({ page }) => {
  await loginAs(page, "tinh", "/");
  await expect(page.getByRole("heading", { name: "Bảng điều hành tỉnh Đồng Tháp" })).toHaveCount(0);
  for (const kpi of ["Diện tích đã gieo sạ", "HTX đang hoạt động", "Thửa đã số hoá", "Nhiệm vụ quá hạn"]) {
    await expect(page.getByRole("group", { name: kpi })).toHaveCount(0);
  }
});

test("bảng điều hành toàn quốc: theo tỉnh, có tiến độ so với mục tiêu", async ({ page }) => {
  await loginAs(page, "setter", "/");
  await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành toàn quốc" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Tiến độ so với mục tiêu" })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "Đồng Tháp" }).first()).toBeVisible();
  await expect(page.getByRole("img", { name: /% mục tiêu/ }).first()).toBeVisible();
});
