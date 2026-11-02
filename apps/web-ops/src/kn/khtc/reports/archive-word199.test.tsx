import {act,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {beforeEach,afterEach,describe,expect,it,vi} from "vitest";
import {useState} from "react";
import {createApiClient,type ApiClient} from "@/api/client";
import {saveBlob} from "@/lib/files";
import {KhtcArchiveScreen} from "./KhtcArchiveScreen";
import {KhtcArchiveWordScreen} from "./KhtcArchiveWordScreen";
import {ArchiveSelectionBoundary} from "./archive-selection";
const period="01900000-0000-7000-8000-000000000001", exp="01900000-0000-7000-8000-000000000002", run="01900000-0000-7000-8000-000000000003";
const auth:{api:ApiClient|null;principal:{subject:string;tenantId:string};kn:{context:{context_id:string}}}={api:null,principal:{subject:"actor",tenantId:"tenant-a"},kn:{context:{context_id:"a"}}};
let params={period,export:exp}, denied=false, delayed:((response:Response)=>void)|null=null, delayDownload=false;
const requests:Request[]=[];
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>auth}));
vi.mock("next/navigation",()=>({useParams:()=>params}));
vi.mock("@/lib/files",()=>({saveBlob:vi.fn()}));
const word=(version:number)=>({period_id:period,export_id:exp,run_id:version===1?run:"01900000-0000-7000-8000-000000000004",version,template:"khtc.mau_1",template_version:2,template_hash:"b".repeat(64),input_hash:"c".repeat(64),body_hash:"a".repeat(64),size:321,file_name:"saved.docx",content_type:"application/vnd.openxmlformats-officedocument.wordprocessingml.document",doc_number:"7/BC-TTKN",doc_number_auto:true,actor_ref:"actual-export-actor",at:"2030-01-01T00:00:00Z"});
beforeEach(()=>{
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
 vi.mocked(saveBlob).mockClear();requests.length=0;params={period,export:exp};denied=false;delayed=null;delayDownload=false;
 auth.principal={subject:"actor",tenantId:"tenant-a"};auth.kn={context:{context_id:"a"}};
 auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock",getContextId:()=>"a",fetch:async request=>{
  const req=request as Request;requests.push(req);const u=new URL(req.url);
  if(denied)return Response.json({status:403,code:"forbidden",title:"Không có quyền"},{status:403});
  if(u.pathname.endsWith("/download") || u.pathname.endsWith("/zip")){
    if(delayDownload)return new Promise<Response>(resolve=>{delayed=resolve;});
    return new Response("stored exact bytes",{headers:{"Content-Type":"application/octet-stream"}});
  }
  if(u.pathname.endsWith("/words"))return Response.json({period_id:period,export_id:exp,export_version:3,export_name:"Báo cáo chuyên đề đã lưu",items:[word(u.searchParams.get("offset")==="20"?1:2)],has_more:u.searchParams.get("offset")!=="20",offset:Number(u.searchParams.get("offset")??0)});
  return Response.json({items:[{id:exp,period_id:period,period_name:"Tuần 41",report_type:"week",document_no:"7/BC",version:3,final:false,provisional:true,actor_ref:"actor",at:"2030-01-01T00:00:00Z"}],has_more:false,offset:0});
 }});
});
afterEach(()=>vi.useRealTimers());
function Journey(){const [word,setWord]=useState(false);return <ArchiveSelectionBoundary><button onClick={()=>setWord(x=>!x)}>Đổi trang thử</button>{word?<KhtcArchiveWordScreen/>:<KhtcArchiveScreen/>}</ArchiveSelectionBoundary>;}
describe("#199 saved Word archive",()=>{
 it("uses a separate read-only detail route, actual export log, and bound download",async()=>{
  render(<KhtcArchiveWordScreen/>);await screen.findByText("Báo cáo chuyên đề đã lưu · Excel v3");
  const log=screen.getByRole("complementary",{name:"Log xuất Word"});expect(within(log).getByText("actual-export-actor",{exact:false})).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button",{name:"Thông tin Word lần 2"}));
  expect(screen.getByText("a".repeat(64))).toBeInTheDocument();
  expect(screen.getByRole("region",{name:"Thông tin Word lần 2"})).toHaveTextContent("321 byte");
  fireEvent.click(screen.getByRole("button",{name:"Tải Word lần 2"}));await waitFor(()=>expect(saveBlob).toHaveBeenCalledOnce());
  expect(vi.mocked(saveBlob).mock.calls[0]?.[1]).toBe("saved.docx");
  expect(requests.at(-1)?.url).toContain(`/exports/${exp}/words/01900000-0000-7000-8000-000000000004/download`);
  expect(requests.every(r=>r.method==="GET" && !new URL(r.url).pathname.endsWith("/report"))).toBe(true);
 });
 it("keeps Excel and multiple Word pages in one bounded mixed ZIP across routes",async()=>{
  render(<Journey/>);await screen.findByText("7/BC");
  expect(screen.getByRole("link",{name:"Word đã lưu Tuần 41 v3"})).toHaveAttribute("href",`/kn/khtc/bao-cao-da-xuat/${period}/${exp}/word`);
  fireEvent.click(screen.getByRole("checkbox",{name:"Chọn Tuần 41 v3"}));fireEvent.click(screen.getByRole("button",{name:"Đổi trang thử"}));
  fireEvent.click(await screen.findByRole("checkbox",{name:"Chọn Word lần 2"}));fireEvent.click(screen.getByRole("button",{name:"Tiếp"}));
  fireEvent.click(await screen.findByRole("checkbox",{name:"Chọn Word lần 1"}));
  expect(screen.getByText("Đã chọn 3/10 tệp Word/Excel · tối đa 20 MiB")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button",{name:"Tải ZIP Word/Excel"}));await waitFor(()=>expect(saveBlob).toHaveBeenCalledOnce());
  const request=requests.find(r=>r.url.endsWith("/zip"))!;expect(await request.clone().json()).toEqual({items:[{period_id:period,export_id:exp},{period_id:period,export_id:exp,format:"word",run_id:"01900000-0000-7000-8000-000000000004"},{period_id:period,export_id:exp,format:"word",run_id:run}]});
 });
 it("clears selected metadata and discards pending download when tenant changes",async()=>{
  const view=render(<KhtcArchiveWordScreen/>);fireEvent.click(await screen.findByRole("checkbox",{name:"Chọn Word lần 2"}));
  delayDownload=true;fireEvent.click(screen.getByRole("button",{name:"Tải Word lần 2"}));await waitFor(()=>expect(delayed).not.toBeNull());
  auth.principal={subject:"actor",tenantId:"tenant-b"};denied=true;view.rerender(<KhtcArchiveWordScreen/>);
  await screen.findByText("Bạn không có quyền thực hiện thao tác này.");
  await act(async()=>{delayed!(new Response("old tenant bytes"));});
  expect(saveBlob).not.toHaveBeenCalled();expect(screen.queryByText("7/BC-TTKN")).not.toBeInTheDocument();expect(screen.getByText("Đã chọn 0/10 bản Excel · tối đa 20 MiB")).toBeInTheDocument();
 });
 it("drops an old mixed ZIP response after changing selected KN session",async()=>{
  const view=render(<KhtcArchiveWordScreen/>);fireEvent.click(await screen.findByRole("checkbox",{name:"Chọn Word lần 2"}));delayDownload=true;
  fireEvent.click(screen.getByRole("button",{name:"Tải ZIP Word/Excel"}));await waitFor(()=>expect(delayed).not.toBeNull());
  auth.kn={context:{context_id:"b"}};view.rerender(<KhtcArchiveWordScreen/>);await screen.findByRole("checkbox",{name:"Chọn Word lần 2"});
  await act(async()=>{delayed!(new Response("old session ZIP"));});expect(saveBlob).not.toHaveBeenCalled();expect(screen.getByRole("checkbox",{name:"Chọn Word lần 2"})).not.toBeChecked();
 });
 it("rejects malformed route identifiers without querying an API",()=>{
  params={period:"1e100000",export:exp};render(<KhtcArchiveWordScreen/>);expect(screen.getByRole("alert")).toHaveTextContent("không hợp lệ");expect(requests).toHaveLength(0);
 });
});
