import { expect, test } from "@playwright/test";
import { sidebar, logout } from "./helpers";

// CNTS-ROSTER-UI-001: chọn tỉnh → phân ca/lưu → chuyển tỉnh không lộ lịch → xem lại; lịch cũ chỉ đọc.
test("W11.46 Admin lập lịch trực riêng theo tỉnh và xem lịch cũ", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  await expect(sidebar(page)).toBeVisible();
  await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
  await page.getByLabel("Trạng thái Phòng Chăn nuôi - Thủy sản",{exact:true}).selectOption("active");
  await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toContainText("Đang áp dụng");
  // Đăng nhập lại trong SPA để lấy menu mới, giữ trạng thái mô phỏng của tab.
  await logout(page);
  await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
  await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");
  await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
  await sidebar(page).getByRole("button",{name:"Phòng Chăn nuôi - Thủy sản",exact:true}).click();
  await sidebar(page).getByRole("link",{name:"Lịch trực KSGM",exact:true}).click();
  await expect(page.getByRole("heading",{level:1,name:"Lịch trực KSGM",exact:true})).toBeVisible();
  const province=page.getByLabel("Tỉnh của lịch trực");
  await expect(province).toHaveValue("");
  await expect(page.getByRole("button",{name:"Thêm hàng"})).toHaveCount(0);
  await province.selectOption("91");
  await page.getByRole("combobox",{name:"Cơ sở",exact:true}).selectOption({index:1});
  await page.getByRole("combobox",{name:"Viên chức",exact:true}).selectOption({index:1});
  await page.getByRole("button",{name:"Thêm hàng"}).click();
  await page.getByRole("button",{name:/ ngày 1: nghỉ( \(cơ sở chưa có người trực\))?$/}).click();
  await page.getByLabel("Ghi chú lần lưu").fill("Phân ca kiểm thử theo tỉnh");
  await page.getByRole("button",{name:"Lưu",exact:true}).click();
  await expect(page.getByRole("region",{name:"Lịch sử lưu"})).toContainText("Phân ca kiểm thử theo tỉnh");
  await province.selectOption("82");
  await expect(page.getByRole("region",{name:"Thông tin lịch"})).toContainText("Chưa lập lịch tháng này.");
  await expect(page.getByText("Phân ca kiểm thử theo tỉnh",{exact:false})).toHaveCount(0);
  await province.selectOption("91");
  await expect(page.getByRole("region",{name:"Lịch sử lưu"})).toContainText("Phân ca kiểm thử theo tỉnh");
  // BF-CN-DUTY: cột Địa chỉ/ĐT; xuất Excel đúng mẫu BM-03 rồi nạp lại tệp vừa xuất (xem trước → áp vào lưới → lưu source=excel).
  await expect(page.getByRole("columnheader",{name:"Địa chỉ",exact:true})).toBeVisible();
  await expect(page.getByRole("columnheader",{name:"ĐT",exact:true})).toBeVisible();
  await page.getByLabel("Dòng căn cứ (xuất Excel)").fill("theo thông báo số 1/TB ngày 01/10");
  const downloaded=page.waitForEvent("download");
  await page.getByRole("button",{name:"Xuất Excel"}).click();
  const download=await downloaded;
  expect(download.suggestedFilename()).toMatch(/^lich-truc-ksgm-91-\d{4}-\d{2}\.xlsx$/);
  await page.getByLabel("Tệp Excel lịch trực").setInputFiles(await download.path());
  const preview=page.getByRole("region",{name:"Xem trước tệp nạp"});
  await expect(preview).toContainText("Không có lỗi.");
  await preview.getByRole("button",{name:"Áp dụng vào lưới"}).click();
  await page.getByRole("button",{name:"Lưu",exact:true}).click();
  await expect(page.getByRole("region",{name:"Lịch sử lưu"})).toContainText("nạp Excel");
  await province.selectOption("legacy");
  await expect(page.getByRole("button",{name:"Lưu",exact:true})).toHaveCount(0);
  await expect(page.getByRole("button",{name:"Thêm hàng"})).toHaveCount(0);
});
