import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetKhtcMock } from "@/api/mock/kn-khtc";
import { createKhtcApi } from "../api";
import { PlanWorkspace } from "./PlanWorkspace";
import { PLAN_BASE, planHref } from "./routes";
const nav=vi.hoisted(()=>({path:"/kn/khtc/ke-hoach-nam",search:"year=2030",push:vi.fn()}));
vi.mock("next/navigation",()=>({usePathname:()=>nav.path,useSearchParams:()=>new URLSearchParams(nav.search),useRouter:()=>({push:nav.push})}));
const auth:{api:ApiClient|null;context:{context_id:string;role:string;department_code:string;read_only:boolean};principal:{actor_ref:string;tenant_id:string;amr:string[]}}={api:null,context:{context_id:"",role:"admin",department_code:"ke_hoach_tai_chinh",read_only:false},principal:{actor_ref:"one",tenant_id:"one",amr:["mfa"]}};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({...auth,status:"authenticated",kn:{context:auth.context}})}));
let loseAck=false;
let completedWrites=0;
let gate:Promise<void>|null=null;
let release:(()=>void)|undefined;
const writes:{key:string;body:Record<string,unknown>}[]=[];
const reads:string[]=[];
const track:typeof mockFetchNoDelay=async req=>{
  const path=new URL(req.url).pathname;
  if(req.method==="GET"&&path.startsWith("/v1/kn/khtc/plans/"))reads.push(req.url);
  if(req.method==="POST"&&path.endsWith("/versions")){
    writes.push({key:req.headers.get("Idempotency-Key")!,body:await req.clone().json()});
    if(gate){const wait=gate;gate=null;await wait;}
    const result=await mockFetchNoDelay(req);completedWrites++;
    if(loseAck){loseAck=false;throw new TypeError("lost acknowledgement");}
    return result;
  }
  return mockFetchNoDelay(req);
};
beforeEach(async()=>{
  vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-06-01T09:00:00Z"));
  resetKhtcMock();nav.path=PLAN_BASE;nav.search="year=2030";nav.push.mockReset();writes.length=0;reads.length=0;completedWrites=0;loseAck=false;gate=null;release=undefined;
  const selected=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"ctx-plan194"},body:JSON.stringify({assignment_id:"asg-admin",department_code:"ke_hoach_tai_chinh"})}));
  const ctx=await selected.json() as {context_id:string};
  auth.context={context_id:ctx.context_id,role:"admin",department_code:"ke_hoach_tai_chinh",read_only:false};auth.principal={actor_ref:"one",tenant_id:"one",amr:["mfa"]};
  auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>ctx.context_id,fetch:track});
});
afterEach(()=>{release?.();cleanup();vi.useRealTimers();});
function navigate(view:ReturnType<typeof render>,href:string){const url=new URL(href,"https://local.invalid");nav.path=url.pathname;nav.search=url.search.slice(1);view.rerender(<PlanWorkspace/>);}
async function dirtyPlan(){
  const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});
  fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));
  fireEvent.change(screen.getByLabelText("Tên"),{target:{value:"Hạng mục điều chỉnh194"}});
  fireEvent.click(screen.getByRole("button",{name:"Xong"}));
}
function fillDecision(){
  fireEvent.change(screen.getByRole("textbox",{name:/Số quyết định/}),{target:{value:"QĐ194"}});
  fireEvent.change(screen.getByRole("textbox",{name:/Ngày quyết định/}),{target:{value:"2030-06-01"}});
  fireEvent.change(screen.getByRole("textbox",{name:/Ngày hiệu lực/}),{target:{value:"2030-07-01"}});
}
describe("kế hoạch194 — phiên bản qua route",()=>{
  it("nháp và đầu quyết định giữ qua lịch sử; Hủy không ghi, chỉ xóa đầu quyết định",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan();
    fireEvent.click(screen.getByRole("button",{name:"Điều chỉnh theo QĐ"}));expect(nav.push).toHaveBeenCalledWith(planHref(PLAN_BASE,2030,"save"));
    navigate(view,planHref(PLAN_BASE,2030,"save"));await screen.findByRole("heading",{name:/Điều chỉnh theo quyết định/});fillDecision();
    navigate(view,planHref(PLAN_BASE,2030,"history"));const table=await screen.findByRole("table",{name:"Lịch sử kế hoạch năm"});
    expect(within(table).getByRole("columnheader",{name:"STT"})).toBeInTheDocument();
    expect(within(table).getByRole("link",{name:"Xem phiên bản 1"})).toHaveAttribute("href",planHref(PLAN_BASE,2030,"version",1));
    navigate(view,planHref(PLAN_BASE,2030,"save"));expect(await screen.findByRole("textbox",{name:/Số quyết định/})).toHaveValue("QĐ194");
    fireEvent.click(screen.getByRole("button",{name:"Hủy"}));navigate(view,planHref(PLAN_BASE,2030));
    expect(await screen.findByText("Hạng mục điều chỉnh194")).toBeInTheDocument();expect(writes).toHaveLength(0);
    navigate(view,planHref(PLAN_BASE,2030,"save"));expect(await screen.findByRole("textbox",{name:/Số quyết định/})).toHaveValue("");
  });
  it("lỗi theo trường chặn ghi; mất ACK thử lại cùng key chỉ tạo một phiên bản, bản cũ giữ nguyên",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan();navigate(view,planHref(PLAN_BASE,2030,"save"));
    const save=await screen.findByRole("button",{name:"Lưu phiên bản"});fireEvent.click(save);
    expect(screen.getByRole("textbox",{name:/Số quyết định/})).toHaveAttribute("aria-invalid","true");expect(writes).toHaveLength(0);
    fillDecision();fireEvent.change(screen.getByRole("textbox",{name:/Ngày hiệu lực/}),{target:{value:"2030-02-30"}});fireEvent.click(save);expect(writes).toHaveLength(0);
    fireEvent.change(screen.getByRole("textbox",{name:/Ngày hiệu lực/}),{target:{value:"2030-07-01"}});loseAck=true;fireEvent.click(save);
    await waitFor(()=>expect(writes).toHaveLength(1));await waitFor(()=>expect(screen.getByRole("button",{name:"Lưu phiên bản"})).toBeEnabled());
    fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(planHref(PLAN_BASE,2030,"version",2)));
    expect(writes).toHaveLength(2);expect(writes[0]!.key).toBe(writes[1]!.key);
    const api=createKhtcApi(auth.api!);expect((await api.versions(2030)).items).toHaveLength(2);
    expect((await api.plan(2030,1))!.entries.some(e=>e.name==="Hạng mục điều chỉnh194")).toBe(false);
  });
  it("phiên bản URL chỉ đọc đúng bản; không tồn tại không rơi về bản mới nhất",async()=>{
    nav.path=`${PLAN_BASE}/phien-ban/1`;const view=render(<PlanWorkspace/>);
    await screen.findByRole("heading",{name:"Kế hoạch năm 2030 — phiên bản 1"});
    expect(screen.getByRole("complementary",{name:"Log"})).toHaveTextContent("Lập kế hoạch v1");
    expect(screen.queryByRole("button",{name:/Lưu|Điều chỉnh/})).not.toBeInTheDocument();
    navigate(view,planHref(PLAN_BASE,2030,"version",999));expect(await screen.findByRole("alert")).toHaveTextContent("Không tìm thấy đúng năm");
  });
  it("đổi ngữ cảnh/vai hoặc MFA bỏ nháp, phản hồi cũ không kéo về phiên bản",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan();navigate(view,planHref(PLAN_BASE,2030,"save"));await screen.findByRole("textbox",{name:/Số quyết định/});fillDecision();
    gate=new Promise<void>(resolve=>{release=resolve;});fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(writes).toHaveLength(1));
    auth.context={...auth.context,context_id:"new-context",read_only:true};auth.principal={...auth.principal,amr:[]};navigate(view,planHref(PLAN_BASE,2030,"save"));
    await screen.findByText(/cần quyền lập kế hoạch/);release!();await waitFor(()=>expect(completedWrites).toBe(1));await act(async()=>{await Promise.resolve();});expect(screen.queryByRole("button",{name:"Lưu phiên bản"})).not.toBeInTheDocument();
    expect(nav.push).not.toHaveBeenCalled();expect(screen.queryByDisplayValue("QĐ194")).not.toBeInTheDocument();
  });
  it("URL số độc hại không gọi API, không lộ dữ liệu cũ",()=>{
    nav.search="year=1e100000";render(<PlanWorkspace/>);expect(screen.getByRole("alert")).toHaveTextContent("Kiểm tra năm");expect(reads).toHaveLength(0);
  });
});
