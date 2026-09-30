import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test.describe("Lịch sử chỉ số đã chốt (pf/metrics — KN US-DASH-01/02, CGH FN-10/11)", () => {
  test("KN US-DASH-02: bảng điều hành có biểu đồ theo ngày; xem giá trị kỳ đã chốt bị thay kèm lý do", async ({ page }) => {
    await loginAs(page, "tw", "/");
    const sec = page.getByTestId("metric-history");
    await expect(sec.getByRole("heading", { name: "Diễn biến theo ngày (lịch sử đã chốt)" })).toBeVisible();
    await expect(sec.getByTestId("line-chart")).toHaveAttribute("data-points", /^[1-9]\d*$/);
    await expect(sec.getByTestId("metric-latest")).toContainText("định nghĩa v2");
    await sec.getByLabel("Xem giá trị đã bị thay (kỳ đã chốt)").check();
    const table = sec.getByRole("table", { name: "Giá trị kỳ đã chốt bị thay thế" });
    await expect(table.locator("tbody tr")).toHaveCount(2);
    await expect(table).toContainText("bản ghi bù");
    // Theo đơn vị và chỉ số khác.
    await sec.getByLabel("Đơn vị", { exact: true }).selectOption({ label: "Đồng Tháp" });
    await sec.getByLabel("Chỉ số", { exact: true }).selectOption("kn.open_tasks");
    await expect(sec.getByTestId("metric-latest")).toContainText("Nhiệm vụ hỗ trợ đang mở");
    await sec.getByLabel("Khoảng thời gian").selectOption("90");
    await expect(sec.getByTestId("line-chart")).toHaveAttribute("data-points", /^(8\d|90)$/);
  });

  test("TTKN tỉnh: lịch sử theo tỉnh mình (không xin phạm vi toàn quốc)", async ({ page }) => {
    await loginAs(page, "tinh", "/");
    const sec = page.getByTestId("metric-history");
    await expect(sec.getByTestId("line-chart")).toHaveAttribute("data-points", /^[1-9]\d*$/);
    await expect(sec.getByRole("alert")).toHaveCount(0);
  });

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
    const sown = page.getByRole("table", { name: "Các phiên bản của kn.sown_area_ha" });
    await expect(sown.locator("tbody tr")).toHaveCount(2);
    await expect(sown).toContainText("kn.dash.v2");
    await expect(sown).toContainText("tạm, chờ khách");
    await expect(page.getByRole("table", { name: /Các phiên bản của / })).toHaveCount(9);
  });
});
