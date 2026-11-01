import {fireEvent,render,screen} from "@testing-library/react";
import {describe,it,expect,vi} from "vitest";
import {ApiProblem} from "@/api/problem";
import {createApiClient} from "@/api/client";
import {mockFetchNoDelay} from "@/api/mock/handler";
import {createRetryingMutation} from "./retry";
import {createAdhocApi} from "./api";
import {createKhtcPeriodApi} from "../periods/api";
import {createKhtcFormApi,emptyContent,type KhtcFormSheet} from "../forms/api";
import {createReportApi} from "../reports/api";
import {AdhocAnswer} from "./AdhocAnswer";
async function client(persona:string,assignment:string){
 const r=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock."+persona,"Content-Type":"application/json","Idempotency-Key":crypto.randomUUID()},body:JSON.stringify({assignment_id:assignment,department_code:"trong_trot"})}));
 expect(r.ok).toBe(true);const ctx=await r.json() as {context_id:string};
 return createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock."+persona,getContextId:()=>ctx.context_id,fetch:mockFetchNoDelay});
}
describe("W11.68 actual caller",()=>{
 it("retains unknown outcomes and releases keys after a confirmed422",async()=>{
 const mutate=createRetryingMutation(),keys:string[]=[];
 await expect(mutate("x",{n:1},async key=>{keys.push(key);throw new Error("lost")})).rejects.toThrow("lost");
 await expect(mutate("x",{n:2},async()=>"bad")).rejects.toThrow("chưa được đối chiếu");
 await mutate("x",{n:1},async key=>{keys.push(key);return "ok"});expect(keys[0]).toBe(keys[1]);
 await expect(mutate("x",{n:"bad"},async key=>{keys.push(key);throw new ApiProblem({type:"about:blank",status:422,title:"Invalid",code:"validation_failed"})})).rejects.toBeInstanceOf(ApiProblem);
 await mutate("x",{n:3},async key=>{keys.push(key);return "fixed"});expect(keys[2]).not.toBe(keys[3]);
 });
 it("requires an explicit seen token and bounds a100k-digit paste",()=>{
 const request={id:crypto.randomUUID(),period_id:crypto.randomUUID(),department:"trong_trot",instruction:"Bổ sung số lớp",indicators:[{code:"SO_LOP",name:"Số lớp mới",unit:"lớp"}],due_at:new Date().toISOString(),state:"issued" as const,overdue:false,return_applied:false,actor_ref:crypto.randomUUID(),at:new Date().toISOString()};
 let content=emptyContent();const update=vi.fn(next=>{content=next});const form={supplement:request} as KhtcFormSheet;
 const view=render(<AdhocAnswer form={form} content={content} onChange={update}/>);
 expect(screen.getByLabelText(/Số lớp mới/)).toBeDisabled();fireEvent.click(screen.getByRole("button",{name:/Đã đọc yêu cầu/}));
 expect(content.adhoc_request_id).toBe(request.id);view.rerender(<AdhocAnswer form={form} content={content} onChange={update}/>);
 fireEvent.change(screen.getByLabelText(/Số lớp mới/),{target:{value:"9".repeat(100000)}});
 expect(screen.getByRole("alert")).toHaveTextContent("Số quá dài");expect(content.adhoc_values).toEqual({});
 });
 it("carries a submitted answer through the actual form API into selected immutable export",async()=>{
 const admin=await client("kn-admin","asg-admin"),dept=await client("kn-binh","asg-binh-tinh");
 const requests=createAdhocApi(admin),forms=createKhtcFormApi(dept),reports=createReportApi(admin);
 const year=new Date().getFullYear(),due=new Date(Date.now()+7*86400000).toISOString();
 const p=await createKhtcPeriodApi(admin).open({report_type:"adhoc",year,name:"Phát sinh kiểm thử caller",from_date:year+"-01-01",to_date:new Date().toISOString().slice(0,10),due_at:due,export_due_at:due,departments:["trong_trot"]});
 const request=await requests.create(p.id,{department:"trong_trot",instruction:"Bổ sung số lớp đã đối chiếu",due_at:due,indicators:[{code:"SO_LOP",name:"Số lớp mới",unit:"lớp"}]});
 expect((await forms.get(p.id,"trong_trot")).supplement?.id).toBe(request.id);
 await expect(forms.submit(p.id,"trong_trot",emptyContent(),0)).rejects.toBeInstanceOf(ApiProblem);
 await forms.submit(p.id,"trong_trot",{...emptyContent(),adhoc_request_id:request.id,adhoc_values:{SO_LOP:"12,5"},adhoc_text:"Đã đối chiếu"},0);
 await createKhtcFormApi(admin).review(p.id,"trong_trot","accepted","Đã đối chiếu");
 let d=await reports.refresh(p.id,0);d=await reports.refresh(p.id,d.version);
 const key="bo_sung:"+request.id+":SO_LOP";expect(d.content.data.metrics.find(m=>m.key===key)?.value.Sum).toBe("12.5");
 d=await reports.save(p.id,{base_version:d.version,texts:{noi_dung:"Báo cáo đã soạn"},adhoc:{rows:[],metrics:[key]}});
 const x=await reports.export(p.id,d.version,false,{});const csv=await(await reports.download(p.id,x.id)).blob.text();
 expect(csv).toContain("Số lớp mới");expect(csv).toContain("12.5");expect(csv).toContain("Báo cáo đã soạn");
 });
});
