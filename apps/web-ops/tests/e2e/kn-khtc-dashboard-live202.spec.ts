import {test,expect,type Page} from "@playwright/test";
import {sidebar,logout} from "./helpers";
import {MOCK_KN_PASSWORD,findKnAccount} from "../../src/api/mock/kn-accounts";
import type {components} from "../../src/api/generated/application";

type Dashboard=components["schemas"]["KHTCDashboard"];
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
 await main.getByRole("button",{name:"Lưu",exact:true}).click();
 await expect(page.getByRole("heading",{level:1,name:"Cán bộ dashboard202"})).toBeVisible();
 await logout(page);await login(identifier);
}

for(const clock of ["2026-10-13T03:00:00Z","2026-10-13T16:59:59Z"]) {
 test(`Dashboard202 cửa sổ thật không mở Mẫu5 (${clock})`,async({page},info)=>{
  test.setTimeout(180000);
  await page.clock.install({time:new Date(clock)});
  await normalKhtc202(page);
  const writes:string[]=[];
  page.on("request",request=>{const u=new URL(request.url());if(u.pathname.includes("/khtc/")&&request.method()!=="GET")writes.push(request.method()+" "+u.pathname);});
  const next=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint&&r.request().method()==="GET");
  await sidebar(page).getByRole("link",{name:"Trang chủ phòng",exact:true}).click();
  const initial=await (await next).json() as Dashboard;
  await expect(page.getByRole("heading",{name:"Trang chủ tổng hợp KHTC",exact:true})).toBeVisible();
  expect(initial.progress_period).toBeNull();
  expect(initial.progress_window).toMatchObject({through_date:"2026-10-13",quantity_to:"2026-10-13",calendar_version:2});
  expect(initial.progress.rows.length).toBeGreaterThan(0);
  await expect(page.getByText(/Mẫu 5 trực tiếp/)).toBeVisible();
  await expect(page.getByRole("link",{name:"Mở kỳ mới",exact:true})).toBeVisible();
  const future=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint&&new URL(r.url()).searchParams.get("through_date")==="2026-12-31");
  await page.getByLabel("Đến ngày",{exact:true}).fill("2026-12-31");
  const f=await(await future).json() as Dashboard;
  expect(f.progress_window?.through_date).toBe("2026-12-31");
  expect(f.progress_window?.quantity_to).toBe("2026-10-13");
  expect(Date.parse(f.progress_window!.effective_cutoff)).toBeLessThanOrEqual(Date.parse(f.as_of));
  expect(f.progress_period).toBeNull();
  const past=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint&&new URL(r.url()).searchParams.get("through_date")==="2026-10-06");
  await page.getByLabel("Đến ngày",{exact:true}).fill("2026-10-06");
  expect((await(await past).json() as Dashboard).progress_window?.quantity_to).toBe("2026-10-06");
  const futureYear=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint&&new URL(r.url()).searchParams.get("year")==="2027");
  await page.getByRole("combobox",{name:"Năm",exact:true}).selectOption("2027");
  const futureData=await(await futureYear).json() as Dashboard;
  expect(futureData.progress_window).toBeNull();expect(futureData.progress.rows).toEqual([]);
  await expect(page.getByLabel("Đến ngày",{exact:true})).toHaveValue("");
  await expect(page.getByText("Chưa có tuần báo cáo đủ kết thúc trong năm, quý đã chọn.")).toBeVisible();
  const current=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint&&new URL(r.url()).searchParams.get("year")==="2026");
  await page.getByRole("combobox",{name:"Năm",exact:true}).selectOption("2026");await current;
  if(clock.endsWith("16:59:59Z")) {
   await page.clock.runFor(2000);
   const refreshed=page.waitForResponse(r=>new URL(r.url()).pathname===endpoint);
   await page.getByRole("button",{name:"Làm mới",exact:true}).click();
   const d=await(await refreshed).json() as Dashboard;
   expect(d.progress_window?.through_date).toBe("2026-10-14");
   expect(d.progress_window?.quantity_to).toBe("2026-10-13");
  }
  await page.screenshot({path:info.outputPath("dashboard-live202.png"),fullPage:true});
  expect(writes).toEqual([]);
 });
}
