import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmStaff, hrmStaffInternals as H } from "@/api/mock/kn-hrm-staff";
import { resetHrmAppraisals } from "@/api/mock/kn-hrm-appraisal";
import { ApiProblem } from "@/api/problem";
import { createAppraisalApi, type AppraisalPeriodInput } from "./appraisal-api";
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
 const api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>`mock.${persona}`,getContextId:()=>c.context_id,fetch:request=>{const key=request.headers.get("Idempotency-Key");if(key)request.headers.set("Idempotency-Key",`${runKey}:${key}`);return mockFetchNoDelay(request);}});return {api,pa:createAppraisalApi(api)};
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
it("tự nộp trên UI, quản lý/trả/chốt qua cùng client, pin mẫu và bốn quý tham khảo",async()=>{
 const {admin,self,input,period}=await setup();auth.api=self.api;
 const list=await self.pa.entries(period.id);expect(list.items).toHaveLength(1);const staff=list.items[0]!.member.staff_id;
 render(<AppraisalEntryScreen period={period.id} staff={staff} mine/>);const u=userEvent.setup();
 await u.type(await screen.findByLabelText("Tự chấm Tiêu chí 1"),"12");await u.type(screen.getByLabelText("Tự chấm Tiêu chí 2"),"12");
 await u.type(screen.getByLabelText("Tóm tắt căn cứ, cơ sở lý do"),"Căn cứ nhiệm vụ được giao");await u.click(screen.getByRole("button",{name:"Nộp phiếu tự đánh giá"}));
 await waitFor(()=>expect(screen.queryByRole("button",{name:"Nộp phiếu tự đánh giá"})).toBeNull());
 expect((await self.pa.detail(period.id,staff)).item.status).toBe("self_submitted");cleanup();
 const published=(await admin.pa.templates("form")).items.find(x=>x.form?.group==="vien_chuc")!;
 await admin.pa.saveTemplate("form",published.code,{version:1,status:"published",form:{...published.form!,name:"Mẫu mới sau khi mở đợt"}},"publish-changed-form");
 expect((await self.pa.detail(period.id,staff)).form.id).toBe(input.forms.vien_chuc);
 const answers=[{code:"tc_1",points:"12",note:""},{code:"tc_2",points:"12",note:""}];
 for(const item of (await admin.pa.entries(period.id)).items){
  if(item.member.staff_id!==staff)await admin.pa.change(period.id,item.member.staff_id,{action:"self_submit",version:1,answers,reason:"Căn cứ nhiệm vụ cá nhân"},`self-${item.member.staff_id}`);
  await admin.pa.change(period.id,item.member.staff_id,{action:"manager_submit",version:2,answers,collective_rating:"HTTNV",reason:"Biên bản họp tập thể đơn vị"},`manage-${item.member.staff_id}`);
  await admin.pa.change(period.id,item.member.staff_id,{action:"send",version:3},`send-${item.member.staff_id}`);
 }
 await admin.pa.change(period.id,staff,{action:"return",version:4,return_to:"manager",reason:"Điều chỉnh đề xuất của đơn vị"},"return-sent-to-manager");
 expect((await self.pa.detail(period.id,staff)).item.returned_to).toBe("manager");
 await admin.pa.change(period.id,staff,{action:"manager_submit",version:5,answers,collective_rating:"HTNV",reason:"Căn cứ biên bản đã điều chỉnh"},"correct-manager-result");
 await admin.pa.change(period.id,staff,{action:"send",version:6},"resend-manager-result");
 await admin.pa.changePeriod(period.id,{action:"close",version:1,acknowledge_unsigned:true},"close-quarter-three");
 const old=(await self.pa.detail(period.id,staff)).item;expect(old.closed_rating).toBe("HTNV");
 const annual=await admin.pa.open({...input,quarter:0,forms:{...input.forms,vien_chuc:(await admin.pa.templates("form")).items.find(x=>x.code===published.code)!.id}},"open-annual-result");
 const annualDetail=await self.pa.detail(annual.id,staff);expect(annualDetail.item.quarter_results?.[0]).toMatchObject({quarter:3,entry_version:old.id,rating:"HTNV"});expect(annualDetail.item.official_rating).toBe("");
 await admin.pa.change(period.id,staff,{action:"reopen",version:old.version,reason:"Mở lại quý sau khi chụp đợt năm"},"reopen-prior-quarter");
 auth.api=self.api;render(<AppraisalEntryScreen period={annual.id} staff={staff} mine/>);
 expect(await screen.findByRole("heading",{name:"Tham khảo bốn quý"})).toBeTruthy();
 expect(screen.getByText(`v${old.version} · ${old.id}`)).toBeTruthy();expect(screen.getAllByText("Chưa có kết quả trong phạm vi")).toHaveLength(3);
});
it("không nhận proxy/ký giả; tự chấm không đọc người khác; khóa bền/replay đúng body",async()=>{
 const {admin,self,period}=await setup();const items=(await admin.pa.entries(period.id)).items;const mine=items.find(x=>x.member.account_ref==="mock-kn-an")!,foreign=items.find(x=>x.member.account_ref!=="mock-kn-an")!;
 expect(await code(self.pa.detail(period.id,foreign.member.staff_id))).toBe("hrm.appraisal_not_found");
 const answers=[{code:"tc_1",points:"12",note:""},{code:"tc_2",points:"12",note:""}];
 const body={action:"self_submit" as const,version:1,answers,reason:"Căn cứ nhiệm vụ cá nhân"};
 const once=await self.pa.change(period.id,mine.member.staff_id,body,"same-self-key-replay");
 expect((await self.pa.change(period.id,mine.member.staff_id,body,"same-self-key-replay")).id).toBe(once.id);
 expect(await code(self.pa.change(period.id,mine.member.staff_id,{...body,reason:"Dữ liệu khác"},"same-self-key-replay"))).toBe("idempotency_key_reused");
 expect(await code(admin.pa.change(period.id,foreign.member.staff_id,{action:"proxy_self",version:1,answers,proxy_document:crypto.randomUUID()},"proxy-needs-real-proof"))).toBe("hrm.appraisal_unavailable");
 expect(await code(admin.pa.changePeriod(period.id,{action:"attach_signed",version:1,signed_documents:{MS05:crypto.randomUUID()}},"signed-needs-real-proof"))).toBe("hrm.appraisal_unavailable");
 await admin.pa.changePeriod(period.id,{action:"lock_self",version:1},"lock-self-before-submit");
 expect(await code(admin.pa.change(period.id,foreign.member.staff_id,body,"locked-self-forbidden"))).toBe("validation_failed");
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