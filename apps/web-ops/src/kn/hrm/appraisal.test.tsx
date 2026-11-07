import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmStaff, hrmStaffInternals as H } from "@/api/mock/kn-hrm-staff";
import { resetHrmAppraisals } from "@/api/mock/kn-hrm-appraisal";
import { KN_ACCOUNTS } from "@/api/mock/kn-accounts";
import { ApiProblem } from "@/api/problem";
import { createAppraisalApi, type AppraisalPeriodInput, type AppraisalEntryChange } from "./appraisal-api";
import { AppraisalEntryScreen } from "./AppraisalEntryScreen";
import { AppraisalListScreen } from "./AppraisalListScreens";
import { AppraisalTemplateEditorScreen } from "./AppraisalTemplateScreen";
import type { components } from "@/api/generated/application";
vi.mock("next/navigation",()=>({usePathname:()=>"/kn/nhan-su/danh-gia",useRouter:()=>({replace:vi.fn(),push:vi.fn()})}));
const auth:{api:ApiClient|null}={api:null};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:auth.api,kn:{context:{role:"admin"}}})}));
type S=components["schemas"];
let runKey="";
async function clientFor(persona:string,assignment:string){
 const r=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:`Bearer mock.${persona}`,"Content-Type":"application/json","Idempotency-Key":`context-${persona}-${crypto.randomUUID()}`},body:JSON.stringify({assignment_id:assignment,department_code:"trong_trot"})}));
 const c=await r.json() as {context_id:string};
 const api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>`mock.${persona}`,getContextId:()=>c.context_id,fetch:request=>{const key=request.headers.get("Idempotency-Key");if(key)request.headers.set("Idempotency-Key",`${runKey}:${key}`);return mockFetchNoDelay(request);}});return {api,pa:createAppraisalApi(api),contextId:c.context_id};
}
async function code(p:Promise<unknown>){try{await p;return "ok";}catch(e){return e instanceof ApiProblem?e.problem.code:String(e);}}
const summary=(kind:"MS05"|"MS06"):S["HrmAppraisalSummary"]=>({code:kind.toLowerCase(),kind,number:kind.slice(-2),name:`Mẫu ${kind}`,title:"QUÝ {QUY} NĂM {NAM}",audience:"Đối tượng đánh giá",signature:"XÁC NHẬN TẬP THỂ",columns:["ordinal","staff","position_unit","self_rating","collective_rating","reason","personnel_proposal","note"].map((key,i)=>({key,title:`Cột ${i+1}`})),ratings:{HTXSNV:"Xuất sắc",HTTNV:"Tốt",HTNV:"Hoàn thành",KHTNV:"Không hoàn thành"}});
async function setup(){
 const admin=await clientFor("kn-admin","asg-admin"),self=await clientFor("kn-an","asg-an-tinh"),manager=await clientFor("kn-binh","asg-binh-tinh");
 H.staff().forEach((x,i)=>{x.account_ref=i===0?"mock-kn-an":"mock-kn-admin";});
 const summaries:Record<string,string>={};
 for(const kind of ["MS05","MS06"] as const)summaries[kind]=(await admin.pa.saveTemplate("summary",kind.toLowerCase(),{version:0,status:"published",summary:summary(kind)},`publish-${kind}`)).id;
 const input:AppraisalPeriodInput={year:2026,quarter:3,cutoff_on:"2026-09-30",manager_due_at:"2026-10-20T16:59:59Z",groups:["vien_chuc","lanh_dao","can_bo_xa"],summaries,acknowledge_warnings:true};
 const period=await admin.pa.open(input,"open-quarter-three");return {admin,self,manager,input,period};
}
beforeEach(()=>{runKey=crypto.randomUUID();vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2026-10-12T03:00:00Z"));resetHrmStaff();resetHrmAppraisals();});
afterEach(()=>{cleanup();vi.useRealTimers();});
it("FN17: UI lưu hai mức trực tiếp, bắt căn cứ trước Lưu và không gửi điểm/scan",async()=>{
 const {admin,period}=await setup(),staff=(await admin.pa.entries(period.id)).items[0]!.member.staff_id;
 const writes:AppraisalEntryChange[]=[];
 auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>admin.contextId,fetch:async request=>{if(request.method==="POST")writes.push(await request.clone().json() as AppraisalEntryChange);return mockFetchNoDelay(request);}});
 render(<AppraisalEntryScreen period={period.id} staff={staff}/>);const u=userEvent.setup();
 await screen.findByRole("button",{name:"Lưu đề xuất"});
 expect(screen.queryByRole("spinbutton")).toBeNull();expect(screen.queryByText("Phiếu đánh giá")).toBeNull();expect(screen.queryByRole("button",{name:/scan|tự nộp|chấm/i})).toBeNull();
 await u.selectOptions(screen.getByLabelText("Cá nhân tự đề xuất"),"HTXSNV");await u.selectOptions(screen.getByLabelText("Tập thể đề xuất"),"HTTNV");
 await u.click(screen.getByRole("button",{name:"Lưu đề xuất"}));expect(writes).toHaveLength(0);expect(await screen.findByText(/Căn cứ bắt buộc/)).toBeVisible();
 await u.type(screen.getByLabelText("Tóm tắt căn cứ, cơ sở lý do"),"x");await u.click(screen.getByRole("button",{name:"Lưu đề xuất"}));
 await waitFor(()=>expect(writes).toHaveLength(1));expect(writes[0]).toMatchObject({action:"manager_submit",version:1,self_rating:"HTXSNV",collective_rating:"HTTNV",reason:"x"});expect(writes[0]).not.toHaveProperty("answers");expect(writes[0]).not.toHaveProperty("proxy_document");
 const saved=await admin.pa.detail(period.id,staff);expect(saved.item.status).toBe("entered");expect(saved.item.manager.total).toBe("");
});
it("FN17/18: mock giữ HS37, khóa sửa sau gửi, yêu cầu điều chỉnh rồi chốt vào hồ sơ",async()=>{
 const {admin,manager,self,period,input}=await setup(),assigned=(await manager.pa.entries(period.id)).items;
 expect((await admin.pa.open(input,"open-quarter-three")).id).toBe(period.id);
 const firstTemplate=period.summaries.MS05!;
 expect((await admin.pa.saveTemplate("summary","ms05",{version:0,status:"published",summary:summary("MS05")},"publish-MS05")).id).toBe(firstTemplate.id);
 const menu=async(who:typeof manager)=>{const response=await mockFetchNoDelay(new Request("http://mock/v1/menu",{headers:{Authorization:`Bearer mock.${who===manager?"kn-binh":"kn-an"}`,"X-AG-Context":who.contextId}}));return JSON.stringify(await response.json());};
 expect(await menu(manager)).toContain("tinh.chung.de_xuat_xep_loai");expect(await menu(self)).not.toContain("tinh.chung.de_xuat_xep_loai");
 expect(assigned.length).toBeGreaterThan(0);expect(assigned.every(x=>x.member.position_name==="Viên chức"&&x.member.unit_name==="Phòng Trồng trọt")).toBe(true);expect(assigned.every(x=>x.member.summary_kind==="MS05"&&x.member.management_unit==="trong_trot")).toBe(true);
 expect(await code(self.pa.entries(period.id))).toBe("forbidden");
 const item=assigned[0]!,save:AppraisalEntryChange={action:"manager_submit",version:1,self_rating:"HTTNV",collective_rating:"KHTNV",reason:"x"};
 const entered=await manager.pa.change(period.id,item.member.staff_id,save,"direct-manager-proposal");expect(entered.status).toBe("entered");
 expect((await manager.pa.change(period.id,item.member.staff_id,save,"direct-manager-proposal")).id).toBe(entered.id);
 const account=KN_ACCOUNTS.find(x=>x.id==="kn-binh")!,assignment={...structuredClone(account.assignments.find(x=>x.assignment_id==="asg-binh-tinh")!),assignment_id:"asg-binh-281-other-node",org_ref:crypto.randomUUID()};
 account.assignments.push(assignment);
 try{const foreign=await clientFor("kn-binh",assignment.assignment_id);expect(await code(foreign.pa.change(period.id,item.member.staff_id,save,"direct-manager-proposal"))).toBe("hrm.appraisal_not_found");}
 finally{account.assignments.splice(account.assignments.indexOf(assignment),1);}
 const sent=await manager.pa.change(period.id,item.member.staff_id,{action:"send",version:entered.version},"send-manager-proposal");
 expect(await code(manager.pa.change(period.id,item.member.staff_id,{...save,version:sent.version},"sent-no-edit"))).toBe("validation_failed");
 const returned=await admin.pa.change(period.id,item.member.staff_id,{action:"return",version:sent.version,reason:"Cần bổ sung căn cứ đề xuất"},"return-manager-proposal");expect(returned.status).toBe("change_requested");
 await manager.pa.change(period.id,item.member.staff_id,{...save,version:returned.version},"correct-manager-proposal");
 for(const x of (await admin.pa.entries(period.id)).items){const current=await admin.pa.detail(period.id,x.member.staff_id);if(current.item.status!=="entered")await admin.pa.change(period.id,x.member.staff_id,{action:"manager_submit",version:current.item.version,self_rating:"HTTNV",collective_rating:"HTNV"},`complete-${x.member.staff_id}`);const v=(await admin.pa.detail(period.id,x.member.staff_id)).item;await admin.pa.change(period.id,x.member.staff_id,{action:"send",version:v.version},`send-${x.member.staff_id}`);}
 expect(await code(admin.pa.changePeriod(period.id,{action:"close",version:1},"close-no-signed-warning"))).toBe("validation_failed");
 const closed=await admin.pa.changePeriod(period.id,{action:"close",version:1,acknowledge_unsigned:true},"close-profile-only");
 expect((await admin.pa.changePeriod(period.id,{action:"close",version:1,acknowledge_unsigned:true},"close-profile-only")).version).toBe(closed.version);
 expect((await admin.pa.periods()).items[0]?.version).toBe(closed.version);
 expect((await admin.pa.results(item.member.staff_id)).items[0]?.rating).toBe("KHTNV");
 const opening=await Promise.allSettled([admin.pa.open({...input,quarter:4},"281-concurrent-a"),admin.pa.open({...input,quarter:4},"281-concurrent-b")]);
 expect(opening.filter(x=>x.status==="fulfilled")).toHaveLength(1);
 const conflict=opening.find(x=>x.status==="rejected") as PromiseRejectedResult;
 expect(conflict.reason).toBeInstanceOf(ApiProblem);expect((conflict.reason as ApiProblem).problem.code).toBe("hrm.appraisal_conflict");
});
it("writers tự chấm/điểm/phiếu/rating cũ trả 404 hoặc 422 và không thêm phiên bản",async()=>{
 const {admin,period}=await setup(),items=(await admin.pa.entries(period.id)).items,staff=items[0]!.member.staff_id;
 const raw=(path:string,body:unknown)=>mockFetchNoDelay(new Request(`http://mock${path}`,{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","X-AG-Context":admin.contextId,"Content-Type":"application/json","Idempotency-Key":`${runKey}:${crypto.randomUUID()}`},body:JSON.stringify(body)}));
 for(const action of ["self_draft","self_submit","proxy_self","manager_draft"])expect((await raw(`/v1/hrm/appraisals/periods/${period.id}/entries/${staff}`,{action,version:1})).status).toBe(422);
 for(const value of ["1e100000","1e-100000","9".repeat(100000),"90"]){expect((await raw("/v1/hrm/appraisals/templates/form/old",{version:0,status:"published",form:{criteria:[{maximum:value}]}})).status).toBe(404);expect((await raw(`/v1/hrm/appraisals/periods/${period.id}/entries/${staff}`,{action:"manager_submit",version:1,answers:[{points:value}]})).status).toBe(422);}
 expect((await raw(`/v1/hrm/appraisals/periods/${period.id}/entries/${staff}/guest-link`,{})).status).toBe(404);
 expect((await raw(`/v1/hrm/staff/${staff}/ratings`,{year:2026,rating:"HTTNV"})).status).toBe(404);
 expect((await raw(`/v1/hrm/appraisals/periods/${period.id}/exports`,{kind:"PHIEU_DIEM",format:"xlsx",version:1})).status).toBe(404);
 expect((await admin.pa.entries(period.id)).items.map(x=>[x.id,x.version])).toEqual(items.map(x=>[x.id,x.version]));
});
it("UI giữ điểm/phiếu cũ chỉ đọc và lịch sử; không còn màn thiết kế phiếu",async()=>{
 const {admin,period}=await setup(),staff=(await admin.pa.entries(period.id)).items[0]!.member.staff_id,detail=await admin.pa.detail(period.id,staff);
 detail.period.workflow_version="";detail.item.legacy_read_only=true;detail.item.can_manage=false;detail.item.can_center=false;detail.item.status="self_submitted";detail.item.self={answers:[{code:"tc1",points:"82",note:"Ghi chú cũ"}],total:"82",suggested:"HTXSNV",complete:true};detail.history=[structuredClone(detail.item)];
 auth.api=createApiClient({baseUrl:"http://legacy",getAccessToken:()=>"test",fetch:async request=>Response.json(new URL(request.url).pathname.includes("/versions/")?detail.item:detail)});
 const view=render(<AppraisalEntryScreen period={period.id} staff={staff}/>);await screen.findByText(/Dữ liệu đánh giá cũ chỉ đọc/);expect(screen.getByRole("region",{name:"Phiếu điểm lịch sử chỉ đọc"})).toBeVisible();expect(screen.queryByRole("button",{name:"Lưu đề xuất"})).toBeNull();
 await userEvent.setup().click(screen.getByRole("button",{name:"Xem bản 1"}));expect(await screen.findByRole("heading",{name:/Phiên bản 1/})).toBeVisible();expect(screen.getAllByText("82").length).toBeGreaterThan(0);
 view.unmount();auth.api=admin.api;const editor=render(<AppraisalTemplateEditorScreen kind="form"/>);expect(await screen.findByRole("alert")).toHaveTextContent("Không còn chức năng thiết kế phiếu");expect(screen.queryByRole("button",{name:"Phát hành phiên bản"})).toBeNull();
 editor.unmount();auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>admin.contextId,fetch:request=>new URL(request.url).pathname.endsWith("/appraisals/periods")?Promise.resolve(Response.json({items:[{...period,workflow_version:"",closed:false}]})):mockFetchNoDelay(request)});
 render(<AppraisalListScreen/>);expect(await screen.findByText("Lịch sử chỉ đọc",{exact:true})).toBeVisible();expect(screen.queryByText("Đang mở",{exact:true})).toBeNull();
});
