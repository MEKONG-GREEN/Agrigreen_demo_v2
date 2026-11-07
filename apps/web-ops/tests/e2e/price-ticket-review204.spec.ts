import { expect, test, type Page } from "@playwright/test";
import { loginAs, logout, sidebar } from "./helpers";
test.use({ actionTimeout: 15_000 });
async function openReview(page: Page) {
  await loginAs(page, "kn-admin", "/kn/tong-quan");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  // Provision a real TTTT assignment through Admin UI. The support menu intentionally
  // targets Trồng trọt, and Admin catalog menus do not include ticket review.
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const form = page.getByRole("main");
  await form.getByRole("textbox", { name: "Họ tên", exact: true }).fill("Kiểm chứng Phiếu204");
  await form.getByRole("textbox", { name: "Email", exact: true }).fill("phieu204@vi-du.vn");
  await form.getByLabel("Số điện thoại (tên đăng nhập)", { exact: true }).fill("0912345204");
  await form.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tinh");
  await form.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await form.getByRole("checkbox", { name: "thong_tin_truyen_thong", exact: true }).check();
  await form.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Kiểm chứng Phiếu204" })).toBeVisible();
  await logout(page);
  await page.getByLabel("Email hoặc số điện thoại").fill("phieu204@vi-du.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026"); // Documented mock-account fixture only.
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(page.getByTestId("account-menu")).toContainText("Thông tin Tuyên truyền");
  await sidebar(page).getByRole("link", { name: "Kiểm duyệt & công bố", exact: true }).click();
  await page.getByLabel("Ngày báo giá", { exact: true }).fill("2026-11-04");
  await page.getByRole("checkbox", { name: "Chỉ xã được phân công" }).uncheck();
  await expect(page.getByRole("checkbox", { name: "Chỉ xã được phân công" })).not.toBeChecked();
  await expect(page).toHaveURL(/kiem-duyet\?day=2026-11-04&mine=false&tab=tickets/);
  await page.getByRole("link", { name: "Xem / sửa", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "Chi tiết phiếu báo giá", exact: true })).toBeVisible();
}
test("204: actual route detail → correct → Log, filter-preserving cancel and soft delete", async ({ page }) => {
  test.setTimeout(180_000);
  await page.clock.setFixedTime(new Date("2026-11-05T06:00:00Z"));
  const posts: string[] = []; page.on("request", r => { if (r.method() === "POST" && /\/price\/tickets\//.test(r.url()) && !r.url().includes("/edit-lease/")) posts.push(r.url()); });
  await openReview(page);
  await expect(page.getByRole("complementary", { name: "Nhật ký phiếu" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 1 })).toHaveCSS("font-family", /Noto Serif/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("link", { name: "Hiệu chỉnh", exact: true }).click();
  await expect(page).toHaveURL(/\/hieu-chinh\?day=2026-11-04&mine=false&tab=tickets/);
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByText("Chọn ít nhất một dòng để hiệu chỉnh.")).toBeVisible();
  const before = posts.length;
  await page.getByRole("link", { name: "Hủy", exact: true }).click();
  expect(posts.length).toBe(before);
  await page.getByRole("link", { name: "Hiệu chỉnh", exact: true }).click();
  await page.getByRole("textbox", { name: /^Giá thấp nhất/ }).first().fill("1e100000");
  await page.getByRole("textbox", { name: /^Giá cao nhất/ }).first().fill("9000");
  await page.getByRole("textbox", { name: /^Lý do/ }).fill("Đã đối chiếu phiếu giấy204");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("textbox", { name: /^Giá thấp nhất/ }).first()).toHaveAttribute("aria-invalid", "true");
  expect(posts.length).toBe(before);
  await page.getByRole("textbox", { name: /^Giá thấp nhất/ }).first().fill("00001.2300");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Chi tiết phiếu báo giá", exact: true })).toBeVisible();
  const log = page.getByRole("complementary", { name: "Nhật ký phiếu" });
  await expect(log).toContainText("Hiệu chỉnh"); await expect(log).toContainText("Đã đối chiếu phiếu giấy204"); await expect(log).toContainText("Giá Xã gửi (lưu nguyên)");
  await page.screenshot({ path: test.info().outputPath("204-detail-log.png"), fullPage: true });
  await page.getByRole("link", { name: "Sửa ngày", exact: true }).click();
  await page.getByLabel("Ngày phiếu mới", { exact: false }).fill("2026-10-22");
  await page.getByRole("textbox", { name: /^Lý do/ }).fill("Sửa ngày phiếu giấy204");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Chi tiết phiếu báo giá", exact: true })).toBeVisible();
  await expect(log).toContainText("Sửa ngày phiếu giấy204");
  await page.getByRole("link", { name: "Xóa phiếu", exact: true }).click();
  await page.getByRole("textbox", { name: /^Lý do/ }).fill("Phiếu sai204");
  await page.getByRole("button", { name: "Xác nhận xóa phiếu", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Chi tiết phiếu báo giá", exact: true })).toBeVisible();
  await expect(log).toContainText("Xóa mềm");await expect(page.getByRole("link", { name: "Hiệu chỉnh", exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "Danh sách kiểm duyệt", exact: true }).click();
  await expect(page).toHaveURL(/kiem-duyet\?day=2026-11-04&mine=false&tab=tickets/);
});
test("204: delete cancel and real logout do not write or retain form access", async ({ page }) => {
  test.setTimeout(150_000);await page.clock.setFixedTime(new Date("2026-11-05T06:00:00Z"));await openReview(page);
  let writes=0;page.on("request",r=>{if(r.method()==="POST"&&/\/price\/tickets\//.test(r.url())&&!r.url().includes("/edit-lease/"))writes++;});
  await page.getByRole("link",{name:"Xóa phiếu",exact:true}).click();await page.getByRole("textbox",{name:/^Lý do/}).fill("Không lưu nội dung này");await page.getByRole("link",{name:"Hủy",exact:true}).click();expect(writes).toBe(0);
  // Cancel must be committed before Back can restore its previous history entry.
  // Next correctly discards a still-pending push when a restore preempts it.
  await expect(page).toHaveURL(/\/phieu\/[0-9a-f-]+\?day=2026-11-04&mine=false&tab=tickets/);
  await expect(page.getByRole("heading",{name:"Chi tiết phiếu báo giá",exact:true})).toBeVisible();
  await page.goBack();await expect(page).toHaveURL(/\/xoa\?day=2026-11-04&mine=false&tab=tickets/);await expect(page.getByRole("textbox",{name:/^Lý do/})).toHaveValue("");
  await page.goForward();await expect(page.getByRole("heading",{name:"Chi tiết phiếu báo giá",exact:true})).toBeVisible();expect(writes).toBe(0);
  await page.getByRole("link", { name: "Danh sách kiểm duyệt", exact: true }).click();
  await expect(page).toHaveURL(/kiem-duyet\?day=2026-11-04&mine=false&tab=tickets/);
  await logout(page);
  await expect(page.getByRole("button", { name: "Đăng nhập", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Xác nhận xóa phiếu", exact: true })).toHaveCount(0);
  expect(writes).toBe(0);

});
