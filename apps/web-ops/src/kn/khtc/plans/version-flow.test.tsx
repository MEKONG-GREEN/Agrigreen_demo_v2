import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetKhtcMock } from "@/api/mock/kn-khtc";
import { createKhtcApi } from "../api";
import { PlanWorkspace } from "./PlanWorkspace";
import { PlanTree } from "./PlanTree";
import { PLAN_BASE, planHref, programHref } from "./routes";
const nav=vi.hoisted(()=>({path:"/kn/khtc/ke-hoach-nam",search:"year=2030",push:vi.fn()}));
vi.mock("next/navigation",()=>({usePathname:()=>nav.path,useSearchParams:()=>new URLSearchParams(nav.search),useRouter:()=>({push:nav.push})}));
const auth:{api:ApiClient|null;context:{context_id:string;role:string;department_code:string;read_only:boolean};principal:{actor_ref:string;tenant_id:string;amr:string[]}}={api:null,context:{context_id:"",role:"admin",department_code:"ke_hoach_tai_chinh",read_only:false},principal:{actor_ref:"one",tenant_id:"one",amr:["mfa"]}};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({...auth,status:"authenticated",kn:{context:auth.context}})}));
let loseAck=false;
let loseProgramAck=false;
const programWrites:{key:string;body:Record<string,unknown>}[]=[];
let completedWrites=0;
let gate:Promise<void>|null=null;
let release:(()=>void)|undefined;
const writes:{key:string;body:Record<string,unknown>}[]=[];
const reads:string[]=[];
const track:typeof mockFetchNoDelay=async req=>{
  const path=new URL(req.url).pathname;
  if(req.method==="POST"&&path==="/v1/kn/khtc/programs"){
    programWrites.push({key:req.headers.get("Idempotency-Key")!,body:await req.clone().json()});const result=await mockFetchNoDelay(req);if(loseProgramAck){loseProgramAck=false;throw new TypeError("lost program acknowledgement");}return result;
  }
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
  resetKhtcMock();nav.path=PLAN_BASE;nav.search="year=2030";nav.push.mockReset();writes.length=0;reads.length=0;completedWrites=0;loseAck=false;loseProgramAck=false;programWrites.length=0;gate=null;release=undefined;
  const selected=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"ctx-plan194"},body:JSON.stringify({assignment_id:"asg-admin",department_code:"ke_hoach_tai_chinh"})}));
  const ctx=await selected.json() as {context_id:string};
  auth.context={context_id:ctx.context_id,role:"admin",department_code:"ke_hoach_tai_chinh",read_only:false};auth.principal={actor_ref:"one",tenant_id:"one",amr:["mfa"]};
  auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>ctx.context_id,fetch:track});
});
afterEach(()=>{release?.();cleanup();vi.useRealTimers();});
function navigate(view:ReturnType<typeof render>,href:string){const url=new URL(href,"https://local.invalid");nav.path=url.pathname;nav.search=url.search.slice(1);view.rerender(<PlanWorkspace/>);}
async function dirtyPlan(view:ReturnType<typeof render>){
  const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});
  fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));
  navigate(view,nav.push.mock.calls.at(-1)![0] as string);
  fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"Hạng mục điều chỉnh194"}});
  fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));
  navigate(view,nav.push.mock.calls.at(-1)![0] as string);
  nav.push.mockClear();
}
function fillDecision(){
  fireEvent.change(screen.getByRole("textbox",{name:/Số quyết định/}),{target:{value:"QĐ194"}});
  fireEvent.change(screen.getByRole("textbox",{name:/Ngày quyết định/}),{target:{value:"2030-06-01"}});
  fireEvent.change(screen.getByRole("textbox",{name:/Ngày hiệu lực/}),{target:{value:"2030-07-01"}});
}
describe("kế hoạch194 — phiên bản qua route",()=>{
  it("206 consumes only the acknowledged copy provenance before saving an adjustment",async()=>{
    vi.setSystemTime(new Date("2031-06-01T09:00:00Z"));resetKhtcMock();nav.search="year=2032";
    const view=render(<PlanWorkspace/>);
    fireEvent.click(await screen.findByRole("button",{name:"Sao chép từ năm trước"}));
    await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});
    fireEvent.click(screen.getByRole("button",{name:"Lưu kế hoạch"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.click(await screen.findByRole("button",{name:"Lưu phiên bản"}));
    await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(planHref(PLAN_BASE,2032,"version",1)));
    expect(writes[0]!.body).toMatchObject({base_version:0,copied_from_year:2031});
    const api=createKhtcApi(auth.api!);const first=await api.plan(2032,1);
    expect(first!.version.copied_from_year).toBe(2031);
    navigate(view,planHref(PLAN_BASE,2032));
    const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"Adjustment after copy206"}});fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));navigate(view,planHref(PLAN_BASE,2032));
    fireEvent.click(screen.getByRole("button",{name:"Điều chỉnh theo QĐ"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.change(screen.getByRole("textbox",{name:/Số quyết định/}),{target:{value:"QĐ206-v2"}});
    fireEvent.change(screen.getByRole("textbox",{name:/Ngày quyết định/}),{target:{value:"2032-06-01"}});
    fireEvent.change(screen.getByRole("textbox",{name:/Ngày hiệu lực/}),{target:{value:"2032-07-01"}});
    fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));
    await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(planHref(PLAN_BASE,2032,"version",2)));
    expect(writes).toHaveLength(2);expect(writes[1]!.body.base_version).toBe(1);expect(writes[1]!.body).not.toHaveProperty("copied_from_year");
    const second=await api.plan(2032,2);expect(second!.version.copied_from_year).toBeNull();
    const firstAgain=(await api.plan(2032,1))!;expect(firstAgain.version).toEqual(first!.version);expect(firstAgain.entries).toEqual(first!.entries);
  });
  it("206 consumed copy metadata does not survive a late ACK with successor entry edits",async()=>{
    vi.setSystemTime(new Date("2031-06-01T09:00:00Z"));resetKhtcMock();nav.search="year=2032";
    const view=render(<PlanWorkspace/>);fireEvent.click(await screen.findByRole("button",{name:"Sao chép từ năm trước"}));await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});
    navigate(view,planHref(PLAN_BASE,2032,"save"));gate=new Promise<void>(resolve=>{release=resolve;});fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(writes).toHaveLength(1));
    navigate(view,planHref(PLAN_BASE,2032));const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"Copy successor206"}});fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));navigate(view,planHref(PLAN_BASE,2032));nav.push.mockClear();
    release!();await waitFor(()=>expect(completedWrites).toBe(1));await act(async()=>{await Promise.resolve();});
    expect(nav.push).not.toHaveBeenCalled();expect(screen.getByText("Copy successor206")).toBeInTheDocument();
    navigate(view,planHref(PLAN_BASE,2032,"save"));
    expect(screen.getByRole("textbox",{name:/Ngày hiệu lực/})).toHaveValue("");
    fireEvent.change(screen.getByRole("textbox",{name:/Số quyết định/}),{target:{value:"QĐ206-successor"}});
    fireEvent.change(screen.getByRole("textbox",{name:/Ngày quyết định/}),{target:{value:"2032-06-01"}});fireEvent.change(screen.getByRole("textbox",{name:/Ngày hiệu lực/}),{target:{value:"2032-07-01"}});
    fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(completedWrites).toBe(2));
    expect(writes[1]!.body.base_version).toBe(1);expect(writes[1]!.body).not.toHaveProperty("copied_from_year");
    const api=createKhtcApi(auth.api!);expect((await api.plan(2032,1))!.entries.some(e=>e.name==="Copy successor206")).toBe(false);expect((await api.plan(2032,2))!.entries.some(e=>e.name==="Copy successor206")).toBe(true);
  });
  it("206 lost copy ACK retries the same provenance and key before consuming it",async()=>{
    vi.setSystemTime(new Date("2031-06-01T09:00:00Z"));resetKhtcMock();nav.search="year=2032";
    const view=render(<PlanWorkspace/>);fireEvent.click(await screen.findByRole("button",{name:"Sao chép từ năm trước"}));await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});navigate(view,planHref(PLAN_BASE,2032,"save"));
    loseAck=true;fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(writes).toHaveLength(1));await waitFor(()=>expect(screen.getByRole("button",{name:"Lưu phiên bản"})).toBeEnabled());
    fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(planHref(PLAN_BASE,2032,"version",1)));
    expect(writes).toHaveLength(2);expect(writes[0]!.body.copied_from_year).toBe(2031);expect(writes[1]).toEqual(writes[0]);
    const api=createKhtcApi(auth.api!);expect((await api.versions(2032)).items).toHaveLength(1);expect((await api.plan(2032,1))!.version.copied_from_year).toBe(2031);
  });
  it("206 tree displays stopped dates in the common day/month/year format",async()=>{
    const plan=(await createKhtcApi(auth.api!).plan(2030))!;
    render(<PlanTree entries={plan.entries.map(e=>e.kind==="item"?{...e,tracking:"stopped",stopped_from:"2030-03-02"}:e)}/>);
    expect(screen.getAllByText("Ngừng từ 02/03/2030").length).toBeGreaterThan(0);expect(screen.queryByText("Ngừng từ 2030-03-02")).not.toBeInTheDocument();
  });
  it("206 late ACK after route ABA updates confirmed base but cannot navigate or recreate a dirty draft",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan(view);navigate(view,planHref(PLAN_BASE,2030,"save"));await screen.findByRole("textbox",{name:/Số quyết định/});fillDecision();
    gate=new Promise<void>(resolve=>{release=resolve;});fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(writes).toHaveLength(1));
    navigate(view,planHref(PLAN_BASE,2030,"history"));navigate(view,planHref(PLAN_BASE,2030,"save"));nav.push.mockClear();release!();await waitFor(()=>expect(completedWrites).toBe(1));await act(async()=>{await Promise.resolve();});
    expect(nav.push).not.toHaveBeenCalled();expect(screen.getByRole("button",{name:"Lưu phiên bản"})).toBeDisabled();
    expect(screen.getByRole("textbox",{name:/Số quyết định/})).toHaveValue("");expect(screen.getByRole("textbox",{name:/Ngày quyết định/})).toHaveValue("");
    navigate(view,planHref(PLAN_BASE,2030));expect(screen.getByText("Phiên bản 2/2")).toBeInTheDocument();
  });
  it.each(["actor","tenant","context","role","mfa"])("206 %s identity replacement discards an unfinished form",async kind=>{
    const view=render(<PlanWorkspace/>);const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));const edit=nav.push.mock.calls.at(-1)![0] as string;navigate(view,edit);
    fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"MustDisappear206"}});
    if(kind==="actor")auth.principal={...auth.principal,actor_ref:"other"};
    if(kind==="tenant")auth.principal={...auth.principal,tenant_id:"other"};
    if(kind==="mfa")auth.principal={...auth.principal,amr:[]};
    if(kind==="context")auth.context={...auth.context,context_id:"other"};
    if(kind==="role")auth.context={...auth.context,role:"kn_tinh"};
    navigate(view,edit);await waitFor(()=>expect(screen.queryByDisplayValue("MustDisappear206")).not.toBeInTheDocument());expect(writes).toHaveLength(0);
  });

  it("206 creates a master program with lost ACK replay, then selects it into a draft only once",async()=>{
    const view=render(<PlanWorkspace/>);fireEvent.click(await screen.findByRole("button",{name:"Thêm CT/DA vào tab"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.click(screen.getByRole("button",{name:"CT/DA mới…"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.click(screen.getByRole("button",{name:"Lưu CT/DA chủ"}));expect(programWrites).toHaveLength(0);
    fireEvent.change(screen.getByRole("textbox",{name:/Mã CT\/DA/}),{target:{value:"CT206"}});fireEvent.change(screen.getByRole("textbox",{name:/Tên CT\/DA/}),{target:{value:"Chương trình206"}});
    loseProgramAck=true;fireEvent.click(screen.getByRole("button",{name:"Lưu CT/DA chủ"}));await waitFor(()=>expect(programWrites).toHaveLength(1));await waitFor(()=>expect(screen.getByRole("button",{name:"Lưu CT/DA chủ"})).toBeEnabled());
    fireEvent.click(screen.getByRole("button",{name:"Lưu CT/DA chủ"}));fireEvent.click(screen.getByRole("button",{name:"Lưu CT/DA chủ"}));
    await waitFor(()=>expect(programWrites).toHaveLength(2));await waitFor(()=>expect(nav.push.mock.calls.at(-1)![0]).toContain("chon-chuong-trinh"));
    expect(programWrites[0]!.key).toBe(programWrites[1]!.key);expect((await createKhtcApi(auth.api!).programs()).items.filter(p=>p.code==="CT206")).toHaveLength(1);
    navigate(view,nav.push.mock.calls.at(-1)![0] as string);fireEvent.click(await screen.findByRole("button",{name:"Chọn CT206"}));navigate(view,planHref(PLAN_BASE,2030));
    expect(await screen.findByText("Chương trình206")).toBeInTheDocument();expect(writes).toHaveLength(0);
    fireEvent.click(screen.getByRole("button",{name:"Thêm CT/DA vào tab"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);expect(screen.queryByRole("button",{name:"Chọn CT206"})).not.toBeInTheDocument();
  });
  it("206 creates group and item on routes with required field errors, preserves existing field metadata",async()=>{
    const view=render(<PlanWorkspace/>);const row=await screen.findByRole("row",{name:/Chương trình khuyến nông trồng trọt/});
    fireEvent.click(within(row).getByRole("button",{name:"+ Nhóm"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));expect(screen.getByRole("textbox",{name:/^Tên/})).toHaveAttribute("aria-invalid","true");
    fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"Nhóm206"}});fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));navigate(view,planHref(PLAN_BASE,2030));
    const group=screen.getByRole("row",{name:/Nhóm206/});fireEvent.click(within(group).getByRole("button",{name:"+ Hạng mục"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    expect(screen.getByText("Nhóm206")).toBeInTheDocument();expect(screen.getByText("Các chương trình",{exact:true})).toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"Mục206"}});fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));expect(screen.getByRole("combobox",{name:/Phòng phụ trách/})).toHaveAttribute("aria-invalid","true");
    fireEvent.change(screen.getByRole("combobox",{name:/Phòng phụ trách/}),{target:{value:"trong_trot"}});fireEvent.change(screen.getByRole("textbox",{name:"Khối lượng KH"}),{target:{value:"123.0001"}});
    fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));navigate(view,planHref(PLAN_BASE,2030));expect(await screen.findByRole("row",{name:/Mục206/})).toHaveTextContent("123,0001");expect(writes).toHaveLength(0);
  });

  it("206 operation route keeps an unfinished form across history; cancel discards only that form",async()=>{
    const view=render(<PlanWorkspace/>);const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});
    fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));const edit=nav.push.mock.calls.at(-1)![0] as string;navigate(view,edit);
    const name=await screen.findByRole("textbox",{name:/^Tên/});fireEvent.change(name,{target:{value:"Unsubmitted206"}});
    navigate(view,planHref(PLAN_BASE,2030,"history"));await screen.findByRole("table",{name:"Lịch sử kế hoạch năm"});navigate(view,edit);
    expect(await screen.findByRole("textbox",{name:/^Tên/})).toHaveValue("Unsubmitted206");
    fireEvent.click(screen.getByRole("button",{name:"Hủy"}));navigate(view,edit);
    expect(await screen.findByRole("textbox",{name:/^Tên/})).toHaveValue("Mô hình lúa chất lượng cao");expect(writes).toHaveLength(0);
  });
  it("206 all rendered numeric fields reject hostile input with individual errors and no tree mutation",async()=>{
    const view=render(<PlanWorkspace/>);const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    const labels=["Thứ tự trong cây","Khối lượng KH","Kinh phí KH (đồng)","Số điểm"];
    for(const raw of ["1e100000","1e-100000","9".repeat(100000),"9999999999999999999999999999999999999999","1.00001"]){
      for(const label of labels)fireEvent.change(screen.getByRole("textbox",{name:label}),{target:{value:raw}});
      fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));
      for(const label of labels)expect(screen.getByRole("textbox",{name:label})).toHaveAttribute("aria-invalid","true");
      expect(writes).toHaveLength(0);
    }
  });
  it("206 readonly direct edit route has no write controls and saved version retains tab hierarchy",async()=>{
    const view=render(<PlanWorkspace/>);const row=await screen.findByRole("row",{name:/Mô hình lúa chất lượng cao/});fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));const edit=nav.push.mock.calls.at(-1)![0] as string;
    auth.context={...auth.context,read_only:true};navigate(view,edit);await screen.findByText(/Chỉ xem nội dung kế hoạch/);
    expect(screen.queryByRole("button",{name:"Lưu vào nháp"})).not.toBeInTheDocument();expect(screen.getByRole("textbox",{name:/^Tên/})).toBeDisabled();
    navigate(view,programHref(PLAN_BASE,2030,"programs",true));expect(await screen.findByRole("note")).toHaveTextContent("Chỉ xem — không có quyền lập kế hoạch.");expect(screen.queryByRole("button",{name:"Lưu CT/DA chủ"})).not.toBeInTheDocument();
    navigate(view,planHref(PLAN_BASE,2030,"version",1));const table=await screen.findByRole("table",{name:"Các chương trình"});expect(within(table).getByText("Mô hình lúa chất lượng cao")).toBeInTheDocument();
    expect(screen.getAllByRole("tab")).toHaveLength(6);expect(screen.queryByRole("button",{name:"Sửa"})).not.toBeInTheDocument();
  });

  it("206 confirmed late ACK rebases only own workspace while preserving successor edits",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan(view);navigate(view,planHref(PLAN_BASE,2030,"save"));
    await screen.findByRole("textbox",{name:/Số quyết định/});fillDecision();
    gate=new Promise<void>(resolve=>{release=resolve;});fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));
    await waitFor(()=>expect(writes).toHaveLength(1));
    navigate(view,planHref(PLAN_BASE,2030));
    const row=await screen.findByRole("row",{name:/Hạng mục điều chỉnh194/});fireEvent.click(within(row).getByRole("button",{name:"Sửa"}));
    navigate(view,nav.push.mock.calls.at(-1)![0] as string);
    fireEvent.change(screen.getByRole("textbox",{name:/^Tên/}),{target:{value:"Successor206"}});fireEvent.click(screen.getByRole("button",{name:"Lưu vào nháp"}));navigate(view,planHref(PLAN_BASE,2030));
    nav.push.mockClear();release!();await waitFor(()=>expect(completedWrites).toBe(1));await act(async()=>{await Promise.resolve();});
    expect(nav.push).not.toHaveBeenCalled();expect(await screen.findByText("Successor206")).toBeInTheDocument();
    navigate(view,planHref(PLAN_BASE,2030,"save"));await screen.findByRole("textbox",{name:/Số quyết định/});fillDecision();
    fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(writes).toHaveLength(2));
    expect(writes[1]!.body.base_version).toBe(2);
    await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(planHref(PLAN_BASE,2030,"version",3)));
    expect((await createKhtcApi(auth.api!).plan(2030,2))!.entries.some(e=>e.name==="Successor206")).toBe(false);
    expect((await createKhtcApi(auth.api!).plan(2030,3))!.entries.some(e=>e.name==="Successor206")).toBe(true);
  });

  it("nháp và đầu quyết định giữ qua lịch sử; Hủy không ghi, chỉ xóa đầu quyết định",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan(view);
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
    const view=render(<PlanWorkspace/>);await dirtyPlan(view);navigate(view,planHref(PLAN_BASE,2030,"save"));
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
  it("sửa trường sau submit lỗi cập nhật riêng lỗi hiện hành và không ghi trước Lưu",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan(view);navigate(view,planHref(PLAN_BASE,2030,"save"));
    fireEvent.click(await screen.findByRole("button",{name:"Lưu phiên bản"}));
    const decision=screen.getByRole("textbox",{name:/Số quyết định/});
    const date=screen.getByRole("textbox",{name:/Ngày quyết định/});
    const effective=screen.getByRole("textbox",{name:/Ngày hiệu lực/});
    expect(decision).toHaveAttribute("aria-invalid","true");
    expect(date).toHaveAttribute("aria-invalid","true");
    expect(effective).toHaveAttribute("aria-invalid","true");
    fireEvent.change(decision,{target:{value:"QĐ194"}});
    expect(decision).not.toHaveAttribute("aria-invalid","true");
    expect(date).toHaveAttribute("aria-invalid","true");
    expect(effective).toHaveAttribute("aria-invalid","true");
    fireEvent.change(date,{target:{value:"2030-02-30"}});
    expect(date).toHaveAttribute("aria-invalid","true");
    expect(screen.getByText("Nhập ngày YYYY-MM-DD hợp lệ.")).toBeInTheDocument();
    fillDecision();
    for(const field of [decision,date,effective])expect(field).not.toHaveAttribute("aria-invalid","true");
    expect(screen.queryByText("Bắt buộc nhập.")).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  });
  it("phiên bản URL chỉ đọc đúng bản; không tồn tại không rơi về bản mới nhất",async()=>{
    nav.path=`${PLAN_BASE}/phien-ban/1`;const view=render(<PlanWorkspace/>);
    await screen.findByRole("heading",{name:"Kế hoạch năm 2030 — phiên bản 1"});
    expect(screen.getByRole("complementary",{name:"Log"})).toHaveTextContent("Lập kế hoạch v1");
    expect(screen.queryByRole("button",{name:/Lưu|Điều chỉnh/})).not.toBeInTheDocument();
    navigate(view,planHref(PLAN_BASE,2030,"version",999));expect(await screen.findByRole("alert")).toHaveTextContent("Không tìm thấy đúng năm");
  });
  it("đổi ngữ cảnh/vai hoặc MFA bỏ nháp, phản hồi cũ không kéo về phiên bản",async()=>{
    const view=render(<PlanWorkspace/>);await dirtyPlan(view);navigate(view,planHref(PLAN_BASE,2030,"save"));await screen.findByRole("textbox",{name:/Số quyết định/});fillDecision();
    gate=new Promise<void>(resolve=>{release=resolve;});fireEvent.click(screen.getByRole("button",{name:"Lưu phiên bản"}));await waitFor(()=>expect(writes).toHaveLength(1));
    auth.context={...auth.context,context_id:"new-context",read_only:true};auth.principal={...auth.principal,amr:[]};navigate(view,planHref(PLAN_BASE,2030,"save"));
    await screen.findByText(/cần quyền lập kế hoạch/);release!();await waitFor(()=>expect(completedWrites).toBe(1));await act(async()=>{await Promise.resolve();});expect(screen.queryByRole("button",{name:"Lưu phiên bản"})).not.toBeInTheDocument();
    expect(nav.push).not.toHaveBeenCalled();expect(screen.queryByDisplayValue("QĐ194")).not.toBeInTheDocument();
  });
  it("URL số độc hại không gọi API, không lộ dữ liệu cũ",()=>{
    nav.search="year=1e100000";render(<PlanWorkspace/>);expect(screen.getByRole("alert")).toHaveTextContent("Kiểm tra năm");expect(reads).toHaveLength(0);
  });
});
