// Test-only positive prerequisite. A separately authorized mock colleague creates
// only the exact requested year's real public plan; the period caller is unchanged.
// This is mock setup, never proof of native Accounts/grant lifecycle.
import { createApiClient, unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { mockToken } from "@/api/mock/personas";
import { KN_PERSONAS } from "@/api/mock/kn-accounts";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { createKhtcApi } from "../api";
export async function mockPlanWriter225():Promise<ApiClient>{
 const choices=[];
 for(const persona of KN_PERSONAS){
  let selected:string|null=null;
  const client=createApiClient({baseUrl:"http://mock",getAccessToken:()=>mockToken(persona.id),getContextId:()=>selected,fetch:mockFetchNoDelay});
  const view=await unwrap(client.GET("/v1/me/contexts"));
  for(const assignment of view.assignments){
   // The public mock RoleAssignment projection has no active/date fields;
   // current existence + actual public selection/revalidation are authoritative.
   const department=assignment.departments.find(x=>x.code==="ke_hoach_tai_chinh"&&x.status==="active"&&!x.read_only);
   if(assignment.role==="kn_tinh"&&department||assignment.role==="admin"&&persona.amr.includes("otp"))choices.push({client,assignment,department,setSelected:(id:string)=>{selected=id;}});
  }
 }
 choices.sort((a,b)=>Number(a.assignment.role==="admin")-Number(b.assignment.role==="admin"));
 for(const candidate of choices){
  const chosen=await unwrap(candidate.client.POST("/v1/me/context",{params:{header:{"Idempotency-Key":newIdempotencyKey()}},body:{assignment_id:candidate.assignment.assignment_id,department_code:candidate.assignment.role==="admin"?null:"ke_hoach_tai_chinh"}}));
  candidate.setSelected(chosen.context_id);
  const current=await unwrap(candidate.client.GET("/v1/me/context",{params:{header:{"X-AG-Context":chosen.context_id}}}));
  if(current.read_only)continue;
  if(!(current.role==="admin"||current.role==="kn_tinh"&&current.department_code==="ke_hoach_tai_chinh"))throw new Error("Public mock selection did not return the requested writer");
  return candidate.client;
 }
 throw new Error("No current authorized mock KHTC/Admin assignment for positive plan fixture");
}
export async function ensurePublishedMockPlan225(year:number):Promise<void>{
 if(!Number.isInteger(year)||year<2000||year>2100)throw new Error("Invalid fixture plan year");
 const writer=createKhtcApi(await mockPlanWriter225());
 const versions=await writer.versions(year);
 if(versions.items.length){if(!versions.items.some(x=>x.effective_from<=`${year}-01-01`))throw new Error("Existing fixture plan does not cover the earliest positive cutoff");return;}
 await writer.save(year,{base_version:0,effective_from:`${year}-01-01`,entries:[]});
}
