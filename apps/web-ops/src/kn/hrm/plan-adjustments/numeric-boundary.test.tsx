import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmPlans } from "@/api/mock/kn-hrm-plans";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { createPlanApi } from "../plan-api";
import { PlanAdjustmentScreen } from "./screens";
vi.mock("next/navigation",()=>({useParams:()=>({year:"2027",unit:"trong_trot"}),usePathname:()=>"/kn/nhan-su/ke-hoach/2027/trong_trot",useRouter:()=>({push:vi.fn()})}));
const auth:{api:ApiClient|null}={api:null};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({status:"authenticated",principal:{subject:"admin",tenantId:"tenant",mfa:true},api:auth.api,kn:{context:{role:"admin",context_id:"ctx",read_only:false}}})}));
const writes:string[]=[];
const tracked:typeof mockFetchNoDelay=async req=>{
  if(req.method==="POST"&&new URL(req.url).pathname.endsWith("/adjustments"))writes.push(await req.clone().text());
  return mockFetchNoDelay(req);
};
beforeEach(async()=>{
  vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2026-11-01T09:00:00Z"));
  resetHrmPlans();resetHrmStaff();writes.length=0;
  const res=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"ctx-plan203-red"},body:JSON.stringify({assignment_id:"asg-admin",department_code:"trong_trot"})}));
  const ctx=await res.json() as {context_id:string};
  auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>ctx.context_id,fetch:tracked});
  const pa=createPlanApi(auth.api);await pa.open({year:2027,due_on:"2026-11-30",cutoff_on:"2026-10-12"});await pa.close(2027);
});
afterEach(()=>{cleanup();vi.useRealTimers();});
describe("FN11 adjustment203 numeric boundary",()=>{
  for(const label of ["Dự kiến nghỉ, chuyển công tác khác","Số cần bổ sung"]){
    for(const raw of ["1e3","1e100000","1e-100000","9".repeat(100000)]){
      it(`${label} rejects ${raw.slice(0,16)} before POST`,async()=>{
        render(<PlanAdjustmentScreen/>);
        await screen.findByRole("button",{name:"Ghi điều chỉnh"});
        fireEvent.change(screen.getByRole("textbox",{name:label}),{target:{value:raw}});
        fireEvent.change(screen.getByRole("textbox",{name:"Lý do điều chỉnh"}),{target:{value:"Điều chỉnh theo chỉ tiêu năm"}});
        const save=screen.getByRole("button",{name:"Ghi điều chỉnh"});fireEvent.click(save);
        await waitFor(()=>expect(save).toBeEnabled());
        expect(writes).toHaveLength(0);
      });
    }
  }
});
