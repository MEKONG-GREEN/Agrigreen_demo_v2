import { expect, test } from "@playwright/test";
import { loginAs, sidebar } from "./helpers";

test.describe("Mô phỏng chọn Hub (CGH FN-02/FN-11/FN-14, mục 12)", () => {
  test("kết quả: chi phí/tấn, TCO, ROI, hoàn vốn, dòng vật liệu, No-Hub; Chính thức khi mọi tham số đã duyệt", async ({ page }) => {
    await loginAs(page, "setter", "/mo-phong");
    await page.getByRole("button", { name: /KB-0001/ }).click();
    await page.getByRole("button", { name: "Xem kết quả" }).first().click();
    const r = page.getByTestId("sim-result");
    for (const k of ["Chi phí logistics / tấn", "TCO (vòng đời)", "ROI", "Thời gian hoàn vốn", "Sản lượng giao nhà máy"]) await expect(r.getByRole("group", { name: k })).toBeVisible();
    await expect(r.getByRole("group", { name: "Chi phí logistics / tấn" })).toContainText(/so với No-Hub/);
    await expect(r.getByLabel("Dòng vật liệu")).toContainText("Hub → nhà máy");
    await expect(r.getByTestId("baseline")).toContainText("No-Hub Baseline");
    const official = r.getByRole("button", { name: "Chính thức" });
    await expect(official).toBeEnabled();
    await official.click();
    await expect(page.getByText("Đã đánh dấu Chính thức.")).toBeVisible();
  });

  test("tham số chưa duyệt: nút Chính thức bị vô hiệu kèm giải thích (simulation.params_unapproved)", async ({ page }) => {
    await loginAs(page, "setter", "/mo-phong");
    await page.getByRole("button", { name: /KB-0002/ }).click();
    await page.getByRole("button", { name: "Xem kết quả" }).first().click();
    const r = page.getByTestId("sim-result");
    await expect(r.getByRole("button", { name: "Chính thức" })).toBeDisabled();
    await expect(r.getByRole("note")).toContainText("Chưa đánh dấu Chính thức được");
    await expect(r.getByRole("note")).toContainText("simulation.params_unapproved");
    await expect(r.getByRole("list", { name: "Tham số chưa phê duyệt" })).toContainText("#31");
  });

  test("gán/gỡ Hub (gỡ cần lý do), chạy mô phỏng 202 → theo dõi việc → kết quả", async ({ page }) => {
    await loginAs(page, "setter", "/mo-phong");
    await page.getByRole("button", { name: /KB-0003/ }).click();
    await expect(page.getByRole("button", { name: "Chạy mô phỏng" })).toBeDisabled();
    await page.getByLabel("Hub ứng viên").selectOption({ label: "HUB-0002 · Hub Cao Lãnh" });
    await page.getByRole("button", { name: "Gán / cập nhật Hub" }).click();
    await expect(page.getByText("Đã gán Hub vào kịch bản.")).toBeVisible();
    await page.getByLabel("Hub ứng viên").selectOption({ label: "HUB-0003 · Hub Thốt Nốt" });
    await page.getByRole("button", { name: "Gán / cập nhật Hub" }).click();
    await expect(page.getByRole("table").getByText(/HUB-0003/)).toBeVisible();
    await page.getByRole("row", { name: /HUB-0003/ }).getByRole("button", { name: "Gỡ Hub" }).click();
    const confirm = page.getByRole("button", { name: "Xác nhận gỡ Hub" });
    await expect(confirm).toBeDisabled();
    await page.getByLabel(/Lý do gỡ/).fill("Trùng vùng phục vụ với Hub Cao Lãnh");
    await confirm.click();
    await expect(page.getByText("Đã gỡ Hub (bản ghi bù kèm lý do).")).toBeVisible();
    await expect(page.getByRole("table").getByText(/HUB-0003/)).toHaveCount(0);
    await page.getByRole("button", { name: "Chạy mô phỏng" }).click();
    await expect(page.getByTestId("job-status")).toContainText(/Đang chờ|Đang chạy|Hoàn tất/);
    await expect(page.getByTestId("job-status")).toContainText("Hoàn tất", { timeout: 20_000 });
    await expect(page.getByTestId("sim-result").getByRole("group", { name: "TCO / tấn" })).toBeVisible();
  });

  test("tạo Hub ứng viên, ngừng dùng cần lý do; so sánh kịch bản có cột No-Hub", async ({ page }) => {
    await loginAs(page, "setter", "/mo-phong");
    await page.getByRole("tab", { name: "Hub ứng viên" }).click();
    await page.getByLabel("Tên Hub").fill("Hub Sa Đéc");
    await page.getByLabel("Vĩ độ").fill("10.29");
    await page.getByLabel("Kinh độ").fill("105.76");
    await page.getByRole("button", { name: "Tạo Hub" }).click();
    await expect(page.getByRole("row", { name: /Hub Sa Đéc/ })).toContainText("Đang dùng");
    await page.getByRole("row", { name: /Hub Sa Đéc/ }).getByRole("button", { name: "Ngừng dùng" }).click();
    await page.getByLabel("Lý do ngừng dùng Hub (ít nhất 10 ký tự)").fill("Không thuê được mặt bằng ven sông");
    await page.getByRole("button", { name: "Xác nhận ngừng dùng" }).click();
    await expect(page.getByRole("row", { name: /Hub Sa Đéc/ })).toContainText("Ngừng dùng");

    await page.getByRole("tab", { name: "So sánh kịch bản" }).click();
    const boxes = page.getByRole("group", { name: "Lượt mô phỏng đã có kết quả" }).getByRole("checkbox");
    await expect(boxes).toHaveCount(2);
    await boxes.nth(0).check();
    await boxes.nth(1).check();
    await page.getByRole("button", { name: "So sánh" }).click();
    const t = page.getByTestId("compare-table");
    await expect(t.locator("tbody tr")).toHaveCount(3);
    await expect(t.locator('tr[data-baseline="true"]')).toContainText("No-Hub");
  });

  test("KN tỉnh không thấy mô phỏng", async ({ page }) => {
    await loginAs(page, "tinh");
    await expect(sidebar(page).getByRole("link", { name: "Mô phỏng chọn Hub" })).toHaveCount(0);
  });
});
