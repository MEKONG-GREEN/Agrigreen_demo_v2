import type { components } from "../generated/application";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { TENANT } from "./personas";
import { khtcMockStore } from "./kn-khtc-periods";
import { khtcPNPrepared215 } from "./kn-khtc-reports";
import { filterRows } from "@/kn/khtc/reports/api";
import { zipStored } from "./kn-khtc-archive-zip";
type S = components["schemas"];
type Row = S["KHTCPNProgressRow"];
const DEPTS = ["trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"];
const PATH = /^\/v1\/extension\/khtc\/departments\/([a-z_]+)\/program-progress\/(periods|[0-9a-f-]+)(\/excel)?$/;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const iso = (s: string) => s.includes("T") ? s : s + "T00:00:00Z";
const number = (s: string) => s.length <= 96 && /^-?[0-9]+(?:\.[0-9]+)?$/.test(s);
const xml = (s: string) => s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
function excel(view: S["KHTCPNProgress"]): Uint8Array {
 const rows = [["Tiến độ chương trình của phòng"], [view.period.name], ["TT", "Chương trình / hạng mục", "ĐVT", "KH năm", "Lũy kế", "% KH"], ...view.rows.map((r,i)=>[String(i+1),r.name,r.unit,r.kind==="item"?r.planned??"—":"",r.year_to_date,r.kind==="item"&&!r.missing?r.percent??"—":"—"])];
 const sheet=rows.map((r,y)=>`<row r="${y+1}">${r.map((v,x)=>`<c r="${String.fromCharCode(65+x)}${y+1}" t="inlineStr"><is><t>${xml(v)}</t></is></c>`).join("")}</row>`).join("");
 const enc=new TextEncoder();
 const files=[
  {name:"[Content_Types].xml",data:'<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>'},
  {name:"_rels/.rels",data:'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>'},
  {name:"xl/workbook.xml",data:'<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Tiến độ phòng" sheetId="1" r:id="rId1"/></sheets></workbook>'},
  {name:"xl/_rels/workbook.xml.rels",data:'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>'},
  {name:"xl/worksheets/sheet1.xml",data:`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>${sheet}</sheetData></worksheet>`},
 ];
 return zipStored(files.map(f=>({name:f.name,data:enc.encode(f.data)})));
}
function handle(ctx: Ctx): Response {
 const match=PATH.exec(ctx.url.pathname)!;
 const department=match[1]!,id=match[2]!,isExcel=!!match[3];
 const kc=knContext(ctx.request.headers.get("X-AG-Context")??"");
 if(!kc || !DEPTS.includes(department) || !["admin","kn_tinh"].includes(kc.role) || kc.role!=="admin"&&kc.department_code!==department)return problem(403,"forbidden","Không có quyền xem tiến độ của phòng này");
 const all=khtcMockStore().filter(s=>(s.adhocTenant??TENANT)===ctx.persona.me.tenant_id && s.p.report_type==="program" && s.terminal!=="cancelled");
 const period=(p:typeof all[number]["p"]):S["KHTCPNPeriod"]=>({id:p.id,name:p.name,year:p.year,from:iso(p.from_date),to:iso(p.to_date),cutoff:iso(p.cutoff_date)});
 if(id==="periods"){
  const year=ctx.url.searchParams.get("year")??"",after=ctx.url.searchParams.get("after");
  if(!/^[0-9]{4}$/.test(year)||Number(year)<2000||Number(year)>2100||after&&!UUID.test(after))return problem(422,"extension.khtc_report_invalid","Năm hoặc con trỏ không hợp lệ");
  const rows=all.filter(s=>s.p.year===Number(year)&&s.p.departments.some(d=>d.code===department)&&(!after||s.p.id<after)).sort((a,b)=>b.p.id.localeCompare(a.p.id));
  const items=rows.slice(0,50).map(s=>period(s.p)),has_more=rows.length>50;
  return json({department,items,has_more,next_after:has_more?items.at(-1)!.id:null},200,{"Cache-Control":"no-store"});
 }
 const s=all.find(s=>s.p.id===id);
 if(!s)return problem(404,"extension.khtc_report_not_found","Không tìm thấy kỳ Mẫu 5");
 if(!s.p.departments.some(d=>d.code===department))return problem(403,"forbidden","Kỳ không giao cho phòng này");
 const snapshotID=ctx.url.searchParams.get("snapshot_id"),program=ctx.url.searchParams.get("program")??"",rawLevel=ctx.url.searchParams.get("level")??"";
 const level=rawLevel as ""|"achieved"|"progressing"|"slow"|"qualitative";
 if(snapshotID&&!UUID.test(snapshotID)||program&&!UUID.test(program)||!["","achieved","progressing","slow","qualitative"].includes(level)||isExcel&&!snapshotID)return problem(422,"extension.khtc_report_invalid","Bộ lọc hoặc snapshot không hợp lệ");
 const snapshot=khtcPNPrepared215(ctx,id,snapshotID??undefined);
 if(!snapshot && snapshotID)return problem(404,"extension.khtc_report_not_found","Không có snapshot đã chọn");
 const view:S["KHTCPNProgress"]={department,period:period(s.p),status:snapshot?"snapshot":"not_prepared",snapshot:snapshot?{id:snapshot.id,version:snapshot.version,captured_at:snapshot.captured_at}:null,programs:[],rows:[]};
 if(snapshot){
  const own=filterRows(snapshot.rows,{department});
  view.programs=own.filter(r=>r.kind==="program").map(r=>({ref:r.ref,name:r.name,roman:r.roman}));
  if(program&&!view.programs.some(p=>p.ref===program))return problem(403,"forbidden","Chương trình ngoài phạm vi phòng");
  for(const r of filterRows(snapshot.rows,{department,program,level})){
   if(!["program","group","item"].includes(r.kind)||r.kind==="item"&&(r.department!==department||!number(r.year_to_date)||r.planned!==null&&!number(r.planned)||r.percent!==null&&!number(r.percent)))return problem(503,"extension.dependency_unavailable","Snapshot chưa đủ số liệu hợp lệ");
   const row:Row={ref:r.ref,parent_ref:r.parent_ref,kind:r.kind as Row["kind"],roman:r.roman,name:r.name,unit:"",planned:null,year_to_date:"",percent:null,level:"",missing:false};
   if(r.kind==="item")Object.assign(row,{department:r.department,unit:r.unit,planned:r.planned,year_to_date:r.year_to_date,percent:r.percent,level:r.level,missing:r.missing});
   view.rows.push(row);
  }
 }
 if(!isExcel)return json(view,200,{"Cache-Control":"no-store"});
 return new Response(new Uint8Array(excel(view)).buffer,{status:200,headers:{"Content-Type":"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet","Content-Disposition":`attachment; filename="tien-do-phong-v${view.snapshot!.version}.xlsx"`,"Cache-Control":"no-store"}});
}
export const khtcPNProgressRoutes215: Route[]=[{method:"GET",pattern:PATH,handler:handle}];
