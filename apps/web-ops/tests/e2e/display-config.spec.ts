import { expect, test } from "@playwright/test";
import { loginViaUi, logout, navTo } from "./helpers";

const HEX_ERR = "Mã màu không đúng định dạng HEX (ví dụ: #1E90FF).";
const PCT_ERR = "Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%.";

test("GIS US-CFG-01/02: HEX sai bị chặn → đề xuất màu chờ duyệt → người khác duyệt → chú giải bản đồ đổi màu → khôi phục mặc định qua hộp thoại", async ({ page }) => {
  // Đăng nhập qua giao diện, điều hướng phía client để giữ trạng thái giả lập giữa các người dùng.
  await page.goto("/login");
  await loginViaUi(page, /Quản trị nền tảng/);
  await navTo(page, "Quy chuẩn hiển thị bản đồ");
  await expect(page.getByRole("heading", { level: 1, name: "Quy chuẩn hiển thị bản đồ" })).toBeVisible();
  await expect(page.getByRole("note")).toContainText("ngay khi được phê duyệt");
  await expect(page.getByRole("note")).toContainText("RB-1");

  const row = page.locator('tr[data-key="gis.stage_color:chin_ro"]');
  await expect(row).toContainText("#D6452F");
  await expect(row).toContainText("Mặc định");
  // AC-2 (US-CFG-01): mã màu sai định dạng → thông điệp đúng.
  await row.getByLabel("Mã màu mới — Chín rộ / đang thu hoạch").fill("#12G45");
  await row.getByRole("button", { name: "Đề xuất", exact: true }).click();
  await expect(row.getByRole("alert")).toHaveText(HEX_ERR);

  // Thiếu văn bản căn cứ → nhắc nhập.
  await row.getByLabel("Mã màu mới — Chín rộ / đang thu hoạch").fill("#1e90ff");
  await row.getByRole("button", { name: "Đề xuất", exact: true }).click();
  await expect(page.getByText("Cần nhập văn bản căn cứ (số/ngày) trước khi đề xuất.")).toBeVisible();
  await page.getByLabel(/Văn bản căn cứ \(số\/ngày\)/).fill("QĐ thử nghiệm E2E số 09/2026");
  await row.getByRole("button", { name: "Đề xuất", exact: true }).click();
  await expect(row.getByTestId("pending-chip")).toContainText("Chờ duyệt: #1E90FF");
  await expect(page.getByRole("table", { name: "Đề xuất quy chuẩn hiển thị chờ duyệt" })).toContainText("Chín rộ / đang thu hoạch");

  // US-CFG-01 AC-2: ngưỡng % không tăng dần → chặn phía client với thông điệp đúng.
  await page.getByLabel("Ngưỡng Sắp đầy (%)").fill("90");
  await page.getByRole("button", { name: "Đề xuất ngưỡng" }).click();
  await expect(page.getByRole("form", { name: "Đề xuất ngưỡng lấp đầy kho/Hub" }).getByRole("alert")).toHaveText(PCT_ERR);

  // Người phê duyệt khác duyệt ở màn Tham số & phê duyệt (R-56).
  await logout(page);
  await loginViaUi(page, /người phê duyệt/);
  await navTo(page, "Tham số & phê duyệt");
  const section = page.getByRole("region", { name: "Màu giai đoạn — Chín rộ / đang thu hoạch", exact: true });
  const pending = section.getByRole("listitem").filter({ hasText: "#1E90FF" });
  await expect(pending.getByRole("button", { name: "Phê duyệt" })).toBeEnabled();
  await pending.getByRole("button", { name: "Phê duyệt" }).click();
  await expect(section.getByRole("listitem").filter({ hasText: "#1E90FF" })).toHaveCount(0);

  // Màu mới áp dụng cho chú giải bản đồ GIS.
  await logout(page);
  await loginViaUi(page, /Quản trị nền tảng/);
  await navTo(page, "Bản đồ GIS nhiều lớp");
  await expect(page.getByRole("list", { name: "Chú giải giai đoạn mùa vụ" }).locator('li[data-stage="chin_ro"]')).toHaveAttribute("data-color", "#1E90FF");

  // US-CFG-02 AC-2: khôi phục mặc định cần xác nhận trong hộp thoại → phiên bản mới chờ duyệt.
  await navTo(page, "Quy chuẩn hiển thị bản đồ");
  const row2 = page.locator('tr[data-key="gis.stage_color:chin_ro"]');
  await expect(row2).toContainText("#1E90FF");
  await expect(row2).toContainText("Đã duyệt v.1");
  await row2.getByRole("button", { name: "Khôi phục mặc định" }).click();
  const dlg = page.getByRole("dialog", { name: "Khôi phục mặc định — Chín rộ / đang thu hoạch" });
  await expect(dlg).toContainText("giá trị cũ vẫn lưu trong lịch sử (RB-1)");
  await dlg.getByRole("button", { name: "Xác nhận khôi phục" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(row2.getByTestId("pending-chip")).toContainText("Chờ duyệt: #D6452F");
});
