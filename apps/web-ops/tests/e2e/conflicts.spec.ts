import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("xung đột đồng bộ: mở từ thông báo, xem hai phiên bản + bản đồ, xử lý giữ phiên bản", async ({ page }) => {
  await loginAs(page, "setter", "/");
  await page.getByTestId("bell").click();
  await page.getByRole("dialog", { name: "Hộp thư thông báo" }).getByRole("button", { name: /Xung đột đồng bộ/ }).click();
  await expect(page).toHaveURL(/\/quan-tri\/xung-dot\?id=/);

  const panel = page.getByTestId("conflict-panel");
  await expect(panel.getByRole("heading", { name: "Ranh giới thửa" })).toBeVisible();
  await expect(panel).toContainText("Quá hạn SLA");
  await expect(panel.getByRole("heading", { name: "Hai phiên bản" })).toBeVisible();
  await expect(panel.getByRole("region", { name: "Xem trước các phiên bản ranh giới" })).toBeVisible();

  const form = panel.getByTestId("resolve-form");
  await form.getByLabel("Giữ một phiên bản").check();
  const keep = form.getByLabel("Phiên bản được giữ");
  await keep.selectOption({ index: 1 });
  await expect(keep.locator("option:checked")).toContainText("Bản hiện hành trên máy chủ");
  await expect(form.getByRole("button", { name: "Đóng xung đột" })).toBeDisabled();
  await form.getByLabel(/Ghi chú xử lý/).fill("Giữ bản hiện hành sau khi đối chiếu với KNV xã");
  await form.getByRole("button", { name: "Đóng xung đột" }).click();
  await expect(panel).toContainText("Đã đóng (giữ phiên bản)");
  await expect(panel.getByTestId("resolve-form")).toHaveCount(0);
});

test("lọc xung đột theo SLA", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/xung-dot");
  await page.getByLabel("SLA").selectOption("due_soon");
  const items = page.getByRole("button", { name: /Sắp hết hạn/ });
  await expect(items.first()).toBeVisible();
  await expect(page.getByRole("button", { name: /Quá hạn SLA/ })).toHaveCount(0);
});
