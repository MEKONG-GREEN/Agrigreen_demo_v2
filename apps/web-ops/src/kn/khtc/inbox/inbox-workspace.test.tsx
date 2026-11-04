import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { KhtcFormSheet } from "../forms/api";
import { InboxWorkspace } from "./inbox-workspace";
import { inboxHref, INBOX_BASE, parseInboxRoute } from "./inbox-routes";

const state=vi.hoisted(()=>({path:"",search:"",api:{} as object,status:"authenticated",principal:{actor_ref:"actor-a",tenant_id:"tenant-a",amr:["mfa"]},context:{context_id:"ctx-a",department_code:"trong_trot",role:"kn_tinh",read_only:false}}));
const get=vi.hoisted(()=>vi.fn());
vi.mock("next/navigation",()=>({usePathname:()=>state.path,useSearchParams:()=>new URLSearchParams(state.search)}));
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:state.api,status:state.status,principal:state.principal,kn:{context:state.context}})}));
vi.mock("@/shell/Breadcrumb",()=>({useBreadcrumbTail:()=>undefined}));
vi.mock("../forms/api",()=>({KHTC_DEPT:"ke_hoach_tai_chinh",createKhtcFormApi:()=>({get})}));
const A="11111111-1111-4111-8111-111111111111",B="22222222-2222-4222-8222-222222222222";
function sheet(id=A,name="Phiếu A",department="trong_trot"):KhtcFormSheet {
  return {period:{id,name,from_date:"2030-06-01",to_date:"2030-06-07",due_at:"2030-06-07T10:00:00Z",max_submissions:3,terminal:""},department,department_name:department==="trong_trot"?"Trồng trọt":"CNTS",state:"submitted",state_label:"Đã gửi",attempt:1,editable:false,template:{code:"KHTC-PHIEU",version:2},plan:{version:3,decision_no:"QĐ226"},content_source:"submitted",draft_at:null,request_note:"",submissions:[{id:"submission-a",attempt:1,submitted_at:"2030-06-05T03:00:00Z",late:false,decision:"returned",review_note:"Bổ sung số liệu thật",reviewed_at:"2030-06-06T04:00:00Z"}]} as KhtcFormSheet;
}
function deferred<T>(){let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>{resolve=r;});return {promise,resolve};}
beforeEach(()=>{state.path=`${INBOX_BASE}/phieu/${A}`;state.search="view=current";state.api={};state.status="authenticated";state.principal={actor_ref:"actor-a",tenant_id:"tenant-a",amr:["mfa"]};state.context={context_id:"ctx-a",department_code:"trong_trot",role:"kn_tinh",read_only:false};get.mockReset();get.mockResolvedValue(sheet());});
afterEach(cleanup);

