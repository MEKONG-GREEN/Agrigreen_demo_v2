import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { KhtcDashboardScreen } from "./KhtcDashboardScreen";
import type { Dashboard } from "./api";
import type {AuthContextValue} from "@/auth/AuthProvider";
type ReadAuth=Pick<AuthContextValue,"status"|"principal"|"kn"|"scopeWarning">;
const activeAuth=():ReadAuth=>({status:"authenticated",scopeWarning:null,principal:{subject:"actor-A",displayName:"Fixture A",tenantId:"tenant-A",orgRef:"org-A",roles:["kn_officer"],mfa:true,level:"tinh",unitName:null,orgName:null,scopeUnits:["X1"],scopeAll:false},kn:{status:"active",assignments:[{assignment_id:"asg-A",role:"kn_tinh",org_ref:"org-A",org_name:"Fixture",mgmt_level:null,communes:[],departments:[{code:"ke_hoach_tai_chinh",name:"KHTC",description:null,status:"active"}]}],context:{context_id:"ctx-A",assignment_id:"asg-A",role:"kn_tinh",department_code:"ke_hoach_tai_chinh",department_name:"KHTC",org_ref:"org-A",org_name:"Fixture",communes:[],default_route:"/kn/khtc/trang-chu",expires_at:"2099-12-31T00:00:00Z"},menu:null,badges:null}});
const auth:ReadAuth&{api:ApiClient}= {...activeAuth(),api:createApiClient({baseUrl:"http://mock",getAccessToken:()=>null})};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
const route:{pathname:string|null}={pathname:"/kn/khtc/trang-chu"};
vi.mock("next/navigation",()=>({usePathname:()=>route.pathname}));
const requests: Request[] = [];
const lastDashboard=()=>requests.filter(r=>r.url.includes("/dashboard")).at(-1)!;
let sent = 1, canAct = false;
let openAll: number | undefined = 17;
let live = false, livePeriod=false;
let planState: "normal"|"missing"|"filtered_empty"|"valid_empty"|"zero"|"unavailable"|"future"="normal";
const data = (): Dashboard => ({ year: 2026, quarter: 4, department: "", field: "", as_of: "2026-10-13T03:00:00Z", can_act: canAct, counts: { exported: 2, open: 1, open_all: openAll, sent, forms: 4, achieved: 1, items: 3, slow: 1 }, progress: { items: 3, achieved: 1, slow: 1, missing: 1, programs: [{ ref: "ct1", name: "Chương trình lúa", items: 3, known: 2, percent: "23.81" }], rows: [] }, progress_period: { id: "p1", name: "Mẫu 5 đến 13/10", type: "program", state: "collecting", cutoff: "2026-10-13", exported_at: null }, reports: [], status: [{ period_id: "p1", period_name: "Tuần 41", department: "trong_trot", name: "Trồng trọt", kind: "form", state: "submitted", at: "2026-10-13T02:00:00Z" }] });
beforeEach(() => { Object.assign(auth,activeAuth());route.pathname="/kn/khtc/trang-chu";vi.setSystemTime(new Date("2026-10-13T03:00:00Z")); requests.length = 0; sent = 1; openAll = 17; canAct = false; live=false;livePeriod=false;planState="normal"; auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "test", fetch: async (r) => { requests.push(r); if (r.url.includes("/dashboard")) {
        const d=data(); if(live){if(!livePeriod)d.progress_period=null;d.progress_window={year:2026,quarter:4,through_date:"2026-12-31",effective_cutoff:"2026-10-13T03:00:00Z",from_date:"2025-12-31",quantity_to:"2026-10-13",calendar_id:"ke_hoach_tai_chinh",calendar_version:2,plan_version_id:planState==="valid_empty"?"real-plan-version":null};}
        if(planState==="unavailable"&&d.progress_window)d.progress_window.unavailable_reason="native_forms_access_unavailable";
        if(planState==="future"){d.progress_window=null;d.progress_period=null;}
        if(["missing","filtered_empty","valid_empty","unavailable","future"].includes(planState)){d.counts.items=0;d.counts.achieved=0;d.counts.slow=0;d.progress={items:0,achieved:0,slow:0,missing:0,rows:[],programs:[]};if(planState==="filtered_empty")d.field="no_matches";}
        if(planState==="zero"){d.counts.achieved=0;d.progress.programs[0]!.percent="0";}
        return Response.json(d); } return Response.json({ counts: [], items: [{ code: "trong_trot", name: "Trồng trọt", active: true }] }); } }); });
