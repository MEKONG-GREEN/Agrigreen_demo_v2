import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";

test("FN11 203: điều chỉnh đã chốt trên route, số lỗi không ghi, lịch sử và bản cũ giữ nguyên",async({page},info)=>{
 test.setTimeout(180_000);await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
 const login=async(identifier="admin.mekong@mekonggreen.vn")=>{await page.getByLabel("Email hoặc số điện thoại").fill(identifier);await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();};
 await page.goto("/login");await login();
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp",{exact:true}).selectOption("active");await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toBeVisible();
 await logout(page);await login("hung.tq@angiang.gov.vn");
 await sidebar(page).getByRole("link",{name:"Kế hoạch nhân lực năm",exact:true}).click();
 await expect(page).toHaveURL(/\/kn\/nhan-su\/ke-hoach$/);
 await page.getByRole("button",{name:"Mở kỳ kế hoạch",exact:true}).click();
 await page.getByLabel("Năm kế hoạch",{exact:true}).fill("2031");await page.getByLabel("Hạn các đơn vị gửi",{exact:true}).fill("2030-11-30");await page.getByLabel("Ngày chốt số liệu hiện có",{exact:true}).fill("2030-06-01");
 await page.getByRole("button",{name:"Mở kỳ",exact:true}).click();await page.getByRole("button",{name:"Chốt kế hoạch",exact:true}).click();
 await page.getByRole("dialog").getByRole("button",{name:"Chốt kế hoạch",exact:true}).click();
 await page.getByRole("main").getByRole("link",{name:"Phòng Trồng trọt",exact:true}).click();
 await page.getByRole("link",{name:"Điều chỉnh (có lý do)",exact:true}).click();await expect(page).toHaveURL(/\/2031\/trong_trot\/dieu-chinh$/);await expect(page.getByRole("dialog")).toHaveCount(0);
 const reason=page.getByRole("textbox",{name:"Lý do điều chỉnh",exact:true});await reason.fill("Điều chỉnh có lý do hợp lệ");
 for(const label of ["Dự kiến nghỉ, chuyển công tác khác","Số cần bổ sung"]){const field=page.getByRole("textbox",{name:label,exact:true});for(const raw of ["1e100000","1e-100000","9".repeat(100000),"99999999999999999999999999999999"]){await field.fill(raw);await page.getByRole("button",{name:"Ghi điều chỉnh",exact:true}).click();await expect(field).toHaveAttribute("aria-invalid","true");await expect(page).toHaveURL(/\/dieu-chinh$/);}await field.fill("0");await expect(field).not.toHaveAttribute("aria-invalid","true");}
 await page.getByRole("button",{name:"Hủy",exact:true}).click();await page.getByRole("link",{name:"Điều chỉnh (có lý do)",exact:true}).click();await page.getByRole("link",{name:"Lịch sử điều chỉnh",exact:true}).click();await expect(page.getByRole("link",{name:/Xem điều chỉnh/})).toHaveCount(0);
 await page.getByRole("main").getByRole("link",{name:"Kế hoạch của đơn vị",exact:true}).click();await page.getByRole("link",{name:"Điều chỉnh (có lý do)",exact:true}).click();
 await page.getByRole("button",{name:"Ghi điều chỉnh",exact:true}).click();await expect(reason).toHaveAttribute("aria-invalid","true");await reason.fill("😀".repeat(9));await expect(reason).toHaveAttribute("aria-invalid","true");await reason.fill("😀".repeat(500));await expect(reason).not.toHaveAttribute("aria-invalid","true");
 await page.getByRole("textbox",{name:"Số cần bổ sung",exact:true}).fill("3");await page.screenshot({path:info.outputPath("adjustment203-valid.png"),fullPage:true});await page.getByRole("button",{name:"Ghi điều chỉnh",exact:true}).click();await expect(page).toHaveURL(/\/dieu-chinh\/1$/);
 const value=()=>page.getByText("Số cần bổ sung",{exact:true}).locator("..").locator("dd");await expect(value()).toHaveText("3");await page.screenshot({path:info.outputPath("adjustment203-version1.png"),fullPage:true});
 await page.getByRole("main").getByRole("link",{name:"Lịch sử điều chỉnh",exact:true}).click();await expect(page.getByRole("table",{name:"Các bản điều chỉnh đã ghi",exact:true}).getByRole("columnheader",{name:"STT",exact:true})).toBeVisible();
 await page.getByRole("main").getByRole("link",{name:"Kế hoạch của đơn vị",exact:true}).click();await page.getByRole("link",{name:"Điều chỉnh (có lý do)",exact:true}).click();await page.getByRole("textbox",{name:"Số cần bổ sung",exact:true}).fill("4");await reason.fill("Điều chỉnh kế tiếp đã ghi");await page.getByRole("button",{name:"Ghi điều chỉnh",exact:true}).click();await expect(page).toHaveURL(/\/dieu-chinh\/2$/);
 await page.getByRole("main").getByRole("link",{name:"Lịch sử điều chỉnh",exact:true}).click();await page.getByRole("link",{name:"Xem điều chỉnh 1",exact:true}).click();await expect(value()).toHaveText("3");await expect(page.getByRole("button",{name:"Ghi điều chỉnh",exact:true})).toHaveCount(0);
});
