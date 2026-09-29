import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test.describe("Giám sát đồng bộ (GIS US-SYNC-01/02)", () => {
  test("GIS US-SYNC-01 AC-1 / US-SYNC-02 AC-1: bảng theo nguồn, trạng thái, DLQ, cảnh báo hàng chờ lỗi, lọc", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/giam-sat-dong-bo");
    await expect(page.getByRole("heading", { level: 1, name: "Giám sát đồng bộ" })).toBeVisible();
    const table = page.getByRole("table", { name: "Tình trạng đồng bộ theo nguồn" });
    await expect(table.locator("tbody tr")).toHaveCount(4);
    const area = table.locator('tr[data-consumer="pf.mechanization.area_sync"]');
    await expect(area).toContainText("App HTX → Bản đồ CGH (diện tích vụ)");
    await expect(area.locator("[data-status]")).toHaveText("Lỗi");
    await expect(area.getByTestId("dlq-depth")).toHaveText("1");
    await expect(table.locator('tr[data-consumer="pf.geo.hub_valve"] [data-status]')).toHaveText("Bình thường");
    await expect(table.locator('tr[data-consumer="pf.geo.tms_export"] [data-status]')).toHaveText("Chưa có dữ liệu");
    await expect(table.locator('tr[data-consumer="pf.geo.tms_export"]').getByTestId("dlq-depth")).toHaveText("không đo được");
    await expect(page.getByTestId("dlq-banner")).toContainText("Có sự kiện trong hàng chờ lỗi — xem runbook integration-alerts");

    await page.getByLabel("Trạng thái", { exact: true }).selectOption("error");
    await expect(table.locator("tbody tr")).toHaveCount(1);
    await expect(table).toContainText("App HTX → Bản đồ CGH");
    await page.getByLabel("Trạng thái", { exact: true }).selectOption("");
    await page.getByLabel("Nguồn", { exact: true }).selectOption("pf.geo.hub_valve");
    await expect(table.locator("tbody tr")).toHaveCount(1);
    await expect(table).toContainText("ERP/MKG → GIS (van công bố kho/Hub)");
    await page.getByLabel("Khoảng thời gian").selectOption("168");
    await expect(table.locator("tbody tr")).toHaveCount(1);
  });

  test("GIS US-SYNC-01 AC-2: nhật ký đồng bộ lọc theo nguồn/kết quả/khoảng ngày, Tải thêm", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/giam-sat-dong-bo");
    const log = page.getByRole("table", { name: "Nhật ký đồng bộ" });
    await expect(log.locator("tbody tr")).toHaveCount(25);
    await page.getByRole("button", { name: "Tải thêm" }).click();
    await expect(log.locator("tbody tr")).toHaveCount(50);

    await page.getByLabel("Kết quả").selectOption("error");
    await expect(log.locator("tbody tr").first()).toBeVisible();
    const rows = log.locator("tbody tr");
    await expect(rows).not.toHaveCount(0);
    expect(await rows.evaluateAll((trs) => trs.every((t) => t.getAttribute("data-status") === "error"))).toBe(true);
    await expect(log).toContainText("Đã vào hàng chờ lỗi");
    await expect(log).toContainText("mechanization.cooperative_invalid");

    await page.getByLabel("Kết quả").selectOption("");
    await page.getByLabel("Nguồn (nhật ký)").selectOption("pf.geo.hub_valve");
    await expect(log.locator("tbody tr").first()).toContainText("ERP/MKG → GIS");
    expect(await log.locator("tbody tr").evaluateAll((trs) => trs.every((t) => (t.textContent ?? "").includes("ERP/MKG → GIS")))).toBe(true);

    await page.getByLabel("Từ ngày").fill("2030-01-02");
    await page.getByLabel("Đến ngày").fill("2030-01-01");
    await expect(page.getByText("Từ ngày phải trước hoặc bằng đến ngày.")).toBeVisible();
  });

  test("GIS US-SYNC-01: vai không phải platform_admin/device_admin không thấy màn giám sát", async ({ page }) => {
    await loginAs(page, "approver", "/quan-tri/tham-so");
    await expect(page.getByRole("link", { name: "Giám sát đồng bộ" })).toHaveCount(0);
  });
});
