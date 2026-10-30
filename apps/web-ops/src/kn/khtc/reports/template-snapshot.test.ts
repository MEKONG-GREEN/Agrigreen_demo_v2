import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetKhtcTemplates, useTemplateMockNarrativeUsage } from "@/api/mock/kn-khtc-templates";
import { createKhtcPeriodApi } from "../periods/api";
import { createTemplateApi } from "../templates/api";
import { createReportApi } from "./api";

beforeEach(()=>{vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2039-10-01T03:00:00Z"));resetKnAdmin();resetKhtcTemplates();useTemplateMockNarrativeUsage(undefined);});
afterEach(()=>{vi.useRealTimers();});
const code=(p:Promise<unknown>)=>p.then(()=>"ok",(e:unknown)=>e instanceof ApiProblem?e.problem.code:String(e));
async function adminApi(){
 const response=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"fixed-context"},body:JSON.stringify({assignment_id:"asg-admin",department_code:null})}));
 expect(response.status).toBe(201);const context=await response.json() as {context_id:string};
 return createTemplateApi(createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>context.context_id,fetch:mockFetchNoDelay}));
}
describe("fixed template composer snapshot",()=>{
 it("pins published frame, rejects locked writes and keeps exports frozen across publish/refresh",async()=>{
  const client=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.tinh",getContextId:()=>null,fetch:mockFetchNoDelay});
  const periods=createKhtcPeriodApi(client),reports=createReportApi(client),templates=await adminApi();
  const p=await periods.open({report_type:"week",year:new Date().getFullYear(),no:35,departments:["trong_trot"]});
  expect((await reports.get(p.id)).content.template_snapshot?.mode).toBe("legacy");
  const definition=(await templates.get("mau_1")).definition;
  definition.nodes.push({id:"client_memo",parent:"B",order:100,title:"Ghi chú khách",repeat:"none",body:"{memo}",bindings:[{variable:"memo",source:"text",field:"narrative"}],text_slot:true,locked_admin:["signer"]});
  const v1=await templates.save("mau_1",{base_version:0,definition});await templates.publish("mau_1",{base_version:1,version_id:v1.id,applies_from:p.from_date.slice(0,10)});
  let d=await reports.save(p.id,{base_version:0,texts:{client_memo:"Lời văn đã ghim"}});
  expect(d.content.template_snapshot?.id).toBe(v1.id);expect(d.locked_admin).toEqual(["signer"]);expect(JSON.stringify(d.template_document)).toContain("Lời văn đã ghim");
  expect(await code(reports.save(p.id,{base_version:d.version,admin:{signer:"forged"}}))).toBe("extension.khtc_report_invalid");
  expect(await code(reports.save(p.id,{base_version:d.version,texts:{"plan.value":"999"}}))).toBe("extension.khtc_report_invalid");
  const x=await reports.export(p.id,d.version,true,{}),download=await (await reports.download(p.id,x.id)).blob.text();
  expect(download).toContain("Lời văn đã ghim");
  const changed=structuredClone(definition);changed.nodes.find(n=>n.id==="client_memo")!.body="KHUÔN CÂU MỚI";
  const v2=await templates.save("mau_1",{base_version:1,definition:changed});await templates.publish("mau_1",{base_version:2,version_id:v2.id,applies_from:p.from_date.slice(0,10)});
  d=await reports.refresh(p.id,d.version);expect(d.content.template_snapshot?.id).toBe(v1.id);expect(d.content.texts.client_memo).toBe("Lời văn đã ghim");
  d=await reports.save(p.id,{base_version:d.version,texts:{client_memo:"Lời văn revision sau"}});
  expect(await (await reports.download(p.id,x.id)).blob.text()).toBe(download);
  const w=await reports.word(p.id,x.id);expect(w.template).toBe("khtc.fixed");
  const word=await (await reports.downloadWord(p.id,w.id)).blob.text();expect(word).toContain("Lời văn đã ghim");expect(word).not.toContain("revision sau");expect(word).not.toContain("KHUÔN CÂU MỚI");
  const next=await periods.open({report_type:"week",year:new Date().getFullYear(),no:36,departments:["trong_trot"]});
  expect((await reports.get(next.id)).content.template_snapshot?.id).toBe(v2.id);
  const month=await periods.open({report_type:"month",year:new Date().getFullYear(),no:10,departments:["trong_trot"]});
  expect((await reports.get(month.id)).content.template_snapshot?.mode).toBe("legacy");
 });
});
