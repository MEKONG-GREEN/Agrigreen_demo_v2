import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const STATEMENTS = "Bảng kê số cuộn MKG đã gom";

test("RB-6 chiều mua (G3-2): quản lý HTX xem lịch gom và bảng kê số cuộn của HTX mình", async ({ page }) => {
  await loginAs(page, "htx", "/htx/thu-gom");
  const sched = page.getByRole("table", { name: "Lịch gom rơm của MKG" });
  await expect(sched.getByRole("row")).toHaveCount(3);
  await expect(sched).toContainText("Đang gom");
  await expect(sched).toContainText("Dự kiến");
  const table = page.getByRole("table", { name: STATEMENTS });
  await expect(table.getByRole("row")).toHaveCount(5);
  await expect(table.getByRole("row").filter({ hasText: "118 cuộn" })).toContainText("Chờ HTX xác nhận");
  await expect(table.getByRole("row").filter({ hasText: "96 cuộn" })).toContainText("Đã xác nhận");
  await expect(table.getByRole("row").filter({ hasText: "140 cuộn" })).toContainText("MKG đã sửa — cần xác nhận lại");
  await expect(page.getByRole("status").filter({ hasText: "bảng kê" })).toContainText("4 bảng kê · 3 chờ xác nhận");

  await page.getByLabel("Trạng thái").selectOption("confirmed");
  await expect(table.getByRole("row")).toHaveCount(2);
});

test("RB-6 chiều mua (G3-2): xác nhận và tranh chấp (số cuộn + lý do) — kiểm trước khi gửi", async ({ page }) => {
  await loginAs(page, "htx", "/htx/thu-gom");
  const table = page.getByRole("table", { name: STATEMENTS });
  const row = table.getByRole("row").filter({ hasText: "118 cuộn" });
  await row.getByRole("button", { name: /Phản hồi bảng kê lô/ }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Tranh chấp — HTX đếm khác").check();
  await dlg.getByLabel("Số cuộn HTX đếm được").fill("118");
  await dlg.getByLabel("Lý do").fill("Đếm lại tại ruộng thiếu hai cuộn");
  await dlg.getByRole("button", { name: "Gửi tranh chấp" }).click();
  await expect(dlg.getByRole("alert")).toContainText("bằng số MKG");
  await dlg.getByLabel("Số cuộn HTX đếm được").fill("116");
  await dlg.getByRole("button", { name: "Gửi tranh chấp" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByText("Đã gửi tranh chấp")).toBeVisible();
  await expect(row).toContainText("Đang tranh chấp");
  await expect(row).toContainText("HTX đếm 116 cuộn");

  const revised = table.getByRole("row").filter({ hasText: "140 cuộn" });
  await revised.getByRole("button", { name: /Phản hồi bảng kê lô/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Gửi xác nhận" }).click();
  await expect(revised).toContainText("Đã xác nhận");
});

test("G3-2: cán bộ KN không thấy trang thu gom trong menu (chỉ quản lý HTX / quản trị)", async ({ page }) => {
  await loginAs(page, "xa", "/");
  await expect(page.getByRole("link", { name: "Thu gom rơm MKG" })).toHaveCount(0);
});
