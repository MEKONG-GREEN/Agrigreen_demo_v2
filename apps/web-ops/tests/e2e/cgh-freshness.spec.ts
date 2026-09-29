import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test.describe("Độ tươi dữ liệu App HTX trên Bản đồ CGH (CGH FN-07)", () => {
  test("CGH FN-07 AC-03: feed quá 48 giờ → banner dự phòng nhập tay; Admin CGH xem nhật ký đồng bộ", async ({ page }) => {
    await loginAs(page, "setter", "/cgh");
    const box = page.getByTestId("area-freshness");
    await expect(box.getByTestId("area-fresh-chip")).toContainText(/Dữ liệu App HTX cập nhật lúc \d{2}:\d{2} \d{2}\/\d{2}\/\d{4}|Dữ liệu App HTX cập nhật lúc .+/);
    await expect(box.getByTestId("area-stale-banner")).toHaveAttribute("role", "alert");
    await expect(box.getByTestId("area-stale-banner")).toContainText("đang dùng dữ liệu nhập tay dự phòng.");
    await box.getByText("Nhật ký đồng bộ").click();
    const log = page.getByRole("table", { name: "Nhật ký đồng bộ diện tích vụ" });
    await expect(log.locator("tbody tr").first()).toBeVisible();
    await expect(log).toContainText("Đã áp");
    await expect(log).toContainText("App HTX");
    await expect(log.locator("tbody tr").first().locator("td").nth(1)).toContainText(/HTX/);
    await page.getByLabel("Nguồn diện tích").selectOption("manual");
    await expect(log.locator("tbody tr").first()).toContainText("Nhập tay");
    expect(await log.locator("tbody tr").evaluateAll((trs) => trs.every((t) => (t.textContent ?? "").includes("Nhập tay")))).toBe(true);
  });

  test("CGH FN-07: Cục (cgh_viewer) thấy banner độ tươi nhưng không có nhật ký đồng bộ", async ({ page }) => {
    await loginAs(page, "cuc", "/cgh");
    await expect(page.getByTestId("area-stale-banner")).toContainText("đang dùng dữ liệu nhập tay dự phòng");
    await expect(page.getByText("Nhật ký đồng bộ")).toHaveCount(0);
  });
});