describe("inbox226 metadata routes",()=>{
  it("accepts private list-prefix routes and rejects malformed or ambiguous query",()=>{
    expect(parseInboxRoute(state.path,new URLSearchParams("view=sent"))).toMatchObject({id:A,view:"sent",history:false});
    expect(parseInboxRoute(`/kn/phong-ban/trong_trot/bao-cao-khtc/phieu/${A}/lich-su`,new URLSearchParams())).toMatchObject({department:"trong_trot",history:true});
    for(const [path,q] of [[`${INBOX_BASE}/phieu/1e100000`,""],[`${INBOX_BASE}/phieu/00000000-0000-0000-0000-000000000000`,""],[state.path,"department=trong_trot&department=trong_trot"],[state.path,"view=unknown"],[state.path,"actor=forged"],[`/kn/phong-ban/unknown/bao-cao-khtc/phieu/${A}`,""],[`/kn/phong-ban/trong_trot/bao-cao-khtc/phieu/${A}`,"department=chan_nuoi_thuy_san"]]) expect(parseInboxRoute(path!,new URLSearchParams(q))).toBeNull();
    expect(inboxHref(INBOX_BASE,A,undefined,"sent",true)).toBe(`${INBOX_BASE}/phieu/${A}/lich-su?view=sent`);
  });
  it("renders real detail metadata and preserves existing entry action without a write",async()=>{
    render(<InboxWorkspace/>);expect(await screen.findByRole("heading",{name:/^Chi tiết phiếu · Phiếu A/})).toBeInTheDocument();
    expect(screen.getByRole("link",{name:"Xem nội dung phiếu"})).toHaveAttribute("href",`/kn/khtc/phieu/${A}?department=trong_trot`);
    expect(screen.getByRole("link",{name:"Lịch sử gửi và rà soát"})).toHaveAttribute("href",`${INBOX_BASE}/phieu/${A}/lich-su?view=current&department=trong_trot`);
    expect(screen.queryByRole("button",{name:/Lưu|Gửi/})).not.toBeInTheDocument();expect(get).toHaveBeenCalledWith(A,"trong_trot");
  });
  it("history shows server metadata only and has no invented actor/content or write controls",async()=>{
    state.path+="/lich-su";render(<InboxWorkspace/>);const table=await screen.findByRole("table",{name:"Các lần gửi phiếu"});
    expect(within(table).getByText("Bổ sung số liệu thật")).toBeInTheDocument();expect(within(table).getByText("Yêu cầu bổ sung")).toBeInTheDocument();
    expect(screen.getByText(/chưa có thông tin người thực hiện/)).toBeInTheDocument();expect(screen.queryByRole("columnheader",{name:/Người/})).not.toBeInTheDocument();expect(screen.queryAllByRole("button")).toHaveLength(0);
  });
  it.each(["api","actor","tenant","context","role","mfa","department"])("clears old header/content synchronously when %s scope changes",async kind=>{
    const view=render(<InboxWorkspace/>);await screen.findByRole("heading",{name:/^Chi tiết phiếu · Phiếu A/});const next=deferred<KhtcFormSheet>();get.mockReturnValue(next.promise);
    if(kind==="api")state.api={};if(kind==="actor")state.principal={...state.principal,actor_ref:"actor-b"};if(kind==="tenant")state.principal={...state.principal,tenant_id:"tenant-b"};if(kind==="context")state.context={...state.context,context_id:"ctx-b"};if(kind==="role")state.context={...state.context,read_only:true,role:"kn_xa"};if(kind==="mfa")state.principal={...state.principal,amr:[]};if(kind==="department")state.context={...state.context,department_code:"chan_nuoi_thuy_san"};
    view.rerender(<InboxWorkspace/>);expect(screen.queryAllByText(/Phiếu A/)).toHaveLength(0);expect(screen.queryByText("QĐ226")).not.toBeInTheDocument();
    await act(async()=>next.resolve(sheet(A,"Phiếu B",state.context.department_code)));expect(await screen.findByRole("heading",{name:/^Chi tiết phiếu · Phiếu B/})).toBeInTheDocument();
  });
  it("committed route A→B→A rejects late oldA and accepts only freshA",async()=>{
    const old=deferred<KhtcFormSheet>(),middle=deferred<KhtcFormSheet>(),fresh=deferred<KhtcFormSheet>();get.mockReset().mockReturnValueOnce(old.promise).mockReturnValueOnce(middle.promise).mockReturnValueOnce(fresh.promise);
    const view=render(<InboxWorkspace/>);await waitFor(()=>expect(get).toHaveBeenCalledTimes(1));state.path=`${INBOX_BASE}/phieu/${B}`;view.rerender(<InboxWorkspace/>);await waitFor(()=>expect(get).toHaveBeenCalledTimes(2));state.path=`${INBOX_BASE}/phieu/${A}`;view.rerender(<InboxWorkspace/>);await waitFor(()=>expect(get).toHaveBeenCalledTimes(3));
    await act(async()=>{old.resolve(sheet(A,"OLD A MUST NOT RETURN"));middle.resolve(sheet(B,"OLD B MUST NOT RETURN"));});expect(screen.queryByText(/MUST NOT RETURN/)).not.toBeInTheDocument();
    await act(async()=>fresh.resolve(sheet(A,"Fresh A")));expect(await screen.findByRole("heading",{name:/^Chi tiết phiếu · Fresh A/})).toBeInTheDocument();
  });
  it("Admin path binds department and bad paths fail before GET",async()=>{
    state.context={...state.context,role:"admin",department_code:"ke_hoach_tai_chinh"};state.path=`/kn/phong-ban/chan_nuoi_thuy_san/bao-cao-khtc/phieu/${A}`;get.mockResolvedValue(sheet(A,"Admin CNTS","chan_nuoi_thuy_san"));const view=render(<InboxWorkspace/>);await screen.findByRole("heading",{name:/^Chi tiết phiếu · Admin CNTS/});expect(get).toHaveBeenCalledWith(A,"chan_nuoi_thuy_san");
    state.search="department=trong_trot";view.rerender(<InboxWorkspace/>);expect(screen.getByRole("alert")).toHaveTextContent("Kiểm tra");expect(get).toHaveBeenCalledTimes(1);expect(screen.queryByText("Admin CNTS")).not.toBeInTheDocument();
  });
  it("rejects mismatched returned resource and denied reads without revealing stale sheet",async()=>{
    get.mockResolvedValue(sheet(B,"Wrong resource"));render(<InboxWorkspace/>);expect(await screen.findByRole("alert")).toHaveTextContent("Không đọc được phiếu");expect(screen.queryByText("Wrong resource")).not.toBeInTheDocument();
  });
});
