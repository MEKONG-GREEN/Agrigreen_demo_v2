import {expect,test} from "@playwright/test";
import {sidebar,logout} from "./helpers";

test("Dashboard189 phản ánh kỳ phát sinh vừa mở bằng phiên KHTC thật",async({page})=>{
 test.setTimeout(180000);page.setDefaultTimeout(20000);
 await page.clock.install({time:new Date("2026-10-20T03:00:00Z")});
 const login=async(identifier="admin.mekong@mekonggreen.vn")=>{await page.getByLabel("Email hoặc số điện thoại").fill(identifier);await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();await expect(sidebar(page)).toBeVisible();};
 await page.goto("/login");await login();
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính",{exact:true}).selectOption("active");await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toBeVisible();
 await sidebar(page).getByRole("link",{name:"Người dùng",exact:true}).click();
 await page.getByRole("link",{name:"Tạo tài khoản",exact:true}).click();
 const main=page.getByRole("main");await expect(main.getByRole("button",{name:"Lưu",exact:true})).toBeEnabled();await main.getByLabel("Họ tên").fill("Cán bộ dashboard189");await main.getByLabel("Email").fill("dashboard189@example.test");await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345689");
 const assignment=main.getByRole("group",{name:"Vai trò và phạm vi"});await assignment.getByRole("combobox",{name:"Vai trò",exact:true}).selectOption("kn_tinh");await assignment.getByRole("checkbox",{name:"trong_trot",exact:true}).uncheck();await assignment.getByRole("checkbox",{name:"ke_hoach_tai_chinh",exact:true}).check();await main.getByLabel("Họ tên").fill("Cán bộ dashboard189");await expect(main.getByLabel("Họ tên")).toHaveValue("Cán bộ dashboard189");await main.getByRole("button",{name:"Lưu",exact:true}).click();await expect(page.getByRole("heading",{level:1,name:"Cán bộ dashboard189"})).toBeVisible();
 await logout(page);await login("0912345689");
 await sidebar(page).getByRole("link",{name:"Trang chủ phòng",exact:true}).click();await expect(page.getByRole("heading",{name:"Trang chủ tổng hợp KHTC",exact:true})).toBeVisible();
 const count=page.getByRole("link",{name:/^Kỳ đang mở/}).locator("strong");await expect(count).toHaveText(/^\d+$/);const before=Number(await count.innerText());

 await sidebar(page).getByRole("link",{name:"Báo cáo phát sinh",exact:true}).click();
 await page.getByText("Mở kỳ phát sinh và giao phiếu",{exact:true}).click();await page.getByRole("button",{name:"Mở kỳ",exact:true}).click();
 await page.getByLabel("Tên báo cáo",{exact:true}).fill("Kỳ dashboard189 vừa mở");await page.getByLabel("Từ ngày",{exact:true}).fill("2026-10-01");await page.getByLabel("Đến ngày phát sinh",{exact:true}).fill("2026-10-20");
 await page.getByLabel("Hạn nộp",{exact:true}).fill("2026-10-27T10:00");await page.getByLabel("Hạn xuất",{exact:true}).fill("2026-10-28T16:00");await page.getByRole("button",{name:"Xem trước",exact:true}).click();await page.getByRole("button",{name:"Mở kỳ và giao phiếu",exact:true}).click();
 await expect(page.getByRole("button",{name:"Kỳ dashboard189 vừa mở",exact:true})).toBeVisible();
 // Navigate inside the SPA: mock ledger state belongs to this browser session.
 await sidebar(page).getByRole("link",{name:"Trang chủ phòng",exact:true}).click();
 await expect(page.getByRole("heading",{name:"Trang chủ tổng hợp KHTC",exact:true})).toBeVisible();await expect(count).toHaveText(String(before+1));
 await expect(page.getByRole("table",{name:"Phiếu và nguồn",exact:true})).toContainText("Kỳ dashboard189 vừa mở");
 await page.getByRole("combobox",{name:"Phòng",exact:true}).selectOption("hanh_chinh_tong_hop");const filtered=page.getByRole("table",{name:"Phiếu và nguồn",exact:true}).getByRole("row").filter({hasText:"Kỳ dashboard189 vừa mở"});await expect(filtered).toHaveCount(1);await expect(filtered).toContainText("Hành chính");
});
