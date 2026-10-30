import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

async function login(page:Page,identifier:string){
 await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
 await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");
 await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
}

test("HCTH: phát hành mẫu/phiếu → mở đợt → phiếu đóng băng → xem trước MS05 tám cột",async({page})=>{
 test.setTimeout(180_000);
 await page.clock.install({time:new Date("2026-10-12T03:00:00Z")});
 await page.goto("/login");await login(page,"admin.mekong@mekonggreen.vn");
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toContainText("Đang áp dụng");
 await logout(page);await login(page,"hung.tq@angiang.gov.vn");
 await sidebar(page).getByRole("link",{name:"Đánh giá",exact:true}).click();
 await page.getByRole("link",{name:"Mẫu và phiếu đánh giá"}).click();
 for(const n of ["05","06"]){
  await page.getByRole("link",{name:`Tạo Mẫu số ${n}`,exact:true}).click();
  await page.getByRole("button",{name:"Phát hành phiên bản",exact:true}).click();
  await expect(page.getByRole("cell",{name:`Mẫu số ${n} · Tổng hợp kết quả đánh giá`,exact:true})).toBeVisible();
 }
 await page.getByRole("link",{name:"Thiết kế phiếu mới"}).click();
 await page.getByLabel("Điểm tối đa 1",{exact:true}).fill("1e100000");
 await page.getByRole("button",{name:"Phát hành phiên bản",exact:true}).click();
 await expect(page.getByLabel("Điểm tối đa 1",{exact:true})).toHaveAttribute("aria-invalid","true");
 await page.getByLabel("Điểm tối đa 1",{exact:true}).fill("15");
 await page.getByRole("button",{name:"Phát hành phiên bản",exact:true}).click();
 await expect(page.getByRole("cell",{name:"Phiếu đánh giá cán bộ khuyến nông cấp xã",exact:true})).toBeVisible();
 await page.getByRole("link",{name:"Đợt đánh giá",exact:true}).click();
 await page.getByRole("link",{name:"Mở đợt mới",exact:true}).click();
 await page.getByLabel(/^Năm đánh giá/).fill("2026");
 await page.getByLabel("Kỳ",{exact:true}).selectOption("3");
 await page.getByLabel("Ngày chốt danh sách",{exact:true}).fill("2026-09-30");
 await page.getByLabel("Hạn tự đánh giá",{exact:true}).fill("2026-10-05T23:59");
 await page.getByLabel("Hạn cấp quản lý",{exact:true}).fill("2026-10-20T23:59");
 await page.getByLabel("Hạn tổng hợp",{exact:true}).fill("2026-10-25T23:59");
 await page.getByLabel("Phiếu Cán bộ khuyến nông cấp xã",{exact:true}).selectOption({index:1});
 await page.getByLabel("Mẫu MS05",{exact:true}).selectOption({index:1});
 await page.getByLabel("Mẫu MS06",{exact:true}).selectOption({index:1});
 await page.getByRole("checkbox",{name:/Xác nhận cảnh báo hồ sơ/}).check();
 await page.getByRole("button",{name:"Mở đợt và phát hành phiếu",exact:true}).click();
 await page.getByRole("link",{name:"Xem phiếu và tiến độ",exact:true}).click();
 await expect(page).toHaveURL(/\/kn\/nhan-su\/danh-gia\/dot\/[0-9a-f-]{36}$/);
 await page.getByRole("link",{name:"Mở phiếu",exact:true}).click();
 await expect(page.getByRole("heading",{name:"Phiếu đánh giá",exact:true})).toBeVisible();
 await expect(page.getByText(/mẫu v1 \(đóng băng\)/)).toBeVisible();
 await expect(page.getByRole("button",{name:"Nộp phiếu tự đánh giá",exact:true})).toHaveCount(0);
 await page.getByRole("link",{name:"Danh sách phiếu",exact:true}).click();
 await page.getByRole("button",{name:"Xem trước tám cột",exact:true}).click();
 const paper=page.getByRole("table").filter({has:page.getByRole("columnheader",{name:"Cá nhân tự đề xuất mức xếp loại",exact:true})});
 await expect(paper.getByRole("columnheader")).toHaveCount(8);
 await expect(paper).toContainText("XA-00001");
 await page.getByRole("button",{name:"Xuất Excel",exact:true}).click();
 await expect(page.getByRole("alert").filter({hasText:/Tệp xuất giả lập chưa sẵn sàng/})).toBeVisible();
});
