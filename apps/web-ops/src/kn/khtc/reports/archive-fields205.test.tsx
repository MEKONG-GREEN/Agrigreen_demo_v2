import {act,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {createApiClient,type ApiClient} from "@/api/client";
import {KhtcArchiveScreen} from "./KhtcArchiveScreen";
import type * as FileModule from "@/lib/files";
import {saveBlob} from "@/lib/files";
const auth:{api:ApiClient|null;context:Record<string,string>}={api:null,context:{id:"a"}};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:auth.api,kn:{context:auth.context}})}));
vi.mock("@/lib/files",async original=>({...await original<typeof FileModule>(),saveBlob:vi.fn()}));
const id="01900000-0000-7000-8000-000000000205";
const requests:{url:URL;body:unknown}[]=[];
const idB="01900000-0000-7000-8000-000000000206";
let twoRows=false,holdLists=false,releasePreview:((r:Response)=>void)|undefined;
let release:((r:Response)=>void)|undefined,hold=false;
beforeEach(()=>{
 requests.length=0;hold=false;twoRows=false;holdLists=false;releasePreview=undefined;release=undefined;auth.context={id:"a"};vi.clearAllMocks();
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
 auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>null,fetch:async input=>{
  const req=input as Request,u=new URL(req.url),body=req.method==="POST"?await req.clone().json():null;requests.push({url:u,body});
  if(u.pathname.endsWith("/fields"))return Response.json({items:[{code:u.searchParams.get("after")?"trong_trot":"chan_nuoi"}],has_more:!u.searchParams.get("after"),next_cursor:u.searchParams.get("after")?"":"chan_nuoi"});
  if(u.pathname.endsWith("/periods"))return Response.json({items:[{id,name:"Kỳ ngoài trang",report_type:"week"}],has_more:false,offset:0});
  if(u.pathname.endsWith("/preview")){if(u.pathname.includes(idB))return new Promise<Response>(resolve=>{releasePreview=resolve;});return Response.json({export_id:id,version:1,content:{texts:{B:"Nội dung đóng băng A"},data:{rows:[]}}});}
  if(u.pathname.endsWith("/download")){if(hold)return new Promise<Response>(resolve=>{release=resolve;});return new Response("frozen bytes");}
  if(u.pathname.endsWith("/zip"))return new Response("zip bytes");
  if(u.pathname.endsWith("/exported-reports")){if(holdLists)return new Promise<Response>(()=>{});return Response.json({items:[...(twoRows?[{id:idB,period_id:idB,period_name:"Kỳ 206",report_type:"week",document_no:"206/BC",version:2,final:true,provisional:false,actor_ref:idB,at:"2030-01-01T00:00:00Z"}]:[]),{id,period_id:id,period_name:"Kỳ 205",report_type:"week",document_no:"205/BC",version:1,final:true,provisional:false,actor_ref:id,at:"2030-01-01T00:00:00Z",field_codes:["chan_nuoi","trong_trot"],field_status:"partial"}],has_more:false,offset:0});}
  return Response.json({code:"unexpected",title:"Unexpected"},{status:500});
 }});
});
afterEach(()=>vi.useRealTimers());
const last=(suffix:string)=>requests.filter(r=>r.url.pathname.endsWith(suffix)).at(-1)!;
async function chooseField(){fireEvent.click(screen.getByRole("button",{name:"Chọn lĩnh vực"}));const picker=await screen.findByRole("region",{name:"Chọn lĩnh vực đã xuất"});await within(picker).findByRole("button",{name:"Chọn lĩnh vực chan_nuoi"});fireEvent.click(within(picker).getByRole("button",{name:"Lĩnh vực tiếp"}));fireEvent.click(await within(picker).findByRole("button",{name:"Chọn lĩnh vực trong_trot"}));await waitFor(()=>expect(last("/exported-reports").url.searchParams.get("field")).toBe("trong_trot"));}
it("keeps multi-field metadata, complete cursor choices and selector through Excel and ZIP",async()=>{
 render(<KhtcArchiveScreen/>);await screen.findByText("205/BC");expect(screen.getByRole("cell",{name:/^chan_nuoi, trong_trot\s*Metadata một phần$/})).toBeInTheDocument();
 fireEvent.click(screen.getByRole("checkbox",{name:"Chọn Kỳ 205 v1"}));await chooseField();expect(screen.getByRole("button",{name:"Tải ZIP Excel"})).toBeDisabled();
 fireEvent.click(screen.getByRole("button",{name:"Tải Excel Kỳ 205 v1"}));await waitFor(()=>expect(last("/download").url.searchParams.get("field")).toBe("trong_trot"));await waitFor(()=>expect(saveBlob).toHaveBeenCalledTimes(1));
 fireEvent.click(screen.getByRole("checkbox",{name:"Chọn Kỳ 205 v1"}));fireEvent.click(screen.getByRole("button",{name:"Tải ZIP Excel"}));await waitFor(()=>expect(last("/zip").body).toMatchObject({field:"trong_trot",items:[{export_id:id}]}));
});
it("intersects period choices with outer document/version/field filters and independent period name",async()=>{
 render(<KhtcArchiveScreen/>);await screen.findByText("205/BC");await chooseField();fireEvent.change(screen.getByLabelText("Tên kỳ hoặc số văn bản"),{target:{value:"205/BC"}});fireEvent.click(screen.getByRole("button",{name:"Tìm kiếm"}));fireEvent.click(screen.getByLabelText("Mọi phiên bản"));fireEvent.click(screen.getByRole("button",{name:"Chọn kỳ"}));
 const picker=await screen.findByRole("region",{name:"Chọn kỳ đã xuất"});await within(picker).findByRole("button",{name:"Lọc kỳ Kỳ ngoài trang"});fireEvent.change(within(picker).getByLabelText("Tìm tên kỳ"),{target:{value:"Kỳ"}});fireEvent.click(within(picker).getByRole("button",{name:"Tìm kỳ"}));
 await waitFor(()=>expect(last("/periods").url.searchParams.get("q")).toBe("Kỳ"));const q=last("/periods").url.searchParams;expect(q.get("document_q")).toBe("205/BC");expect(q.get("versions")).toBe("true");expect(q.get("field")).toBe("trong_trot");
});
it.each(["filter","authority","api"])("discards late downloaded bytes after %s epoch changes",async epoch=>{
 hold=true;const view=render(<KhtcArchiveScreen/>);await screen.findByText("205/BC");fireEvent.click(screen.getByRole("button",{name:"Tải Excel Kỳ 205 v1"}));await waitFor(()=>expect(release).toBeTypeOf("function"));
 if(epoch==="filter")fireEvent.change(screen.getByLabelText("Metadata lĩnh vực"),{target:{value:"unknown"}});else{if(epoch==="authority")auth.context={id:"b"};else auth.api={...auth.api!};view.rerender(<KhtcArchiveScreen/>);}
 await act(async()=>{release!(new Response("stale bytes"));});expect(saveBlob).not.toHaveBeenCalled();
});

it("does not relabel preview A contents as B while B is deferred",async()=>{
 twoRows=true;render(<KhtcArchiveScreen/>);await screen.findByText("205/BC");
 fireEvent.click(screen.getByRole("button",{name:"Xem Kỳ 205 v1"}));await screen.findByText("Nội dung đóng băng A");
 fireEvent.click(screen.getByRole("button",{name:"Xem Kỳ 206 v2"}));await waitFor(()=>expect(releasePreview).toBeTypeOf("function"));
 expect(screen.queryByText("Nội dung đóng băng A")).not.toBeInTheDocument();
 await act(async()=>{releasePreview!(Response.json({export_id:idB,version:2,content:{texts:{B:"Nội dung đóng băng B"},data:{rows:[]}}}));});
 expect(await screen.findByText("Nội dung đóng băng B")).toBeInTheDocument();
});
it("removes old filter rows while the new matching data is pending",async()=>{
 render(<KhtcArchiveScreen/>);await screen.findByText("205/BC");holdLists=true;
 fireEvent.change(screen.getByLabelText("Metadata lĩnh vực"),{target:{value:"unknown"}});
 expect(screen.queryByRole("checkbox",{name:"Chọn Kỳ 205 v1"})).not.toBeInTheDocument();
});
