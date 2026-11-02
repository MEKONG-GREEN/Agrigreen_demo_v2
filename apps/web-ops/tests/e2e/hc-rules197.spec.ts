import { expect, test, type Page } from "@playwright/test";
import { loginAs, logout, sidebar } from "./helpers";

async function enterCatalog(page:Page){
 await page.clock.setFixedTime(new Date("2026-10-12T03:00:00Z"));
 await loginAs(page,"kn-admin","/kn/tong-quan");
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toContainText("Đang áp dụng");
 await sidebar(page).getByRole("link",{name:"Menu các cấp",exact:true}).click();
 await page.getByLabel("Xem như vai trò*").selectOption("kn_tinh");
 await page.getByLabel(/^Lý do hỗ trợ/).fill("Kiểm tra tham số FN19 và lịch sử phiên bản");
 await page.getByRole("button",{name:"Vào menu Cấp Tỉnh",exact:true}).click();
 await page.getByRole("button",{name:"Thoát chế độ hỗ trợ",exact:true}).click();
 await sidebar(page).getByRole("button",{name:"Phòng Hành chính - Tổng hợp",exact:true}).click();
 await sidebar(page).getByRole("link",{name:"Danh mục nhân sự",exact:true}).click();
}

test("hc-rules197: tham số qua route riêng, thêm phiên bản và giữ bản cũ/Log",async({page})=>{
 await enterCatalog(page);
 await page.getByRole("link",{name:"Quy tắc nâng bậc",exact:true}).click();
 await expect(page).toHaveURL(/\/quy-tac\/promotion\?at=2026-10-12$/);
 await expect(page.getByRole("heading",{name:"Phiên bản đang xem",exact:true})).toBeVisible();
 await page.getByRole("link",{name:"Phiên bản mới",exact:true}).click();
 await expect(page.getByRole("dialog")).toHaveCount(0);
 const percent=page.getByRole("textbox",{name:"Tỷ lệ chỉ tiêu NLTH (%)",exact:true});
 for(const raw of ["1e100000","1e-100000","9".repeat(100000)]){
  await percent.fill(raw);await page.getByRole("button",{name:"Lưu",exact:true}).click();
  await expect(percent).toHaveAttribute("aria-invalid","true");await expect(page).toHaveURL(/\/them-moi\?/);
 }
 await percent.fill("12,34");await page.getByLabel(/^Ngày hiệu lực/).fill("2027-01-01");
 await page.getByRole("textbox",{name:"Ghi chú (căn cứ)",exact:true}).fill("Căn cứ kiểm thử FN19 197");
 await page.getByRole("button",{name:"Lưu",exact:true}).click();
 await expect(page).toHaveURL(/\/promotion\?at=2027-01-01&version=2$/);
 const history=page.getByRole("table",{name:"Lịch sử tham số",exact:true});await expect(history.getByRole("row")).toHaveCount(3);
 await expect(history).toContainText("Căn cứ kiểm thử FN19 197");
 const log=page.getByRole("complementary",{name:"Log",exact:true});await expect(log).toContainText("Phiên bản 2");await expect(log).toContainText("Chưa có thông tin người thực hiện");
 await history.getByRole("link",{name:"Xem phiên bản 1",exact:true}).click();
 await expect(page).toHaveURL(/&version=1$/);await expect(page.getByRole("heading",{name:"Nội dung quy tắc",exact:true})).toBeVisible();
 await expect(history).toContainText("Căn cứ kiểm thử FN19 197");
 await page.getByRole("link",{name:"Phiên bản mới",exact:true}).click();await page.getByRole("link",{name:"Hủy",exact:true}).click();
 await expect(page).toHaveURL(/&version=1$/);await expect(history.getByRole("row")).toHaveCount(3);
 // Keep the same mock tab when changing the selected account; no reload erases the ledger.
 await logout(page);await page.getByLabel("Email hoặc số điện thoại").fill("hung.tq@angiang.gov.vn");await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
 await sidebar(page).getByRole("link",{name:"Danh mục nhân sự",exact:true}).first().click();await page.getByRole("link",{name:"Quy tắc nâng bậc",exact:true}).click();
 await expect(history).toContainText("Căn cứ kiểm thử FN19 197");await expect(page.getByRole("link",{name:"Phiên bản mới",exact:true})).toHaveCount(0);
});

test("hc-rules197-alias: quy đổi qua alias phòng, hủy không ghi",async({page})=>{
 await page.clock.setFixedTime(new Date("2026-10-12T03:00:00Z"));
 await loginAs(page,"kn-admin","/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su/quy-tac/achievement_conversion?at=2026-10-12");
 const aliasHistoryLength=await page.evaluate(()=>window.history.length);
 // A fresh tab starts with HCTH closed. Enable it through Admin and use browser
 // history within this tab, retaining the actual department state and context.
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toContainText("Đang áp dụng");
 await sidebar(page).getByRole("link",{name:"Menu các cấp",exact:true}).click();
 await page.getByLabel("Xem như vai trò*").selectOption("kn_tinh");
 await page.getByLabel(/^Lý do hỗ trợ/).fill("Kiểm tra alias tham số FN19");
 await page.getByRole("button",{name:"Vào menu Cấp Tỉnh",exact:true}).click();
 await page.getByRole("button",{name:"Thoát chế độ hỗ trợ",exact:true}).click();
 await expect(page.getByRole("button",{name:"Thoát chế độ hỗ trợ",exact:true})).toHaveCount(0);
 await expect(sidebar(page).getByRole("button",{name:"Phòng Hành chính - Tổng hợp",exact:true})).toBeVisible();
 const delta=aliasHistoryLength-await page.evaluate(()=>window.history.length);
 expect(delta).toBeLessThan(0);await page.evaluate((steps)=>window.history.go(steps),delta);
 await expect(page).toHaveURL(/\/phong-ban\/hanh_chinh_tong_hop\/danh-muc-nhan-su\/quy-tac\/achievement_conversion\?/);
 const history=page.getByRole("table",{name:"Lịch sử tham số",exact:true});await expect(history.getByRole("row")).toHaveCount(2);
 await page.getByRole("link",{name:"Phiên bản mới",exact:true}).click();await expect(page).toHaveURL(/phong-ban\/hanh_chinh_tong_hop\/danh-muc-nhan-su\/quy-tac\/achievement_conversion\/them-moi\?/);
 const months=page.getByRole("textbox",{name:"Số tháng 1",exact:true});await months.fill("1e-100000");await page.getByRole("button",{name:"Lưu",exact:true}).click();await expect(months).toHaveAttribute("aria-invalid","true");
 await page.getByRole("link",{name:"Hủy",exact:true}).click();await expect(history.getByRole("row")).toHaveCount(2);await expect(page.getByRole("table",{name:"Bảng quy đổi thành tích",exact:true})).toBeVisible();
});
