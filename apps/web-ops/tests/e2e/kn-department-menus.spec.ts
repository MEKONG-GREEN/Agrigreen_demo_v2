import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

async function login(page: Page, identifier: string, navigate = true) {
 if (navigate) await page.goto("/login");
 await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
 await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");
 await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
 await expect(sidebar(page)).toBeVisible();
}

test("TT keeps its reports and denies FN20 and Q4 direct URLs",async({page})=>{
 await login(page,"quan.lm@ttknqg.gov.vn");
 await expect(sidebar(page).getByRole("link",{name:"Báo cáo tuần",exact:true})).toBeVisible();
 await expect(sidebar(page).getByRole("link",{name:"Quy trình, văn bản",exact:true})).toHaveCount(0);
 await page.goto("/kn/van-ban");
 await expect(page.getByRole("heading",{name:"Không có quyền truy cập",exact:true})).toBeVisible();
 const response=await page.goto("/kn/bao-cao/dot-xuat");
 expect(response?.status()).toBe(404);
 await expect(page.getByRole("heading",{name:"Báo cáo đột xuất",exact:true})).toHaveCount(0);
});

test("Admin keeps TT agricultural catalogs and denies copied department URLs",async({page})=>{
 await login(page,"admin.mekong@mekonggreen.vn");
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 for(const name of ["Phòng Chăn nuôi - Thủy sản","Phòng Kế hoạch - Tài chính","Phòng Thông tin Tuyên truyền","Phòng Hành chính - Tổng hợp"]) {
  await page.getByLabel(`Trạng thái ${name}`,{exact:true}).selectOption("active");
  await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toContainText("Đang áp dụng");
 }
 await logout(page);
 await login(page,"admin.mekong@mekonggreen.vn",false);
 for(const [code,name,own] of [
  ["chan_nuoi_thuy_san","Phòng Chăn nuôi - Thủy sản","/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts"],
  ["ke_hoach_tai_chinh","Phòng Kế hoạch - Tài chính","/kn/phong-ban/ke_hoach_tai_chinh/danh-muc-khtc"],
  ["thong_tin_truyen_thong","Phòng Thông tin Tuyên truyền","/kn/bao-gia/mat-hang"],
  ["hanh_chinh_tong_hop","Phòng Hành chính - Tổng hợp","/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su"],
 ]) {
  await sidebar(page).getByRole("button",{name,exact:true}).click();
  await expect(sidebar(page).locator(`a[href="${own}"]`)).toBeVisible();
  for(const removed of ["danh-muc","theo-doi-nop","tong-hop","du-lieu-bao-cao"]) await expect(sidebar(page).locator(`a[href="/kn/phong-ban/${code}/${removed}"]`)).toHaveCount(0);
 }
 await sidebar(page).getByRole("button",{name:"Phòng Trồng trọt",exact:true}).click();
 await expect(sidebar(page).getByRole("link",{name:"Danh mục nông nghiệp",exact:true})).toHaveAttribute("href","/kn/phong-ban/trong_trot/danh-muc");
 for(const dept of ["chan_nuoi_thuy_san","ke_hoach_tai_chinh","thong_tin_truyen_thong","hanh_chinh_tong_hop"]){
  await page.goto(`/kn/phong-ban/${dept}/danh-muc`);
  await expect(page.getByRole("heading",{name:"Không có quyền truy cập",exact:true})).toBeVisible();
 }
});
