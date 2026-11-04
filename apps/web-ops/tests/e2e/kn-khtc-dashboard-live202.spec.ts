import {test,expect,type Page} from "@playwright/test";
import {sidebar,logout} from "./helpers";
import {MOCK_KN_PASSWORD,findKnAccount} from "../../src/api/mock/kn-accounts";

const endpoint="/v1/extension/khtc/dashboard";

async function normalKhtc202(page:Page) {
 const admin=findKnAccount("kn-admin")!;
 const identifier="dashboard202@example.test";
 const login=async(who:string)=>{
  await page.getByLabel("Email hoặc số điện thoại").fill(who);
  await page.getByLabel("Mật khẩu",{exact:true}).fill(MOCK_KN_PASSWORD);
  await page.getByRole("button",{name:"Đăng nhập",exact:true}).click();
  await expect(sidebar(page)).toBeVisible();
 };
 await page.goto("/login");await login(admin.identifiers[0]!);
 await sidebar(page).getByRole("link",{name:"Phòng ban",exact:true}).click();
 await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính",{exact:true}).selectOption("active");
 await expect(page.getByRole("status").filter({hasText:"Đã đổi"})).toBeVisible();
 await sidebar(page).getByRole("link",{name:"Người dùng",exact:true}).click();
 await page.getByRole("link",{name:"Tạo tài khoản",exact:true}).click();
 const main=page.getByRole("main");
 await main.getByLabel("Họ tên").fill("Cán bộ dashboard202");
 await main.getByLabel("Email").fill(identifier);
 await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912020202");
 const assignment=main.getByRole("group",{name:"Vai trò và phạm vi"});
 await assignment.getByRole("combobox",{name:"Vai trò",exact:true}).selectOption("kn_tinh");
 await assignment.getByRole("checkbox",{name:"trong_trot",exact:true}).uncheck();
 await assignment.getByRole("checkbox",{name:"ke_hoach_tai_chinh",exact:true}).check();
 await main.getByLabel("Họ tên").fill("Cán bộ dashboard202");
 await expect(main.getByLabel("Họ tên")).toHaveValue("Cán bộ dashboard202");
 await main.getByRole("button",{name:"Lưu",exact:true}).click();
 await expect(page.getByRole("heading",{level:1,name:"Cán bộ dashboard202"})).toBeVisible();
 await logout(page);await login(identifier);
}