afterEach(() => { vi.useRealTimers(); });
describe("Trang chủ tổng hợp KHTC", () => {
    it("hiện 5 thẻ, số thập phân và mốc Mẫu 5; lãnh đạo chỉ xem", async () => { render(<KhtcDashboardScreen />); await screen.findByText("23,81%"); expect(screen.getByRole("region", { name: "Chỉ số nóng" }).querySelectorAll("a")).toHaveLength(5); expect(screen.getByText("1/4")).toBeInTheDocument(); expect(screen.queryByRole("link", { name: "Mở kỳ mới" })).not.toBeInTheDocument(); expect(screen.getByRole("link", { name: "Chương trình lúa" })).toHaveAttribute("href", "/kn/khtc/tong-hop/p1"); expect(requests.every(r => r.method === "GET")).toBe(true); });
    it("gửi bộ lọc và làm mới đọc lại trạng thái, không sửa bản soạn", async () => { render(<KhtcDashboardScreen />); await screen.findByText("23,81%"); fireEvent.change(screen.getByRole("combobox", { name: "Quý" }), { target: { value: "3" } }); await waitFor(() => expect(requests.some(r => r.url.includes("quarter=3"))).toBe(true)); fireEvent.change(screen.getByRole("combobox", { name: "Lĩnh vực" }), { target: { value: "trong_trot" } }); await waitFor(() => expect(requests.some(r => r.url.includes("field=trong_trot"))).toBe(true)); sent = 2; fireEvent.click(screen.getByRole("button", { name: "Làm mới" })); await screen.findByText("2/4"); expect(requests.some(r => r.url.includes("/draft") || r.url.includes("/refresh"))).toBe(false); });
    it("hiện cửa sổ thật không cần kỳ, tách ngày yêu cầu/mốc thực tế và chỉ GET", async()=>{
        live=true;render(<KhtcDashboardScreen/>);await screen.findByText("23,81%");
        expect(screen.getByText(/Mẫu 5 trực tiếp.*2026-12-31.*2026-10-13/)).toBeInTheDocument();
        expect(screen.queryByRole("link",{name:"Chương trình lúa"})).not.toBeInTheDocument();
        expect(screen.getByText("1/3")).toBeInTheDocument();
        fireEvent.change(screen.getByLabelText("Đến ngày"),{target:{value:"2026-10-06"}});
        await waitFor(()=>expect(lastDashboard().url).toContain("through_date=2026-10-06"));
        fireEvent.change(screen.getByRole("combobox",{name:"Năm"}),{target:{value:"2025"}});
        await waitFor(()=>expect(lastDashboard().url).toContain("year=2025"));
        expect(lastDashboard().url).not.toContain("through_date");
        expect(requests.every(r=>r.method==="GET")).toBe(true);
    });
    it("giữ liên kết kỳ thật và ngày chốt riêng cạnh cửa sổ live",async()=>{
        live=true;livePeriod=true;render(<KhtcDashboardScreen/>);await screen.findByText("23,81%");
        expect(screen.getByText(/Kỳ Mẫu 5 đã mở:/)).toHaveTextContent("Số liệu của kỳ đến 2026-10-13");
        expect(screen.getByRole("link",{name:"Mẫu 5 đến 13/10"})).toHaveAttribute("href","/kn/khtc/tong-hop/p1");
        expect(screen.getByText(/Mẫu 5 trực tiếp.*2026-12-31/)).toBeInTheDocument();
    });
    it.each(["missing","filtered_empty","valid_empty"] as const)("phân biệt %s với số đạt đã biết, không hiện 0/0",async state=>{
        live=true;planState=state;render(<KhtcDashboardScreen/>);
        const card=await screen.findByRole("link",{name:/Hạng mục đạt ít nhất 100% KH/});expect(card).toHaveTextContent("—");expect(card).not.toHaveTextContent("0/0");
        const notice=screen.queryByText("Chưa có Kế hoạch năm hiệu lực cho mốc số liệu đã chọn.");
        if(state==="missing")expect(notice).toBeInTheDocument();else expect(notice).not.toBeInTheDocument();
    });
    it("giá trị 0 thật vẫn là 0, không đổi thành thiếu số",async()=>{
        live=true;planState="zero";render(<KhtcDashboardScreen/>);await screen.findByText("0%");
        expect(screen.getByRole("link",{name:/Hạng mục đạt ít nhất 100% KH/})).toHaveTextContent("0/3");
        expect(screen.queryByText("Chưa có Kế hoạch năm hiệu lực cho mốc số liệu đã chọn.")).not.toBeInTheDocument();
    });
    it.each(["unavailable","future"] as const)("TW %s giữ metadata nhưng không giả thiếu kế hoạch hoặc đạt 0",async state=>{
        live=true;livePeriod=true;planState=state;
        auth.principal={...auth.principal!,orgRef:"org-national",roles:["kn_national_admin"],level:"tw",scopeAll:true,scopeUnits:[]};
        auth.kn={...auth.kn,assignments:[{...auth.kn.assignments[0]!,role:"kn_tw",org_ref:"org-national"}],context:{...auth.kn.context!,role:"kn_tw",org_ref:"org-national"}};
        render(<KhtcDashboardScreen/>);
        expect(await screen.findByRole("link",{name:/Hạng mục đạt ít nhất 100% KH/})).toHaveTextContent("—");
        expect(screen.getByText("2")).toBeInTheDocument();
        expect(screen.getByText("1/4")).toBeInTheDocument();
        expect(screen.queryByText("Chưa có Kế hoạch năm hiệu lực cho mốc số liệu đã chọn.")).not.toBeInTheDocument();
        expect(screen.queryByText("23,81%")).not.toBeInTheDocument();
        if(state==="unavailable"){
            expect(screen.getByText(/Tiến độ trực tiếp chưa khả dụng trong ngữ cảnh Trung ương/)).toBeInTheDocument();
            expect(screen.getByRole("link",{name:"Mẫu 5 đến 13/10"})).toHaveAttribute("href","/kn/khtc/tong-hop/p1");
            expect(screen.queryByText("Chưa có tuần báo cáo đủ kết thúc trong năm, quý đã chọn.")).not.toBeInTheDocument();
        }else{
            expect(screen.getByText("Chưa có tuần báo cáo đủ kết thúc trong năm, quý đã chọn.")).toBeInTheDocument();
            expect(screen.queryByText(/Tiến độ trực tiếp chưa khả dụng/)).not.toBeInTheDocument();
        }
        expect(requests.every(r=>r.method==="GET")).toBe(true);
    });
});

