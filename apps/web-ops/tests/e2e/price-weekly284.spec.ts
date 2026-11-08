import { expect, test, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { loginAs, logout, sidebar } from "./helpers";

async function provinceTTTT(page: Page) {
  await page.clock.setFixedTime(new Date("2026-11-08T04:00:00Z"));
  await loginAs(page, "kn-admin", "/kn/tong-quan");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const form = page.getByRole("main");
  await form.getByRole("textbox", { name: "Họ tên", exact: true }).fill("Kiểm chứng Chương trình284");
  await form.getByRole("textbox", { name: "Email", exact: true }).fill("chuongtrinh284@vi-du.vn");
  await form.getByLabel("Số điện thoại (tên đăng nhập)", { exact: true }).fill("0912345284");
  await form.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tinh");
  await form.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await form.getByRole("checkbox", { name: "thong_tin_truyen_thong", exact: true }).check();
  await form.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Kiểm chứng Chương trình284" })).toBeVisible();
  await logout(page);
  await page.getByLabel("Email hoặc số điện thoại").fill("chuongtrinh284@vi-du.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026"); // Documented synthetic mock-account password.
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(page.getByTestId("account-menu")).toContainText("Thông tin Tuyên truyền");
}

const farmers = "Tập huấn nông dân chủ chốt (lớp)";
const field = (page: Page, label: string, name = farmers) => page.getByRole("textbox", { name: `${label} ${name}`, exact: true });

async function save(page: Page) {
  await page.getByRole("button", { name: "Lưu nháp", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã lưu nháp." })).toBeVisible();
}

async function closeAndNew(page: Page) {
  await page.getByRole("button", { name: /^Chốt báo cáo tuần/ }).click();
  await page.getByRole("button", { name: "Xác nhận chốt", exact: true }).click();
  await expect(page.getByRole("button", { name: "Lưu nháp", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Nhập từ Excel", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Tạo báo cáo tuần mới", exact: true }).click();
  await expect(page.getByRole("button", { name: "Lưu nháp", exact: true })).toBeVisible();
}

/** Read actual uncompressed ZIP entries emitted by the mock's valid OOXML writer. */
function zipEntries(data: Buffer): Map<string, string> {
  const files = new Map<string, string>(); let at = 0;
  while (at + 30 <= data.length && data.readUInt32LE(at) === 0x04034b50) {
    const method = data.readUInt16LE(at + 8), size = data.readUInt32LE(at + 18), nameLength = data.readUInt16LE(at + 26), extraLength = data.readUInt16LE(at + 28);
    expect(method).toBe(0); const start = at + 30 + nameLength + extraLength;
    files.set(data.subarray(at + 30, at + 30 + nameLength).toString("utf8"), data.subarray(start, start + size).toString("utf8")); at = start + size;
  }
  expect(data.readUInt32LE(at)).toBe(0x02014b50); return files;
}

test("284 training: exact live AC01/03, override carry-forward, readonly history and mobile export", async ({ page }) => {
  await provinceTTTT(page);
  await sidebar(page).getByRole("link", { name: "Đào tạo huấn luyện và hợp tác", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Đào tạo huấn luyện và hợp tác" })).toBeVisible();
  await field(page, "Tiến độ tuần").fill("10"); await field(page, "Kinh phí tuần").fill("803458000"); await save(page); await closeAndNew(page);
  await field(page, "Tiến độ tuần").fill("2"); await field(page, "Kinh phí tuần").fill("10000000");
  const row = page.getByRole("row").filter({ has: page.getByRole("rowheader", { name: farmers, exact: true }) });
  await expect(row).toContainText("12 / 12"); await expect(row).toContainText("đạt 100,00% KH"); await expect(page.getByTestId("weekly-total-money")).toHaveText("813.458.000");
  await page.evaluate(() => window.scrollTo(0, 0)); await page.screenshot({ path: test.info().outputPath("284-training-ac01-ac03.png"), fullPage: true });
  await field(page, "Kinh phí lũy kế").fill("900000000"); await expect(page.getByTestId("weekly-total-money")).toHaveText("900.000.000"); await save(page); await closeAndNew(page);
  await field(page, "Kinh phí tuần").fill("10000000"); await expect(page.getByTestId("weekly-total-money")).toHaveText("910.000.000"); await save(page);
  const selected = await page.getByRole("combobox", { name: "Kỳ báo cáo" }).locator("option:checked").textContent();
  const date = selected!.match(/(\d{2})\/(\d{2})\/(\d{4})/)!; const expectedDate = `${date[3]}-${date[2]}-${date[1]}`;
  const download = page.waitForEvent("download"); await page.getByRole("button", { name: "Xuất Excel", exact: true }).click(); const file = await download;
  const exportPath = test.info().outputPath("284-training-export.xlsx"); await file.saveAs(exportPath);
  const entries = zipEntries(await readFile(exportPath)); expect(entries.has("[Content_Types].xml")).toBe(true); expect(entries.get("xl/styles.xml")).toContain('borderId="1"');
  const sheet = entries.get("xl/worksheets/sheet1.xml")!; expect(sheet).toContain('ref="D4:E4"'); expect(sheet).toContain('ref="G4:H4"'); expect(sheet).toContain(expectedDate); expect(sheet).toContain("910000000");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(sidebar(page)).not.toBeInViewport(); // Wait for the existing drawer transition before capturing mobile evidence.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  expect(await page.locator(".weekly-table").evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true);
  await page.evaluate(() => window.scrollTo(0, 0)); await page.screenshot({ path: test.info().outputPath("284-training-mobile.png"), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  const periods = page.getByRole("combobox", { name: "Kỳ báo cáo" }); const old = await periods.locator("option").last().getAttribute("value"); await periods.selectOption(old!);
  await expect(page.getByRole("note")).toContainText("Báo cáo đã chốt — chỉ xem"); await expect(page.getByRole("button", { name: "Lưu nháp", exact: true })).toHaveCount(0); await expect(page.getByRole("button", { name: "Thêm hạng mục", exact: true })).toHaveCount(0);
});

test("284 information: adjusted plan, partial Excel/overwrite/error download and group deletion", async ({ page }) => {
  await provinceTTTT(page); await sidebar(page).getByRole("link", { name: "Chương trình thông tin", exact: true }).click();
  const prices = "Thu thập thông tin giá cả nông sản (phiếu)";
  await page.getByRole("button", { name: `Điều chỉnh kế hoạch ${prices}`, exact: true }).click(); await page.getByLabel("Quy mô điều chỉnh", { exact: true }).fill("3822"); await page.getByLabel("Lý do điều chỉnh", { exact: true }).fill("Fixture UAT284 theo kế hoạch được duyệt");
  await page.keyboard.press("Escape"); await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: `Điều chỉnh kế hoạch ${prices}`, exact: true }).click(); await expect(page.getByLabel("Quy mô điều chỉnh", { exact: true })).toHaveValue("6552");
  await page.getByLabel("Quy mô điều chỉnh", { exact: true }).fill("3822"); await page.getByLabel("Lý do điều chỉnh", { exact: true }).fill("Fixture UAT284 theo kế hoạch được duyệt"); await page.getByRole("button", { name: "Lưu điều chỉnh", exact: true }).click(); await expect(page.getByRole("dialog")).toHaveCount(0);
  await field(page, "Tiến độ tuần", prices).fill("1e100000"); await expect(field(page, "Tiến độ tuần", prices)).toHaveAttribute("aria-invalid", "true"); await expect(page.getByRole("button", { name: "Lưu nháp", exact: true })).toBeDisabled();
  await field(page, "Tiến độ tuần", prices).fill("1934"); await expect(page.getByRole("row").filter({ has: page.getByRole("rowheader", { name: prices, exact: true }) })).toContainText("đạt 50,60% KH"); await save(page);
  await page.evaluate(() => window.scrollTo(0, 0)); await page.screenshot({ path: test.info().outputPath("284-information-ac02.png"), fullPage: true });
  await page.getByRole("button", { name: "Nhập từ Excel", exact: true }).click();
  const templateDownload = page.waitForEvent("download"); await page.getByRole("button", { name: "Tải tệp mẫu", exact: true }).click(); const template = await templateDownload; const templatePath = test.info().outputPath("284-information-template.xlsx"); await template.saveAs(templatePath);
  const templateEntries = zipEntries(await readFile(templatePath)); expect(templateEntries.get("xl/worksheets/sheet1.xml")).toContain('ref="D4:E4"');
  const csv = `Tên hạng mục,Tiến độ tuần,Kinh phí tuần,Đánh giá tuần,Lũy kế\r\n"${prices}",1935,,Fixture hợp lệ,1e100000\r\nTruyền hình (chuyên mục),-1,5,,\r\nTên chưa có,1,2,,\r\n`;
  await page.getByLabel(/Chọn tệp Excel hoặc CSV/).setInputFiles({ name: "284-partial.csv", mimeType: "text/csv", buffer: Buffer.from(csv, "utf8") }); await page.getByRole("button", { name: "Kiểm tra tệp", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("1 dòng hợp lệ · 1 cảnh báo ghi đè · 2 dòng lỗi"); await expect(page.getByRole("button", { name: "Ghi nhận dòng hợp lệ", exact: true })).toBeDisabled();
  const errorsDownload = page.waitForEvent("download"); await page.getByRole("button", { name: "Tải danh sách dòng lỗi", exact: true }).click(); const errors = await errorsDownload; await errors.saveAs(test.info().outputPath("284-import-errors.csv"));
  await page.getByRole("checkbox", { name: "Xác nhận ghi đè các ô có số liệu khác", exact: true }).check(); await page.getByRole("button", { name: "Ghi nhận dòng hợp lệ", exact: true }).click(); await expect(page.getByRole("dialog")).toHaveCount(0); await expect(field(page, "Tiến độ tuần", prices)).toHaveValue("1935");
  // The actual downloaded Excel template can be uploaded through the same UI; blank cells preserve existing numbers.
  await page.getByRole("button", { name: "Nhập từ Excel", exact: true }).click(); await page.getByLabel(/Chọn tệp Excel hoặc CSV/).setInputFiles(templatePath); await page.getByRole("button", { name: "Kiểm tra tệp", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("17 dòng hợp lệ · 0 cảnh báo ghi đè · 2 dòng lỗi"); await page.getByRole("button", { name: "Ghi nhận dòng hợp lệ", exact: true }).click(); await expect(field(page, "Tiến độ tuần", prices)).toHaveValue("1935");
  await page.getByRole("button", { name: "Thêm hạng mục", exact: true }).click(); await page.getByLabel("Tên hạng mục (kèm đơn vị tính)", { exact: true }).fill("Nhóm UAT284"); await page.getByRole("checkbox", { name: "Mục nhóm gồm các mục con", exact: true }).check();
  await page.getByLabel("Tên mục con 1", { exact: true }).fill("Hoạt động UAT284 (lớp)"); await page.getByLabel("Quy mô mục con 1", { exact: true }).fill("2"); await page.getByRole("button", { name: "Thêm vào báo cáo", exact: true }).click(); await expect(field(page, "Tiến độ tuần", "Hoạt động UAT284 (lớp)")).toBeVisible();
  await field(page, "Tiến độ tuần", "Hoạt động UAT284 (lớp)").fill("1"); await save(page);
  await page.getByRole("button", { name: "Xóa Nhóm UAT284", exact: true }).click(); await expect(page.getByRole("dialog")).toContainText("kể cả tuần đã chốt"); await page.getByRole("button", { name: "Đóng", exact: true }).click(); await expect(page.getByRole("button", { name: "Xóa Nhóm UAT284", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Xóa Nhóm UAT284", exact: true }).click(); await page.getByRole("button", { name: "Xác nhận xóa", exact: true }).click(); await expect(field(page, "Tiến độ tuần", "Hoạt động UAT284 (lớp)")).toHaveCount(0);
  await page.locator(".weekly-audit summary").click(); await expect(page.locator(".weekly-audit")).toContainText("Xóa hạng mục"); await page.evaluate(() => window.scrollTo(0, 0)); await page.screenshot({ path: test.info().outputPath("284-information-audit.png"), fullPage: true });
});
