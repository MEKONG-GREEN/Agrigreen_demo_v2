import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { coop } from "./htx-data";

test("vô hiệu hoá HTX: hộp thoại hiện số vụ đang mở / nhật ký chờ duyệt (cropcycle stats), lý do ≥ 20 ký tự", async ({ page }) => {
  await loginAs(page, "setter", `/htx/${coop.api.ref}`);
  await page.getByRole("button", { name: "Vô hiệu hoá HTX" }).click();
  const dlg = page.getByRole("dialog", { name: `Vô hiệu hoá ${coop.api.name}` });
  const note = dlg.getByRole("note", { name: "Hoạt động đang dở" });
  await expect(note).toContainText(/\d+ vụ đang mở/);
  await expect(note).toContainText(/\d+ nhật ký chờ duyệt/);
  await dlg.getByLabel(/Lý do vô hiệu hoá/).fill("HTX giải thể");
  await dlg.getByRole("button", { name: "Vô hiệu hoá HTX" }).click();
  await expect(dlg).toContainText("Lý do tối thiểu 20 ký tự");
  await dlg.getByLabel(/Lý do vô hiệu hoá/).fill("HTX giải thể theo nghị quyết đại hội thành viên");
  await dlg.getByRole("button", { name: "Vô hiệu hoá HTX" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(page.getByText("Đã vô hiệu hoá").first()).toBeVisible();
});

test("cán bộ KN không có nút vô hiệu hoá HTX", async ({ page }) => {
  await loginAs(page, "xa", `/htx/${coop.api.ref}`);
  await expect(page.getByRole("heading", { level: 1, name: coop.api.name })).toBeVisible();
  await expect(page.getByRole("button", { name: "Vô hiệu hoá HTX" })).toHaveCount(0);
});
