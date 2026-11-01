import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";
test("FN07 phần đầu: mở menu thật, xem cấu trúc và kiểm CSV không lưu", async ({ page }) => {
  const login = async () => { await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn"); await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026"); await page.getByRole("button", { name: "Đăng nhập", exact: true }).click(); await expect(sidebar(page)).toBeVisible(); };
  await page.goto("/login"); await login();
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click(); await page.getByLabel("Trạng thái Phòng Chăn nuôi - Thủy sản", { exact: true }).selectOption("active"); await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toContainText("Đang áp dụng");
  await logout(page); await login(); await sidebar(page).getByRole("button", { name: "Phòng Chăn nuôi - Thủy sản", exact: true }).click(); await sidebar(page).getByRole("link", { name: "Thiết kế biểu mẫu", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Thiết kế biểu mẫu" })).toBeVisible(); await page.getByLabel("Biểu kỳ", { exact: true }).selectOption("CNTS-BM04"); await expect(page.getByText("Chưa cài — cấu trúc chuẩn tham khảo")).toBeVisible();
  await page.getByRole("button", { name: "Xem trước phiên bản" }).click(); await expect(page.getByRole("region", { name: "Xem trước biểu" })).toBeVisible();
  const file = page.getByLabel("Tệp cấu trúc", { exact: true }); const button = page.getByRole("button", { name: "Kiểm tra và xem trước tệp" });
  const header = "key,label,type,required,unit,group,non_negative,options\n";
  await file.setInputFiles({ name: "good.csv", mimeType: "text/csv", buffer: Buffer.from(header + "so_con,Số con,integer,true,con,Đàn,true,\n") }); await button.click(); await expect(page.getByRole("status").filter({ hasText: "Cấu trúc hợp lệ" })).toContainText("Chưa lưu phiên bản");
  await file.setInputFiles({ name: "bad.csv", mimeType: "text/csv", buffer: Buffer.from(header + "so_con,Số con,integer,1e100000,con,Đàn,true,\n") }); await button.click(); await expect(page.getByText(/Dòng 2 · required/)).toBeVisible(); await expect(page.getByRole("status").filter({ hasText: "Tệp có" })).toContainText("Không có thay đổi nào được lưu");
  await expect(page.getByRole("button", { name: "Lưu phiên bản" })).toHaveCount(0); await expect(page.getByText("Chưa cài — cấu trúc chuẩn tham khảo")).toBeVisible();
});
