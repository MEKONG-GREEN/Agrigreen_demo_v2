import { expect, test, type Page } from "@playwright/test";
import { accountMenu, sidebar } from "./helpers";

// KN v7.7 P3b — FN-AD-04.06 / UR-AD-06: Admin › Phòng ban › Phòng Trồng trọt › Mẫu báo cáo (Hình 27).
// Trạng thái giả lập nằm trong bộ nhớ tab → đổi người dùng bằng Đăng xuất/Đăng nhập trên giao diện (không page.goto).
const PASSWORD = "KhuyenNong@2026";
const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });

async function submitLogin(page: Page, identifier: string): Promise<void> {
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

async function openForms(page: Page): Promise<void> {
  const parent = menuNav(page).getByRole("button", { name: /Phòng Trồng trọt/ });
  if ((await parent.getAttribute("aria-expanded")) !== "true") await parent.click();
  await menuNav(page).getByRole("link", { name: "Mẫu báo cáo", exact: true }).click();
  await expect(page).toHaveURL(/\/kn\/phong-ban\/trong_trot\/mau-bao-cao$/);
  await expect(page.getByRole("heading", { level: 1, name: "Quản lý mẫu báo cáo" })).toBeVisible();
}

async function loginAdmin(page: Page): Promise<void> {
  await page.goto("/login");
  await submitLogin(page, "admin.mekong@mekonggreen.vn");
  await expect(page).toHaveURL(/\/kn\/tong-quan$/);
  await openForms(page);
}

const alert = (page: Page) => page.locator("main .alert-bad[role=alert]");
const notice = (page: Page) => page.locator("main .alert-info[role=status]");
const designGrid = (page: Page) => page.getByRole("grid", { name: /^Cấu trúc mẫu/ });

test.describe("FN-AD-04.06 Mẫu báo cáo (Admin)", () => {
  test("vòng đời: tạo mẫu, sửa cấu trúc, lỗi công thức từ máy chủ, gán, đề nghị, tự phát hành bị chặn (R-56), người khác phát hành, ngừng dùng", async ({ page }) => {
    await loginAdmin(page);
    const list = page.getByRole("table").first();
    await expect(list).toContainText("T01");
    await expect(list).toContainText("T04");
    await expect(list).toContainText("CC01");
    await expect(page.getByRole("table", { name: "Gán mẫu vào loại báo cáo của phòng ban" })).toContainText("Báo cáo gửi Chi cục");

    // B1 Tạo mẫu mới (trống) → nháp v1.
    await page.getByRole("button", { name: "+ Tạo mẫu mới" }).click();
    const dlg = page.getByRole("dialog", { name: "Tạo mẫu mới" });
    await dlg.getByLabel("Mã mẫu *").fill("CC02");
    await dlg.getByLabel("Tên mẫu *").fill("Báo cáo nhanh gửi Chi cục");
    await dlg.getByLabel("Cấp nhập liệu *").selectOption("province");
    await dlg.getByRole("button", { name: "Tạo nháp v1" }).click();
    await expect(page).toHaveURL(/\/mau-bao-cao\/[0-9a-f-]{36}\/v\/1$/);
    await expect(page.getByRole("heading", { level: 1, name: /Chỉnh sửa cấu trúc mẫu CC02/ })).toBeVisible();
    await expect(designGrid(page).getByRole("columnheader", { name: "Cột D" })).toBeVisible();

    // B2 + cột sau D → E; B3 thuộc tính cột.
    await designGrid(page).getByRole("button", { name: "D", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Thuộc tính cột D" })).toBeVisible();
    await page.getByRole("button", { name: "+ Cột sau" }).click();
    await expect(designGrid(page).getByRole("columnheader", { name: "Cột E" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Thuộc tính cột E" })).toBeVisible();
    await page.getByLabel("Tiêu đề", { exact: true }).fill("Diện tích quy đổi");
    await page.getByRole("button", { name: "Áp dụng cho cột E" }).click();
    await expect(designGrid(page)).toContainText("Diện tích quy đổi");

    // Công thức sai → lỗi của máy chủ gắn với cột E; sửa → dòng loại cột hiện ƒx.
    await page.getByRole("button", { name: "ƒx Công thức" }).click();
    await page.getByLabel(/^Công thức/).fill("SUM(D#:");
    await page.getByRole("button", { name: "Áp dụng cho cột E" }).click();
    await expect(alert(page)).toContainText("Công thức sai cú pháp");
    await expect(alert(page)).toContainText("reports.formula_invalid");
    await expect(alert(page)).toContainText("Cột E · công thức");
    await page.getByLabel(/^Công thức/).fill("D#*2");
    await page.getByRole("button", { name: "Áp dụng cho cột E" }).click();
    await expect(alert(page)).toHaveCount(0);
    await expect(designGrid(page).locator("tr.g-kinds td").nth(4)).toHaveText("ƒx");
    // Chuyển cột E sang trái (đổi chỗ với D, công thức đổi tham chiếu) rồi chuyển lại.
    await page.getByRole("button", { name: "← Chuyển cột" }).click();
    await expect(designGrid(page).locator("tr.g-kinds td").nth(3)).toHaveText("ƒx");
    await expect(page.getByRole("heading", { name: "Thuộc tính cột D" })).toBeVisible();
    await expect(page.getByLabel(/^Công thức/)).toHaveValue("E#*2");
    await page.getByRole("button", { name: "Chuyển cột →" }).click();
    await expect(designGrid(page).locator("tr.g-kinds td").nth(4)).toHaveText("ƒx");
    // Xóa cột D đang được công thức tham chiếu → máy chủ từ chối.
    await designGrid(page).getByRole("button", { name: "D", exact: true }).click();
    await page.getByRole("button", { name: "− Xóa cột" }).click();
    await expect(alert(page)).toContainText("reports.formula_invalid_ref");
    await alert(page).getByRole("button", { name: "Đóng" }).click();

    // Tiêu đề: thêm dòng tiêu đề, gộp D1:E1, đặt chữ, tách.
    await page.getByRole("button", { name: "+ Dòng tiêu đề" }).click();
    await designGrid(page).locator('th[data-cell="D1"]').click();
    await designGrid(page).locator('th[data-cell="E1"]').click({ modifiers: ["Shift"] });
    await page.getByRole("button", { name: "Gộp ô" }).click();
    await expect(designGrid(page).locator('th[data-cell="D1"]')).toHaveAttribute("colspan", "2");
    await designGrid(page).locator('th[data-cell="D1"]').click();
    await page.getByLabel("Chữ ô tiêu đề D1").fill("DIỆN TÍCH (ha)");
    await page.getByRole("button", { name: "Đặt chữ" }).click();
    await expect(designGrid(page).locator('th[data-cell="D1"]')).toHaveText("DIỆN TÍCH (ha)");

    // Xem trước: máy chủ tính E = D × 2 trên số liệu mẫu.
    await page.getByRole("button", { name: "Xem trước màn hình nhập liệu" }).click();
    const preview = page.getByRole("region", { name: /Xem trước màn hình nhập liệu/ });
    await expect(preview.getByRole("tab", { name: /Mỹ Hòa Hưng/ })).toBeVisible();
    await expect(preview.getByRole("grid").locator('tr[aria-rowindex="1"] [data-letter="E"]')).not.toBeEmpty();

    // Chưa gán loại báo cáo → không đề nghị được.
    await page.getByLabel("Áp dụng từ kỳ (ngày bắt đầu)").fill("2026-09-07");
    await page.getByLabel("Nhãn kỳ").fill("Tuần 37/2026");
    await page.getByRole("button", { name: "Đề nghị phát hành" }).click();
    await expect(alert(page)).toContainText("reports.form_not_assigned");
    await alert(page).getByRole("button", { name: "Đóng" }).click();

    // B6 gán mẫu vào loại báo cáo (trang chi tiết).
    await page.getByRole("link", { name: "Lịch sử phiên bản" }).click();
    await expect(page.getByRole("heading", { level: 1, name: /CC02 · Báo cáo nhanh gửi Chi cục/ })).toBeVisible();
    await page.getByLabel("Loại báo cáo cần gán").selectOption("dispatch");
    await page.getByRole("button", { name: "Gán mẫu" }).click();
    await expect(page.getByLabel("Gán vào loại báo cáo")).toContainText("Báo cáo gửi Chi cục");
    await expect(page.getByRole("complementary", { name: "Log" })).toContainText("Gán loại báo cáo");
    await page.getByRole("link", { name: "Mở nháp v1" }).click();

    // B5 đề nghị phát hành (khóa nội dung) → tự phát hành bị chặn.
    await page.getByLabel("Áp dụng từ kỳ (ngày bắt đầu)").fill("2026-09-07");
    await page.getByLabel("Nhãn kỳ").fill("Tuần 37/2026");
    await page.getByRole("button", { name: "Đề nghị phát hành" }).click();
    await expect(notice(page)).toContainText("Đã đề nghị phát hành");
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Chờ phát hành");
    await expect(page.locator(".fd-sod")).toContainText("bạn là người đề nghị");
    await expect(page.getByRole("button", { name: "+ Cột sau" })).toHaveCount(0);
    await page.getByRole("button", { name: "Phát hành phiên bản" }).click();
    await expect(alert(page)).toContainText("Không được tự phê duyệt");
    await expect(alert(page)).toContainText("self_approval_forbidden");
    const editorUrl = page.url();

    // Admin khác (Nguyễn Văn An, vai Admin) phát hành.
    await accountMenu(page).click();
    await page.getByRole("menuitem", { name: "Đăng xuất" }).click();
    await submitLogin(page, "an.nv@angiang.gov.vn");
    await expect(page).toHaveURL(/\/chon-vai-tro/);
    await page.getByRole("button", { name: /^Admin/ }).click();
    await expect(page).toHaveURL(/\/kn\/tong-quan$/);
    await openForms(page);
    await page.getByRole("link", { name: "Sửa mẫu CC02" }).click();
    await expect(page).toHaveURL(editorUrl);
    await expect(page.locator(".fd-sod")).not.toContainText("bạn là người đề nghị");
    await page.getByRole("button", { name: "Phát hành phiên bản" }).click();
    await expect(notice(page)).toContainText("Đã phát hành CC02 v1");
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Đã phát hành");

    // Ngừng dùng phiên bản kèm lý do.
    await page.getByRole("button", { name: "Ngừng dùng phiên bản" }).click();
    const rd = page.getByRole("dialog", { name: /Ngừng dùng CC02 v1/ });
    await rd.getByLabel(/Lý do/).fill("Chi cục đổi mẫu từ quý IV");
    await rd.getByRole("button", { name: "Xác nhận" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Ngừng dùng");
    await page.getByRole("link", { name: "Lịch sử phiên bản" }).click();
    const log = page.getByRole("complementary", { name: "Log" });
    await expect(log).toContainText("Phát hành phiên bản · v1");
    await expect(log).toContainText("Nguyễn Văn An");
    await expect(log).toContainText("Lý do: Chi cục đổi mẫu từ quý IV");
  });

  test("tải mẫu Excel → nạp lại thành mẫu mới (báo cáo nạp có cảnh báo); khởi tạo 14 mẫu bỏ qua mẫu đã có", async ({ page }) => {
    await loginAdmin(page);
    await page.getByRole("link", { name: /Tiến độ xuống giống lúa/ }).click();
    await expect(page.getByRole("heading", { level: 1, name: /T01 · Tiến độ xuống giống lúa/ })).toBeVisible();
    const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name: "Tải mẫu Excel" }).click()]);
    expect(download.suggestedFilename()).toBe("T01-v1.xlsx");
    const file = await download.path();

    await page.getByRole("link", { name: "Danh sách mẫu báo cáo" }).click();
    await page.getByRole("button", { name: "Nạp từ Excel" }).click();
    const dlg = page.getByRole("dialog", { name: "Nạp mẫu từ Excel" });
    await dlg.getByLabel("Mã mẫu *").fill("T01X");
    await dlg.getByLabel("Tên mẫu *").fill("Tiến độ xuống giống (nạp lại)");
    await dlg.getByLabel("Tệp Excel (.xlsx) *").setInputFiles({ name: "T01-v1.xlsx", mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buffer: await (await import("node:fs/promises")).readFile(file) });
    await dlg.getByRole("button", { name: "Nạp tệp" }).click();
    const rep = dlg.getByRole("region", { name: "Kết quả nạp" });
    await expect(rep).toContainText("PL 01_Tđộ XG TĐ 2026");
    await expect(rep).toContainText("15");
    await expect(rep).toContainText("Cảnh báo khi nạp (7)");
    await dlg.getByRole("link", { name: "Mở nháp T01X v1" }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Chỉnh sửa cấu trúc mẫu T01X/ })).toBeVisible();
    await expect(designGrid(page).getByRole("columnheader", { name: "Cột O" })).toBeVisible();
    await expect(designGrid(page).locator("tr.g-kinds td").nth(12)).toHaveText("ƒx");

    await page.getByRole("link", { name: /T01X · / }).first().click();
    await page.getByRole("link", { name: "Danh sách mẫu báo cáo" }).click();
    await page.getByRole("button", { name: "Khởi tạo 14 mẫu" }).click();
    await page.getByRole("dialog", { name: /Khởi tạo 14 mẫu/ }).getByRole("button", { name: "Khởi tạo" }).click();
    await expect(notice(page)).toContainText("tạo 12 mẫu nháp");
    await expect(notice(page)).toContainText("bỏ qua 2 mẫu đã có (T01, T04)");
    await expect(page.getByRole("table").first()).toContainText("V06");
  });

  test("T04 vẽ đủ tới cột HR, cố định A–J khi cuộn ngang, cuộn mượt, công thức do máy chủ tính", async ({ page }) => {
    await loginAdmin(page);
    await page.getByRole("link", { name: /Xuống giống, thu hoạch rau màu/ }).click();
    await page.getByRole("link", { name: "v1", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Chỉnh sửa cấu trúc mẫu T04/ })).toContainText("Đã phát hành");
    const t0 = Date.now();
    await page.getByRole("button", { name: "Xem trước màn hình nhập liệu" }).click();
    const preview = page.getByRole("region", { name: /Xem trước màn hình nhập liệu/ });
    const grid = preview.getByRole("grid");
    await expect(grid.getByRole("columnheader", { name: "Cột HR" })).toBeAttached();
    // E = tổng DT xuống giống các cây (công thức máy chủ) có số.
    await expect(grid.locator('tr[aria-rowindex="1"] [data-letter="E"]')).toHaveText(/\d/);
    expect(Date.now() - t0).toBeLessThan(8000);
    await expect(preview.getByText("Rau muống").first()).toBeVisible();

    const wrap = preview.getByTestId("form-grid");
    const metrics = await wrap.evaluate(async (el) => {
      const before = el.querySelector<HTMLElement>('[data-letter="J"]')!.getBoundingClientRect().left;
      const max = el.scrollWidth - el.clientWidth;
      const frames: number[] = [];
      let last = performance.now();
      for (let i = 1; i <= 30; i++) {
        el.scrollLeft = (max * i) / 30;
        await new Promise((r) => requestAnimationFrame(() => r(null)));
        const now = performance.now();
        frames.push(now - last);
        last = now;
      }
      const after = el.querySelector<HTMLElement>('[data-letter="J"]')!.getBoundingClientRect().left;
      const hr = el.querySelector<HTMLElement>('[data-letter="HR"]')!.getBoundingClientRect();
      const box = el.getBoundingClientRect();
      frames.sort((a, b) => a - b);
      return { max, before, after, hrVisible: hr.right <= box.right + 1 && hr.left >= box.left, p90: frames[Math.floor(frames.length * 0.9)]!, total: frames.reduce((s, x) => s + x, 0) };
    });
    expect(metrics.max).toBeGreaterThan(10_000); // 226 cột
    expect(Math.abs(metrics.after - metrics.before)).toBeLessThan(2); // cột J cố định
    expect(metrics.hrVisible).toBe(true);
    expect(metrics.p90).toBeLessThan(100);
    expect(metrics.total).toBeLessThan(3000);

    // "Đi tới" nhóm cây cuộn tới nhóm.
    await wrap.evaluate((el) => (el.scrollLeft = 0));
    await preview.getByLabel("Đi tới nhóm cột").selectOption({ index: 9 });
    await expect.poll(() => wrap.evaluate((el) => el.scrollLeft)).toBeGreaterThan(5000);

    // Tab "Tất cả các xã": dòng Cộng khu vực, Toàn tỉnh.
    await preview.getByRole("tab", { name: /Tất cả các xã/ }).click();
    await expect(preview.getByRole("grid")).toContainText("Cộng Long Xuyên");
    await expect(preview.getByRole("grid")).toContainText("Toàn tỉnh");
  });
});
