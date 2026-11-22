import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmStaff, hrmStaffInternals as H } from "@/api/mock/kn-hrm-staff";
import { resetHrmAppraisals } from "@/api/mock/kn-hrm-appraisal";
import { ApiProblem } from "@/api/problem";
import { createAppraisalApi, type AppraisalPeriodInput, type AppraisalEntryChange } from "./appraisal-api";
import { AppraisalEntryScreen } from "./AppraisalEntryScreen";
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
 const admin=await clientFor("kn-admin","asg-admin"),self=await clientFor("kn-an","asg-an-tinh");
 H.staff().forEach((x,i)=>{x.account_ref=i===0?"mock-kn-an":"mock-kn-admin";});
 const forms:Record<string,string>={},summaries:Record<string,string>={};
 for(const group of ["vien_chuc","lanh_dao","can_bo_xa"] as const){const form:S["HrmAppraisalForm"]={code:`phieu_${group}`,name:`Phiếu ${group}`,group,criteria:[{code:"tc_1",title:"Tiêu chí 1",group:"",guide:"",maximum:"15"},{code:"tc_2",title:"Tiêu chí 2",group:"",guide:"",maximum:"15"}],bands:[{minimum:"25",rating:"HTXSNV"},{minimum:"20",rating:"HTTNV"},{minimum:"10",rating:"HTNV"},{minimum:"0",rating:"KHTNV"}]};forms[group]=(await admin.pa.saveTemplate("form",form.code,{version:0,status:"published",form},`publish-${group}`)).id;}
 for(const kind of ["MS05","MS06"] as const)summaries[kind]=(await admin.pa.saveTemplate("summary",kind.toLowerCase(),{version:0,status:"published",summary:summary(kind)},`publish-${kind}`)).id;
 const input:AppraisalPeriodInput={year:2026,quarter:3,cutoff_on:"2026-09-30",self_due_at:"2026-10-05T16:59:59Z",manager_due_at:"2026-10-20T16:59:59Z",summary_due_at:"2026-10-25T16:59:59Z",forms,summaries,acknowledge_warnings:true};
 const period=await admin.pa.open(input,"open-quarter-three");return {admin,self,input,period};
}
beforeEach(()=>{runKey=crypto.randomUUID();vi.setSystemTime(new Date("2026-10-12T03:00:00Z"));resetHrmStaff();resetHrmAppraisals();});
afterEach(()=>{cleanup();vi.useRealTimers();});
it("HCTH giữ mẫu đóng băng và lịch sử, chỉ gửi đề xuất quản lý trên UI",async()=>{
 const {admin,input,period}=await setup();
 const staff=(await admin.pa.entries(period.id)).items[0]!.member.staff_id;
 const published=(await admin.pa.templates("form")).items.find(x=>x.form?.group==="vien_chuc")!;
 await admin.pa.saveTemplate("form",published.code,{version:1,status:"published",form:{...published.form!,name:"Mẫu mới sau khi mở đợt"}},"publish-changed-form");
 const detail=await admin.pa.detail(period.id,staff);
 expect(detail.form.id).toBe(input.forms[detail.item.member.group]);
 // A stored historical snapshot is a UI fixture; native tests exercise the
 // real proof/permission/state transitions instead of inventing a mock scan.
 detail.item.status="self_submitted";
 detail.item.self={answers:[{code:"tc_1",points:"12",note:"Bản giấy"},{code:"tc_2",points:"12",note:""}],total:"24",suggested:"HTTNV",complete:true};
 detail.item.self_rating="HTTNV";
 detail.item.manager.answers=detail.item.self.answers;
 detail.item.version=2;
 const history=structuredClone(detail.item);detail.history=[history];
 const writes:AppraisalEntryChange[]=[];
 auth.api=createApiClient({baseUrl:"http://historical",getAccessToken:()=>"mock.kn-admin",fetch:async request=>{
  if(request.method==="POST"){
   const body=await request.json() as AppraisalEntryChange;writes.push(body);
   detail.item={...detail.item,status:"manager_submitted",version:3,collective_rating:body.collective_rating??"",action:body.action};
   return Response.json(detail.item,{status:201});
  }
  return Response.json(new URL(request.url).pathname.includes("/versions/")?history:detail);
 }});
 render(<AppraisalEntryScreen period={period.id} staff={staff}/>);
 expect(await screen.findByRole("button",{name:"Hoàn tất đề xuất"})).toBeVisible();
 expect(screen.queryByRole("button",{name:"Nộp phiếu tự đánh giá"})).toBeNull();
 expect(screen.queryByRole("button",{name:"Lưu nháp tự chấm"})).toBeNull();
 const u=userEvent.setup();
 await u.selectOptions(screen.getByLabelText("Tập thể đề xuất"),"HTTNV");
 await u.type(screen.getByLabelText("Tóm tắt căn cứ, cơ sở lý do"),"Căn cứ biên bản họp tập thể");
 await u.click(screen.getByRole("button",{name:"Hoàn tất đề xuất"}));
 await waitFor(()=>expect(writes).toHaveLength(1));
 expect(writes[0]).toMatchObject({action:"manager_submit",version:2,collective_rating:"HTTNV"});
 expect(detail.item.self).toEqual(history.self);
 await u.click(await screen.findByRole("button",{name:"Xem bản 2"}));
 expect(await screen.findByRole("heading",{name:/Phiên bản 2/})).toBeVisible();
 expect(screen.getAllByText("12").length).toBeGreaterThan(1);
});
it("vai tự chấm và action đã bỏ không ghi thêm; guest endpoint trả404; không giả scan",async()=>{
 const {admin,self,period}=await setup();
 const items=(await admin.pa.entries(period.id)).items;
 const member=items[0]!;
 expect(await code(self.pa.entries(period.id))).toBe("forbidden");
 expect(await code(self.pa.detail(period.id,member.member.staff_id))).toBe("forbidden");
 const raw=async(action:string,guest=false)=>mockFetchNoDelay(new Request(`http://mock/v1/hrm/appraisals/periods/${period.id}/entries/${member.member.staff_id}${guest?"/guest-link":""}`,{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","X-AG-Context":admin.contextId,"Content-Type":"application/json","Idempotency-Key":`hc0-${crypto.randomUUID()}`},body:JSON.stringify({action,version:1})}));
 for(const action of ["self_draft","self_submit"])expect((await raw(action)).status).toBe(422);
 expect((await raw("proxy_self",true)).status).toBe(404);
 expect((await admin.pa.entries(period.id)).items.map(x=>[x.id,x.version])).toEqual(items.map(x=>[x.id,x.version]));
 const answers=[{code:"tc_1",points:"12",note:""},{code:"tc_2",points:"12",note:""}];
 expect(await code(admin.pa.change(period.id,member.member.staff_id,{action:"proxy_self",version:1,answers,proxy_document:crypto.randomUUID()},"proxy-needs-real-proof"))).toBe("hrm.appraisal_unavailable");
 expect(await code(admin.pa.changePeriod(period.id,{action:"attach_signed",version:1,signed_documents:{MS05:crypto.randomUUID()}},"signed-needs-real-proof"))).toBe("hrm.appraisal_unavailable");
});
it("thiết kế phiếu dùng NumberField: chặn số mũ trước phát hành",async()=>{
 const {admin}=await setup();auth.api=admin.api;render(<AppraisalTemplateEditorScreen kind="form"/>);const u=userEvent.setup();
 const field=screen.getByLabelText("Điểm tối đa 1");
 await u.clear(field);await u.type(field,"1e100000");await u.click(screen.getByRole("button",{name:"Phát hành phiên bản"}));
 expect(screen.getByLabelText("Điểm tối đa 1").getAttribute("aria-invalid")).toBe("true");
 expect((await admin.pa.templates("form")).items).toHaveLength(3);
});
it("nháp rỗng lưu được, phát hành bắt đủ tiêu chí và tổng/cận không vượt thang điểm",async()=>{
 const admin=await clientFor("kn-admin","asg-admin");
 const form:S["HrmAppraisalForm"]={code:"phieu_draft",name:"Phiếu đang thiết kế",group:"vien_chuc",criteria:[],bands:[]};
 const saved=await admin.pa.saveTemplate("form",form.code,{version:0,status:"draft",form},"empty-draft-form");expect(saved.status).toBe("draft");
 expect(await code(admin.pa.saveTemplate("form",form.code,{version:1,status:"published",form},"empty-publish-deny"))).toBe("validation_failed");
 const valid:S["HrmAppraisalForm"]={...form,criteria:[{code:"tc_1",title:"Nhiệm vụ",group:"",guide:"",maximum:"100"}],bands:[{minimum:"90",rating:"HTXSNV"},{minimum:"70",rating:"HTTNV"},{minimum:"50",rating:"HTNV"},{minimum:"0",rating:"KHTNV"}]};
 for(const [i,maximum] of ["1000000.01","1e100000","1e-100000","9".repeat(100000)].entries())expect(await code(admin.pa.saveTemplate("form",form.code,{version:1,status:"published",form:{...valid,criteria:[{...valid.criteria[0]!,maximum}]}},`bad-max-${i}`))).toBe("validation_failed");
 expect(await code(admin.pa.saveTemplate("form",form.code,{version:1,status:"published",form:{...valid,criteria:[{...valid.criteria[0]!,maximum:"600000"},{...valid.criteria[0]!,code:"tc_2",maximum:"600000"}]}},"total-max-deny"))).toBe("validation_failed");
 expect(await code(admin.pa.saveTemplate("form",form.code,{version:1,status:"published",form:{...valid,bands:[{minimum:"101",rating:"HTXSNV"},...valid.bands.slice(1)]}},"band-over-total-deny"))).toBe("validation_failed");
 expect((await admin.pa.saveTemplate("form",form.code,{version:1,status:"published",form:valid},"valid-publish-form")).status).toBe("published");
});