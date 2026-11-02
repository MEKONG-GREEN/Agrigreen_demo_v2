import {fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,beforeEach,describe,expect,it,vi} from "vitest";
import {createApiClient,type ApiClient} from "@/api/client";
import {KhtcArchiveScreen} from "./KhtcArchiveScreen";

const auth:{api:ApiClient|null}={api:null};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:auth.api})}));
const requests:URL[]=[];
const a="01900000-0000-7000-8000-000000000001", b="01900000-0000-7000-8000-000000000002";
let denyChoices=false;
beforeEach(()=>{
 requests.length=0;denyChoices=false;
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
 auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.tw",getContextId:()=>"test",fetch:async(req)=>{
  const u=new URL((req as Request).url);requests.push(u);
  if(u.pathname.endsWith("/exported-reports/periods")) {
   if(denyChoices)return Response.json({type:"about:blank",title:"Không có quyền",status:403,code:"forbidden"},{status:403});
   const next=u.searchParams.get("offset")==="20";
   return Response.json({items:[{id:next?b:a,name:next?"Báo cáo chuyên đề ngoài trang đầu":"Tuần 41",report_type:"week"}],has_more:!next,offset:next?20:0});
  }
  if(u.pathname.endsWith("/exported-reports"))return Response.json({items:[{id:a,period_id:a,period_name:"Tuần 41",report_type:"week",document_no:"12/BC",version:1,final:false,provisional:true,actor_ref:a,at:"2030-01-01T00:00:00Z"}],has_more:true,offset:Number(u.searchParams.get("offset")??0)});
  return Response.json({code:"unexpected",title:"Unexpected"},{status:500});
 }});
});
afterEach(()=>vi.useRealTimers());
const lastList=()=>requests.filter(u=>u.pathname.endsWith("/exported-reports")).at(-1)!;
const lastChoices=()=>requests.filter(u=>u.pathname.endsWith("/exported-reports/periods")).at(-1)!;
async function chooseDistantPeriod(){
 fireEvent.click(screen.getByRole("button",{name:"Chọn kỳ"}));
 const picker=await screen.findByRole("region",{name:"Chọn kỳ đã xuất"});
 await within(picker).findByRole("button",{name:"Lọc kỳ Tuần 41"});
 fireEvent.click(within(picker).getByRole("button",{name:"Kỳ tiếp"}));
 fireEvent.click(await within(picker).findByRole("button",{name:"Lọc kỳ Báo cáo chuyên đề ngoài trang đầu"}));
 await waitFor(()=>expect(lastList().searchParams.get("period_id")).toBe(b));
}
describe("Archive190 complete period picker",()=>{
 it("chooses a named period beyond the first export page and resets export pagination",async()=>{
  render(<KhtcArchiveScreen/>);await screen.findByText("12/BC");
  fireEvent.click(screen.getByRole("button",{name:"Tiếp"}));
  await waitFor(()=>expect(lastList().searchParams.get("offset")).toBe("20"));
  await chooseDistantPeriod();
  expect(lastChoices().searchParams.get("offset")).toBe("20");
  expect(lastList().searchParams.get("offset")).toBe("0");
  expect(screen.getByText("Kỳ: Báo cáo chuyên đề ngoài trang đầu")).toBeInTheDocument();
  expect(screen.queryByRole("region",{name:"Chọn kỳ đã xuất"})).not.toBeInTheDocument();
  expect(requests.some(u=>/\/periods$/.test(u.pathname)&&!u.pathname.includes("exported-reports"))).toBe(false);
 });
 it("searches period names independently and resets picker pagination",async()=>{
  render(<KhtcArchiveScreen/>);await screen.findByText("12/BC");
  fireEvent.click(screen.getByRole("button",{name:"Chọn kỳ"}));
  const picker=await screen.findByRole("region",{name:"Chọn kỳ đã xuất"});
  await within(picker).findByRole("button",{name:"Lọc kỳ Tuần 41"});
  fireEvent.click(within(picker).getByRole("button",{name:"Kỳ tiếp"}));
  await within(picker).findByText("Báo cáo chuyên đề ngoài trang đầu");
  fireEvent.change(within(picker).getByLabelText("Tìm tên kỳ"),{target:{value:"  Tuần 41  "}});
  expect(within(picker).getByLabelText("Tìm tên kỳ")).toHaveAttribute("maxlength","200");
  fireEvent.click(within(picker).getByRole("button",{name:"Tìm kỳ"}));
  await waitFor(()=>expect(lastChoices().searchParams.get("q")).toBe("Tuần 41"));
  expect(lastChoices().searchParams.get("offset")).toBe("0");
  expect(lastChoices().searchParams.get("year")).toBe("2030");
  expect(lastList().searchParams.get("q")).toBeNull();
 });
 it("clears the selected period when year or kind changes, without stale ZIP selection",async()=>{
  render(<KhtcArchiveScreen/>);await screen.findByText("12/BC");
  fireEvent.click(screen.getByRole("checkbox",{name:"Chọn Tuần 41 v1"}));
  await chooseDistantPeriod();
  expect(screen.getByRole("button",{name:"Tải ZIP Excel"})).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Năm"),{target:{value:"2029"}});
  await waitFor(()=>expect(lastList().searchParams.get("year")).toBe("2029"));
  expect(lastList().searchParams.has("period_id")).toBe(false);
  expect(screen.getByText("Kỳ: Tất cả")).toBeInTheDocument();
  await chooseDistantPeriod();
  fireEvent.change(screen.getByLabelText("Loại báo cáo"),{target:{value:"month"}});
  await waitFor(()=>expect(lastList().searchParams.get("kind")).toBe("month"));
  expect(lastList().searchParams.has("period_id")).toBe(false);
 });
 it("removes only the period filter and preserves other archive filters",async()=>{
  render(<KhtcArchiveScreen/>);await screen.findByText("12/BC");
  fireEvent.click(screen.getByLabelText("Mọi phiên bản"));
  await chooseDistantPeriod();
  fireEvent.click(screen.getByRole("button",{name:"Bỏ lọc kỳ"}));
  await waitFor(()=>expect(lastList().searchParams.has("period_id")).toBe(false));
  expect(lastList().searchParams.get("versions")).toBe("true");
 });
 it("fails closed when period lookup is denied",async()=>{
  denyChoices=true;
  render(<KhtcArchiveScreen/>);await screen.findByText("12/BC");
  fireEvent.click(screen.getByRole("button",{name:"Chọn kỳ"}));
  const picker=await screen.findByRole("region",{name:"Chọn kỳ đã xuất"});
  expect(await within(picker).findByText("Bạn không có quyền thực hiện thao tác này.")).toBeInTheDocument();
  expect(within(picker).queryByRole("table")).not.toBeInTheDocument();
  expect(lastList().searchParams.has("period_id")).toBe(false);
 });
});
