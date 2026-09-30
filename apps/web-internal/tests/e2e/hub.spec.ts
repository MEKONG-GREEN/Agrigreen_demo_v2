import { expect, test } from "@playwright/test";
import { alert, IDS, loginAs, navTo, switchUser } from "./helpers";

test.describe("Hub & kho", () => {
  test("tồn theo lô xếp theo rủi ro xuống cấp, chi tiết lô và bù sai", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/ton-kho");
    const rows = page.getByRole("table", { name: "Lô theo rủi ro" }).getByRole("row");
    await expect(rows.nth(1)).toContainText("LO-TB-0901"); // 29 ngày, rủi ro cao nhất
    await expect(page.getByText("hub.risk.v2#1").first()).toBeVisible();
    await page.getByRole("button", { name: "Chi tiết lô LO-TB-0927" }).click();
    const moves = page.getByRole("table", { name: "Dịch chuyển của lô" });
    await expect(moves).toContainText("84.200");
    await moves.getByRole("button", { name: "Bù sai" }).nth(1).click();
    await page.getByLabel("Lý do bù sai").fill("Nhập nhầm số phiếu cân của ghe khác");
    await page.getByRole("button", { name: "Ghi dòng bù" }).click();
    await expect(page.getByText("Đã ghi dòng bù ngược dấu")).toBeVisible();
  });

  test("xuất theo đề xuất rủi ro phân bổ đủ theo lô; thiếu tồn thì chặn", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/xuat-hang");
    await page.getByLabel("Khối lượng xuất (kg)").fill("20000");
    await page.getByRole("button", { name: "Xem đề xuất theo rủi ro" }).click();
    const t = page.getByRole("table", { name: "Phân bổ đề xuất" });
    await expect(t.getByRole("row").nth(1)).toContainText("12.000 kg");
    await page.getByRole("button", { name: "Xuất theo đề xuất rủi ro" }).click();
    await expect(page.getByText(/Đã xuất 20\.000 kg theo hub\.risk\.v2#1/)).toBeVisible();

    await page.getByLabel("Khối lượng xuất (kg)").fill("900000");
    await page.getByRole("button", { name: "Xem đề xuất theo rủi ro" }).click();
    await expect(page.getByText(/Thiếu .* so với tồn theo lô/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Xuất theo đề xuất rủi ro" })).toBeDisabled();
  });

  test("nhận hàng theo phiếu cân HUB_IN (Q-MKG-04)", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/nhan-hang");
    await page.getByRole("button", { name: "LG-TH-0929 (ghe đã cân, chờ nhập hub)" }).click();
    await expect(page.getByRole("status", { name: "Phiếu cân HUB_IN" })).toContainText("86.260 kg");
    await page.getByLabel("Mã nguồn 1").fill(IDS.htxTanHiep);
    await page.getByRole("button", { name: "Ghi nhận lô" }).click();
    await expect(page.getByText(/Đã nhận lô .* 86\.260 kg/)).toBeVisible();
    // Phiếu đã dùng → không nhận lần hai.
    await page.getByRole("button", { name: "Ghi nhận lô" }).click();
    await expect(alert(page)).toContainText("hub.weigh_ticket_used");
  });

  test("hàng sắp đến (FM-05): chuyến xuất phát tới hub → thông báo Chờ đến mang mã chuyến; lọc theo cơ sở / trạng thái", async ({ page }) => {
    await loginAs(page, "van-tai", "/van-tai/chuyen");
    await page.getByRole("button", { name: "Mở chuyến CH000101" }).click();
    await page.getByRole("button", { name: "Lập chặng", exact: true }).click();
    await page.getByLabel("Điểm đến chặng 1").selectOption("hub-tb");
    await page.getByRole("button", { name: "Lưu chặng" }).click();
    await expect(page.getByText("Đã lập chặng — lưu kết quả định tuyến nguyên văn + SHA-256")).toBeVisible();
    await page.getByRole("button", { name: "Ghi mốc xuất phát" }).click();
    await expect(page.getByText("Đã ghi mốc xuất phát")).toBeVisible();

    await switchUser(page, "dieu-phoi");
    await navTo(page, "Nhận hàng");
    const t = page.getByRole("table", { name: "Thông báo hàng đến" });
    await expect(t.getByRole("row", { name: /CH000101/ })).toContainText("Chờ đến");
    await expect(t.getByRole("row", { name: /CH000101/ })).toContainText("HUB-TB");
    await expect(t.getByRole("row", { name: /CH000101/ })).toContainText("88,0 t");
    await expect(t).toContainText("CH000102");
    await page.getByLabel("Cơ sở đến").selectOption({ label: "HUB-TB · Hub Thới Bình" });
    await expect(t.getByRole("row", { name: /CH000102/ })).toHaveCount(0);
    await expect(t).toContainText("CH000101");
    await page.getByLabel("Cơ sở đến").selectOption("");
    await page.getByLabel("Trạng thái hàng đến").selectOption("arrived");
    await expect(t.getByRole("row", { name: /CH000105/ })).toContainText("Đã đến");
    await expect(t.getByRole("row", { name: /CH000101/ })).toHaveCount(0);
  });

  test("chuyển kho: nhận ở đích, hao hụt đường = xuất − nhận", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/chuyen-kho");
    await page.getByRole("button", { name: "Thới Bình → Cần Thơ, 20 t" }).click();
    await expect(page.getByText("Đang đi đường")).toBeVisible();
    await page.getByLabel("Khối lượng nhận (kg)").fill("19850");
    await page.getByRole("button", { name: "Xác nhận nhận hàng" }).click();
    await expect(page.getByText(/hao hụt đường 150 kg/)).toBeVisible();
  });

  test("hao hụt theo cơ sở và van công bố", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/hao-hut");
    const t = page.getByRole("table", { name: "Hao hụt theo cơ sở" });
    await expect(t).toContainText("HUB-TB");
    await expect(t).toContainText("KHO-CT");
    await navTo(page, "Van công bố");
    await expect(page.getByRole("table", { name: "Bản ghi van" })).toContainText("hub_capacity v1");
    await page.getByRole("tab", { name: /Lệnh gom/ }).click();
    await expect(page.getByRole("table", { name: "Bản ghi van" })).toContainText("collection_schedule");
  });
});
