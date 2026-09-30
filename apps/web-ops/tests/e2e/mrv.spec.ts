import { expect, test } from "@playwright/test";
import { loginAs, loginViaUi, logout, navTo } from "./helpers";

test.describe("MRV (pf/mrv G3): hồ sơ, bất thường FR-08, ước tính tạm, đối chiếu KN US-MRV-02", () => {
  test("KNV xã: đủ hồ sơ theo thửa/vụ, bất thường FR-08, ước tính CO2 dán nhãn TẠM", async ({ page }) => {
    await loginAs(page, "xa", "/");
    await navTo(page, "MRV: hồ sơ & đối chiếu");
    await expect(page.getByRole("heading", { level: 1, name: "MRV — hồ sơ, đối chiếu, ước tính" })).toBeVisible();
    const table = page.getByRole("table", { name: "Tình trạng hồ sơ MRV theo thửa/vụ" });
    await expect(table.locator("tbody tr")).toHaveCount(5);
    await expect(table).toContainText("Minh chứng cán bộ KN");
    await expect(table).toContainText("Chưa (thiếu minh chứng đạt chuẩn)");

    await page.getByRole("tab", { name: "Bất thường (FR-08)" }).click();
    const anomalies = page.getByRole("table", { name: "Vụ không ghi nhận (FR-08)" });
    await expect(anomalies.locator("tbody tr")).toHaveCount(2);
    await expect(anomalies.locator("tbody tr").first()).toContainText(/1\d ngày/);

    await page.getByRole("tab", { name: "Ước tính CO2 (tạm)" }).click();
    await expect(page.getByTestId("estimate-provisional")).toContainText("TẠM — chờ khách ký phương pháp");
    const labels = page.getByTestId("estimate-label");
    await expect(labels.first()).toHaveText("TẠM — chờ khách ký phương pháp");
    await expect(page.getByRole("table", { name: "Ước tính CO2 theo thửa/vụ" })).toContainText("(mặc định, chưa duyệt)");
    // KNV xã không có vai duyệt → không có nút tính lại.
    await expect(page.getByRole("button", { name: "Tính lại" })).toHaveCount(0);
  });

  test("KN US-MRV-02: xác nhận Khác biệt bắt buộc ghi chú; sửa kết quả = bản ghi bù kèm lý do", async ({ page }) => {
    await loginAs(page, "xa", "/");
    await navTo(page, "MRV: hồ sơ & đối chiếu");
    await page.getByRole("link", { name: "Đối chiếu HTX ↔ KN" }).first().click();
    await expect(page.getByRole("heading", { level: 1, name: "Đối chiếu nhật ký HTX ↔ minh chứng KN" })).toBeVisible();
    const row = page.locator("tr[data-log=fertilizing]");
    await expect(row.getByTestId("line-result")).toHaveText("Khác biệt");
    await expect(row).toContainText("Gợi ý tự động: Khác biệt");
    await expect(page.getByTestId("comparison-tally")).toContainText("Cán bộ đã xác nhận 0/3");

    await row.getByRole("button", { name: "Xác nhận" }).click();
    const dlg = page.getByRole("dialog");
    await dlg.getByRole("radio", { name: "Khác biệt" }).check();
    await dlg.getByRole("button", { name: "Ghi kết quả" }).click();
    await expect(dlg).toContainText("Khác biệt bắt buộc ghi chú");
    await dlg.getByRole("textbox", { name: /Ghi chú/ }).fill("HTX ghi bón phân sớm hơn 5 ngày so với biên bản hiện trường");
    await dlg.getByRole("button", { name: "Ghi kết quả" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Đã ghi kết quả “Khác biệt”" })).toBeVisible();
    await expect(row).toContainText("Cán bộ đã xác nhận");
    await expect(page.getByTestId("comparison-tally")).toContainText("Cán bộ đã xác nhận 1/3");

    await row.getByRole("button", { name: "Sửa kết quả" }).click();
    const dlg2 = page.getByRole("dialog");
    await dlg2.getByRole("radio", { name: "Khớp" }).check();
    await dlg2.getByLabel("Lý do sửa (ít nhất 10 ký tự)").fill("ngắn");
    await dlg2.getByRole("button", { name: "Ghi kết quả" }).click();
    await expect(dlg2).toContainText("Lý do sửa tối thiểu 10 ký tự");
    await dlg2.getByLabel("Lý do sửa (ít nhất 10 ký tự)").fill("Đối chiếu lại nhật ký giấy, ngày bón khớp");
    await dlg2.getByRole("button", { name: "Ghi kết quả" }).click();
    await expect(row.getByTestId("line-result")).toHaveText("Khớp");
    await expect(row.locator("ol li")).toHaveCount(2);
    await expect(row).toContainText("sửa: Đối chiếu lại nhật ký giấy");
  });

  test("TTKN tỉnh (duyệt/xem MRV): chỉ xem đối chiếu, tính lại ước tính không ghi thêm dòng", async ({ page }) => {
    await loginAs(page, "tinh", "/mrv");
    await expect(page.getByRole("table", { name: "Tình trạng hồ sơ MRV theo thửa/vụ" }).locator("tbody tr")).toHaveCount(14);
    await page.getByRole("tab", { name: "Ước tính CO2 (tạm)" }).click();
    await page.getByRole("button", { name: "Tính lại" }).first().click();
    await expect(page.getByText("Không đổi đầu vào — giữ ước tính hiện hành.")).toBeVisible();
    await page.getByRole("tab", { name: "Đủ hồ sơ theo thửa/vụ" }).click();
    await page.getByRole("link", { name: "Đối chiếu HTX ↔ KN" }).first().click();
    await expect(page.getByRole("table", { name: "Bảng đối chiếu HTX và KN" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Xác nhận" })).toHaveCount(0);
    await expect(page.getByText("Chỉ cán bộ thu minh chứng (mrv_collector)")).toBeVisible();
  });
});

test("phương pháp ước tính MRV: người đề xuất không tự duyệt (R-56) → người khác phê duyệt", async ({ page }) => {
  await page.goto("/login");
  await loginViaUi(page, /Quản trị nền tảng/);
  await navTo(page, "Phương pháp ước tính MRV");
  await expect(page.getByRole("heading", { level: 1, name: "Phương pháp ước tính MRV" })).toBeVisible();
  const list = page.getByRole("list", { name: "Phiên bản phương pháp" });
  await expect(list.locator("li[data-status=rejected]")).toContainText("Chưa có văn bản khách hàng ký");

  await page.getByRole("button", { name: "Đề xuất và gửi duyệt" }).click();
  await expect(page.getByText("Văn bản phương pháp đã ký: 5–1.000 ký tự.")).toBeVisible();
  await page.getByLabel("Văn bản phương pháp đã ký").fill("Biên bản ký phương pháp E2E số 01/2027");
  await page.getByRole("button", { name: "Đề xuất và gửi duyệt" }).click();
  const mine = list.locator("li[data-version='2']");
  await expect(mine).toContainText("Chờ duyệt");
  await expect(mine).toContainText("Người phê duyệt phải khác người đề xuất (R-56)");
  await expect(mine.getByRole("button", { name: "Phê duyệt" })).toBeDisabled();

  await logout(page);
  await loginViaUi(page, /người phê duyệt/);
  await navTo(page, "Phương pháp ước tính MRV");
  const pending = page.getByRole("list", { name: "Phiên bản phương pháp" }).locator("li[data-version='2']");
  await pending.getByRole("button", { name: "Phê duyệt" }).click();
  await expect(pending).toContainText("Đã duyệt");
  await expect(pending).toContainText("Quản trị viên B (giả lập)");
  await expect(pending.getByRole("button", { name: "Phê duyệt" })).toHaveCount(0);
});