function deferred<T>() { let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}; }
function pendingApi202() {
 const dashboard:ReturnType<typeof deferred<Response>>[]=[],catalog:ReturnType<typeof deferred<Response>>[]=[];
 const api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"test",getContextId:()=>auth.kn.context?.context_id??null,fetch:r=>{
  requests.push(r);const pending=deferred<Response>();(r.url.includes("/dashboard")?dashboard:catalog).push(pending);return pending.promise;
 }});
 return {api,dashboard,catalog};
}
function tagged202(tag:string):Dashboard {const d=data();d.progress.programs[0]!.name=`Program ${tag}`;d.progress_period!.name=`Mẫu5 ${tag}`;d.status[0]!.name=`Source ${tag}`;d.reports=[{id:`report-${tag}`,name:`Report ${tag}`,type:"quarter",state:"opened",cutoff:"2026-10-13",exported_at:null}];return d;}
async function finish202(p:ReturnType<typeof pendingApi202>,index:number,tag:string) {
 await act(async()=>{p.dashboard[index]!.resolve(Response.json(tagged202(tag)));p.catalog[index]?.resolve(Response.json({counts:[],items:[{code:`field-${tag}`,name:`Field ${tag}`,active:true}]}));});
 await screen.findByText(`Program ${tag}`);
}
function absent202(tag:string){
 for(const name of [`Program ${tag}`,`Mẫu5 ${tag}`,`Source ${tag}`,`Report ${tag}`,`Field ${tag}`])expect(screen.queryByText(name,{exact:true})).not.toBeInTheDocument();
 expect(screen.queryByText("1/4",{exact:true})).not.toBeInTheDocument();
 expect(screen.queryByRole("link",{name:"Mở kỳ mới"})).not.toBeInTheDocument();
}

