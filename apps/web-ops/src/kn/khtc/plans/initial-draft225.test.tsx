import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AuthContextValue } from "@/auth/AuthProvider";
import { ApiProblem } from "@/api/problem";
import { InitialDraftPage225 } from "./InitialDraftPage225";
import type { InitialDraft225, InitialDraftState225 } from "./draft-api225";
import type * as KhtcModule from "../api";
import { blankEntry } from "../api";
import type { PlanRoute } from "./routes";
const w=vi.hoisted(()=>({auth:{} as AuthContextValue,api:{current:vi.fn(),history:vi.fn(),save:vi.fn(),discard:vi.fn()},khtc:{programs:vi.fn(),catalog:vi.fn(),createProgram:vi.fn()}}));
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>w.auth}));
vi.mock("./draft-api225",()=>({createDraftApi225:()=>w.api}));
vi.mock("../api",async()=>({...await vi.importActual<typeof KhtcModule>("../api"),useKhtcApi:()=>w.khtc,useKhtcRights:()=>({plan:!w.auth.kn.context?.read_only,catalog:false})}));
const base="/kn/khtc/ke-hoach-nam/nhap-may-chu";
const route=(year=2035):PlanRoute=>({base,initial:true,year,view:"list",go:vi.fn()});
let state:InitialDraftState225;
function receipt(revision=1):InitialDraft225{return {event_id:"01990000-0000-7000-8000-000000002251",draft_id:"01990000-0000-7000-8000-000000002252",year:2035,revision,action:"create",state:"active",base_published_version:0,entries_count:0,entries_hash:"a".repeat(64),note:"",event_time:"2035-06-01T03:00:00Z",recorded_at:"2035-06-01T03:00:00Z",actor_ref:"01990000-0000-7000-8000-000000002253",entries:[]};}
function deferred<T>(){let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>{resolve=r;});return {promise,resolve};}
beforeEach(()=>{
 vi.clearAllMocks();vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2035-06-01T03:00:00Z"));
 Object.assign(w.auth,{api:{},status:"authenticated",scopeWarning:null,principal:{subject:"actor225",tenantId:"tenant225",orgRef:"org225",roles:["kn_officer"],mfa:false,level:"tinh",displayName:"Tên hiển thị",scopeAll:false,scopeUnits:["X1"]},kn:{status:"active",assignments:[],menu:null,badges:null,context:{context_id:"contextA",assignment_id:"assignment225",role:"kn_tinh",department_code:"ke_hoach_tai_chinh",org_ref:"org225",communes:[],expires_at:"2035-06-02T03:00:00Z",default_route:"/kn/khtc"}}});
 state={year:2035,revision:0,draft:null,available:true,unavailable_reason:null};
 w.api.current.mockImplementation(async()=>structuredClone(state));w.api.history.mockResolvedValue({items:[],next_before_revision:null});w.khtc.programs.mockResolvedValue({items:[]});w.khtc.catalog.mockResolvedValue({items:[]});
 w.api.save.mockImplementation(async()=>{const ack=receipt();state={...state,revision:ack.revision,draft:ack};return ack;});
});
afterEach(()=>{cleanup();vi.useRealTimers();});
describe("initial persisted draft225",()=>{
 it("retains the exact server ACK/head through reload rather than restoring the old empty state",async()=>{
 render(<InitialDraftPage225 route={route()}/>);fireEvent.click(await screen.findByRole("button",{name:"Tạo nháp máy chủ"}));
 await screen.findByText(/Đã xác nhận lưu nháp máy chủ/);await screen.findByText(/Đã nhận nháp máy chủ lần ghi 1/);
 expect(w.api.save).toHaveBeenCalledTimes(1);expect(w.api.save.mock.calls[0]![1]).toEqual({expected_revision:0,entries:[]});expect(screen.getByRole("button",{name:"Lưu nháp máy chủ"})).toBeEnabled();expect(screen.getByRole("button",{name:/Áp dụng/})).toBeDisabled();
 });
 it("retains a known mutation ACK when the subsequent current-state reload fails",async()=>{
 w.api.current.mockResolvedValueOnce(structuredClone(state)).mockRejectedValueOnce(new ApiProblem({type:"about:blank",title:"Không đọc được trạng thái225",status:503,code:"service_unavailable"}));
 render(<InitialDraftPage225 route={route()}/>);fireEvent.click(await screen.findByRole("button",{name:"Tạo nháp máy chủ"}));
 await screen.findByRole("alert");expect(w.api.save).toHaveBeenCalledTimes(1);expect(screen.getByText(/Đã xác nhận lưu nháp máy chủ/)).toBeInTheDocument();expect(screen.getByText(/Đã nhận nháp máy chủ lần ghi 1/)).toBeInTheDocument();
 });
 it("retries an unknown ACK with the same immutable request/key instead of creating another operation",async()=>{
 w.api.save.mockRejectedValueOnce(new Error("transport unavailable"));render(<InitialDraftPage225 route={route()}/>);fireEvent.click(await screen.findByRole("button",{name:"Tạo nháp máy chủ"}));
 fireEvent.click(await screen.findByRole("button",{name:"Thử lại đúng thao tác chưa xác nhận"}));await screen.findByText(/Đã xác nhận lưu nháp máy chủ/);
 expect(w.api.save).toHaveBeenCalledTimes(2);expect(w.api.save.mock.calls[1]).toEqual(w.api.save.mock.calls[0]);
 });
 it("keeps the original operation through transport loss and idempotency-in-progress",async()=>{
 w.api.save.mockRejectedValueOnce(new Error("transport unavailable")).mockRejectedValueOnce(new ApiProblem({type:"about:blank",title:"Đang ghi225",status:409,code:"idempotency_in_progress"}));
 render(<InitialDraftPage225 route={route()}/>);fireEvent.click(await screen.findByRole("button",{name:"Tạo nháp máy chủ"}));fireEvent.click(await screen.findByRole("button",{name:"Thử lại đúng thao tác chưa xác nhận"}));
 await screen.findByRole("alert");fireEvent.click(await screen.findByRole("button",{name:"Thử lại đúng thao tác chưa xác nhận"}));await screen.findByText(/Đã xác nhận lưu nháp máy chủ/);
 expect(w.api.save).toHaveBeenCalledTimes(3);expect(w.api.save.mock.calls[1]).toEqual(w.api.save.mock.calls[0]);expect(w.api.save.mock.calls[2]).toEqual(w.api.save.mock.calls[0]);
 });
 it("does not navigate from an old master-create ACK after committed route ABA",async()=>{
 const pending=deferred<object>();w.khtc.createProgram.mockReturnValueOnce(pending.promise);const go=vi.fn();const a:PlanRoute={...route(),view:"new-program",tab:"programs",go};
 const r=render(<InitialDraftPage225 route={a}/>);fireEvent.change(await screen.findByLabelText(/Mã CT\/DA/),{target:{value:"CT225"}});fireEvent.change(screen.getByLabelText(/Tên CT\/DA/),{target:{value:"CT chờ xác nhận225"}});fireEvent.click(screen.getByRole("button",{name:"Lưu CT/DA chủ"}));await waitFor(()=>expect(w.khtc.createProgram).toHaveBeenCalledTimes(1));
 expect(screen.getByLabelText(/Tên CT\/DA/)).toBeDisabled();
 r.rerender(<InitialDraftPage225 route={{...a,view:"list"}}/>);await screen.findByRole("button",{name:"Chọn CT/DA vào tab"});
 r.rerender(<InitialDraftPage225 route={a}/>);await screen.findByRole("heading",{name:"CT/DA chủ mới"});expect(go).not.toHaveBeenCalled();
 await act(async()=>pending.resolve({}));expect(go).not.toHaveBeenCalled();
 });
 it("still navigates after master creation acknowledged in the same committed route",async()=>{
 w.khtc.createProgram.mockResolvedValueOnce({});const go=vi.fn();render(<InitialDraftPage225 route={{...route(),view:"new-program",tab:"programs",go}}/>);
 fireEvent.change(await screen.findByLabelText(/Mã CT\/DA/),{target:{value:"CT225"}});fireEvent.change(screen.getByLabelText(/Tên CT\/DA/),{target:{value:"CT đúng tuyến225"}});fireEvent.click(screen.getByRole("button",{name:"Lưu CT/DA chủ"}));await waitFor(()=>expect(go).toHaveBeenCalledTimes(1));expect(go).toHaveBeenCalledWith(base+"/chon-chuong-trinh?year=2035&tab=programs");
 });
 for(const change of ["contextABA","yearABA","api","actor","MFA","readonly"]){it(`retires pending ACK across ${change}`,async()=>{
 const pending=deferred<InitialDraft225>();w.api.save.mockReturnValueOnce(pending.promise);const r=render(<InitialDraftPage225 route={route()}/>);fireEvent.click(await screen.findByRole("button",{name:"Tạo nháp máy chủ"}));await waitFor(()=>expect(w.api.save).toHaveBeenCalledTimes(1));
 if(change==="contextABA"){w.auth.kn.context={...w.auth.kn.context!,context_id:"contextB"};r.rerender(<InitialDraftPage225 route={route()}/>);await screen.findByRole("button",{name:"Tạo nháp máy chủ"});w.auth.kn.context={...w.auth.kn.context!,context_id:"contextA"};}
 else if(change==="yearABA"){r.rerender(<InitialDraftPage225 route={route(2036)}/>);await screen.findByRole("heading",{name:/năm 2036/});}
 else if(change==="api")w.auth.api={} as AuthContextValue["api"];
 else if(change==="actor")w.auth.principal={...w.auth.principal!,subject:"successorActor"};
 else if(change==="MFA")w.auth.principal={...w.auth.principal!,mfa:true};
 else w.auth.kn.context={...w.auth.kn.context!,read_only:true};
 r.rerender(<InitialDraftPage225 route={route()}/>);await screen.findByRole("heading",{name:/năm 2035/});await act(async()=>pending.resolve(receipt()));
 expect(screen.queryByText(/Đã xác nhận lưu nháp máy chủ/)).not.toBeInTheDocument();expect(screen.queryByText(/Đã nhận nháp máy chủ lần ghi 1/)).not.toBeInTheDocument();
 });}
 for(const status of [409,422])it(`keeps a populated local tree after native ${status} without claiming success`,async()=>{
 const e={...blankEntry("programs","program",null,10),name:"Cây đã tải225",program_ref:"01990000-0000-7000-8000-000000002254"};const ack={...receipt(),entries:[e],entries_count:1};state={...state,revision:1,draft:ack};
 w.api.save.mockRejectedValueOnce(new ApiProblem({type:"about:blank",title:"Không thể ghi225",status,code:status===409?"extension.khtc_draft_revision_conflict":"extension.khtc_plan_invalid"}));
 render(<InitialDraftPage225 route={route()}/>);fireEvent.click(await screen.findByRole("button",{name:"Lưu nháp máy chủ"}));await screen.findByRole("alert");expect(screen.getByText("Cây đã tải225")).toBeInTheDocument();expect(screen.queryByText(/Đã xác nhận lưu nháp máy chủ/)).not.toBeInTheDocument();
 });
 it("does not fetch unpublished drafts for other departments while allowing read-only KHTC",async()=>{
 w.auth.kn.context={...w.auth.kn.context!,read_only:true};const r=render(<InitialDraftPage225 route={route()}/>);expect(await screen.findByRole("button",{name:"Tạo nháp máy chủ"})).toBeDisabled();expect(w.api.current).toHaveBeenCalledTimes(1);
 w.auth.kn.context={...w.auth.kn.context!,department_code:"trong_trot",read_only:false};r.rerender(<InitialDraftPage225 route={route()}/>);expect(await screen.findByText(/Nháp chưa áp dụng chỉ dành/)).toBeInTheDocument();expect(w.api.current).toHaveBeenCalledTimes(1);
 });
});
