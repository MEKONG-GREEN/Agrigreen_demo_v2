import {expect,test,type Page} from "@playwright/test";
import {readFile} from "node:fs/promises";
import {sidebar,logout} from "./helpers";
// Synthetic fixtures only. Real native commits, hashes and DOCX/XLSX bytes are
// covered by TestW1168* against private PostgreSQL.
async function login(page:Page,identifier:string){
 await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
 await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");
 await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
}
async function adhocMenu(page:Page){
 const folder=sidebar(page).getByRole("button",{name:"Phòng Kế hoạch - Tài chính",exact:true});
 if(await folder.getAttribute("aria-expanded")!=="true")await folder.click();
 await sidebar(page).getByRole("link",{name:"Báo cáo phát sinh",exact:true}).click();
 await expect(page.getByRole("heading",{level:1,name:"Báo cáo phát sinh",exact:true})).toBeVisible();
}
test("W11.68 Admin giao số liệu mới, phòng trả lời, bốn mắt rà soát rồi chọn và xuất",async({page})=>{
 test.setTimeout(180000);page.setDefaultTimeout(20000);
 await page.goto("/login");await login(page,"admin.mekong@mekonggreen.vn");
 await expect(sidebar(page)).toBeVisible();
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toBeVisible();
 await logout(page);await login(page,"admin.mekong@mekonggreen.vn");await adhocMenu(page);
 await expect(page.getByRole("region",{name:"Khôi phục phát hành kỳ"})).toContainText("Không có thao tác phát hành dang dở");
 await page.getByText("Mở kỳ phát sinh và giao phiếu",{exact:true}).click();
 await page.getByRole("button",{name:"Mở kỳ",exact:true}).click();
 const year=new Date().getFullYear(),reportName="Phát sinh browser "+Date.now(),instruction="Bổ sung số lớp đã đối chiếu browser";
 const local=(days:number)=>new Date(Date.now()+days*86400000).toISOString().slice(0,16);
 await page.getByLabel("Tên báo cáo",{exact:true}).fill(reportName);
 await page.getByLabel("Từ ngày",{exact:true}).fill(year+"-01-01");
 await page.getByLabel("Đến ngày phát sinh",{exact:true}).fill(new Date().toISOString().slice(0,10));
 await page.getByLabel("Hạn nộp",{exact:true}).fill(local(7));
 await page.getByLabel("Hạn xuất",{exact:true}).fill(local(8));
 await page.getByRole("button",{name:"Xem trước",exact:true}).click();
 await page.getByRole("button",{name:"Mở kỳ và giao phiếu",exact:true}).click();
 // Composer refreshes from the real successful open action without page reload.
 await page.getByRole("button",{name:reportName,exact:true}).click();
 const requests=page.getByRole("region",{name:"Yêu cầu số liệu bổ sung",exact:true});
 await requests.getByRole("combobox",{name:"Phòng bổ sung",exact:true}).selectOption("trong_trot");
 await requests.getByRole("textbox",{name:"Nội dung yêu cầu",exact:true}).fill(instruction);
 await requests.getByLabel("Hạn bổ sung",{exact:true}).fill(local(5));
 await requests.getByRole("button",{name:"Thêm chỉ tiêu số",exact:true}).click();
 await requests.getByLabel("Mã chỉ tiêu 1",{exact:true}).fill("SO_LOP");
 await requests.getByLabel("Tên chỉ tiêu 1",{exact:true}).fill("Số lớp mới browser");
 await requests.getByLabel("Đơn vị tính 1",{exact:true}).fill("lớp");
 await requests.getByRole("button",{name:"Phát hành yêu cầu",exact:true}).click();
 await expect(requests).toContainText("Đã phát hành");
 await logout(page);await login(page,"binh.tt@angiang.gov.vn");
 await page.getByRole("button",{name:/^Cấp Tỉnh/}).click();
 await page.getByRole("button",{name:/Phòng Trồng trọt — Vào hệ thống/}).click();
 const pnFolder=sidebar(page).getByRole("button",{name:"Báo cáo cho KHTC",exact:true});
 if(await pnFolder.getAttribute("aria-expanded")!=="true")await pnFolder.click();
 await sidebar(page).getByRole("link",{name:"Phiếu kỳ này",exact:true}).click();
 await page.getByRole("link",{name:instruction,exact:true}).click();
 await expect(page.getByRole("button",{name:"Gửi KHTC",exact:true})).toBeDisabled();
 await page.getByRole("button",{name:"Đã đọc yêu cầu — nhập câu trả lời",exact:true}).click();
 await page.getByLabel(/^Số lớp mới browser/).fill("12,5");
 await page.getByLabel("Nội dung trả lời bổ sung",{exact:true}).fill("Đã kiểm tra danh sách và số liệu.");
 await page.getByRole("button",{name:"Gửi KHTC",exact:true}).click();
 await expect(page.getByRole("status").filter({hasText:"Đã gửi KHTC lần 1"})).toBeVisible();
 await logout(page);await login(page,"admin.mekong@mekonggreen.vn");await adhocMenu(page);
 await page.getByText("Mở kỳ phát sinh và giao phiếu",{exact:true}).click();
 await page.getByRole("row").filter({has:page.getByRole("cell",{name:reportName,exact:true})}).click();
 await page.getByRole("link",{name:"Rà soát",exact:true}).first().click();
 await expect(page.getByLabel(/^Số lớp mới browser/)).toBeDisabled();
 await page.getByRole("button",{name:"Xác nhận",exact:true}).click();
 await expect(page.getByRole("status").filter({hasText:"Đã xác nhận phiếu"})).toBeVisible();
 await adhocMenu(page);await page.getByRole("button",{name:reportName,exact:true}).click();
 // The synthetic source deliberately advances once after its first snapshot;
 // follow the visible update action again, as an operator would after that alert.
 await page.getByRole("button",{name:"Cập nhật số liệu",exact:true}).click();
 await expect(page.getByText(/^Phiên bản soạn 1 ·/)).toBeVisible();
 await page.getByRole("button",{name:"Cập nhật số liệu",exact:true}).click();
 await expect(page.getByText(/^Phiên bản soạn 2 ·/)).toBeVisible();
 await expect(page.getByText("Nguồn đã thay đổi. Cập nhật số liệu trước khi đánh dấu Bản cuối. Các tệp đã xuất được giữ nguyên.",{exact:true})).not.toBeVisible();
 await page.getByLabel("Chọn Số lớp mới browser",{exact:true}).check();
 await page.getByLabel(/^noi_dung /).fill("Nội dung báo cáo browser đã soạn.");
 await page.getByRole("button",{name:"Lưu bản soạn",exact:true}).click();
 await expect(page.getByRole("status").filter({hasText:"Đã lưu bản soạn"})).toBeVisible();
 const provisional=page.getByLabel("Tôi xác nhận xuất bản Tạm tính",{exact:true});
 if(await provisional.count())await provisional.check();
 const excelWait=page.waitForEvent("download");
 await page.getByRole("button",{name:"Xuất Excel",exact:true}).click();const excel=await excelWait;
 const file=await excel.path();expect(file).toBeTruthy();const csv=await readFile(file!,"utf8");
 expect(csv).toContain("Số lớp mới browser");expect(csv).toContain("12.5");expect(csv).toContain("Nội dung báo cáo browser đã soạn");
 const wordWait=page.waitForEvent("download");await page.getByRole("button",{name:"Xuất Word v1",exact:true}).click();
 const word=await wordWait;expect(await word.path()).toBeTruthy();
 await page.screenshot({path:test.info().outputPath("w1168-exported.png"),fullPage:true});
});
