import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmPlans } from "@/api/mock/kn-hrm-plans";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { createPlanApi, type HrmPlanUnit } from "../plan-api";
import { PlanAdjustmentScreen } from "./screens";
import { adjustmentHref, planUnitHref } from "./routes";
const nav={year:"2027",unit:"trong_trot",seq:undefined as string|undefined,push:vi.fn()};
vi.mock("next/navigation",()=>({useParams:()=>nav,useRouter:()=>({push:nav.push})}));
const auth={api:null as ApiClient|null,principal:{subject:"admin",tenantId:"tenant",mfa:true},context:{role:"admin",context_id:"ctx",read_only:false}};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({status:"authenticated",principal:auth.principal,api:auth.api,kn:{context:auth.context}})}));
let loseAck=false;let gate:Promise<void>|null=null;let release:(()=>void)|undefined;let deny=false;let wrongYear=false;
const writes:{key:string;body:Record<string,unknown>}[]=[];const reads:string[]=[];
const track:typeof mockFetchNoDelay=async req=>{
  const path=new URL(req.url).pathname;
  if(req.method==="POST"&&path.endsWith("/adjustments")){
    writes.push({key:req.headers.get("Idempotency-Key")!,body:await req.clone().json()});
    if(gate){const wait=gate;gate=null;await wait;}
    const result=await mockFetchNoDelay(req);
    if(loseAck){loseAck=false;throw new TypeError("lost acknowledgement after commit");}
    return result;
  }
  if(req.method==="GET"&&/\/units\/[^/]+$/.test(path)){
    reads.push(path);const res=await mockFetchNoDelay(req);
    if(res.ok&&(deny||wrongYear)){const d=await res.json() as HrmPlanUnit;if(deny)d.can_adjust=false;if(wrongYear)d.period.year=2028;return new Response(JSON.stringify(d),{status:200,headers:{"Content-Type":"application/json"}});}
    return res;
  }
  return mockFetchNoDelay(req);
};
async function seed(){
  const res=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"ctx-plan203-route"},body:JSON.stringify({assignment_id:"asg-admin",department_code:"trong_trot"})}));
  const ctx=await res.json() as {context_id:string};
  auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>ctx.context_id,fetch:track});
  const api=createPlanApi(auth.api);await api.open({year:2027,due_on:"2026-11-30",cutoff_on:"2026-10-12"});await api.close(2027);
  await api.adjust(2027,"trong_trot",{leaving:1,additional:2,positions:[{title:"Kỹ sư mẫu",count:2,when:"Quý I/2027"}],note:"Ghi chú giữ nguyên",reason:"Điều chỉnh gốc đã ghi"});writes.length=0;reads.length=0;
}
beforeEach(async()=>{vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2026-11-01T09:00:00Z"));resetHrmPlans();resetHrmStaff();nav.year="2027";nav.unit="trong_trot";nav.seq=undefined;nav.push.mockReset();auth.principal={subject:"admin",tenantId:"tenant",mfa:true};auth.context={role:"admin",context_id:"ctx",read_only:false};loseAck=false;gate=null;release=undefined;deny=false;wrongYear=false;await seed();});
afterEach(()=>{release?.();cleanup();vi.useRealTimers();});
async function editor(){const view=render(<PlanAdjustmentScreen/>);await screen.findByRole("button",{name:"Ghi điều chỉnh"});return view;}
function reason(value="Điều chỉnh theo chỉ tiêu mới"){fireEvent.change(screen.getByRole("textbox",{name:"Lý do điều chỉnh"}),{target:{value}});}
describe("FN11 adjustment203 route and ledger contract",()=>{
  it("lost ACK retries identical key once; retains positions/note and old snapshot",async()=>{
    const before=await createPlanApi(auth.api!).unit(2027,"trong_trot");
    await editor();fireEvent.change(screen.getByRole("textbox",{name:"Số cần bổ sung"}),{target:{value:"3"}});reason();
    loseAck=true;const save=screen.getByRole("button",{name:"Ghi điều chỉnh"});fireEvent.click(save);await waitFor(()=>expect(writes).toHaveLength(1));await waitFor(()=>expect(save).toBeEnabled());
    expect(screen.getByRole("alert")).toHaveTextContent("network_error");
    fireEvent.click(save);await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(adjustmentHref(2027,"trong_trot",2)));
    expect(writes).toHaveLength(2);expect(writes[0]!.key).toBe(writes[1]!.key);
    const after=await createPlanApi(auth.api!).unit(2027,"trong_trot");expect(after.adjustments).toHaveLength(2);expect(after.adjustments[0]).toEqual(before.adjustments[0]);
    expect(writes[0]!.body).toMatchObject({leaving:1,additional:3,note:"Ghi chú giữ nguyên",positions:[{title:"Kỹ sư mẫu",count:2,when:"Quý I/2027"}]});
    expect(after.submissions).toEqual(before.submissions);expect(after.period).toEqual(before.period);
  });
  it("required/invalid/valid field errors update individually; Unicode reason is not UTF16 truncated",async()=>{
    await editor();const save=screen.getByRole("button",{name:"Ghi điều chỉnh"});
    const count=screen.getByRole("textbox",{name:"Số cần bổ sung"});fireEvent.change(count,{target:{value:"-1"}});fireEvent.click(save);
    const why=screen.getByRole("textbox",{name:"Lý do điều chỉnh"});expect(why).toHaveAttribute("aria-invalid","true");expect(count).toHaveAttribute("aria-invalid","true");
    fireEvent.change(count,{target:{value:"4"}});expect(count).not.toHaveAttribute("aria-invalid","true");expect(why).toHaveAttribute("aria-invalid","true");
    reason("😀".repeat(9));expect(why).toHaveAttribute("aria-invalid","true");expect(writes).toHaveLength(0);
    reason("\u0085"+"😀".repeat(500)+"\u0085");expect(why).not.toHaveAttribute("aria-invalid","true");expect(why).not.toHaveAttribute("maxlength","500");
    fireEvent.click(save);await waitFor(()=>expect(nav.push).toHaveBeenCalledWith(adjustmentHref(2027,"trong_trot",2)));
    expect(writes[0]!.body.reason).toBe("😀".repeat(500));
  });
  it("cancel makes no write; double submit and late ACK cannot navigate new context",async()=>{
    const view=await editor();reason();fireEvent.click(screen.getByRole("button",{name:"Hủy"}));expect(nav.push).toHaveBeenCalledWith(planUnitHref(2027,"trong_trot"));expect(writes).toHaveLength(0);nav.push.mockReset();
    gate=new Promise<void>(resolve=>{release=resolve;});const save=screen.getByRole("button",{name:"Ghi điều chỉnh"});fireEvent.click(save);fireEvent.click(save);await waitFor(()=>expect(writes).toHaveLength(1));
    auth.context={...auth.context,context_id:"changed",read_only:true};auth.principal={...auth.principal,mfa:false};view.rerender(<PlanAdjustmentScreen/>);
    await screen.findByText(/Chỉ HCTH và Admin/);release!();await act(async()=>{await new Promise(resolve=>setTimeout(resolve,25));});
    expect(nav.push).not.toHaveBeenCalled();expect(screen.queryByRole("button",{name:"Ghi điều chỉnh"})).not.toBeInTheDocument();expect(screen.queryByDisplayValue("Điều chỉnh theo chỉ tiêu mới")).not.toBeInTheDocument();
  });
  it("MFA alone and year changes invalidate draft and late acknowledgement",async()=>{
    const view=await editor();reason();
    auth.principal={...auth.principal,mfa:false};view.rerender(<PlanAdjustmentScreen/>);
    await screen.findByText(/Chỉ HCTH và Admin/);expect(screen.queryByRole("button",{name:"Ghi điều chỉnh"})).not.toBeInTheDocument();
    auth.principal={...auth.principal,mfa:true};view.rerender(<PlanAdjustmentScreen/>);
    expect(await screen.findByRole("textbox",{name:"Lý do điều chỉnh"})).toHaveValue("");
    reason();gate=new Promise<void>(resolve=>{release=resolve;});fireEvent.click(screen.getByRole("button",{name:"Ghi điều chỉnh"}));await waitFor(()=>expect(writes).toHaveLength(1));
    nav.year="2028";view.rerender(<PlanAdjustmentScreen/>);await screen.findByRole("alert");release!();await act(async()=>{await new Promise(resolve=>setTimeout(resolve,25));});
    expect(nav.push).not.toHaveBeenCalled();expect(screen.queryByDisplayValue("Điều chỉnh theo chỉ tiêu mới")).not.toBeInTheDocument();
  });
  it("server can_adjust false and wrong-year projection fail closed",async()=>{
    deny=true;const view=render(<PlanAdjustmentScreen/>);await screen.findByText(/Chỉ HCTH và Admin/);expect(screen.queryByRole("button",{name:"Ghi điều chỉnh"})).not.toBeInTheDocument();view.unmount();
    deny=false;wrongYear=true;render(<PlanAdjustmentScreen/>);expect(await screen.findByRole("alert")).toHaveTextContent("Không tìm thấy đúng năm");expect(writes).toHaveLength(0);
  });
  it("history and exact old seq show true metadata/snapshot, missing seq never falls back",async()=>{
    const api=createPlanApi(auth.api!);const a=(await api.unit(2027,"trong_trot")).adjustments[0]!;
    await api.adjust(2027,"trong_trot",{leaving:0,additional:9,reason:"Điều chỉnh kế tiếp đã ghi"});writes.length=0;
    nav.seq="1";const view=render(<PlanAdjustmentScreen mode="detail"/>);
    await screen.findByRole("heading",{name:/Điều chỉnh số 1/});expect(screen.getAllByText(a.actor_ref).length).toBeGreaterThan(0);expect(screen.getAllByText(a.recorded_at).length).toBeGreaterThan(0);
    expect(screen.getByRole("table",{name:"Nhu cầu theo vị trí của bản đã ghi"})).toHaveTextContent("Kỹ sư mẫu");expect(screen.queryByRole("button",{name:"Ghi điều chỉnh"})).not.toBeInTheDocument();
    nav.seq="999";view.rerender(<PlanAdjustmentScreen mode="detail"/>);expect(await screen.findByRole("alert")).toHaveTextContent("Không tìm thấy đúng số điều chỉnh");
    nav.seq=undefined;view.rerender(<PlanAdjustmentScreen mode="history"/>);const table=await screen.findByRole("table",{name:"Các bản điều chỉnh đã ghi"});expect(within(table).getByRole("columnheader",{name:"STT"})).toBeInTheDocument();expect(within(table).getByRole("link",{name:"Xem điều chỉnh 1"})).toHaveAttribute("href",adjustmentHref(2027,"trong_trot",1));expect(writes).toHaveLength(0);
  });
  it("malformed numeric URLs never call plan API",()=>{
    nav.year="1e100000";render(<PlanAdjustmentScreen/>);expect(screen.getByRole("alert")).toHaveTextContent("Kiểm tra năm");expect(reads).toHaveLength(0);
  });
});
