import {render,screen,fireEvent,waitFor} from "@testing-library/react";
import {describe,it,expect,vi} from "vitest";
import {createApiClient} from "@/api/client";
import {AdhocOpenRecovery} from "./AdhocOpenRecovery";
const state=vi.hoisted(()=>({auth:{} as {api:ReturnType<typeof createApiClient>}}));
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>state.auth}));
describe("W11.68 recovery caller",()=>{
 it("reloads without browser key and adopts by intent/version, then resumes after unknown result",async()=>{
  const id=crypto.randomUUID(),calls:{body:unknown;key:string|null}[]=[];
  let generation=0,first=true,complete=false;
  const fetcher=vi.fn(async(input:RequestInfo|URL)=>{
   const r=input instanceof Request?input:new Request(input);
   if(r.method==="GET")return Response.json({items:complete?[]:[{id,name:"Phát sinh đã gửi",from:"2026-10-01T00:00:00Z",to:"2026-10-13T00:00:00Z",due_at:"2026-11-01T00:00:00Z",departments:["trong_trot"],generation,requires_adoption:generation===0,can_adopt:generation===0,lease_expires_at:"2026-10-13T00:00:00Z",created_at:"2026-10-12T00:00:00Z"}]});
   expect(r.url).toContain("/adhoc-open-intents/"+id+"/reconcile");
   calls.push({body:await r.json(),key:r.headers.get("Idempotency-Key")});
   if(first){first=false;generation=1;throw new TypeError("response lost after adoption")}
   complete=true;return Response.json({id:crypto.randomUUID()});
  });
  state.auth={api:createApiClient({baseUrl:"http://test",getAccessToken:()=>"test",getContextId:()=>crypto.randomUUID(),fetch:fetcher})};
  render(<AdhocOpenRecovery/>);
  const reason=await screen.findByLabelText(/Lý do tiếp nhận/);const adopt=screen.getByRole("button",{name:"Tiếp nhận và đối chiếu đợt cũ"});
  expect(adopt).toBeDisabled();fireEvent.change(reason,{target:{value:"Tiếp nhận sau phiên cũ hết hạn"}});fireEvent.click(adopt);
  await screen.findByText(/Chưa xác nhận kết quả/);
  expect(reason).toBeDisabled();expect(calls[0]?.body).toEqual({expected_generation:0,reason:"Tiếp nhận sau phiên cũ hết hạn"});
  fireEvent.click(screen.getByRole("button",{name:"Tải lại thao tác dang dở"}));
  const resume=await screen.findByRole("button",{name:"Đối chiếu đợt đã gửi"});fireEvent.click(resume);
  await screen.findByText(/Không có thao tác phát hành dang dở/);
  expect(calls).toHaveLength(2);expect(calls[1]?.body).toEqual({expected_generation:1,reason:"Tiếp nhận sau phiên cũ hết hạn"});
  expect(calls[1]?.key).toBeTruthy();expect(calls[0]?.key).not.toBe(calls[1]?.key);
  await waitFor(()=>expect(fetcher.mock.calls.length).toBeGreaterThanOrEqual(4));
 });
});
