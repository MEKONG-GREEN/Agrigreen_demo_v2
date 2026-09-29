import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { plot } from "./htx-data";

const done = plot(0); // vụ trước đã hoàn thành
const growing = plot(1); // đang canh tác vụ Hè Thu
const fresh = plot(4); // chưa có vụ nào
const done2 = plot(5); // vụ trước đã hoàn thành (giống OM18)

test("HTX US-SEASON-02/04: mở vụ hàng loạt cho nhiều lô — lô đang canh tác bị bỏ qua kèm lý do", async ({ page }) => {
  await loginAs(page, "htx", "/htx/mua-vu");
  await expect(page.getByRole("heading", { level: 1, name: "Mùa vụ" })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: growing.code }).first()).toContainText("Đang canh tác");
  await page.getByRole("button", { name: "Mở vụ cho nông hộ" }).click();
  const form = page.getByRole("form", { name: "Mở vụ cho nông hộ" });
  await form.getByRole("checkbox", { name: new RegExp(done.code) }).check();
  await form.getByRole("checkbox", { name: new RegExp(growing.code) }).check();
  await form.getByLabel("Giống lúa", { exact: true }).selectOption("OM5451");
  await form.getByRole("button", { name: "Mở vụ cho 2 lô" }).click();
  await expect(form.getByRole("status")).toContainText("1 lô đã mở vụ · 1 lô bỏ qua");
  const results = form.getByRole("table", { name: "Kết quả mở vụ theo lô" });
  await expect(results.getByRole("row").filter({ hasText: growing.code })).toContainText("Lô đang canh tác vụ khác");
  await expect(results.getByRole("row").filter({ hasText: done.code })).toContainText("Đã mở vụ");
});

test("HTX US-SEASON-01 AC-2: ngày sạ ngoài khoảng cho phép bị chặn ngay trên form", async ({ page }) => {
  await loginAs(page, "htx", "/htx/mua-vu");
  await page.getByRole("button", { name: "Mở vụ cho nông hộ" }).click();
  const form = page.getByRole("form", { name: "Mở vụ cho nông hộ" });
  await form.getByRole("checkbox", { name: new RegExp(fresh.code) }).check();
  await form.getByLabel("Giống lúa", { exact: true }).selectOption("OM18");
  await form.getByLabel("Ngày gieo sạ").fill("2020-01-01");
  await form.getByRole("button", { name: "Mở vụ cho 1 lô" }).click();
  await expect(form).toContainText("Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại.");
});

test("HTX US-SEASON-03: sao chép cấu hình vụ trước — lô chưa có vụ hoàn thành bị bỏ qua", async ({ page }) => {
  await loginAs(page, "htx", "/htx/mua-vu");
  await page.getByRole("button", { name: "Mở vụ cho nông hộ" }).click();
  const form = page.getByRole("form", { name: "Mở vụ cho nông hộ" });
  await form.getByRole("checkbox", { name: new RegExp(done2.code) }).check();
  await form.getByRole("button", { name: `Xem cấu hình vụ trước của lô ${done2.code}` }).click();
  await expect(form).toContainText("Vụ DX 2026 · giống OM18");
  await form.getByRole("checkbox", { name: new RegExp(fresh.code) }).check();
  await form.getByRole("checkbox", { name: /Sao chép cấu hình vụ trước/ }).check();
  await expect(form.getByLabel("Giống lúa", { exact: true })).toHaveCount(0);
  await form.getByRole("button", { name: "Mở vụ cho 2 lô" }).click();
  await expect(form.getByRole("status")).toContainText("1 lô đã mở vụ (sao chép cấu hình vụ trước) · 1 lô bỏ qua");
  await expect(form.getByRole("row").filter({ hasText: fresh.code })).toContainText("chưa có vụ nào đã hoàn thành");
  // Vụ mới mang giống của vụ trước.
  await expect(page.getByRole("table", { name: "Danh sách vụ" }).getByRole("row").filter({ hasText: done2.code }).first()).toContainText("OM18");
});

test("hủy vụ mở nhầm cần lý do ≥ 10 ký tự (bản ghi bù, RB-1)", async ({ page }) => {
  await loginAs(page, "htx", "/htx/mua-vu");
  const row = page.getByRole("table", { name: "Danh sách vụ" }).getByRole("row").filter({ hasText: growing.code });
  await row.getByRole("button", { name: `Hủy vụ lô ${growing.code}` }).click();
  const dlg = page.getByRole("dialog", { name: `Hủy vụ lô ${growing.code}` });
  await dlg.getByLabel(/Lý do hủy/).fill("nhầm");
  await dlg.getByRole("button", { name: "Hủy vụ" }).click();
  await expect(dlg).toContainText("Lý do tối thiểu 10 ký tự");
  await dlg.getByLabel(/Lý do hủy/).fill("Mở nhầm lô của hộ bên cạnh");
  await dlg.getByRole("button", { name: "Hủy vụ" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(row).toHaveCount(0); // bộ lọc mặc định: đang canh tác
  await page.getByLabel("Trạng thái").selectOption("cancelled");
  const cancelled = page.getByRole("table", { name: "Danh sách vụ" }).getByRole("row").filter({ hasText: growing.code });
  await expect(cancelled).toContainText("Đã hủy");
  await cancelled.getByRole("button", { name: `Lịch sử vụ lô ${growing.code}` }).click();
  await expect(page.getByRole("dialog")).toContainText("Mở nhầm lô của hộ bên cạnh");
});

test("cán bộ KN xã chỉ xem vụ của HTX trong xã, không có nút ghi", async ({ page }) => {
  await loginAs(page, "xa", "/htx/mua-vu");
  await expect(page.getByRole("note")).toContainText("chỉ xem");
  await expect(page.getByRole("table", { name: "Danh sách vụ" }).getByRole("row").filter({ hasText: growing.code }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Mở vụ cho nông hộ" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Sửa vụ/ })).toHaveCount(0);
});
