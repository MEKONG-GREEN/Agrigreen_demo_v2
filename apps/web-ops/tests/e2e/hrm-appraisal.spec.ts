import { expect, test, type Page } from "@playwright/test";
import { logout, sidebar } from "./helpers";

// Synthetic UI fixtures. Native tests cover committed ledger, real XLSX/DOCX,
// signed files, RLS, legacy hashes, and the absence of annual salary ratings.
async function login(page:Page,identifier:string){
 await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
 await page.getByLabel("Mật khẩu",{exact:true}).fill("KhuyenNong@2026");
 await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
}
async function appraisalMenu(page:Page,name="Đánh giá"){
 await sidebar(page).getByRole("link",{name,exact:true}).click();
 await expect(page.getByRole("heading",{name:"Đợt đánh giá",exact:true})).toBeVisible();
}
async function period(page:Page){await page.getByRole("link",{name:"Xem đề xuất và tiến độ",exact:true}).click();}
async function proposal(page:Page,name:string){
 await page.getByRole("row").filter({has:page.getByRole("cell",{name,exact:true})}).getByRole("link",{name:"Xem đề xuất",exact:true}).click();
}
async function screenshot(page:Page,name:string){
 if(process.env.AG_281_ARTIFACT_DIR)await page.screenshot({path:`${process.env.AG_281_ARTIFACT_DIR}/281-${name}.png`,fullPage:true});
}

