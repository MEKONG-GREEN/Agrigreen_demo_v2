import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test.describe("Lịch sử chỉ số đã chốt (pf/metrics — CGH FN-10/11)", () => {
  test("Cổng CGH: xu hướng theo vụ — chọn vụ đã ghi", async ({ page }) => {
    await loginAs(page, "cuc", "/cgh");
    const sec = page.getByTestId("metric-history");
    await expect(sec.getByRole("heading", { name: /Xu hướng cân đối máy theo vụ/ })).toBeVisible();
    const vu = sec.getByLabel("Vụ", { exact: true });
    await expect(vu.locator("option")).toHaveCount(4);
    await expect(sec.getByTestId("line-chart")).toHaveAttribute("data-points", /^[1-9]\d*$/);
    await vu.selectOption("DX-2025");
    await expect(sec.getByTestId("metric-latest")).toContainText("Tỷ lệ HTX đủ máy");
  });

  test("Định nghĩa chỉ số: chỉ xem, các phiên bản, ghi rõ tạm chờ khách", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/chi-so");
    await expect(page.getByRole("heading", { level: 1, name: "Định nghĩa chỉ số" })).toBeVisible();
    const plots = page.getByRole("table", { name: "Các phiên bản của geo.plots_digitised" });
    await expect(plots.locator("tbody tr")).toHaveCount(1);
    await expect(plots).toContainText("geo.plot.v1");
    await expect(plots).toContainText("tạm, chờ khách");
    // PA1 (#314): không còn chỉ số kn.* (pf/metrics ngừng đọc view công bố của KN).
    await expect(page.getByRole("table", { name: /Các phiên bản của kn\./ })).toHaveCount(0);
    await expect(page.getByRole("table", { name: /Các phiên bản của / })).toHaveCount(5);
  });
});
