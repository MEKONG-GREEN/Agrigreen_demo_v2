import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { HrmCatalogScreen } from "./CatalogScreen";
import { HrmRuleScreen } from "./RuleScreens";
import { createHrmApi } from "./api";
import userEvent from "@testing-library/user-event";
const route=vi.hoisted(()=>({params:{key:"promotion"},path:"/kn/nhan-su/danh-muc",query:"at=2026-10-12",push:vi.fn(),replace:vi.fn()}));
vi.mock("next/navigation",()=>({useParams:()=>route.params,usePathname:()=>route.path,useSearchParams:()=>new URLSearchParams(route.query),useRouter:()=>({push:route.push,replace:route.replace})}));
const auth:{api:ApiClient|null;context:string}={api:null,context:"admin"};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:auth.api,kn:{context:{context_id:auth.context}}})}));
afterEach(()=>vi.useRealTimers());
async function clientFor(persona="kn-admin",assignment="asg-admin",fetcher:(request:Request)=>Promise<Response>=mockFetchNoDelay){
 if(persona==="kn-hung"){
  const headers={Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"rule197-enable-hcth"};
  const selected=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers,body:JSON.stringify({assignment_id:"asg-admin",department_code:"trong_trot"})}));
  const selectedContext=await selected.json() as {context_id:string};Object.assign(headers,{"X-AG-Context":selectedContext.context_id});
  const response=await mockAdminFetchNoDelay(new Request("http://mock/v1/departments",{headers}));
  const departments=await response.json() as {items:{id:string;code:string;status:string}[]};const department=departments.items.find((item)=>item.code==="hanh_chinh_tong_hop")!;
  if(department.status!=="active"){const enabled=await mockAdminFetchNoDelay(new Request(`http://mock/v1/departments/${department.id}/status`,{method:"POST",headers:{...headers,"Idempotency-Key":"rule197-hcth-enable"},body:JSON.stringify({status:"active"})}));if(enabled.status!==201)throw new Error(`fixture enable: ${enabled.status} ${await enabled.text()}`);}
 }
 const r=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:`Bearer mock.${persona}`,"Content-Type":"application/json","Idempotency-Key":`rule197-context:${persona}`},body:JSON.stringify({assignment_id:assignment,department_code:persona==="kn-admin"?"trong_trot":"hanh_chinh_tong_hop"})}));
 const selectedBody=await r.json() as {context_id?:string};if(r.status!==201&&r.status!==200)throw new Error(`fixture context ${persona}: ${r.status} ${JSON.stringify(selectedBody)}`);const context_id=selectedBody.context_id!;
 return createApiClient({baseUrl:"http://mock",getAccessToken:()=>`mock.${persona}`,getContextId:()=>context_id,fetch:fetcher});
}
beforeEach(async()=>{
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2026-10-12T03:00:00Z"));resetHrm();route.path="/kn/nhan-su/danh-muc";route.query="at=2026-10-12";route.params.key="promotion";auth.context="admin";route.push.mockClear();route.replace.mockClear();auth.api=await clientFor();
});
it("FN19 tham số dùng liên kết route riêng thay panel/popup",async()=>{
 render(<HrmCatalogScreen/>);await screen.findByRole("button",{name:/^Bảng hệ số theo bậc/});
 expect(screen.getByRole("link",{name:"Quy tắc nâng bậc"})).toHaveAttribute("href","/kn/nhan-su/danh-muc/quy-tac/promotion?at=2026-10-12");
 expect(screen.getByRole("link",{name:"Quy đổi thành tích"})).toHaveAttribute("href","/kn/nhan-su/danh-muc/quy-tac/achievement_conversion?at=2026-10-12");
 expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("URL selectors fail closed before any API load",()=>{
 const get=vi.spyOn(auth.api! as unknown as {GET:(path:string)=>Promise<unknown>},"GET");
 for(const [key,query] of [["unknown","at=2026-10-12"],["promotion","at=2026-02-30"],["promotion","at=2026-10-12&version=1e100000"],["promotion","at=2026-10-12&version=0"],["promotion","at=2026-10-12&at=2027-01-01"]]){
  route.params.key=key!;route.query=query!;const tree=render(<HrmRuleScreen create/>);expect(screen.getByRole("alert")).toHaveTextContent("URL không hợp lệ");expect(screen.queryByRole("button",{name:"Lưu"})).not.toBeInTheDocument();tree.unmount();
 }
 expect(get).not.toHaveBeenCalled();
});
it("immutable version, real Log metadata and department alias",async()=>{
 route.path="/kn/phong-ban/hanh_chinh/danh-muc-nhan-su/quy-tac/promotion";
 const ha=createHrmApi(auth.api!);const original=await ha.rule("promotion","2026-10-12");
 await ha.addRule("promotion",{valid_from:"2027-01-01",payload:original.current!.payload as unknown as Parameters<typeof ha.addRule>[1]["payload"],note:"Căn cứ thay đổi197"});
 const before=await ha.rule("promotion","2026-10-12");route.query="at=2026-10-12&version=2";
 render(<HrmRuleScreen/>);await screen.findByRole("heading",{name:"Phiên bản đang xem"});
 expect(screen.getByRole("link",{name:"Phiên bản mới"})).toHaveAttribute("href","/kn/phong-ban/hanh_chinh/danh-muc-nhan-su/quy-tac/promotion/them-moi?at=2026-10-12&version=2");
 const history=screen.getByRole("table",{name:"Lịch sử tham số"});expect(within(history).getByRole("columnheader",{name:"STT"})).toBeVisible();expect(history).toHaveTextContent("Căn cứ thay đổi197");
 expect(within(history).getByRole("link",{name:"Xem phiên bản 1"})).toHaveAttribute("href",expect.stringContaining("&version=1"));
 const log=screen.getByRole("complementary",{name:"Log"});expect(log).toHaveTextContent("Phiên bản 2");expect(log).toHaveTextContent("Chưa có thông tin người thực hiện");
 expect(await ha.rule("promotion","2026-10-12")).toEqual(before);
});
it.each(["promotion","achievement_conversion"] as const)("every numeric input %s rejects hostile values before POST; cancel writes nothing",async(key)=>{
 route.params.key=key;route.path=`/kn/nhan-su/danh-muc/quy-tac/${key}/them-moi`;
 const post=vi.spyOn(auth.api!,"POST");render(<HrmRuleScreen create/>);await screen.findByRole("button",{name:"Lưu"});
 const inputs=screen.getAllByRole("textbox").filter((el)=>["numeric","decimal"].includes(el.getAttribute("inputmode")??""));expect(inputs.length).toBeGreaterThan(0);
 for(const input of inputs){const original=(input as HTMLInputElement).value;
  for(const raw of ["1e100000","1e-100000","9".repeat(100000),"99999999999999999999999999999999"]){
   fireEvent.change(input,{target:{value:raw}});fireEvent.blur(input);expect(input).toHaveAttribute("aria-invalid","true");fireEvent.click(screen.getByRole("button",{name:"Lưu"}));expect(input).toHaveAttribute("aria-invalid","true");expect(post).not.toHaveBeenCalled();
  }
  fireEvent.change(input,{target:{value:original}});
 }
 expect(screen.getByRole("link",{name:"Hủy"})).toHaveAttribute("href",expect.stringContaining(`/quy-tac/${key}?at=2026-10-12&version=1`));expect(screen.queryByRole("dialog")).not.toBeInTheDocument();expect(post).not.toHaveBeenCalled();
});
it("HCTH reads versions and direct create route has no form",async()=>{
 auth.api=await clientFor("kn-hung","asg-hung-tinh");auth.context="hcth";
 const tree=render(<HrmRuleScreen/>);await screen.findByRole("table",{name:"Lịch sử tham số"});expect(screen.queryByRole("link",{name:"Phiên bản mới"})).not.toBeInTheDocument();tree.unmount();
 const get=vi.spyOn(auth.api! as unknown as {GET:(path:string)=>Promise<unknown>},"GET");render(<HrmRuleScreen create/>);expect(await screen.findByRole("alert")).toHaveTextContent("không có quyền thêm");expect(screen.queryByRole("button",{name:"Lưu"})).not.toBeInTheDocument();expect(get.mock.calls.some(([path])=>String(path).includes("/rules/"))).toBe(false);
});
it("context change removes draft and does not navigate after a committed response arrives late",async()=>{
 let release!:()=>void;let committed!:()=>void;
 const gate=new Promise<void>((resolve)=>{release=resolve;});const ready=new Promise<void>((resolve)=>{committed=resolve;});
 auth.api=await clientFor("kn-admin","asg-admin",async(request)=>{const response=await mockFetchNoDelay(request);if(request.method==="POST"&&request.url.includes("/hrm/rules/")){committed();await gate;}return response;});
 const old=createHrmApi(auth.api);const before=(await old.rule("promotion")).versions.length;
 const tree=render(<HrmRuleScreen create/>);await screen.findByRole("button",{name:"Lưu"});
 fireEvent.change(screen.getByRole("textbox",{name:"Ghi chú (căn cứ)"}),{target:{value:"Draft private197"}});fireEvent.click(screen.getByRole("button",{name:"Lưu"}));await ready;
 auth.api=await clientFor("kn-hung","asg-hung-tinh");auth.context="hcth";tree.rerender(<HrmRuleScreen create/>);
 expect(await screen.findByRole("alert")).toHaveTextContent("không có quyền thêm");expect(screen.queryByDisplayValue("Draft private197")).not.toBeInTheDocument();
 release();await waitFor(async()=>expect((await old.rule("promotion")).versions).toHaveLength(before+1));expect(route.push).not.toHaveBeenCalled();expect(screen.queryByRole("button",{name:"Lưu"})).not.toBeInTheDocument();
});
it("lost ACK retry keeps its operation key, appends once and returns the saved version",async()=>{
 let lose=true;const keys:string[]=[];
 auth.api=await clientFor("kn-admin","asg-admin",async(request)=>{
  const response=await mockFetchNoDelay(request);
  if(request.method==="POST"&&request.url.includes("/hrm/rules/")){keys.push(request.headers.get("Idempotency-Key")!);if(lose){lose=false;throw new TypeError("lost ACK197");}}
  return response;
 });
 const ha=createHrmApi(auth.api);const old=await ha.rule("promotion","2026-10-12");
 render(<HrmRuleScreen create/>);await screen.findByRole("button",{name:"Lưu"});
 fireEvent.change(screen.getByLabelText(/Ngày hiệu lực/),{target:{value:"2027-01-01"}});
 fireEvent.change(screen.getByRole("textbox",{name:"Tỷ lệ chỉ tiêu NLTH (%)"}),{target:{value:"12,34"}});
 fireEvent.change(screen.getByRole("textbox",{name:"Ghi chú (căn cứ)"}),{target:{value:"Căn cứ197 retry"}});
 await userEvent.click(screen.getByRole("button",{name:"Lưu"}));await waitFor(()=>expect(keys).toHaveLength(1));expect(route.push).not.toHaveBeenCalled();
 await userEvent.click(screen.getByRole("button",{name:"Lưu"}));await waitFor(()=>expect(route.push).toHaveBeenCalledWith("/kn/nhan-su/danh-muc/quy-tac/promotion?at=2027-01-01&version=2"));
 expect(keys).toHaveLength(2);expect(keys[1]).toBe(keys[0]);const current=await ha.rule("promotion","2027-01-01");expect(current.versions).toHaveLength(old.versions.length+1);expect(current.versions).toEqual(expect.arrayContaining(old.versions));expect(current.current?.payload).toMatchObject({nlth_quota_pct:"12.34"});
});