describe("lifetime202",()=>{
 it("blank data and catalog on query replacement and reject late committed query ABA",async()=>{
  const p=pendingApi202();auth.api=p.api;render(<KhtcDashboardScreen/>);
  await waitFor(()=>expect(p.dashboard).toHaveLength(1));await finish202(p,0,"first-A");
  fireEvent.change(screen.getByRole("combobox",{name:"Quý"}),{target:{value:"3"}});absent202("first-A");
  await waitFor(()=>expect(p.dashboard).toHaveLength(2));
  fireEvent.change(screen.getByRole("combobox",{name:"Quý"}),{target:{value:"4"}});
  await waitFor(()=>expect(p.dashboard).toHaveLength(3));await finish202(p,2,"current-A");
  await act(async()=>{p.dashboard[1]!.resolve(Response.json(tagged202("late-B")));p.catalog[1]?.resolve(Response.json({counts:[],items:[{code:"late",name:"Field late-B",active:true}]}));});
  expect(screen.getByText("Program current-A")).toBeInTheDocument();expect(screen.queryByText("Program late-B")).not.toBeInTheDocument();expect(screen.queryByText("Field late-B")).not.toBeInTheDocument();
 });
 it("rejects late API-A after a committed raw client A to B to A replacement",async()=>{
  const a=pendingApi202(),b=pendingApi202();auth.api=a.api;const view=render(<KhtcDashboardScreen/>);
  await waitFor(()=>expect(a.dashboard).toHaveLength(1));auth.api=b.api;view.rerender(<KhtcDashboardScreen/>);
  await waitFor(()=>expect(b.dashboard).toHaveLength(1));await finish202(b,0,"B");
  auth.api=a.api;view.rerender(<KhtcDashboardScreen/>);absent202("B");
  await waitFor(()=>expect(a.dashboard).toHaveLength(2));await finish202(a,1,"new-A");
  await act(async()=>{a.dashboard[0]!.resolve(Response.json(tagged202("old-A")));a.catalog[0]?.resolve(Response.json({counts:[],items:[{code:"old",name:"Field old-A",active:true}]}));});
  expect(screen.getByText("Program new-A")).toBeInTheDocument();expect(screen.queryByText("Program old-A")).not.toBeInTheDocument();expect(screen.queryByText("Field old-A")).not.toBeInTheDocument();
 });
 it.each(["context","actor","tenant","org","scope","role","mfa","assignment_status","support_target"] as const)("wipes on same-client %s authority replacement before new reads resolve",async change=>{
  canAct=true;const p=pendingApi202();auth.api=p.api;const view=render(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(1));await finish202(p,0,"old");expect(screen.getByRole("link",{name:"Mở kỳ mới"})).toBeInTheDocument();
  if(change==="context")auth.kn.context={...auth.kn.context!,context_id:"ctx-B",read_only:true};
  if(change==="actor")auth.principal={...auth.principal!,subject:"actor-B"};
  if(change==="tenant")auth.principal={...auth.principal!,subject:"actor-B",tenantId:"tenant-B",scopeUnits:["X2"]};
  if(change==="org")auth.kn.context={...auth.kn.context!,org_ref:"org-B"};
  if(change==="scope")auth.principal={...auth.principal!,scopeUnits:["X2"]};
  if(change==="role")auth.kn.context={...auth.kn.context!,role:"kn_tw",read_only:true};
  if(change==="mfa")auth.principal={...auth.principal!,mfa:false};
  if(change==="assignment_status")auth.kn.assignments=[{...auth.kn.assignments[0]!,departments:[{...auth.kn.assignments[0]!.departments[0]!,status:"suspended"}]}];
  if(change==="support_target")auth.kn.support={target:"different-target",reason:"A valid audit reason"};
  view.rerender(<KhtcDashboardScreen/>);absent202("old");await waitFor(()=>expect(p.dashboard).toHaveLength(2));await finish202(p,1,"current");
  expect(screen.getByText("Program current")).toBeInTheDocument();expect(requests.every(r=>r.method==="GET")).toBe(true);
 });
 it("rejects pending old query A after committed query A to B to A",async()=>{
  const p=pendingApi202();auth.api=p.api;render(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(1));
  fireEvent.change(screen.getByRole("combobox",{name:"Quý"}),{target:{value:"3"}});await waitFor(()=>expect(p.dashboard).toHaveLength(2));
  fireEvent.change(screen.getByRole("combobox",{name:"Quý"}),{target:{value:"4"}});await waitFor(()=>expect(p.dashboard).toHaveLength(3));await finish202(p,2,"current-A");
  await act(async()=>{p.dashboard[0]!.resolve(Response.json(tagged202("old-A")));p.catalog[0]?.resolve(Response.json({counts:[],items:[{code:"old",name:"Field old-A",active:true}]}));p.dashboard[1]!.resolve(Response.json(tagged202("old-B")));p.catalog[1]?.resolve(Response.json({counts:[],items:[]}));});
  expect(screen.getByText("Program current-A")).toBeInTheDocument();expect(screen.queryByText("Program old-A")).not.toBeInTheDocument();expect(screen.queryByText("Field old-A")).not.toBeInTheDocument();expect(screen.queryByText("Program old-B")).not.toBeInTheDocument();
 });
 it("rejects pending old context A after committed same-client context A to B to A",async()=>{
  const p=pendingApi202();auth.api=p.api;const original=auth.kn.context!;const view=render(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(1));
  auth.kn.context={...original,context_id:"ctx-B"};view.rerender(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(2));await finish202(p,1,"context-B");
  auth.kn.context=original;view.rerender(<KhtcDashboardScreen/>);absent202("context-B");await waitFor(()=>expect(p.dashboard).toHaveLength(3));await finish202(p,2,"current-A");
  await act(async()=>{p.dashboard[0]!.resolve(Response.json(tagged202("old-A")));p.catalog[0]?.resolve(Response.json({counts:[],items:[{code:"old",name:"Field old-A",active:true}]}));});
  expect(screen.getByText("Program current-A")).toBeInTheDocument();expect(screen.queryByText("Program old-A")).not.toBeInTheDocument();expect(screen.queryByText("Field old-A")).not.toBeInTheDocument();
 });
 it("wipes on route replacement and rejects late committed route ABA",async()=>{
  const p=pendingApi202();auth.api=p.api;const view=render(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(1));
  route.pathname="/kn/phong-ban/ke_hoach_tai_chinh/trang-chu";view.rerender(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(2));await finish202(p,1,"route-B");
  route.pathname="/kn/khtc/trang-chu";view.rerender(<KhtcDashboardScreen/>);absent202("route-B");await waitFor(()=>expect(p.dashboard).toHaveLength(3));await finish202(p,2,"new-route-A");
  await act(async()=>{p.dashboard[0]!.resolve(Response.json(tagged202("old-route-A")));p.catalog[0]?.resolve(Response.json({counts:[],items:[{code:"old",name:"Field old-route-A",active:true}]}));});
  expect(screen.getByText("Program new-route-A")).toBeInTheDocument();expect(screen.queryByText("Program old-route-A")).not.toBeInTheDocument();expect(screen.queryByText("Field old-route-A")).not.toBeInTheDocument();
 });
 it.each(["anonymous","no_principal","legacy_kn","no_context","no_route"] as const)("clears resolved resources for %s without sending another GET",async state=>{
  const p=pendingApi202();auth.api=p.api;const view=render(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(1));await finish202(p,0,"old");
  if(state==="anonymous")auth.status="anonymous";
  if(state==="no_principal")auth.principal=null;
  if(state==="legacy_kn")auth.kn.status="legacy";
  if(state==="no_context")auth.kn.context=null;
  if(state==="no_route")route.pathname=null;
  view.rerender(<KhtcDashboardScreen/>);absent202("old");await act(async()=>{});expect(p.dashboard).toHaveLength(1);expect(p.catalog).toHaveLength(1);
 });
 it("blocks old data while auth/KN selection is unresolved and does not treat reason text as authority",async()=>{
  const p=pendingApi202();auth.api=p.api;auth.kn.support={target:"same-target",reason:"Initial audit reason"};const view=render(<KhtcDashboardScreen/>);
  await waitFor(()=>expect(p.dashboard).toHaveLength(1));await finish202(p,0,"active");
  auth.kn.support={target:"same-target",reason:"Changed audit reason only"};view.rerender(<KhtcDashboardScreen/>);
  await act(async()=>{});expect(p.dashboard).toHaveLength(1);expect(screen.getByText("Program active")).toBeInTheDocument();
  auth.status="loading";view.rerender(<KhtcDashboardScreen/>);absent202("active");expect(screen.getByText("Chọn ngữ cảnh Khuyến nông để xem số liệu.")).toBeInTheDocument();
  auth.status="authenticated";auth.kn.status="choose";view.rerender(<KhtcDashboardScreen/>);expect(p.dashboard).toHaveLength(1);
  auth.kn.status="active";view.rerender(<KhtcDashboardScreen/>);await waitFor(()=>expect(p.dashboard).toHaveLength(2));await finish202(p,1,"restored");
 });
});

it("open230 hot card uses global metadata while filtered open remains separate", async () => {
 render(<KhtcDashboardScreen />);
 expect(await screen.findByRole("link", { name: "Kỳ đang mở 17" })).toBeInTheDocument();
 expect(screen.getByText(/độc lập bộ lọc số liệu/)).toBeInTheDocument();
});
it("open230 does not substitute a filtered number when an older server omits open_all", async () => {
 openAll = undefined;
 render(<KhtcDashboardScreen />);
 expect(await screen.findByRole("link", { name: "Kỳ đang mở —" })).toBeInTheDocument();
 expect(screen.queryByRole("link", { name: "Kỳ đang mở 1" })).not.toBeInTheDocument();
});
