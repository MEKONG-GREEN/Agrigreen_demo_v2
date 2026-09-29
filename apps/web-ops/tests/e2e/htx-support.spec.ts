import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const TABLE = "Yêu cầu hỗ trợ và nhiệm vụ tự sinh";

test("HTX US-SUPPORT-01 / KN US-TASK-01: Quản lý HTX thấy yêu cầu hỗ trợ và trạng thái nhiệm vụ tự sinh", async ({ page }) => {
  await loginAs(page, "htx", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: TABLE });
  const pest = table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" });
  await expect(pest).toContainText("Dịch hại");
  await expect(pest).toContainText("Khẩn");
  await expect(pest).toContainText("Mới — chờ phân công");
  await expect(table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" })).toContainText("Đang xử lý");
  await expect(page.getByRole("status")).toContainText(/\d+ yêu cầu · \d+ chưa xử lý xong/);

  await page.getByLabel("Trạng thái nhiệm vụ").selectOption("resolved");
  await expect(table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toHaveCount(0);
  await expect(table).toContainText("Đã hướng dẫn phun thuốc đặc trị");
});

test("HTX US-SUPPORT-01 AC-1: ảnh, nhãn “Thiếu vị trí GPS”, ghi chú cuối và liên kết nhiệm vụ", async ({ page }) => {
  await loginAs(page, "htx", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: TABLE });
  const rain = table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" });
  await expect(rain).toContainText("4 ảnh");
  await expect(rain).toContainText("Thiếu vị trí GPS");
  await expect(rain).toContainText("Ghi chú: Đã hẹn xuống ruộng sáng mai");
  await rain.getByText("4 ảnh").click();
  await expect(rain.getByRole("list", { name: "Mã tệp ảnh" }).getByRole("listitem")).toHaveCount(4);

  const pest = table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" });
  await expect(pest).toContainText("Không có ảnh");
  await expect(pest).toContainText("Có vị trí GPS (±8 m)");
  await expect(pest).toContainText("Chưa giao — chờ quản trị tỉnh");
  await expect(pest.getByRole("link", { name: "Mở nhiệm vụ" })).toHaveAttribute("href", /\/nhiem-vu\?task=/);

  // Cách ly HTX: yêu cầu của HTX khác không hiện cho quản lý HTX này.
  await expect(table.getByRole("row").filter({ hasText: "Lúa vàng lá chín sớm" })).toHaveCount(0);
});

test("KN US-TASK-03: lọc theo loại sự cố, mức khẩn và vị trí GPS", async ({ page }) => {
  await loginAs(page, "htx", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: TABLE });
  await expect(table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toBeVisible();

  await page.getByLabel("Vị trí GPS").selectOption("true");
  await expect(table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" })).toBeVisible();
  await expect(table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toHaveCount(0);
  await page.getByLabel("Vị trí GPS").selectOption("");

  await page.getByLabel("Mức khẩn").selectOption("khan");
  await expect(table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toBeVisible();
  await expect(table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" })).toHaveCount(0);
  await page.getByLabel("Mức khẩn").selectOption("");

  await page.getByLabel("Loại sự cố").selectOption("may_moc");
  await expect(table.getByRole("row").filter({ hasText: "Cần hỗ trợ đặt lịch máy gặt" })).toBeVisible();
  await expect(table.getByRole("row")).toHaveCount(2); // tiêu đề + 1 yêu cầu
});

test("cán bộ KN xã thấy yêu cầu hỗ trợ của nông dân trong xã", async ({ page }) => {
  await loginAs(page, "xa", "/htx/yeu-cau-ho-tro");
  await expect(page.getByRole("table", { name: TABLE }).getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toBeVisible();
});

test("TTKN trung ương thấy yêu cầu của mọi HTX trong phạm vi", async ({ page }) => {
  await loginAs(page, "tw", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: TABLE });
  await expect(table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" })).toBeVisible();
  await expect(table.getByRole("row").filter({ hasText: "Lúa vàng lá chín sớm" })).toBeVisible();
});
