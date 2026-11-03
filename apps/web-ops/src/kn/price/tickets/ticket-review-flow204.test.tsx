import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { KN_ACCOUNTS, KN_DEPARTMENTS, resetKnAccounts } from "@/api/mock/kn-accounts";
const originalAccounts = structuredClone(KN_ACCOUNTS);
const originalDepartmentStatus = KN_DEPARTMENTS.find(d => d.code === "thong_tin_truyen_thong")!.status;
import { resetPriceBoards } from "@/api/mock/kn-price-boards";
import { resetPriceItems } from "@/api/mock/kn-price-items";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { resetPriceTickets } from "@/api/mock/kn-price-tickets";
import { createPriceTicketApi, lineInput, type PriceSheet } from "./api";
import { draftsOf, fillFromRef } from "./SheetLines";
import { TicketReviewWorkspace } from "./TicketReviewWorkspace";
const nav=vi.hoisted(()=>({query:"day=2026-10-12&mine=false&tab=alerts",push:vi.fn(),replace:vi.fn()}));
vi.mock("next/navigation",()=>({useSearchParams:()=>new URLSearchParams(nav.query),usePathname:()=>"/kn/bao-gia/kiem-duyet",useRouter:()=>({push:nav.push,replace:nav.replace})}));
const state=vi.hoisted(()=>({api:null as ApiClient|null,subject:"kn-binh",context:"ctx204",role:"kn_tinh",readOnly:false,mfa:true,status:"authenticated"}));
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:state.api,status:state.status,principal:{subject:state.subject,tenantId:"tenant204",mfa:state.mfa},kn:{status:"active",context:{context_id:state.context,role:state.role,department_code:"thong_tin_truyen_thong",read_only:state.readOnly},support:null}})}));
async function client(persona:string,assignment:string,fetcher:(input:Request,init?:RequestInit)=>Promise<Response>=mockFetchNoDelay){
 const res=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:`Bearer mock.${persona}`,"Content-Type":"application/json","Idempotency-Key":`ctx204-${persona}-${Math.random()}`},body:JSON.stringify({assignment_id:assignment,department_code:"thong_tin_truyen_thong"})}));
 if(!res.ok) throw new Error(`context fixture ${res.status}: ${await res.text()}`);
 const context=await res.json() as {context_id:string};
 const api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>`mock.${persona}`,getContextId:()=>context.context_id,fetch:fetcher});
 return {api,ta:createPriceTicketApi(api),context:context.context_id};
}
async function fixture(fetcher:(input:Request,init?:RequestInit)=>Promise<Response>=mockFetchNoDelay){
 const xa=await client("kn-an","asg-an-xa");const sheet=await xa.ta.sheet({day:"2026-10-12"});const d=fillFromRef(sheet,draftsOf(sheet));
 for(const l of sheet.lines)d[l.item_code]={...d[l.item_code]!,min:d[l.item_code]?.min||"9000",max:d[l.item_code]?.max||"9000",note:"Đối chiếu204"};
 const sent=await xa.ta.save({point:sheet.point.code,day:sheet.day,version:0,submit:true,lines:sheet.lines.map(l=>lineInput(l.item_code,d[l.item_code]!))});
 vi.setSystemTime(new Date("2026-10-12T05:01:00Z"));
 const tinh=await client("kn-binh","asg-binh-tinh",fetcher);state.api=tinh.api;state.context=tinh.context;
 const sh=await tinh.ta.ticket(sent.ticket!.id);return {...tinh,sheet:sh,id:sent.ticket!.id};
}
function correctPath(id:string){return ["phieu",id,"hieu-chinh"];}
function inputs(sheet:PriceSheet){const l=sheet.lines.find(l=>!!l.min&&!l.no_goods)!;return {lo:screen.getByLabelText(`Giá thấp nhất ${l.name} ${l.variety} (${l.item_code})`,{exact:false}),hi:screen.getByLabelText(`Giá cao nhất ${l.name} ${l.variety} (${l.item_code})`,{exact:false})};}
const fill=(sheet:PriceSheet)=>{const p=inputs(sheet);fireEvent.change(p.lo,{target:{value:"00001.2300"}});fireEvent.change(p.hi,{target:{value:"9000.00"}});fireEvent.change(screen.getByLabelText("Lý do",{exact:false}),{target:{value:"Đã đối chiếu204"}});};
beforeEach(()=>{vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2026-10-12T03:00:00Z"));resetKnAccounts(structuredClone(originalAccounts));const department=KN_DEPARTMENTS.find(d=>d.code==="thong_tin_truyen_thong")!;department.status="active";for(const account of KN_ACCOUNTS)for(const assignment of account.assignments)assignment.departments=assignment.departments.map(d=>d.code===department.code?department:d);KN_ACCOUNTS.find(a=>a.id==="kn-binh")!.assignments.find(a=>a.assignment_id==="asg-binh-tinh")!.departments.push(department);resetPriceItems();resetPricePoints();resetPriceTickets();resetPriceBoards();nav.push.mockReset();nav.replace.mockReset();nav.query="day=2026-10-12&mine=false&tab=alerts";state.role="kn_tinh";state.readOnly=false;state.mfa=true;state.status="authenticated";state.subject="kn-binh";});
afterEach(()=>{cleanup();resetKnAccounts(structuredClone(originalAccounts));KN_DEPARTMENTS.find(d=>d.code==="thong_tin_truyen_thong")!.status=originalDepartmentStatus;vi.useRealTimers();});
describe("204 route workspace using actual typed client + native mock contract (not PG proof)",()=>{
 it("details/log use ticket ID; filter-preserving routes and cancel never write",async()=>{
  const fetcher=vi.fn(mockFetchNoDelay),f=await fixture(fetcher);render(<TicketReviewWorkspace path={["phieu",f.id]} />);
  expect(await screen.findByRole("heading",{name:"Chi tiết phiếu báo giá"})).toBeInTheDocument();expect(await screen.findByRole("heading",{name:"Nhật ký phiếu"})).toBeInTheDocument();
  expect(screen.getByRole("link",{name:"Hiệu chỉnh"})).toHaveAttribute("href",`/kn/bao-gia/kiem-duyet/phieu/${f.id}/hieu-chinh?day=2026-10-12&mine=false&tab=alerts`);expect(screen.queryByRole("dialog")).not.toBeInTheDocument();expect(fetcher.mock.calls.filter(a=>(a[0] as Request).method==="POST")).toHaveLength(0);
 });
 it("rejects both hostile fields before API; required reason/pair; cancel is a link",async()=>{
  const fetcher=vi.fn(mockFetchNoDelay),f=await fixture(fetcher);render(<TicketReviewWorkspace path={correctPath(f.id)} />);await screen.findByRole("button",{name:"Lưu"});
  fireEvent.click(screen.getByRole("button",{name:"Lưu"}));expect(await screen.findByText("Chọn ít nhất một dòng để hiệu chỉnh.")).toBeInTheDocument();
  for(const side of ["lo","hi"] as const)for(const bad of ["1e100000","1e-100000","9".repeat(100000)]){fill(f.sheet);fireEvent.change(inputs(f.sheet)[side],{target:{value:bad}});fireEvent.click(screen.getByRole("button",{name:"Lưu"}));expect(inputs(f.sheet)[side]).toHaveAttribute("aria-invalid","true");}
  expect(screen.getByRole("link",{name:"Hủy"})).toHaveAttribute("href",expect.stringContaining(`/phieu/${f.id}?`));expect(fetcher.mock.calls.filter(a=>(a[0] as Request).method==="POST")).toHaveLength(0);
 });
 it("lost ACK retries same normalized body/key and appends only once",async()=>{
  let lost=true;const requests:Request[]=[];const f=await fixture(async(input,init)=>{const req=input instanceof Request?input:new Request(input,init);if(req.method==="POST"&&req.url.endsWith("/correct")){requests.push(req.clone());const response=await mockFetchNoDelay(req);if(lost){lost=false;throw new TypeError("lost ACK after commit");}return response;}return mockFetchNoDelay(req);});
  const before=await f.ta.log(f.id);render(<TicketReviewWorkspace path={correctPath(f.id)} />);await screen.findByRole("button",{name:"Lưu"});fill(f.sheet);fireEvent.click(screen.getByRole("button",{name:"Lưu"}));await waitFor(()=>expect(requests).toHaveLength(1));await waitFor(()=>expect(screen.getByRole("button",{name:"Lưu"})).not.toBeDisabled());fireEvent.click(screen.getByRole("button",{name:"Lưu"}));await waitFor(()=>expect(nav.push).toHaveBeenCalledTimes(1));expect(requests).toHaveLength(2);expect(requests[0]!.headers.get("Idempotency-Key")).toBe(requests[1]!.headers.get("Idempotency-Key"));const body0=await requests[0]!.text(),body1=await requests[1]!.text();expect(body0).toBe(body1);expect(JSON.parse(body1).lines[0].min).toBe("1.23");const after=await f.ta.log(f.id);expect(after.ticket.length).toBe(before.ticket.length+1);expect(after.lines.length).toBe(before.lines.length+1);
 });
 it("context/MFA/API identity change clears form, ignores late ACK and read-only cannot write",async()=>{
  let release:(()=>void)|undefined;const held=new Promise<void>(r=>{release=r;});const f=await fixture(async(input,init)=>{const req=input instanceof Request?input:new Request(input,init);const response=await mockFetchNoDelay(req);if(req.method==="POST"&&req.url.endsWith("/correct"))await held;return response;});
  const v=render(<TicketReviewWorkspace path={correctPath(f.id)} />);await screen.findByRole("button",{name:"Lưu"});fill(f.sheet);fireEvent.click(screen.getByRole("button",{name:"Lưu"}));await waitFor(()=>expect(screen.getByRole("button",{name:"Lưu"})).toBeDisabled());
  state.readOnly=true;state.context="different-context204";state.mfa=false;v.rerender(<TicketReviewWorkspace path={correctPath(f.id)} />);expect(await screen.findByText(/Chỉ xem — thao tác/)).toBeInTheDocument();await act(async()=>{release?.();await held;});expect(nav.push).not.toHaveBeenCalled();expect(screen.queryByRole("button",{name:"Lưu"})).not.toBeInTheDocument();
 });
 it("unknown/invalid ID and wrong role never fall back to another sheet",async()=>{
  await fixture();const v=render(<TicketReviewWorkspace path={["phieu","0192a5a0-0000-7000-8000-000000000404"]} />);await screen.findByRole("alert");expect(screen.queryByRole("link",{name:"Hiệu chỉnh"})).not.toBeInTheDocument();v.rerender(<TicketReviewWorkspace path={["phieu","bad"]} />);expect(screen.getByText("Đường dẫn phiếu không hợp lệ.")).toBeInTheDocument();state.role="kn_tw";v.rerender(<TicketReviewWorkspace path={[]} />);expect(screen.getByText(/Chọn ngữ cảnh Cấp Tỉnh/)).toBeInTheDocument();
 });
 it("business date collision can be corrected without trapping the current version",async()=>{
  let collide=true;const f=await fixture(async(input,init)=>{const req=input instanceof Request?input:new Request(input,init);if(req.method==="POST"&&req.url.endsWith("/redate")&&collide){collide=false;return new Response(JSON.stringify({type:"about:blank",title:"Conflict",status:409,code:"price.ticket_exists",detail:"Xã đã có phiếu ngày này"}),{status:409,headers:{"Content-Type":"application/problem+json"}});}return mockFetchNoDelay(req);});
  render(<TicketReviewWorkspace path={["phieu",f.id,"doi-ngay"]} />);await screen.findByRole("button",{name:"Lưu"});fireEvent.change(screen.getByLabelText("Lý do",{exact:false}),{target:{value:"Sửa ngày giấy204"}});fireEvent.click(screen.getByRole("button",{name:"Lưu"}));await screen.findByText(/Chưa xác nhận lưu thành công/);fireEvent.change(screen.getByLabelText("Ngày phiếu mới",{exact:false}),{target:{value:"2026-10-01"}});expect(screen.getByRole("button",{name:"Lưu"})).not.toBeDisabled();fireEvent.click(screen.getByRole("button",{name:"Lưu"}));await waitFor(()=>expect(nav.push).toHaveBeenCalledTimes(1));
 });
 it.each(["context","MFA","API","session"])("%s identity change resets fields independently",async(kind)=>{
  const f=await fixture();const v=render(<TicketReviewWorkspace path={correctPath(f.id)} />);await screen.findByRole("button",{name:"Lưu"});fill(f.sheet);
  if(kind==="context")state.context+="-new";if(kind==="MFA")state.mfa=false;if(kind==="API")state.api={...state.api!};if(kind==="session")state.subject+="-new";
  v.rerender(<TicketReviewWorkspace path={correctPath(f.id)} />);await waitFor(()=>expect(screen.getByLabelText("Lý do",{exact:false})).toHaveValue(""));expect(inputs(f.sheet).lo).toHaveValue("");
 });

 it("Admin without MFA cannot Save/POST even when backend can.correct=true",async()=>{
  const fetcher=vi.fn(mockFetchNoDelay),f=await fixture(fetcher);expect(f.sheet.can.correct).toBe(true);state.role="admin";state.mfa=false;
  render(<TicketReviewWorkspace path={correctPath(f.id)} />);await screen.findByRole("heading",{name:"Hiệu chỉnh phiếu báo giá"});
  const save=screen.queryByRole("button",{name:"Lưu"});if(save){fill(f.sheet);fireEvent.click(save);await waitFor(()=>expect(fetcher.mock.calls.filter(a=>(a[0] as Request).method==="POST")).toHaveLength(1));}
  expect(screen.queryByRole("button",{name:"Lưu"})).not.toBeInTheDocument();expect(fetcher.mock.calls.filter(a=>(a[0] as Request).method==="POST")).toHaveLength(0);
 });

 it("reason input permits 500 Unicode runes without a UTF-16 maxlength",async()=>{
  const f=await fixture();render(<TicketReviewWorkspace path={["phieu",f.id,"xoa"]} />);const reason=await screen.findByLabelText("Lý do",{exact:false});expect(reason).not.toHaveAttribute("maxlength");fireEvent.change(reason,{target:{value:"😀".repeat(500)}});expect(reason).toHaveValue("😀".repeat(500));
 });

 it("rapid day/mine/tab intents survive delayed URL ACK and back/forward",async()=>{
  await fixture();nav.query="day=2026-10-12&mine=true&tab=tickets";const v=render(<TicketReviewWorkspace />);
  fireEvent.change(screen.getByLabelText("Ngày báo giá",{exact:true}),{target:{value:"2026-10-13"}});fireEvent.click(screen.getByRole("checkbox",{name:"Chỉ xã được phân công"}));fireEvent.click(screen.getByRole("tab",{name:"Cảnh báo biến động"}));
  expect(nav.replace).toHaveBeenLastCalledWith("/kn/bao-gia/kiem-duyet?day=2026-10-13&mine=false&tab=alerts");expect(screen.getByRole("checkbox",{name:"Chỉ xã được phân công"})).not.toBeChecked();
  nav.query="day=2026-10-13&mine=true&tab=tickets";v.rerender(<TicketReviewWorkspace />);expect(screen.getByRole("checkbox",{name:"Chỉ xã được phân công"})).not.toBeChecked();expect(screen.getByRole("tab",{name:"Cảnh báo biến động"})).toHaveAttribute("aria-selected","true");
  act(()=>window.dispatchEvent(new PopStateEvent("popstate")));nav.query="day=2026-10-12&mine=true&tab=tickets";v.rerender(<TicketReviewWorkspace />);await waitFor(()=>expect(screen.getByRole("checkbox",{name:"Chỉ xã được phân công"})).toBeChecked());expect(screen.getByLabelText("Ngày báo giá",{exact:true})).toHaveValue("2026-10-12");
  nav.query="day=2026-10-13&mine=false&tab=alerts";v.rerender(<TicketReviewWorkspace />);expect(screen.getByRole("checkbox",{name:"Chỉ xã được phân công"})).toBeChecked();expect(nav.replace).toHaveBeenLastCalledWith("/kn/bao-gia/kiem-duyet?day=2026-10-12&mine=true&tab=tickets");
  nav.query="day=2026-10-12&mine=true&tab=tickets";v.rerender(<TicketReviewWorkspace />);
  act(()=>window.dispatchEvent(new PopStateEvent("popstate")));nav.query="day=2026-10-13&mine=false&tab=alerts";v.rerender(<TicketReviewWorkspace />);await waitFor(()=>expect(screen.getByRole("checkbox",{name:"Chỉ xã được phân công"})).not.toBeChecked());expect(screen.getByLabelText("Ngày báo giá",{exact:true})).toHaveValue("2026-10-13");
 });

});
