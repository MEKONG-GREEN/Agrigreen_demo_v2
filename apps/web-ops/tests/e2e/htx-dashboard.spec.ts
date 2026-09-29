import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { coop } from "./htx-data";

test("HTX US-DASH-01: thẻ chỉ số + biểu đồ dự kiến – thực tế, đổi bộ lọc cập nhật ngay không tải lại trang", async ({ page }) => {
  await loginAs(page, "htx", "/");
  await expect(page).toHaveURL(/\/htx\/dashboard$/); // trang đích của Quản lý HTX
  await expect(page.getByRole("heading", { level: 1, name: "Bảng điều hành sản xuất" })).toBeVisible();
  await expect(page.getByText(coop.api.name).first()).toBeVisible(); // chỉ HTX của mình, không có ô chọn HTX
  await expect(page.getByLabel("HTX", { exact: true })).toHaveCount(0);
  const area = page.getByRole("group", { name: "Tổng diện tích" });
  await expect(area).toContainText("ha");
  await expect(page.getByRole("group", { name: "Tiến độ SOP" })).toContainText("nhật ký đã duyệt");
  await expect(page.getByRole("img", { name: "Biểu đồ sản lượng dự kiến và thực tế (tấn)" })).toBeVisible();
  const before = await area.textContent();
  await page.getByLabel("Giống lúa", { exact: true }).selectOption("ST25");
  await expect(area).not.toHaveText(before ?? "");
  await expect(page.getByRole("table", { name: "Số liệu theo nhóm" }).getByRole("row")).toHaveCount(2); // tiêu đề + ST25

  // Di chuột lên cột hiện chú thích (AC-2).
  await page.getByTestId("bar-ST25").locator("rect").first().hover();
  await expect(page.getByRole("img", { name: /Biểu đồ sản lượng/ })).toContainText("Dự kiến:");
});

test("HTX US-DASH-02: xuất CSV khớp bộ lọc hiện hành (xuất phía trình duyệt — chưa có mẫu báo cáo HTX)", async ({ page }) => {
  await loginAs(page, "htx", "/htx/dashboard");
  await expect(page.getByRole("note")).toContainText("Chưa có mẫu báo cáo sản lượng HTX");
  await page.getByLabel("Vụ", { exact: true }).selectOption("HT");
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Xuất Excel (CSV)" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/^san-luong-htx-\d{4}-HT\.csv$/);
});

test("HTX US-DASH-01 AC-4: quản trị xem toàn bộ hoặc lọc từng HTX, nhóm theo HTX", async ({ page }) => {
  await loginAs(page, "setter", "/htx/dashboard");
  await expect(page.getByLabel("HTX", { exact: true })).toHaveValue("");
  await page.getByLabel("Theo").selectOption("by_cooperative");
  const table = page.getByRole("table", { name: "Số liệu theo nhóm" });
  await expect(table.getByRole("row").filter({ hasText: coop.api.name })).toHaveCount(1);
  expect(await table.getByRole("row").count()).toBeGreaterThan(2);
  await page.getByLabel("HTX", { exact: true }).selectOption(coop.api.ref);
  await expect(table.getByRole("row")).toHaveCount(2);
});
