import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import type * as Files from "@/lib/files";
import { saveBlob } from "@/lib/files";
import { KhtcArchiveScreen } from "./KhtcArchiveScreen";
import type { ArchiveRow } from "./archive-api";
const auth: {api: ApiClient|null}={api:null};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:auth.api})}));
vi.mock("@/lib/files",async (original)=>({...await original<typeof Files>(),saveBlob:vi.fn()}));
const requests:Request[]=[];
const row:ArchiveRow={id:"01900000-0000-7000-8000-000000000001",period_id:"01900000-0000-7000-8000-000000000002",period_name:"Tuần 41",report_type:"week",document_no:"12/BC-TTKN",version:2,final:true,provisional:false,actor_ref:"01900000-0000-7000-8000-000000000003",at:"2026-10-14T04:00:00Z"};
beforeEach(()=>{requests.length=0;vi.mocked(saveBlob).mockClear();auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.tw",getContextId:()=>"test",fetch:async(req)=>{
 const r=req as Request;requests.push(r);const url=new URL(r.url);
 if(url.pathname.endsWith("/download"))return new Response("exact exported bytes",{headers:{"Content-Disposition":'attachment; filename="frozen.xlsx"'}});
 if(url.pathname.endsWith("/exported-reports")){const page=url.searchParams.get("offset")==="20";return Response.json({items:[{...row,id:page?"01900000-0000-7000-8000-000000000004":row.id,period_name:page?"Tuần 40":row.period_name}],has_more:!page,offset:page?20:0});}
 return Response.json({code:"unexpected_request",title:"Unexpected"},{status:500});
 }});});
describe("W11.69 immutable exported archive",()=>{
 it("lists exported metadata without ever requesting a draft; downloads the frozen file",async()=>{
  render(<KhtcArchiveScreen/>);expect(await screen.findByText("12/BC-TTKN")).toBeInTheDocument();expect(screen.getByText("Bản cuối")).toBeInTheDocument();
  expect(screen.queryByRole("button",{name:"Lưu bản soạn"})).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button",{name:"Tải Excel Tuần 41 v2"}));
  await waitFor(()=>expect(saveBlob).toHaveBeenCalledTimes(1));
  expect(vi.mocked(saveBlob).mock.calls[0]?.[1]).toBe("frozen.xlsx");
  expect(requests.every(r=>r.method==="GET")).toBe(true);
  expect(requests.some(r=>new URL(r.url).pathname.endsWith("/report"))).toBe(false);
  expect(requests.at(-1)?.url).toContain(`/periods/${row.period_id}/report/exports/${row.id}/download`);
 });
 it("paginates and resets to page one on filters; bounds free text",async()=>{
  render(<KhtcArchiveScreen/>);await screen.findByText("Tuần 41");
  fireEvent.click(screen.getByRole("button",{name:"Tiếp"}));await screen.findByText("Tuần 40");expect(screen.getByText("Trang 2")).toBeInTheDocument();
  fireEvent.click(screen.getByLabelText("Mọi phiên bản"));await screen.findByText("Tuần 41");
  await waitFor(()=>expect(new URL(requests.at(-1)!.url).searchParams.get("versions")).toBe("true"));
  expect(new URL(requests.at(-1)!.url).searchParams.get("offset")).toBe("0");
  fireEvent.change(screen.getByLabelText("Tên kỳ hoặc số văn bản"),{target:{value:"12/BC"}});
  expect(screen.getByLabelText("Tên kỳ hoặc số văn bản")).toHaveAttribute("maxlength","200");
  fireEvent.click(screen.getByRole("button",{name:"Tìm kiếm"}));
  await waitFor(()=>expect(new URL(requests.at(-1)!.url).searchParams.get("q")).toBe("12/BC"));
  expect(screen.getByLabelText("Năm").tagName).toBe("SELECT");
 });
 it("denied API never presents archived rows or editing actions",async()=>{
  auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.xa",getContextId:()=>"test",fetch:async()=>Response.json({type:"about:blank",title:"Không có quyền",status:403,code:"forbidden"},{status:403})});
  render(<KhtcArchiveScreen/>);expect(await screen.findByText("Bạn không có quyền thực hiện thao tác này.")).toBeInTheDocument();expect(screen.queryByRole("table")).not.toBeInTheDocument();
 });
});