import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

// W11.53b (#158 — BRD CNTS v3.7 FN-19 luồng chính, UR-QG-01): lập Mẫu 05 → trình ký → tải bản scan đã ký → lập Mẫu 07 → gửi
// TTKN Quốc gia → xác nhận; thêm/gỡ ảnh thực địa Mẫu 03. Chạy trên bản build giả lập, giữ trạng thái trong tab (điều hướng
// phía client, không tải lại trang). Admin mở phòng; tài khoản thử "kn-cnts" (Cấp Tỉnh Trưởng phòng) lập, ký, gửi; cùng tài
// khoản ở vai Cấp Quốc gia xác nhận.

async function login(page: Page, who = "admin.mekong@mekonggreen.vn") {
  await page.getByLabel("Email hoặc số điện thoại").fill(who);
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

/** Màn chọn vai trò: vai level rồi phòng Chăn nuôi – Thủy sản. */
async function enterCnts(page: Page, level: RegExp) {
  await expect(page).toHaveURL(/\/chon-vai-tro/);
  await page.getByRole("button", { name: level }).click();
  await page.getByRole("button", { name: /Phòng Chăn nuôi - Thủy sản — Vào hệ thống/ }).click();
  await expect(sidebar(page)).toBeVisible();
}

const CNTS = "cnts.thu@demo.agrigreen.test";

/** Mở mục menu (nhóm thu gọn → bấm mở nếu link chưa hiện). */
async function openMenu(page: Page, link: string) {
  const l = sidebar(page).getByRole("link", { name: link, exact: true });
  if (!(await l.isVisible().catch(() => false))) {
    const group = sidebar(page).getByRole("button", { name: /Dự án KNQG|Mẫu biểu|Nghiệm thu/ }).first();
    if ((await group.getAttribute("aria-expanded")) !== "true") await group.click();
  }
  await l.click();
}

async function createDoc(page: Page, form: RegExp, opts: { site?: boolean; year?: string } = {}) {
  await page.getByRole("button", { name: "Lập biểu", exact: true }).click();
  const box = page.getByRole("region", { name: "Lập biểu" });
  await expect(box.getByLabel("Dự án KNQG").locator("option")).toHaveCount(2);
  await box.getByLabel("Dự án KNQG").selectOption({ index: 1 });
  const form_ = box.getByLabel("Mẫu");
  const label = (await form_.locator("option").allTextContents()).find((t) => form.test(t))!;
  const value = await form_.locator("option", { hasText: label }).getAttribute("value");
  await form_.selectOption(value!);
  await expect(form_).toHaveValue(value!);
  await expect(box.getByLabel("Dự án KNQG")).not.toHaveValue("");
  if (opts.site) {
    const site = box.getByLabel("Điểm trình diễn");
    await expect(site.locator("option")).toHaveCount(2);
    await site.selectOption({ index: 1 });
  }
  if (opts.year) await box.getByRole("textbox", { name: "Năm", exact: true }).fill(opts.year);
  await box.getByRole("button", { name: "Lập biểu", exact: true }).click();
}

test("W11.53b luồng chính Mẫu 05 → ký → Mẫu 07 gửi → TW xác nhận; ảnh thực địa Mẫu 03", async ({ page }) => {
  await page.goto("/login");
  await login(page);
  await expect(sidebar(page)).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Chăn nuôi - Thủy sản", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page);
  await login(page, CNTS);
  await enterCnts(page, /^Cấp Tỉnh/);
  await openMenu(page, "Mẫu biểu dự án");
  await expect(page.getByRole("heading", { level: 1, name: "Mẫu biểu dự án KNQG" })).toBeVisible();

  // Mẫu 05: lập (điền sẵn từ dự án) → điền ô bắt buộc → trình ký → tải bản scan đã ký.
  await createDoc(page, /^Mẫu 05/, { site: true });
  const m05 = page.getByRole("region", { name: /^Mẫu 05/ });
  await expect(m05).toBeVisible();
  await m05.getByLabel(/^3\. Đơn vị thực hiện/).fill("Trung tâm Khuyến nông");
  await m05.getByLabel(/^IV\.1\. Đánh giá/).fill("Đạt yêu cầu, đề nghị quyết toán");
  await m05.getByRole("button", { name: "Trình ký", exact: true }).click();
  await expect(m05.getByText("Chờ ký", { exact: true }).first()).toBeVisible();
  await m05.getByLabel("Bản scan đã ký").setInputFiles({ name: "bien-ban-05.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.4 %%EOF") });
  await m05.getByRole("button", { name: "Tải bản đã ký", exact: true }).first().click();
  await expect(m05.getByText("Đã ký", { exact: true }).first()).toBeVisible();

  // Mẫu 07: lập năm 2027 → điền → gửi TTKN Quốc gia.
  await createDoc(page, /^Mẫu 07/, { year: "2027" });
  const m07 = page.getByRole("region", { name: /^Mẫu 07/ });
  await expect(m07).toBeVisible();
  await m07.getByLabel(/^Căn cứ hợp đồng số/).fill("12/HĐ-KN ngày 01/03/2027");
  await m07.getByLabel(/^IV\. Nhận xét chung/).fill("Dự án đạt yêu cầu");
  await m07.getByRole("button", { name: "Gửi TTKN Quốc gia", exact: true }).click();
  await expect(m07.getByText("Đã gửi Quốc gia", { exact: true }).first()).toBeVisible();
  await expect(m07.getByRole("button", { name: "Xác nhận", exact: true })).toHaveCount(0);

  // Mẫu 03: ảnh thực địa — thêm có chú thích, gỡ.
  await createDoc(page, /^Mẫu 03/);
  const m03 = page.getByRole("region", { name: /^Mẫu 03/ });
  const photos = m03.getByRole("group", { name: "Ảnh thực địa" });
  await expect(photos.getByText("Chưa có ảnh.")).toBeVisible();
  await photos.getByLabel("Chọn ảnh thực địa").setInputFiles({
    name: "chuong-nuoi.png",
    mimeType: "image/png",
    buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  });
  await photos.getByLabel("Chú thích").fill("Chuồng nuôi điểm Bình Hòa");
  await photos.getByRole("button", { name: "Thêm ảnh" }).click();
  await expect(photos.getByText(/Chuồng nuôi điểm Bình Hòa/)).toBeVisible();
  await photos.getByRole("button", { name: "Gỡ" }).click();
  await expect(photos.getByText("Chưa có ảnh.")).toBeVisible();

  // Cấp Quốc gia (UR-QG-01): thấy Mẫu 07 đã gửi, xác nhận → khóa hẳn.
  await logout(page);
  await login(page, CNTS);
  await enterCnts(page, /^Cấp Quốc gia/);
  await openMenu(page, "Mẫu biểu, nghiệm thu năm (07)");
  await expect(page.getByRole("heading", { level: 1, name: "Mẫu biểu dự án KNQG" })).toBeVisible();
  await page.getByRole("button", { name: /^Mẫu 07/ }).first().click();
  const tw07 = page.getByRole("region", { name: /^Mẫu 07/ });
  await tw07.getByRole("button", { name: "Xác nhận", exact: true }).click();
  await expect(tw07.getByText("Đã xác nhận", { exact: true }).first()).toBeVisible();
  await expect(tw07.getByRole("button", { name: "Trả lại", exact: true })).toHaveCount(0);
});