type DashboardRequest202={method:string;path:string;query:string};
declare global {interface Window {__dashboardRequests202:DashboardRequest202[]}}
async function observeDashboard202(page:Page) {
 await page.addInitScript(()=>{
  const events:{method:string;path:string;query:string}[]=[];
  Object.defineProperty(window,"__dashboardRequests202",{value:events});
  const NativeRequest=window.Request;
  window.Request=class extends NativeRequest {
   constructor(input:RequestInfo|URL,init?:RequestInit) {
    super(input,init);
    const url=new URL(this.url);
    // Passive construction metadata only. Never inspect headers or payload.
    if(url.pathname.includes("/khtc/"))events.push({method:this.method,path:url.pathname,query:url.searchParams.toString()});
   }
  };
 });
}
async function dashboardReads202(page:Page) {
 return page.evaluate(path=>window.__dashboardRequests202.filter(r=>r.path===path&&r.method==="GET"),endpoint);
}
async function changedDashboard202(page:Page,action:()=>Promise<unknown>,query:Record<string,string|null>) {
 const before=(await dashboardReads202(page)).length;
 await action();
 await expect.poll(async()=> (await dashboardReads202(page)).slice(before).some(r=>Object.entries(query).every(([key,value])=>new URLSearchParams(r.query).get(key)===value))).toBe(true);
 await expect(page.getByRole("button",{name:"Làm mới",exact:true})).toBeEnabled();
}
for(const clock of ["2026-10-13T03:00:00Z","2026-10-13T16:59:59Z"]) {
 test(`Dashboard202 cửa sổ thật không mở Mẫu5 (${clock})`,async({page},info)=>{
  test.setTimeout(180000);
  await page.clock.setFixedTime(new Date(clock));
  await observeDashboard202(page);
  await normalKhtc202(page);
  // The read-only observation window starts after legitimate account/setup writes.
  await page.evaluate(()=>{window.__dashboardRequests202.length=0;});
  await sidebar(page).getByRole("link",{name:"Trang chủ phòng",exact:true}).click();
  await expect(page.getByRole("heading",{name:"Trang chủ tổng hợp KHTC",exact:true})).toBeVisible();
  // Mock transport is in-process. Observe native Request construction, then
  // actual UI completion; browser network responses cannot witness this GET.
  await changedDashboard202(page,()=>page.getByRole("button",{name:"Làm mới",exact:true}).click(),{year:"2026",quarter:"4",through_date:null});
  const live=page.getByText(/^Mẫu 5 trực tiếp/);
  const readTime=clock.endsWith("16:59:59Z")?"23:59":"10:00";
  await expect(live).toContainText("Yêu cầu đến 2026-10-13");
  await expect(live).toContainText("Số liệu tuần đến 2026-10-13");
  await expect(live).toContainText(`Mốc thực tế 13/10/2026 ${readTime}`);
  await expect(live).toContainText(`Đọc lúc 13/10/2026 ${readTime}`);
  await expect(page.getByText(/\d+\/\d+ hạng mục có số liệu/).first()).toBeVisible();
  await expect(page.getByText("Chưa mở kỳ báo cáo quý hoặc năm.")).toBeVisible();
  await expect(page.getByText(/^Kỳ Mẫu 5 đã mở:/)).toHaveCount(0);
  await expect(page.getByRole("link",{name:"Mở kỳ mới",exact:true})).toBeVisible();
  await changedDashboard202(page,()=>page.getByLabel("Đến ngày",{exact:true}).fill("2026-12-31"),{year:"2026",through_date:"2026-12-31"});
  await expect(live).toContainText("Yêu cầu đến 2026-12-31");
  await expect(live).toContainText("Số liệu tuần đến 2026-10-13");
  await expect(live).toContainText(`Mốc thực tế 13/10/2026 ${readTime}`);
  await expect(page.getByText(/^Kỳ Mẫu 5 đã mở:/)).toHaveCount(0);
  await changedDashboard202(page,()=>page.getByLabel("Đến ngày",{exact:true}).fill("2026-10-06"),{through_date:"2026-10-06"});
  await expect(live).toContainText("Số liệu tuần đến 2026-10-06");
  await expect(live).toContainText("Mốc thực tế 06/10/2026 23:59");
  await changedDashboard202(page,()=>page.getByRole("combobox",{name:"Năm",exact:true}).selectOption("2027"),{year:"2027",through_date:null});
  await expect(page.getByLabel("Đến ngày",{exact:true})).toHaveValue("");
  await expect(page.getByText("Chưa có tuần báo cáo đủ kết thúc trong năm, quý đã chọn.")).toBeVisible();
  await expect(page.getByText(/\d+\/\d+ hạng mục có số liệu/)).toHaveCount(0);
  await expect(page.getByText(/^Chưa có Kế hoạch năm hiệu lực/)).toHaveCount(0);
  await changedDashboard202(page,()=>page.getByRole("combobox",{name:"Năm",exact:true}).selectOption("2026"),{year:"2026",through_date:null});
  await expect(live).toContainText("Yêu cầu đến 2026-10-13");
  if(clock.endsWith("16:59:59Z")) {
   await page.clock.setFixedTime(new Date(Date.parse(clock)+2000));
   await changedDashboard202(page,()=>page.getByRole("button",{name:"Làm mới",exact:true}).click(),{year:"2026",quarter:"4"});
   await expect(live).toContainText("Yêu cầu đến 2026-10-14");
   await expect(live).toContainText("Số liệu tuần đến 2026-10-13");
   await expect(live).toContainText("Mốc thực tế 14/10/2026 00:00");
   await expect(live).toContainText("Đọc lúc 14/10/2026 00:00");
  }
  await page.screenshot({path:info.outputPath("dashboard-live202.png"),fullPage:true});
  expect((await dashboardReads202(page)).length).toBeGreaterThan(0);
  const writes=await page.evaluate(()=>window.__dashboardRequests202.filter(r=>r.method!=="GET"));
  expect(writes).toEqual([]);
 });
}
