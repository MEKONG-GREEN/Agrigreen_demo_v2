import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";

// Production Next build with the repository's mock provider. Native PG tests
// separately prove persistence and real COMMIT/lost-ACK/rollback boundaries.
test("244 report save exact receipt survives UI remount and renders actual before-after audit",async({page},testInfo)=>{
 test.setTimeout(180000);
 await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
 await page.goto("/login");
 const login=async(identifier:string)=>{await page.getByLabel("Email hoặc số điện thoại").fill(identifier);await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();};
 await login("admin.mekong@mekonggreen.vn");
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toBeVisible();
 await sidebar(page).getByRole("link",{name:"Người dùng",exact:true}).click();
 await page.getByRole("link",{name:"Tạo tài khoản",exact:true}).click();
 const main=page.getByRole("main");
 await main.getByLabel("Họ tên").fill("Cán bộ KHTC receipt244");
 await main.getByLabel("Email").fill("khtc.receipt244@example.test");
 await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345244");
 const assignment=main.getByRole("group",{name:"Vai trò và phạm vi"});
 await assignment.getByRole("combobox",{name:"Vai trò",exact:true}).selectOption("kn_tinh");
 await assignment.getByRole("checkbox",{name:"trong_trot",exact:true}).uncheck();
 await assignment.getByRole("checkbox",{name:"ke_hoach_tai_chinh",exact:true}).check();
 await main.getByRole("button",{name:"Lưu",exact:true}).click();
 await expect(page.getByRole("heading",{level:1,name:"Cán bộ KHTC receipt244"})).toBeVisible();
 await logout(page);await login("0912345244");
 await page.getByRole("button",{name:"Mở kỳ",exact:true}).click();
 await page.getByRole("combobox",{name:"Loại báo cáo",exact:true}).selectOption("month");
 await page.getByRole("combobox",{name:"Số kỳ",exact:true}).selectOption("12");
 await page.getByRole("button",{name:"Xem trước",exact:true}).click();
 await page.getByRole("button",{name:"Mở kỳ và giao phiếu",exact:true}).click();
 const reportLink=sidebar(page).getByRole("link",{name:"Soạn báo cáo (Mẫu 1–5)",exact:true});
 await expect(reportLink).toBeVisible();
 await reportLink.click();
 const period=page.getByRole("button",{name:/^Tháng 12 năm 2030/});await period.click();
 await page.getByLabel("Người ký",{exact:true}).fill("Người ký lần đầu244");
 // Fail local cleanup AFTER the mock's real operation receipt was committed.
 // No context, actor, report data or receipt is injected by the browser test.
 await page.evaluate(()=>{const original=Storage.prototype.removeItem;let fail=true;Storage.prototype.removeItem=function(key){if(fail&&key.startsWith("kn:khtc:report-save:v1:")){fail=false;throw new Error("controlled local cleanup unavailable244");}return original.call(this,key);};});
 await page.getByRole("button",{name:"Lưu bản soạn",exact:true}).click();
 await expect(page.getByRole("button",{name:"Đối chiếu lần lưu",exact:true})).toBeVisible();
 await expect(page.getByRole("button",{name:"Lưu bản soạn",exact:true})).toBeDisabled();
 expect(await page.evaluate(()=>Object.keys(sessionStorage).filter(k=>k.startsWith("kn:khtc:report-save:v1:")).length)).toBe(1);
 await page.screenshot({path:testInfo.outputPath("244-unresolved-local-cleanup.png"),fullPage:true});
 await sidebar(page).getByRole("link",{name:/^Kỳ báo cáo & phiếu các phòng/}).click();
 await reportLink.click();await period.click();
 await expect(page.getByRole("status").filter({hasText:"Đã đối chiếu lần lưu phiên bản 1"})).toBeVisible();
 await expect(page.getByLabel("Người ký",{exact:true})).toHaveValue("Người ký lần đầu244");
 await expect(page.getByRole("button",{name:"Lưu bản soạn",exact:true})).toBeEnabled();
 await page.getByLabel("Người ký",{exact:true}).fill("Người ký lần hai244");
 await page.getByRole("button",{name:"Lưu bản soạn",exact:true}).click();
 await expect(page.getByText(/^Phiên bản soạn 2 ·/)).toBeVisible();
 await page.getByText("Lịch sử lưu nháp",{exact:true}).click();
 const audit=page.locator("details").filter({has:page.locator("summary",{hasText:"Lịch sử lưu nháp"})});
 await expect(audit.getByText("Phiên bản 1",{exact:true})).toBeVisible();
 await expect(audit.getByText("Phiên bản 2",{exact:true})).toBeVisible();
 await expect(audit).toContainText("Người ký lần đầu244");await expect(audit).toContainText("Người ký lần hai244");
 expect(await page.evaluate(()=>Object.keys(sessionStorage).filter(k=>k.startsWith("kn:khtc:report-save:v1:")).length)).toBe(0);
 await page.screenshot({path:testInfo.outputPath("244-save-receipt-audit.png"),fullPage:true});
});