test("HC2: lãnh đạo nhập DG, HCTH trả điều chỉnh, chốt hồ sơ và xem biểu tám cột",async({page})=>{
 test.setTimeout(240_000);
 await page.clock.install({time:new Date("2026-10-12T03:00:00Z")});
 await page.goto("/login");await login(page,"admin.mekong@mekonggreen.vn");
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Hành chính - Tổng hợp",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toContainText("Đang áp dụng");
 await logout(page);await login(page,"hung.tq@angiang.gov.vn");await appraisalMenu(page);
 await page.getByRole("link",{name:"Mẫu số 05, 06",exact:true}).click();
 await expect(page.getByRole("link",{name:"Thiết kế phiếu mới",exact:true})).toHaveCount(0);
 for(const n of ["05","06"]){
  await page.getByRole("link",{name:`Tạo Mẫu số ${n}`,exact:true}).click();
  await expect(page.getByRole("columnheader")).toHaveCount(8);
  await page.getByRole("button",{name:"Phát hành phiên bản",exact:true}).click();
  await expect(page.getByRole("cell",{name:`Mẫu số ${n} · Tổng hợp kết quả đánh giá`,exact:true})).toBeVisible();
 }
 await page.getByRole("link",{name:"Đợt đánh giá",exact:true}).click();
 await page.getByRole("link",{name:"Mở đợt mới",exact:true}).click();
 await page.getByLabel(/^Năm đánh giá/).fill("2026");await page.getByLabel("Kỳ",{exact:true}).selectOption("3");
 await page.getByLabel("Ngày chốt danh sách",{exact:true}).fill("2026-09-30");
 await page.getByLabel("Hạn gửi Phòng HCTH",{exact:true}).fill("2026-10-20T23:59");
 await expect(page.getByLabel("Hạn tự đánh giá",{exact:true})).toHaveCount(0);
 await page.getByLabel("Mẫu MS05",{exact:true}).selectOption({index:1});
 await page.getByLabel("Mẫu MS06",{exact:true}).selectOption({index:1});
 await page.getByRole("checkbox",{name:/Xác nhận cảnh báo thiếu chức vụ/}).check();
 await page.getByRole("button",{name:"Mở đợt và giao các phòng nhập đề xuất",exact:true}).click();await period(page);
 await expect(page.getByRole("table").filter({has:page.getByRole("columnheader",{name:"Người được đánh giá",exact:true})}).getByRole("row")).toHaveCount(4);
 await page.getByRole("button",{name:"Xuất Excel",exact:true}).click();
 await expect(page.getByRole("alert").filter({hasText:/Còn người chưa nhập hoặc thiếu căn cứ/})).toBeVisible();
 await proposal(page,"Trần Văn Thử");
 await page.getByLabel("Cá nhân tự đề xuất",{exact:true}).selectOption("HTTNV");
 await page.getByLabel("Tập thể đề xuất",{exact:true}).selectOption("HTTNV");
 await page.getByRole("button",{name:"Lưu đề xuất",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Đã nhập");
 await page.getByRole("button",{name:"Gửi Phòng HCTH",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Đã gửi HCTH");
 await logout(page);await login(page,"binh.tt@angiang.gov.vn");
 await page.getByRole("button",{name:/^Cấp Tỉnh/}).click();
 await page.getByRole("button",{name:/Phòng Trồng trọt — Vào hệ thống/}).click();
 await appraisalMenu(page,"Đề xuất xếp loại");await expect(page.getByRole("link",{name:"Mở đợt mới",exact:true})).toHaveCount(0);await period(page);
 await expect(page.getByText("Trần Văn Thử",{exact:true})).toHaveCount(0);
 await proposal(page,"Nguyễn Thị Mẫu");
 await page.getByLabel("Cá nhân tự đề xuất",{exact:true}).selectOption("HTXSNV");
 await page.getByLabel("Tập thể đề xuất",{exact:true}).selectOption("HTTNV");
 await page.getByRole("button",{name:"Lưu đề xuất",exact:true}).click();
 await expect(page.getByLabel("Tóm tắt căn cứ, cơ sở lý do",{exact:true})).toHaveAttribute("aria-invalid","true");
 await page.getByLabel("Tóm tắt căn cứ, cơ sở lý do",{exact:true}).fill("Đã đối chiếu kết quả công tác và biên bản tập thể.");
 await page.getByRole("button",{name:"Lưu đề xuất",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Đã nhập");
 await page.getByRole("link",{name:"Danh sách đề xuất",exact:true}).click();
 await proposal(page,"Lê Thị Giả Lập");
 await page.getByLabel("Cá nhân tự đề xuất",{exact:true}).selectOption("HTNV");
 await page.getByLabel("Tập thể đề xuất",{exact:true}).selectOption("HTNV");
 await page.getByRole("button",{name:"Lưu đề xuất",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Đã nhập");
 await page.getByRole("link",{name:"Danh sách đề xuất",exact:true}).click();
 await page.getByRole("button",{name:"Gửi Phòng HCTH",exact:true}).click();
 await expect(page.getByText("Đã gửi HCTH: 2",{exact:true})).toBeVisible();await screenshot(page,"manager-ms05-hs37");
 await logout(page);await login(page,"hung.tq@angiang.gov.vn");await appraisalMenu(page);await period(page);
 await proposal(page,"Nguyễn Thị Mẫu");
 await page.getByLabel("Lý do điều chỉnh hoặc mở lại",{exact:true}).fill("Đề nghị đối chiếu lại biên bản tập thể trước khi chốt.");
 await page.getByRole("button",{name:"Yêu cầu đơn vị điều chỉnh",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Yêu cầu điều chỉnh");
 await logout(page);await login(page,"binh.tt@angiang.gov.vn");
 await page.getByRole("button",{name:/^Cấp Tỉnh/}).click();await page.getByRole("button",{name:/Phòng Trồng trọt — Vào hệ thống/}).click();
 await appraisalMenu(page,"Đề xuất xếp loại");await period(page);await proposal(page,"Nguyễn Thị Mẫu");
 await page.getByLabel("Tóm tắt căn cứ, cơ sở lý do",{exact:true}).fill("Đã bổ sung đối chiếu biên bản tập thể và kết quả công tác.");
 await page.getByRole("button",{name:"Lưu đề xuất",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Đã nhập");
 await page.getByRole("button",{name:"Gửi Phòng HCTH",exact:true}).click();
 await expect(page.getByRole("heading",{level:1})).toContainText("Đã gửi HCTH");
 await logout(page);await login(page,"hung.tq@angiang.gov.vn");await appraisalMenu(page);await period(page);
 await page.getByRole("button",{name:"Xem trước tám cột",exact:true}).click();
 const paper=page.getByRole("table").filter({has:page.getByRole("columnheader",{name:"Cá nhân tự đề xuất mức xếp loại",exact:true})});
 await expect(paper.getByRole("columnheader")).toHaveCount(8);await expect(paper).toContainText("Lê Thị Giả Lập");
 await expect(paper).toContainText("Đã bổ sung đối chiếu");await screenshot(page,"hcth-ms05-preview");
 await page.getByRole("button",{name:"Chốt kết quả đợt",exact:true}).click();
 await expect(page.getByRole("alert").filter({hasText:/Cần xác nhận thiếu bản ký/})).toBeVisible();
 await page.getByRole("checkbox",{name:/Tôi xác nhận cảnh báo/}).check();
 await page.getByRole("button",{name:"Chốt kết quả đợt",exact:true}).click();
 await expect(page.getByText("Đã chốt: 3",{exact:true})).toBeVisible();await screenshot(page,"closed-profile-only");
 await sidebar(page).getByRole("link",{name:"Hồ sơ nhân sự",exact:true}).click();
 await page.getByRole("link",{name:"VC-00001",exact:true}).click();
 await page.getByRole("tab",{name:"Kết quả đánh giá",exact:true}).click();
 const result=page.getByRole("table",{name:"Kết quả xếp loại trong hồ sơ"});
 await expect(result).toContainText("Quý III/2026");await expect(result).toContainText("Hoàn thành tốt nhiệm vụ");await screenshot(page,"profile-result");
});

test("HC2: cá nhân không có menu tự đánh giá và các URL writer cũ trả404",async({page})=>{
 await page.goto("/login");await login(page,"an.nv@angiang.gov.vn");
 await page.getByRole("button",{name:/^Cấp Xã/}).click();
 await page.getByRole("button",{name:/Phòng Trồng trọt — Vào hệ thống/}).click();
 await expect(sidebar(page)).toBeVisible();
 await expect(sidebar(page).getByRole("link",{name:/Đề xuất xếp loại|Đánh giá của tôi/})).toHaveCount(0);
 await screenshot(page,"personal-no-assessment");
 const base="/kn/nhan-su/danh-gia-cua-toi",period="019abcde-0000-7000-8000-000000000001",staff="019abcde-0000-7000-8000-000000000002";
 for(const path of [base,`${base}/dot/${period}`,`${base}/dot/${period}/phieu/${staff}`,`/kn/nhan-su/danh-gia/dot/${period}/phieu/${staff}`]){
  const response=await page.goto(path);expect(response?.status(),path).toBe(404);
  await expect(page.getByRole("button",{name:"Nộp phiếu tự đánh giá",exact:true})).toHaveCount(0);
 }
});

test("HC2: truy vấn phiếu chấm cũ kích hoạt server notFound",async({request})=>{
 // Next16 retains status200 after the parent layout has streamed. Verify the
 // server404 fallback and noindex directly, independently of menu permission.
 const response=await request.get("/kn/nhan-su/danh-gia/mau/them-moi?kind=form");
 expect(response.status()).toBe(200);
 const html=await response.text();
 expect(html).toContain("NEXT_HTTP_ERROR_FALLBACK;404");
 expect(html).toContain("noindex");
 expect(html).toContain("Không tìm thấy trang");
 expect(html).not.toContain("Nội dung biểu tổng hợp");
});
