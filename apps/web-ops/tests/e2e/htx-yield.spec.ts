import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { byKind, plot } from "./htx-data";

const [readyA, readyB, readyC, readyD] = byKind(3); // vụ đang mở đã tới thu hoạch
const early = plot(1); // chưa tới giai đoạn thu hoạch

test("HTX US-YIELD-02 AC-2 / US-YIELD-03: bảng nhập nhanh lưu nhiều lô một lần; bất thường cần xác nhận", async ({ page }) => {
  await loginAs(page, "htx", "/htx/san-luong");
  const grid = page.getByRole("form", { name: "Bảng nhập nhanh sản lượng" });
  await grid.getByLabel(`Sản lượng kg lô ${readyA!.code}`).fill("abc");
  await grid.getByRole("button", { name: "Lưu 1 lô" }).click();
  await expect(grid).toContainText("Sản lượng phải là số");

  await grid.getByLabel(`Sản lượng kg lô ${readyA!.code}`).fill("2500");
  await grid.getByLabel(`Sản lượng kg lô ${readyB!.code}`).fill("90000"); // vượt ngưỡng năng suất theo giống
  await grid.getByLabel(`Sản lượng kg lô ${early.code}`).fill("3000");
  await grid.getByRole("button", { name: "Lưu 3 lô" }).click();
  await expect(grid.getByRole("status")).toContainText("1 lô đã lưu · 0 đã có · 2 lỗi");
  const results = grid.getByRole("table", { name: "Kết quả lưu bảng nhập nhanh" });
  await expect(results.getByRole("row").filter({ hasText: early.code })).toContainText("chưa đến giai đoạn thu hoạch");
  await expect(results.getByRole("row").filter({ hasText: readyB!.code })).toContainText("Cần xác nhận");

  // Lô đã lưu rời bảng (vụ đã hoàn thành); xác nhận cố ý lô bất thường rồi lưu lại.
  await expect(grid.getByLabel(`Sản lượng kg lô ${readyA!.code}`)).toHaveCount(0);
  await grid.getByRole("checkbox", { name: `Xác nhận sản lượng lô ${readyB!.code} là đúng` }).check();
  await grid.getByLabel(`Sản lượng kg lô ${early.code}`).fill("");
  await grid.getByRole("button", { name: "Lưu 1 lô" }).click();
  await expect(grid.getByRole("status")).toContainText("1 lô đã lưu · 0 đã có · 0 lỗi");
  await expect(grid.getByLabel(`Sản lượng kg lô ${readyB!.code}`)).toHaveCount(0);
});

test("HTX US-YIELD-02 AC-1: nhập CSV — dòng lỗi bị bỏ qua, dòng hợp lệ vẫn lưu, lỗi hiện ngay trên màn hình", async ({ page }) => {
  await loginAs(page, "htx", "/htx/san-luong");
  await page.getByRole("button", { name: "Nhập từ Excel" }).click();
  const form = page.getByRole("form", { name: "Nhập sản lượng từ tệp" });
  const csv = ["Mã lô,Sản lượng kg,Độ ẩm", `${readyC!.code},4200,14.5`, "TH9999999,5000,", `${readyD!.code},,`].join("\n");
  await form.getByLabel("Tệp sản lượng").setInputFiles({ name: "san-luong.csv", mimeType: "text/csv", buffer: Buffer.from(csv, "utf-8") });
  await form.getByRole("button", { name: "Nhập tệp" }).click();
  await expect(form.getByRole("status")).toContainText("1 dòng thành công · 2 dòng lỗi");
  const table = form.getByRole("table", { name: "Kết quả nhập sản lượng theo dòng" });
  await expect(table.getByRole("row").filter({ hasText: "Dòng 3" })).toContainText("Không tìm thấy vụ đang mở");
  await expect(table.getByRole("row").filter({ hasText: "Dòng 4" })).toContainText("Thiếu sản lượng");
  await expect(table.getByRole("row").filter({ hasText: "Dòng 2" })).toContainText("Đã lưu");
});

test("cán bộ KN chỉ xem bảng sản lượng, không có nút lưu", async ({ page }) => {
  await loginAs(page, "xa", "/htx/san-luong");
  await expect(page.getByRole("note")).toContainText("chỉ xem");
  await expect(page.getByRole("button", { name: /^Lưu/ })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Nhập từ Excel" })).toHaveCount(0);
});
