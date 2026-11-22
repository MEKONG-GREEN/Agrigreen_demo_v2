import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DepartmentFormsScreen } from "./DepartmentFormsScreen";
import type * as InboxApi from "./api";
const state=vi.hoisted(()=>({api:{} as object,dept:"trong_trot",status:"active",search:""}));
const list=vi.hoisted(()=>vi.fn());
vi.mock("next/navigation",()=>({useParams:()=>({dept:state.dept}),useSearchParams:()=>new URLSearchParams(state.search)}));
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:state.api,status:"authenticated",principal:{actor_ref:"a",tenant_id:"t",amr:["mfa"]},kn:{status:state.status,context:{context_id:"ctx",department_code:"ke_hoach_tai_chinh",role:"admin"}}})}));
vi.mock("../adhoc/AdhocInbox",()=>({AdhocInbox:()=>null}));
vi.mock("./api",async original=>({...await original<typeof InboxApi>(),createDeptFormsApi:()=>({list})}));
function payload(name:string,view="current",department="trong_trot"){return {department,view,department_name:name,counts:{total:1,needs_action:1,supplement:0,late:0},read_only:false,items:[{period_id:"11111111-1111-4111-8111-111111111111",report_type:"week",report_type_label:"Báo cáo tuần",name,state:"not_started",state_label:"Chưa nhập",needs_action:true,due_at:"2030-06-07T10:00:00Z",period_state:"opened",attempt:0,max_submissions:3,late:false,action:"enter",action_label:"Nhập",entry_route:"/kn/khtc/phieu/11111111-1111-4111-8111-111111111111?department=trong_trot"}]};}
function defer(){let resolve!:(v:ReturnType<typeof payload>)=>void;const promise=new Promise<ReturnType<typeof payload>>(r=>{resolve=r;});return {promise,resolve};}
beforeEach(()=>{state.api={};state.dept="trong_trot";state.status="active";state.search="";list.mockReset();});afterEach(cleanup);
it("Admin tab replaces list/header immediately and rejects committed current→sent→current old callbacks",async()=>{
  const sent=defer(),fresh=defer();list.mockResolvedValueOnce(payload("Old current")).mockReturnValueOnce(sent.promise).mockReturnValueOnce(fresh.promise);
  render(<DepartmentFormsScreen view="current"/>);await screen.findByRole("heading",{name:/Old current/});
  expect(screen.getByRole("link",{name:"Nhập"})).toHaveAttribute("href","/kn/khtc/phieu/11111111-1111-4111-8111-111111111111?department=trong_trot");
  expect(screen.getByRole("link",{name:"Chi tiết phiếu"})).toHaveAttribute("href","/kn/phong-ban/trong_trot/bao-cao-khtc/phieu/11111111-1111-4111-8111-111111111111?view=current");
  fireEvent.click(screen.getByRole("button",{name:"Phiếu đã gửi"}));expect(screen.queryAllByText(/Old current/)).toHaveLength(0);await waitFor(()=>expect(list).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole("button",{name:"Phiếu kỳ này"}));await waitFor(()=>expect(list).toHaveBeenCalledTimes(3));
  await act(async()=>sent.resolve(payload("Stale sent","sent")));expect(screen.queryAllByText(/Stale sent/)).toHaveLength(0);
  await act(async()=>fresh.resolve(payload("Fresh current")));expect(await screen.findByRole("heading",{name:/Fresh current/})).toBeInTheDocument();
});
it("unavailable selected session does not fetch department data",()=>{
  state.status="loading";render(<DepartmentFormsScreen view="current"/>);expect(list).not.toHaveBeenCalled();expect(screen.queryByRole("table")).not.toBeInTheDocument();
});

it.each(["view","department"])("rejects mismatched server %s before header or rows render",async kind=>{
  list.mockResolvedValue(payload("FOREIGN MUST NOT RENDER",kind==="view"?"sent":"current",kind==="department"?"chan_nuoi_thuy_san":"trong_trot"));
  render(<DepartmentFormsScreen view="current"/>);await screen.findByRole("alert");expect(screen.queryAllByText(/FOREIGN MUST NOT RENDER/)).toHaveLength(0);expect(screen.queryByRole("table")).not.toBeInTheDocument();
});
