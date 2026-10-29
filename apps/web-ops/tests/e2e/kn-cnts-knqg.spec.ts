import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

// W11.53b (#158 — BRD CNTS v3.7 FN-19 luồng chính, UR-QG-01): lập Mẫu 05 → trình ký → tải bản scan đã ký → lập Mẫu 07 → gửi
// TTKN Quốc gia → xác nhận (Admin giữ quyền thao tác của Cấp TW trong giả lập); thêm/gỡ ảnh thực địa Mẫu 03. Chạy trên bản
// build giả lập, giữ trạng thái trong tab (điều hướng bằng menu, không tải lại trang).

async function login(page: Page) {
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
}

async function createDoc(page: Page, form: RegExp, opts: { site?: boolean; year?: string } = {}) {
  await page.getByRole("button", { name: "Lập biểu", exact: true }).click();
  const box = page.getByRole("region", { name: "Lập biểu" });
  await box.getByLabel("Mẫu").selectOption({ label: (await box.getByLabel("Mẫu").locator("option").allTextContents()).find((t) => form.test(t))! });
  await box.getByLabel("Dự án KNQG").selectOption({ index: 1 });
  if (opts.site) {
    const site = box.getByLabel("Điểm trình diễn");
    await expect(site.locator("option")).toHaveCount(2);
    await site.selectOption({ index: 1 });
  }
  if (opts.year) await box.getByLabel("Năm").fill(opts.year);
  await box.getByRole("button", { name: "Lập biểu", exact: true }).click();
}

test("W11.53b luồng chính Mẫu 05 → ký → Mẫu 07 gửi → TW xác nhận; ảnh thực địa Mẫu 03", async ({ page }) => {
  await page.goto("/login");
  await login(page);
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Chăn nuôi - Thủy sản", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await login(page);
  await sidebar(page).getByRole("button", { name: "Phòng Chăn nuôi - Thủy sản", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Mẫu biểu dự án", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Mẫu biểu dự án KNQG" })).toBeVisible();

  // Mẫu 05: lập (điền sẵn từ dự án) → điền ô bắt buộc → trình ký → tải bản scan đã ký.
  await createDoc(page, /^Mẫu 05/, { site: true });
  const m05 = page.getByRole("region", { name: /^Mẫu 05/ });
  await expect(m05).toBeVisible();
  await m05.getByLabel(/^3\. Đơn vị thực hiện/).fill("Trung tâm Khuyến nông");
  await m05.getByLabel(/^IV\.1\. Đánh giá/).fill("Đạt yêu cầu, đề nghị quyết toán");
  await m05.getByRole("button", { name: "Trình ký", exact: true }).click();
  await expect(m05.getByText("Chờ ký", { exact: true }).first()).toBeVisible();
  await m05.getByLabel("Bản scan đã ký").setInputFiles({ name: "bien-ban-05.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.4\n%%EOF\n") });
  await m05.getByRole("button", { name: "Tải bản đã ký", exact: true }).first().click();
  await expect(m05.getByText("Đã ký", { exact: true }).first()).toBeVisible();

  // Mẫu 07: lập năm 2027 → điền → gửi TTKN Quốc gia → xác nhận (khóa hẳn).
  await createDoc(page, /^Mẫu 07/, { year: "2027" });
  const m07 = page.getByRole("region", { name: /^Mẫu 07/ });
  await expect(m07).toBeVisible();
  await m07.getByLabel(/^Căn cứ hợp đồng số/).fill("12/HĐ-KN ngày 01/03/2027");
  await m07.getByLabel(/^IV\. Nhận xét chung/).fill("Dự án đạt yêu cầu");
  await m07.getByRole("button", { name: "Gửi TTKN Quốc gia", exact: true }).click();
  await expect(m07.getByText("Đã gửi Quốc gia", { exact: false }).first()).toBeVisible();
  await m07.getByRole("button", { name: "Xác nhận", exact: true }).click();
  await expect(m07.getByText("Đã xác nhận", { exact: false }).first()).toBeVisible();
  await expect(m07.getByRole("button", { name: "Gửi TTKN Quốc gia" })).toHaveCount(0);

  // Mẫu 03: ảnh thực địa — thêm có chú thích, gỡ.
  await createDoc(page, /^Mẫu 03/);
  const m03 = page.getByRole("region", { name: /^Mẫu 03/ });
  const photos = m03.getByRole("group", { name: "Ảnh thực địa" });
  await expect(photos.getByText("Chưa có ảnh.")).toBeVisible();
  await photos.getByLabel("Chọn ảnh thực địa").setInputFiles({ name: "chuong-nuoi.png", mimeType: "image/png",
    buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]) });
  await photos.getByLabel("Chú thích").fill("Chuồng nuôi điểm Bình Hòa");
  await photos.getByRole("button", { name: "Thêm ảnh" }).click();
  await expect(photos.getByText(/Chuồng nuôi điểm Bình Hòa/)).toBeVisible();
  await photos.getByRole("button", { name: "Gỡ" }).click();
  await expect(photos.getByText("Chưa có ảnh.")).toBeVisible();
});
