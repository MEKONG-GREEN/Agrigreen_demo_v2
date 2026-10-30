import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

const proposalRoute = /\/kn\/bao-gia\/de-xuat-mat-hang$/;
async function login(page: Page, identifier: string) {
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026"); // Credential for the documented mock personas only.
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}
async function enterXa(page: Page) {
  await login(page, "an.nv@angiang.gov.vn");
  await page.getByRole("button", { name: /^Cấp Xã/ }).click();
  await page.getByRole("button", { name: "Phòng Thông tin Tuyên truyền — Vào hệ thống", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Đề xuất mặt hàng", exact: true }).click();
  await expect(page).toHaveURL(proposalRoute);
}
async function propose(page: Page, name: string) {
  await page.getByRole("button", { name: "Đề xuất mặt hàng", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Đề xuất mặt hàng mới", exact: true });
  await dialog.getByLabel("Mã xã báo giá", { exact: true }).fill("91301");
  await dialog.getByLabel("Tên loại", { exact: true }).fill(name);
  await dialog.getByLabel("Giống / phẩm cấp", { exact: true }).fill("loại 1");
  await dialog.getByLabel("Nhóm hàng", { exact: true }).selectOption("rau_mau");
  await dialog.getByLabel("Lý do", { exact: true }).fill("Có tại chợ xã, cần theo dõi giá");
  await dialog.getByRole("button", { name: "Gửi đề xuất", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("table", { name: "Đề xuất mặt hàng từ xã" })).toContainText(name);
}

test("BG-ITEM-PROPOSAL: menu Tỉnh/Xã → đề xuất → Admin duyệt/từ chối → lịch sử riêng của Xã", async ({ page }) => {
  test.setTimeout(180_000);
  await page.goto("/login");
  await login(page, "admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await sidebar(page).getByRole("link", { name: "Menu các cấp", exact: true }).click();
  await page.getByLabel("Xem như vai trò*").selectOption("kn_tinh");
  await page.getByLabel(/^Lý do hỗ trợ/).fill("Kiểm chứng đường vào đề xuất mặt hàng");
  await page.getByRole("button", { name: "Vào menu Cấp Tỉnh", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Đề xuất mặt hàng", exact: true }).click();
  await expect(page).toHaveURL(proposalRoute);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Đề xuất mặt hàng");
  await expect(page.getByRole("table", { name: "Đề xuất mặt hàng từ xã" })).toBeVisible();
  await page.getByRole("button", { name: "Thoát chế độ hỗ trợ", exact: true }).click();
  await logout(page);
  await enterXa(page);
  await expect(page.getByRole("tab")).toHaveCount(1);
  await expect(page.getByRole("button", { name: "Duyệt", exact: true })).toHaveCount(0);
  const history = page.getByRole("table", { name: "Đề xuất mặt hàng từ xã" });
  await expect(history).not.toContainText("Sầu riêng"); // Another proposer's seed stays private.
  await propose(page, "Mít kiểm chứng 37");
  await propose(page, "Bưởi kiểm chứng 37");
  await expect(history.getByRole("row")).toHaveCount(3); // Header + own two proposals.
  await logout(page);
  await login(page, "admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("button", { name: "Phòng Thông tin Tuyên truyền", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Mặt hàng", exact: true }).click();
  await expect(page).toHaveURL(/\/kn\/bao-gia\/mat-hang$/);
  await page.getByRole("tab", { name: /Đề xuất từ xã/ }).click();
  const proposals = page.getByRole("table", { name: "Đề xuất mặt hàng từ xã" });
  await proposals.getByRole("row").filter({ hasText: "Mít kiểm chứng 37" }).getByRole("button", { name: "Duyệt", exact: true }).click();
  const approve = page.getByRole("dialog", { name: "Duyệt đề xuất của xã 91301", exact: true });
  await expect(approve.getByLabel("Tên loại", { exact: true })).toHaveValue("Mít kiểm chứng 37");
  await approve.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(approve).toHaveCount(0);
  await proposals.getByRole("row").filter({ hasText: "Bưởi kiểm chứng 37" }).getByRole("button", { name: "Từ chối", exact: true }).click();
  const reject = page.getByRole("dialog", { name: /Từ chối đề xuất/ });
  await expect(reject.getByRole("button", { name: "Từ chối", exact: true })).toBeDisabled();
  await reject.getByLabel("Lý do từ chối", { exact: true }).fill("Đã có mặt hàng tương đương trong danh mục");
  await reject.getByRole("button", { name: "Từ chối", exact: true }).click();
  await expect(reject).toHaveCount(0);
  await logout(page);
  await enterXa(page);
  await expect(history.getByRole("row")).toHaveCount(3);
  const approved = history.getByRole("row").filter({ hasText: "Mít kiểm chứng 37" });
  await expect(approved).toContainText("Đã duyệt");
  await expect(approved).toContainText("Mặt hàng MH");
  await expect(history.getByRole("row").filter({ hasText: "Bưởi kiểm chứng 37" })).toContainText("Đã có mặt hàng tương đương trong danh mục");
  await expect(history).not.toContainText("Sầu riêng");
  await expect(page.getByRole("button", { name: "Duyệt", exact: true })).toHaveCount(0);
});
