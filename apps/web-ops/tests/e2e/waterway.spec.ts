import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test.describe("Mạng đường thủy (mục 16; GIS US-WATER-01/02/03)", () => {
  test("người dựng tự phát hành → lỗi R-56; yêu cầu dựng lại (202 → job) dùng lại phiên bản; phát hiện lọc theo loại", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/thuy-he");
    const v3 = page.getByRole("row", { name: /Phiên bản 3/ });
    await expect(v3).toContainText("Đã dựng — chờ phát hành");
    await expect(page.getByRole("row", { name: /Phiên bản 2/ })).toContainText("Đang có hiệu lực");
    await v3.getByRole("button", { name: "Phát hành phiên bản 3" }).click();
    const alert = page.getByRole("region", { name: "Phiên bản mạng" }).getByRole("alert");
    await expect(alert).toContainText("Không được tự phê duyệt");
    await expect(alert).toContainText("self_approval_forbidden");
    await page.getByRole("button", { name: "Yêu cầu dựng phiên bản mới" }).click();
    await expect(page.getByTestId("build-status")).toContainText("Hoàn tất", { timeout: 15_000 });
    await expect(page.getByText(/dùng lại phiên bản 3/)).toBeVisible();
    // Phát hiện của phiên bản đang chọn.
    const d = page.getByTestId("version-detail");
    await expect(d.getByRole("list", { name: "Phát hiện khi dựng" }).locator("li")).toHaveCount(4);
    await d.getByLabel("Lọc phát hiện").selectOption("gap_reported");
    await expect(d.getByRole("list", { name: "Phát hiện khi dựng" }).locator("li")).toHaveCount(1);
    await expect(d).toContainText("cần kiểm tra");
  });

  test("người phát hành khác người dựng phát hành được; phiên bản cũ chuyển 'Đã bị thay'", async ({ page }) => {
    await loginAs(page, "approver", "/quan-tri/thuy-he");
    await page.getByRole("row", { name: /Phiên bản 3/ }).getByRole("button", { name: "Phát hành phiên bản 3" }).click();
    await expect(page.getByText("Đã phát hành phiên bản 3")).toBeVisible();
    await expect(page.getByRole("row", { name: /Phiên bản 3/ })).toContainText("Đang có hiệu lực");
    await expect(page.getByRole("row", { name: /Phiên bản 2/ })).toContainText("Đã bị thay");
    // Người phát hành không có vai biên tập → không có nút dựng hiệu lực (backend trả 403).
    await page.getByRole("button", { name: "Yêu cầu dựng phiên bản mới" }).click();
    await expect(page.getByRole("region", { name: "Phiên bản mạng" }).getByRole("alert")).toContainText("không có quyền");
  });

  test("thử định tuyến: có đường thủy (km, nguồn, ràng buộc); tải trọng quá lớn → Haversine", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/thuy-he");
    await page.getByRole("tab", { name: "Thử định tuyến" }).click();
    await page.getByLabel("Điểm đi (kinh độ, vĩ độ)").fill("105.2, 10.25");
    await page.getByLabel("Điểm đến (kinh độ, vĩ độ)").fill("105.85, 9.85");
    await page.getByLabel("Tải trọng sà lan (tấn)").fill("100");
    await page.getByRole("button", { name: "Tìm đường thủy" }).click();
    const r = page.getByTestId("route-result");
    await expect(r).toContainText("Có đường thủy");
    await expect(r).toContainText(/Quãng đường[\s\S]*km/);
    await expect(r).toContainText("Đường thủy theo mạng phiên bản 2");
    await expect(r).toContainText(/Ràng buộc bất lợi nhất[\s\S]*tấn/);
    await page.getByLabel("Tải trọng sà lan (tấn)").fill("5000");
    await page.getByRole("button", { name: "Tìm đường thủy" }).click();
    await expect(r).toContainText("Không có đường thủy");
    await expect(r).toContainText("Ước tính đường chim bay");
  });

  test("GIS US-WATER-02: vẽ tuyến kênh — thông báo lỗi theo backlog, chiều dài hệ thống tính", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/thuy-he");
    await page.getByRole("tab", { name: "Tuyến sông/kênh" }).click();
    await page.getByRole("button", { name: "Vẽ tuyến sông/kênh mới" }).click();
    const input = page.getByLabel("Thêm điểm theo toạ độ (kinh độ, vĩ độ)");
    for (const p of ["105.90, 10.40", "105.93, 10.43"]) {
      await input.fill(p);
      await input.press("Enter");
    }
    await expect(page.getByTestId("draw-measure")).toContainText("Chiều dài:");
    await page.getByRole("button", { name: "Lưu tuyến" }).click();
    await expect(page.getByText("Vui lòng nhập đầy đủ Tên tuyến và Phân loại.")).toBeVisible();
    await page.getByLabel("Tên tuyến").fill("Kênh nội đồng mới ấp 3");
    await page.getByLabel("Phân loại").selectOption("noi_dong");
    await page.getByLabel("Chiều rộng lòng kênh (m)").fill("0");
    await page.getByRole("button", { name: "Lưu tuyến" }).click();
    await expect(page.getByText("Chiều rộng, độ sâu và tải trọng tối đa phải là số lớn hơn 0.")).toBeVisible();
    await page.getByLabel("Chiều rộng lòng kênh (m)").fill("8");
    await page.getByRole("button", { name: "Lưu tuyến" }).click();
    await expect(page.getByText(/Đã thêm tuyến \(nháp/)).toBeVisible();
    await expect(page.getByLabel("Tuyến đang có").locator("option", { hasText: "Kênh nội đồng mới ấp 3 · Nháp" })).toHaveCount(1);
  });
});
