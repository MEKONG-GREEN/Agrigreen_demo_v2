import { expect, test } from "@playwright/test";
import { sidebar } from "./helpers";

// CNTS-ROSTER-UI-001: chọn tỉnh → phân ca/lưu → chuyển tỉnh không lộ lịch → xem lại; lịch cũ chỉ đọc.
test("W11.46 Admin lập lịch trực riêng theo tỉnh và xem lịch cũ", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
  await page.goto("/kn/cnts/lich-truc");
  await expect(page.getByRole("heading",{level:1,name:"Lịch trực KSGM",exact:true})).toBeVisible();
  const province=page.getByLabel("Tỉnh của lịch trực");
  await expect(province).toHaveValue("");
  await expect(page.getByRole("button",{name:"Thêm hàng"})).toHaveCount(0);
  await province.selectOption("91");
  await page.getByRole("combobox",{name:"Cơ sở",exact:true}).selectOption({index:1});
  await page.getByRole("combobox",{name:"Viên chức",exact:true}).selectOption({index:1});
  await page.getByRole("button",{name:"Thêm hàng"}).click();
  await page.getByRole("button",{name:/ ngày 1: nghỉ$/}).click();
  await page.getByLabel("Ghi chú lần lưu").fill("Phân ca kiểm thử theo tỉnh");
  await page.getByRole("button",{name:"Lưu",exact:true}).click();
  await expect(page.getByRole("region",{name:"Lịch sử lưu"})).toContainText("Phân ca kiểm thử theo tỉnh");
  await province.selectOption("82");
  await expect(page.getByRole("region",{name:"Thông tin lịch"})).toContainText("Chưa lập lịch tháng này.");
  await expect(page.getByText("Phân ca kiểm thử theo tỉnh",{exact:false})).toHaveCount(0);
  await province.selectOption("91");
  await expect(page.getByRole("region",{name:"Lịch sử lưu"})).toContainText("Phân ca kiểm thử theo tỉnh");
  const downloaded=page.waitForEvent("download");
  await page.getByRole("button",{name:"Xuất CSV"}).click();
  expect((await downloaded).suggestedFilename()).toMatch(/^lich-truc-ksgm-\d{4}-\d{2}\.csv$/);
  await province.selectOption("legacy");
  await expect(page.getByRole("button",{name:"Lưu",exact:true})).toHaveCount(0);
  await expect(page.getByRole("button",{name:"Thêm hàng"})).toHaveCount(0);
});
