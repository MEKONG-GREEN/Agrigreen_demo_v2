import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { plot } from "./htx-data";

const growing = plot(1);
const growing2 = plot(2);
const fresh = plot(4); // chưa mở vụ

test("HTX US-LOG-05 AC-1: nhãn “Vị trí không khớp”, “Thiếu GPS” trong danh sách nhật ký", async ({ page }) => {
  await loginAs(page, "htx", "/htx/nhat-ky");
  const table = page.getByRole("table", { name: "Nhật ký canh tác" });
  await page.getByLabel("Vị trí").selectOption("mismatch");
  await expect(table.getByRole("row").nth(1)).toContainText("Vị trí không khớp");
  await expect(table.getByText("Thiếu GPS")).toHaveCount(0);
  await page.getByLabel("Vị trí").selectOption("missing");
  await expect(table.getByRole("row").nth(1)).toContainText("Thiếu GPS");
});

test("HTX US-LOG-02: ghi hộ nông dân — chặn lô chưa mở vụ, ghi được lô đang canh tác", async ({ page }) => {
  await loginAs(page, "htx", "/htx/nhat-ky");
  await page.getByRole("button", { name: "Ghi hộ nông dân" }).click();
  const form = page.getByRole("form", { name: "Ghi nhật ký hộ nông dân" });
  await form.getByLabel("Lô ruộng").selectOption(fresh.ref);
  await form.getByRole("button", { name: "Ghi nhật ký hộ" }).click();
  await expect(form.getByRole("alert")).toContainText("Lô ruộng này chưa được mở vụ. Vui lòng mở vụ trước khi ghi nhật ký.");

  await form.getByLabel("Lô ruộng").selectOption(growing.ref);
  await form.getByLabel("Hoạt động").selectOption("phun_thuoc");
  await form.getByLabel("Vật tư").fill("Thuốc đặc trị đạo ôn");
  await form.getByLabel("Lượng").fill("0,5");
  await form.getByLabel("Đơn vị").fill("lít");
  await form.getByRole("checkbox", { name: /Máy phun thuốc/ }).check();
  await form.getByRole("button", { name: "Ghi nhật ký hộ" }).click();
  await expect(form.getByRole("status")).toContainText("Đã ghi nhật ký hộ — trạng thái Chờ duyệt.");
  const row = page.getByRole("table", { name: "Nhật ký canh tác" }).getByRole("row").filter({ hasText: "Phun thuốc" }).filter({ hasText: growing.code }).first();
  await expect(row).toContainText("Ghi hộ");
  await expect(row).toContainText("Thiếu GPS");
  await expect(row).toContainText("Chờ duyệt");
});

test("HTX US-LOG-03: ghi hàng loạt — một lượt, lô chưa mở vụ bị loại kèm lý do", async ({ page }) => {
  await loginAs(page, "htx", "/htx/nhat-ky");
  await page.getByRole("button", { name: "Ghi hàng loạt" }).click();
  const form = page.getByRole("form", { name: "Ghi nhật ký hàng loạt" });
  for (const p of [growing, growing2, fresh]) await form.getByRole("checkbox", { name: new RegExp(p.code) }).check();
  await form.getByLabel("Hoạt động").selectOption("tuoi_nuoc");
  await form.getByRole("button", { name: "Ghi cho 3 lô" }).click();
  await expect(form.getByRole("status")).toContainText("Đã ghi 2/3 lô trong một lượt");
  await expect(form.getByRole("table", { name: "Lô bị loại khỏi lượt ghi hàng loạt" }).getByRole("row").filter({ hasText: fresh.code })).toContainText("Lô ruộng chưa được mở vụ");
});

test("sửa nhật ký bằng phiên bản mới kèm lý do; thời điểm và GPS giữ nguyên", async ({ page }) => {
  await loginAs(page, "htx", "/htx/nhat-ky");
  await page.getByLabel("Trạng thái duyệt").selectOption("pending");
  const table = page.getByRole("table", { name: "Nhật ký canh tác" });
  await table.getByRole("button", { name: new RegExp(`Sửa nhật ký lô ${growing2.code}`) }).first().click();
  const dlg = page.getByRole("dialog", { name: "Sửa nhật ký (bản ghi bù)" });
  await expect(dlg).toContainText("không sửa được sau khi lưu");
  await dlg.getByLabel("Ghi chú (không ghi SĐT/CCCD)").fill("Bón thúc đợt 1 — sửa lượng phân");
  await dlg.getByLabel("Lý do sửa (ít nhất 10 ký tự)").fill("Ghi nhầm lượng phân bón");
  await dlg.getByRole("button", { name: "Lưu phiên bản mới" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(table.getByRole("row").filter({ hasText: growing2.code }).filter({ hasText: "Phiên bản 2" })).toHaveCount(1);
});

test("cán bộ KN chỉ xem nhật ký, không có nút ghi hộ", async ({ page }) => {
  await loginAs(page, "xa", "/htx/nhat-ky");
  await expect(page.getByRole("table", { name: "Nhật ký canh tác" }).getByRole("row").nth(1)).toBeVisible();
  await expect(page.getByRole("button", { name: "Ghi hộ nông dân" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Sửa nhật ký/ })).toHaveCount(0);
});
