import { expect, test, type Page } from "@playwright/test";
import { MOCK_KN_PASSWORD } from "../../src/api/mock/kn-accounts";
import { logout, sidebar } from "./helpers";

async function login245(page: Page, identifier: string) {
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(MOCK_KN_PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}
async function adhoc245(page: Page) {
  const folder = sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true });
  if (await folder.count() && await folder.getAttribute("aria-expanded") !== "true") await folder.click();
  await sidebar(page).getByRole("link", { name: "Báo cáo phát sinh", exact: true }).click();
}

async function pending245(page: Page) {
  const folder = sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true });
  if (await folder.count() && await folder.getAttribute("aria-expanded") !== "true") await folder.click();
  await sidebar(page).getByRole("link", { name: "Phiếu số liệu các phòng", exact: true }).click();
}

test("245 actual mock UI: adhoc issued, native answer, filtered inbox, desktop/mobile and accepted removal", async ({ page }, info) => {
  test.setTimeout(180000);
  await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
  const name = "Phát sinh hộp phiếu 245", instruction = "Bổ sung số lớp đã đối chiếu hộp phiếu 245";
  await page.goto("/login"); await login245(page, "admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  // The official inbox menu belongs to Province KHTC. Create that real
  // assignment through Admin UI; do not synthesize an Admin KHTC context.
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const main = page.getByRole("main");
  await expect(main.getByRole("button", { name: "Lưu", exact: true })).toBeEnabled();
  await main.getByLabel("Họ tên").fill("Cán bộ KHTC hộp phiếu 245");
  await main.getByLabel("Email").fill("khtc.pending245@example.test");
  await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345245");
  const assignment = main.getByRole("group", { name: "Vai trò và phạm vi" });
  await assignment.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tinh");
  await assignment.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await assignment.getByRole("checkbox", { name: "ke_hoach_tai_chinh", exact: true }).check();
  await main.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Cán bộ KHTC hộp phiếu 245" })).toBeVisible();
  await logout(page); await login245(page, "0912345245"); await adhoc245(page);
  await page.getByText("Mở kỳ phát sinh và giao phiếu", { exact: true }).click();
  await page.getByRole("button", { name: "Mở kỳ", exact: true }).click();
  await page.getByLabel("Tên báo cáo", { exact: true }).fill(name);
  await page.getByLabel("Từ ngày", { exact: true }).fill("2030-01-01");
  await page.getByLabel("Đến ngày phát sinh", { exact: true }).fill("2030-06-01");
  await page.getByLabel("Hạn nộp", { exact: true }).fill("2030-06-08T16:00");
  await page.getByLabel("Hạn xuất", { exact: true }).fill("2030-06-09T16:00");
  await page.getByRole("button", { name: "Xem trước", exact: true }).click();
  await page.getByRole("button", { name: "Mở kỳ và giao phiếu", exact: true }).click();
  await page.getByRole("button", { name, exact: true }).click();
  const request = page.getByRole("region", { name: "Yêu cầu số liệu bổ sung", exact: true });
  await request.getByRole("combobox", { name: "Phòng bổ sung", exact: true }).selectOption("trong_trot");
  await request.getByRole("textbox", { name: "Nội dung yêu cầu", exact: true }).fill(instruction);
  await request.getByLabel("Hạn bổ sung", { exact: true }).fill("2030-06-06T16:00");
  await request.getByRole("button", { name: "Thêm chỉ tiêu số", exact: true }).click();
  await request.getByLabel("Mã chỉ tiêu 1", { exact: true }).fill("SO_LOP");
  await request.getByLabel("Tên chỉ tiêu 1", { exact: true }).fill("Số lớp hộp phiếu 245");
  await request.getByLabel("Đơn vị tính 1", { exact: true }).fill("lớp");
  await request.getByRole("button", { name: "Phát hành yêu cầu", exact: true }).click();
  await expect(request).toContainText("Đã phát hành");
  await logout(page); await login245(page, "binh.tt@angiang.gov.vn");
  await page.getByRole("button", { name: /^Cấp Tỉnh/ }).click();
  await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
  const folder = sidebar(page).getByRole("button", { name: "Báo cáo cho KHTC", exact: true });
  if (await folder.count() && await folder.getAttribute("aria-expanded") !== "true") await folder.click();
  await sidebar(page).getByRole("link", { name: "Phiếu kỳ này", exact: true }).click();
  await page.getByRole("link", { name: instruction, exact: true }).click();
  await page.getByRole("button", { name: "Đã đọc yêu cầu — nhập câu trả lời", exact: true }).click();
  await page.getByLabel(/^Số lớp hộp phiếu 245/).fill("12,5");
  await page.getByLabel("Nội dung trả lời bổ sung", { exact: true }).fill("Đã đối chiếu danh sách lớp.");
  await page.getByRole("button", { name: "Gửi KHTC", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã gửi KHTC lần 1" })).toBeVisible();
  await logout(page); await login245(page, "0912345245");
  await pending245(page);
  await expect(page.getByRole("combobox", { name: "Loại phiếu" })).toHaveValue("regular");
  await page.getByRole("combobox", { name: "Loại phiếu" }).selectOption("adhoc");
  await page.getByRole("combobox", { name: "Phòng", exact: true }).selectOption("trong_trot");
  const table = page.getByRole("table", { name: "Phiếu chờ rà soát · Trang 1", exact: true });
  const target = table.getByRole("row").filter({ has: page.getByRole("cell", { name, exact: true }) });
  await expect(target).toHaveCount(1);
  const period = await page.getByRole("combobox", { name: "Kỳ báo cáo trong nhóm" }).getByRole("option", { name, exact: true }).getAttribute("value");
  expect(period).toBeTruthy(); await page.getByRole("combobox", { name: "Kỳ báo cáo trong nhóm" }).selectOption(period!);
  await expect(target).toHaveCount(1); await expect(page.getByRole("button", { name: "Trang trước" })).toBeDisabled();
  await page.screenshot({ path: info.outputPath("245-adhoc-desktop.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  // Wait for the actual responsive drawer transition before judging the image.
  await expect(sidebar(page)).not.toBeInViewport();
  await page.getByRole("button", { name: "Mở menu", exact: true }).click();
  await expect(sidebar(page)).toBeInViewport();
  await sidebar(page).getByRole("link", { name: "Phiếu số liệu các phòng", exact: true }).click();
  await expect(sidebar(page)).not.toBeInViewport();
  await expect(page.getByRole("combobox", { name: "Loại phiếu" })).toHaveValue("adhoc");
  await expect(target).toHaveCount(1);
  await expect(page.getByRole("heading", { level: 1, name: "Phiếu số liệu các phòng", exact: true })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await page.screenshot({ path: info.outputPath("245-adhoc-mobile.png"), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await target.getByRole("link", { name: "Xem phiếu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: /Rà soát phiếu Phòng Trồng trọt/ })).toBeVisible();
  await expect(page.getByLabel(/^Số lớp hộp phiếu 245/)).toBeDisabled();
  await page.getByRole("button", { name: "Xác nhận", exact: true }).click();
  await page.getByRole("dialog", { name: "Xác nhận phiếu số liệu" }).getByRole("button", { name: "Xác nhận lần gửi", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã xác nhận lần gửi 1." })).toBeVisible();
  await pending245(page);
  await page.getByRole("combobox", { name: "Loại phiếu" }).selectOption("adhoc");
  await page.getByRole("combobox", { name: "Phòng", exact: true }).selectOption("trong_trot");
  await expect(target).toHaveCount(0);
  await expect(page.getByRole("status").filter({ hasText: "Chưa có phiếu chờ rà soát trong nhóm kỳ này." })).toBeVisible();
});
