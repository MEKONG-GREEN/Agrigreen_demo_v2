import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("HTX US-CAT-02: nhập danh mục hàng loạt — dòng hợp lệ được tạo, dòng lỗi kèm nguyên nhân ngay trên màn hình", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/danh-muc");
  await page.getByRole("button", { name: "Nhập từ Excel" }).click();
  const form = page.getByRole("form", { name: "Nhập danh mục từ tệp" });
  const csv = [
    "catalog,code,name,growth_days,expected_yield_t_per_ha",
    "rice_variety,OM9582,OM9582 (thử nhập),95,6.8",
    "rice_variety,OM5451,OM5451,95,6.75",
    "rice_variety,XLOI,Giống lỗi,0,",
    "khong_co,A1,Mục lạ,,",
  ].join("\n");
  await form.getByLabel("Tệp danh mục").setInputFiles({ name: "danh-muc.csv", mimeType: "text/csv", buffer: Buffer.from(csv, "utf-8") });
  await form.getByRole("button", { name: "Nhập", exact: true }).click();
  await expect(form.getByRole("status")).toContainText("1 dòng đã tạo · 1 bỏ qua · 2 dòng lỗi (tổng 4)");
  const table = form.getByRole("table", { name: "Kết quả nhập danh mục theo dòng" });
  await expect(table.getByRole("row").filter({ hasText: "Dòng 4" })).toContainText("Thời gian sinh trưởng phải lớn hơn 0.");
  await expect(table.getByRole("row").filter({ hasText: "Dòng 5" })).toContainText("không tồn tại");
  await expect(table.getByRole("row").filter({ hasText: "Dòng 3" })).toContainText("Bỏ qua (đã có)");
  // Mục mới hiện ngay trong danh mục giống lúa.
  await expect(page.getByRole("table", { name: /Các mục của danh mục/ })).toContainText("OM9582 (thử nhập)");
});
