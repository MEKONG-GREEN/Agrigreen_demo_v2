import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("xuất báo cáo: 202 + việc xuất → theo dõi trạng thái → tải tệp; nhãn Số liệu tạm tính", async ({ page }) => {
  await loginAs(page, "tinh", "/bao-cao");
  await expect(page.getByRole("heading", { level: 1, name: "Báo cáo & xuất dữ liệu" })).toBeVisible();
  await page.getByLabel("Mẫu báo cáo").selectOption("kn.sowing_progress");
  await page.getByLabel("Kỳ").selectOption("month");
  await page.getByRole("radio", { name: /Excel/ }).check();
  await page.getByRole("button", { name: "Xuất báo cáo" }).click();

  const row = page.getByTestId("export-row").first();
  await expect(row).toContainText("Tiến độ gieo sạ theo đơn vị");
  await expect(row).toContainText("Sẵn sàng tải", { timeout: 15_000 });
  await expect(row).toContainText("Số liệu tạm tính");
  const download = page.waitForEvent("download");
  await row.getByRole("button", { name: "Tải xuống" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/^kn\.sowing_progress/);
});

test("lịch gửi báo cáo email (US-DASH-03): lập lịch hằng tháng cho tài khoản cán bộ → đang chạy → ngừng có lý do", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/mau-bao-cao");
  await page.getByRole("tab", { name: "Lịch gửi" }).click();
  const form = page.getByRole("form", { name: "Lập lịch gửi báo cáo qua email" });
  await expect(form).toBeVisible();
  const schedule = page.getByRole("button", { name: "Lập lịch" });
  await expect(schedule).toBeDisabled(); // chưa chọn người nhận
  await form.getByLabel("Tần suất").selectOption("monthly");
  await form.getByLabel("Ngày trong tháng (1–28)").fill("5");
  await form.getByLabel("Giờ gửi (giờ Việt Nam)").fill("7");
  await form.getByRole("checkbox").first().check();
  await expect(form).toContainText("Người nhận (1)");
  await schedule.click();

  const row = page.locator("li").filter({ hasText: "Hằng tháng, ngày 5 lúc 7 giờ" }).first();
  await expect(row).toContainText("Đang chạy");
  await row.getByRole("button", { name: "Chi tiết" }).click();
  await expect(row).toContainText("Chưa tới mốc chạy đầu tiên.");
  const stop = row.getByRole("button", { name: "Ngừng lịch" });
  await row.getByLabel("Lý do ngừng lịch (≥ 10 ký tự)").fill("ngắn");
  await expect(stop).toBeDisabled();
  await row.getByLabel("Lý do ngừng lịch (≥ 10 ký tự)").fill("Đổi sang lịch gửi hằng tuần");
  await stop.click();
  await expect(page.locator("li").filter({ hasText: "Hằng tháng, ngày 5 lúc 7 giờ" }).first()).toContainText("Đã ngừng");
});

test("mẫu CGH cho Cục: chọn vụ; PDF chỉ bật khi mẫu hỗ trợ", async ({ page }) => {
  await loginAs(page, "cuc", "/bao-cao");
  await page.getByLabel("Mẫu báo cáo").selectOption("cgh.season_compare");
  await expect(page.getByLabel("Vụ A", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Vụ B", { exact: true })).toBeVisible();
  await expect(page.getByRole("radio", { name: /PDF/ })).toBeDisabled();
});
