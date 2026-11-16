import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

// HTX US-SUPPORT-01 — module chủ HTX pf/cooperative (#313): GET /v1/cropcycle/support-requests, không phụ thuộc pf/extension.
const TABLE = "Yêu cầu hỗ trợ của nông dân";

test("HTX US-SUPPORT-01: Quản lý HTX thấy yêu cầu hỗ trợ của HTX mình, trạng thái nhiệm vụ cũ lúc chuyển", async ({ page }) => {
  await loginAs(page, "htx", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: TABLE });
  const pest = table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" });
  await expect(pest).toContainText("Dịch hại");
  await expect(pest).toContainText("Khẩn");
  await expect(pest).toContainText("Nhiệm vụ khuyến nông (trước khi chuyển): Mới — chờ phân công");
  await expect(table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" })).toContainText("Đang xử lý");
  await expect(page.getByRole("status")).toContainText(/\d+ yêu cầu/);
  // Cách ly HTX: yêu cầu của HTX khác không hiện cho quản lý HTX này.
  await expect(table.getByRole("row").filter({ hasText: "Lúa vàng lá chín sớm" })).toHaveCount(0);
});

test("HTX US-SUPPORT-01 AC-1: ảnh và nhãn “Thiếu vị trí GPS”", async ({ page }) => {
  await loginAs(page, "htx", "/htx/yeu-cau-ho-tro");
  const table = page.getByRole("table", { name: TABLE });
  const rain = table.getByRole("row").filter({ hasText: "Mưa lớn kéo dài" });
  await expect(rain).toContainText("4 ảnh");
  await expect(rain).toContainText("Thiếu vị trí GPS");
  await rain.getByText("4 ảnh").click();
  await expect(rain.getByRole("list", { name: "Mã tệp ảnh" }).getByRole("listitem")).toHaveCount(4);

  const pest = table.getByRole("row").filter({ hasText: "Rầy nâu mật độ cao" });
  await expect(pest).toContainText("Không có ảnh");
  await expect(pest).toContainText("Có vị trí GPS (±8 m)");
});

test("lọc theo loại sự cố, mức khẩn và vị trí GPS", async ({ page }) => {
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
