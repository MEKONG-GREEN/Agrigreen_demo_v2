import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("HTX US-CAT-01 AC-6: bước SOP phải liên tục; phát hành bản do chính mình soạn bị chặn (R-56)", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/quy-trinh");
  await page.getByRole("button", { name: "Tạo quy trình" }).click();
  const form = page.getByRole("form", { name: "Tạo quy trình SOP" });
  await form.getByLabel("Mã quy trình").fill("SOP-E2E-OM5451");
  await form.getByLabel("Tên quy trình").fill("Quy trình thử nghiệm E2E");
  await form.getByLabel("Thứ tự bước 2").fill("5");
  await form.getByRole("button", { name: "Lưu bản nháp" }).click();
  await expect(form.getByRole("alert")).toContainText("Thứ tự bước không hợp lệ, vui lòng kiểm tra lại.");
  await form.getByRole("button", { name: "Đánh số lại 1..n" }).click();
  await form.getByRole("button", { name: "+ Thêm bước" }).click();
  await form.getByLabel("Tên bước 4").fill("Kiểm tra rầy");
  await form.getByRole("button", { name: "Chuyển bước 4 lên" }).click();
  await expect(form.getByLabel("Tên bước 3")).toHaveValue("Kiểm tra rầy");
  await form.getByRole("button", { name: "Lưu bản nháp" }).click();

  const detail = page.getByRole("region", { name: "Chi tiết quy trình" });
  await expect(detail.getByRole("heading", { name: "Quy trình thử nghiệm E2E" })).toBeVisible();
  await expect(detail).toContainText("Bản nháp");
  await detail.getByRole("button", { name: "Phát hành" }).click();
  await expect(detail.getByRole("alert")).toContainText("Không được tự phê duyệt");
});

test("người phê duyệt khác người soạn phát hành bản nháp", async ({ page }) => {
  await loginAs(page, "approver", "/quan-tri/quy-trinh");
  await page.getByRole("button", { name: /Quy trình OM18 vùng ngập/ }).click();
  const detail = page.getByRole("region", { name: "Chi tiết quy trình" });
  await expect(detail.getByLabel("Tên bước 1")).toHaveAttribute("readonly", ""); // người phê duyệt không sửa bước
  await detail.getByRole("button", { name: "Phát hành" }).click();
  await expect(detail.getByRole("status")).toContainText("Đã phát hành");
  await expect(detail).toContainText("Đã phát hành (bất biến)");
});

test("HTX US-CAT-03: nhân bản giữ nguyên bước; tên trùng bị chặn", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/quy-trinh");
  await page.getByRole("button", { name: /Quy trình lúa chất lượng cao/ }).click();
  const detail = page.getByRole("region", { name: "Chi tiết quy trình" });
  await detail.getByRole("button", { name: "Nhân bản" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByLabel("Tên quy trình mới").fill("Quy trình lúa thơm ST25");
  await dlg.getByRole("button", { name: "Nhân bản", exact: true }).click();
  await expect(dlg.getByRole("alert")).toContainText("Tên SOP này đã tồn tại, vui lòng chọn tên khác.");
  await dlg.getByLabel("Tên quy trình mới").fill("Quy trình CLC cho OM18");
  await dlg.getByRole("button", { name: "Nhân bản", exact: true }).click();
  await expect(detail.getByRole("heading", { name: "Quy trình CLC cho OM18" })).toBeVisible();
  await expect(detail.getByLabel("Tên bước 8")).toHaveValue("Thu hoạch bằng máy gặt đập liên hợp");
  await expect(detail).toContainText("Bản nháp");
});

test("gắn giống lúa ↔ quy trình SOP hệ thống đã ban hành", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/quy-trinh");
  const sel = page.getByLabel("Quy trình cho giống OM5451");
  await expect(sel).toHaveValue(/.+/);
  await sel.selectOption({ label: "Quy trình lúa thơm ST25" });
  await expect(page.getByRole("region", { name: "Gắn giống lúa ↔ quy trình SOP" }).getByRole("status")).toContainText("Đã gắn giống OM5451");
});
