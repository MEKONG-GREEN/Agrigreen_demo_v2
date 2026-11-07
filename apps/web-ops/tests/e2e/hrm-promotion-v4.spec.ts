import { expect, test, type Page } from "@playwright/test";
import { join } from "node:path";
import { logout, sidebar } from "./helpers";

async function login(page: Page, identifier: string) {
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

test("HC3 FN09: ba trạng thái, lọc tương lai, chọn trực tiếp, xem trước và xuất Mẫu1", async ({ page }, info) => {
  test.setTimeout(180_000);
  await page.clock.install({ time: new Date("2026-10-12T03:00:00Z") });
  await page.goto("/login");
  await login(page, "admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  await logout(page);
  await login(page, "hung.tq@angiang.gov.vn");
  await sidebar(page).getByRole("link", { name: "Danh sách nâng bậc", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Danh sách nâng bậc", exact: true })).toBeVisible();
  await expect(page.getByText("Tổng số đến kỳ", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Lập danh sách trước hạn", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("combobox", { name: "Trạng thái", exact: true }).locator("option")).toHaveText(["Tất cả", "Chưa xử lý", "Chờ quyết định", "Đã có quyết định"]);
  await page.getByRole("textbox", { name: "Năm", exact: true }).fill("2027");
  const table = page.getByRole("table", { name: "Danh sách nâng lương thường xuyên" });
  await expect(table.getByRole("link", { name: "Nguyễn Thị Mẫu", exact: true })).toBeVisible();
  await expect(table.getByText("Trần Văn Thử", { exact: true })).toHaveCount(0);
  await expect(table.getByRole("cell", { name: "36", exact: true })).toBeVisible();
  const artifact = (name: string) => process.env.AG278_ARTIFACT_DIR ? join(process.env.AG278_ARTIFACT_DIR, name) : info.outputPath(name);
  await page.screenshot({ path: artifact("278-list.png"), fullPage: true });
  await page.getByRole("checkbox", { name: "Chọn Nguyễn Thị Mẫu", exact: true }).check();
  await page.getByRole("link", { name: "Xuất danh sách (1)", exact: true }).click();
  await expect(page).toHaveURL(/\/kn\/nhan-su\/nang-bac\/xuat$/);
  await page.getByRole("button", {name:"Xem trước",exact:true}).click();
  const preparer=page.getByRole("combobox",{name:"Người lập",exact:true});
  await expect(preparer).toHaveAttribute("aria-invalid","true");
  const preparerValue=await preparer.locator("option").filter({hasText:"VC-00001"}).getAttribute("value");
  await preparer.selectOption(preparerValue!);
  const months = page.getByRole("textbox", { name: "Số tháng được hưởng của Nguyễn Thị Mẫu", exact: true });
  for (const raw of ["1e100000", "1e-100000", "9".repeat(10000)]) {
    await months.fill(raw); await page.getByRole("button", { name: "Xem trước", exact: true }).click();
    await expect(months).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("button", { name: "Xuất và tải Mẫu số 1", exact: true })).toBeDisabled();
  }
  await months.fill("1");
  const reserved = page.getByRole("textbox", { name: "HS bảo lưu hiện có của Nguyễn Thị Mẫu", exact: true });
  for (const raw of ["1e100000", "1e-100000", "9".repeat(10000)]) {
    await reserved.fill(raw); await page.getByRole("button", { name: "Xem trước", exact: true }).click();
    await expect(reserved).toHaveAttribute("aria-invalid", "true");
  }
  await reserved.fill("0,2");
  await expect(page.getByRole("textbox",{name:"HS bảo lưu sau nâng của Nguyễn Thị Mẫu",exact:true})).toHaveValue("0,2");
  await page.getByRole("button", { name: "Xem trước", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Xem trước Mẫu số 1", exact: true })).toBeVisible();
  await expect(page.getByText(/File xuất đủ 20 cột/)).toBeVisible();
  await page.screenshot({ path: artifact("278-preview.png"), fullPage: true });
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Xuất và tải Mẫu số 1", exact: true }).click();
  expect((await download).suggestedFilename()).toBe("MauSo1_T10_2026.xlsx");
  await expect(page.getByRole("status").filter({ hasText: "Đã xuất Mẫu số 1" })).toBeVisible();
  await page.getByRole("link", { name: "Danh sách nâng bậc", exact: true }).last().click();
  await expect(page.getByRole("table").getByText("Chờ quyết định", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Nguyễn Thị Mẫu", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Căn cứ tính ngày đến kỳ", exact: true })).toBeVisible();
  await expect(page.getByText("Lịch sử xử lý", { exact: true })).toBeVisible();
  await page.screenshot({ path: artifact("278-detail.png"), fullPage: true });
  await page.getByRole("link", { name: "Mở hồ sơ", exact: true }).click();
  await page.getByRole("tab", { name: "Ngạch - Bậc", exact: true }).click();
  await page.getByRole("button", { name: "Thêm dòng", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const nb07 = dialog.getByRole("textbox", { name: "Thời gian xét nâng lương (tháng)", exact: true });
  for (const raw of ["1e100000", "1e-100000", "9".repeat(10000), "0", "121"]) {
    await nb07.fill(raw); await dialog.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(nb07).toHaveAttribute("aria-invalid", "true");
  }
  await page.screenshot({ path: artifact("278-nb07-bound.png"), fullPage: true });
});
