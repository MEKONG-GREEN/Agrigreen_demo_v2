// Giả lập FN16–18 v4 dùng cùng kho nhân sự và ma trận quyền; native tests kiểm tệp/DB thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { provinceOrgRef } from "./data";
import { hrmActiveItem } from "./kn-hrm";
import { hrmStaffInternals as H } from "./kn-hrm-staff";
import { APPRAISAL_RATINGS, validateAppraisalProposal } from "@/kn/hrm/appraisal-form";
type S=components["schemas"];type Template=S["HrmAppraisalTemplate"];type Period=S["HrmAppraisalPeriod"];type Item=S["HrmAppraisalItem"];type Change=S["HrmAppraisalEntryChange"];
const V4="hrm.appraisal-proposal/v4",GROUPS=["vien_chuc","lanh_dao","can_bo_xa"];
const quarterMembers=new Map<string,Item["member"]>();
const templates=new Map<string,Template[]>(),periods=new Map<string,Period[]>(),entries=new Map<string,Item[]>(),replays=new Map<string,{body:string;value:unknown}>();
export function resetHrmAppraisals(){quarterMembers.clear();templates.clear();periods.clear();entries.clear();replays.clear();}
function tenant(ctx:Ctx){return ctx.persona.me.tenant_id;}
function actor(ctx:Ctx){const c=H.caller(ctx),k=knContext(ctx.request.headers.get("X-AG-Context")??"");return c instanceof Response?c:{...c,manager:!!k?.manager,node:c.group==="admin"?provinceOrgRef(1):k?.org_ref??tenant(ctx)};}
type Caller=Exclude<ReturnType<typeof actor>,Response>;
function central(c:Caller){return ["admin","hcth"].includes(c.group);}
function center(c:Caller){return central(c)&&c.m.assessment==="all";}
function nodeAllows(c:Caller,p:Period){return c.group==="admin"||c.node===p.node_ref;}
function canRead(c:Caller,x:Item){const self=x.member.account_ref===c.ref,unit=x.member.management_unit===c.unit;
  return (c.m.assessment==="all"||c.m.assessment==="unit"&&(unit||self)||c.m.assessment==="self"&&self)&&(central(c)||c.group==="manager"&&c.manager&&unit&&!self&&x.member.summary_kind==="MS05");}
function projected(c:Caller,x:Item,p:Period):Item {const out=structuredClone(x);out.legacy_read_only=p.workflow_version!==V4;out.can_manage=!out.legacy_read_only&&!c.readOnly&&(x.member.summary_kind==="MS06"?central(c):c.group==="admin"||c.manager&&["manager","hcth"].includes(c.group)&&x.member.management_unit===c.unit&&x.member.account_ref!==c.ref);out.can_center=!out.legacy_read_only&&center(c)&&!c.readOnly;out.quarter_results=out.quarter_results?.filter(q=>{const member=quarterMembers.get(q.entry_version);return !!member&&canRead(c,{...x,member});});out.manager_late=!!out.manager_submitted_at?out.manager_submitted_at>p.manager_due_at:["not_started","change_requested"].includes(out.status)&&new Date().toISOString()>p.manager_due_at;return out;}
function key(ctx:Ctx,kind:string,code:string){return `${tenant(ctx)}:${kind}:${code}`;}
function itemKey(ctx:Ctx,period:string,staff:string){return key(ctx,period,staff);}
function members(ctx:Ctx,id:string){return [...entries.entries()].filter(([k])=>k.startsWith(`${tenant(ctx)}:${id}:`)).map(([,xs])=>xs.at(-1)!);}
function latestTemplates(ctx:Ctx,kind:string){return [...templates.entries()].filter(([k])=>k.startsWith(`${tenant(ctx)}:${kind}:`)).map(([,v])=>v.at(-1)!);}
function failure(field:string,reason:string){return validation([{field,reason}]);}
function proposalFailure(x:Pick<Item,"self_rating"|"collective_rating"|"reason">){const errors=validateAppraisalProposal(x.self_rating,x.collective_rating,x.reason);return Object.keys(errors).length?validation(Object.entries(errors).map(([field,reason])=>({field,reason}))):null;}
function redacted(c:Caller,p:Period):Period{return center(c)?p:{...p,forms:{},summaries:{},counts:{},warnings:[],signed_documents:{}};}
function countPeriod(ctx:Ctx,c:Caller,p:Period):Period {const out=structuredClone(redacted(c,p));if(!center(c))return out;const units=new Map<string,boolean>();out.counts={};for(const x of members(ctx,p.id)){if(x.member.excluded_reason){out.counts.excluded=(out.counts.excluded??0)+1;continue;}for(const k of [x.status,x.member.summary_kind])out.counts[k]=(out.counts[k]??0)+1;const unit=x.member.management_unit;units.set(unit,(units.get(unit)??true)&&["sent","closed"].includes(x.status));}out.counts.units_sent=[...units.values()].filter(Boolean).length;return out;}
function append(ctx:Ctx,c:Caller,p:Period,x:Item,action:string){const next={...structuredClone(x),id:uuidv7(),version:x.version+1,actor_ref:c.ref,recorded_at:new Date().toISOString(),action};entries.get(itemKey(ctx,p.id,x.member.staff_id))!.push(next);return next;}
function handle(ctx:Ctx):Response {
  const c=actor(ctx);if(c instanceof Response)return c;if(!["admin","hcth","manager"].includes(c.group)||c.m.assessment==="none"||!c.m.assessment)return problem(403,"forbidden","Không có quyền đánh giá");
  const path=ctx.url.pathname.replace("/v1/hrm/appraisals","").split("/").filter(Boolean),write=ctx.request.method==="POST";
  if(write&&c.readOnly)return problem(403,"forbidden","Ngữ cảnh chỉ xem");
  const body=ctx.body as Record<string,unknown>|null,idem=ctx.request.headers.get("Idempotency-Key")??"",fp=JSON.stringify([c.ref,path,body]),rk=key(ctx,"request",idem);
  if(write){if(idem.length<8||idem.length>200)return failure("Idempotency-Key","Khóa từ 8 đến 200 ký tự");const prior=replays.get(rk);if(prior){
    if(path[0]==="templates"&&!center(c))return problem(403,"forbidden","Chỉ HCTH/Admin quản lý mẫu");
    if(path[0]==="periods"){
      const requiresCenter=!path[1]||!path[2]&&body?.action!=="send"||path[2]==="exports";
      if(requiresCenter&&!center(c)||path[2]==="exports"&&c.m.export!=="all")return problem(403,"forbidden","Không được thực hiện thao tác HCTH");
      const historical=path[1]?(periods.get(tenant(ctx))??[]).find(p=>p.id===path[1]):prior.value as Period;
      if(!historical||!nodeAllows(c,historical)||!center(c)&&!members(ctx,historical.id).some(x=>canRead(c,x)))return problem(404,"hrm.appraisal_not_found","Đợt ngoài phạm vi phiên hiện tại");
      if(path[2]==="entries"){const item=entries.get(itemKey(ctx,historical.id,path[3]!))?.at(-1);if(!item||!canRead(c,item))return problem(404,"hrm.appraisal_not_found","Đề xuất ngoài phạm vi phiên hiện tại");const caps=projected(c,item,historical),manage=["manager_submit","send"].includes(String(body?.action));if(manage?!caps.can_manage:!caps.can_center)return problem(403,"forbidden","Không được ghi đề xuất trong phiên hiện tại");}
    }
    return prior.body===fp?json(structuredClone(prior.value),201):problem(409,"hrm.appraisal_conflict","Khóa đã dùng cho dữ liệu khác");
   }}
  const saved=(value:unknown)=>{replays.set(rk,{body:fp,value:structuredClone(value)});return json(value,201);};
  if(path[0]==="templates"){
    if(!center(c))return problem(403,"forbidden","Chỉ HCTH/Admin quản lý mẫu");const kind=path[1]!,code=path[2];if(!["summary","form"].includes(kind)||write&&kind!=="summary")return problem(404,"hrm.appraisal_not_found","Không còn writer phiếu chấm điểm");
    if(!write)return json({items:code?structuredClone(templates.get(key(ctx,kind,code))??[]):latestTemplates(ctx,kind)});
    if(!code||!/^[a-z][a-z0-9_]{0,39}$/.test(code))return failure("code","Mã mẫu không hợp lệ");const old=templates.get(key(ctx,kind,code))??[];
    if(body?.version!==(old.at(-1)?.version??0))return problem(409,"hrm.appraisal_conflict","Phiên bản mẫu đã đổi");
    const status=body?.status as Template["status"],summary=body?.summary as S["HrmAppraisalSummary"]|undefined;
    if(!["draft","published","retired"].includes(status))return failure("status","Chọn trạng thái mẫu");
    if(!summary||body?.form||summary.code!==code||!["MS05","MS06"].includes(summary.kind)||summary.columns.length!==8||new Set(summary.columns.map(x=>x.key)).size!==8||APPRAISAL_RATINGS.some(r=>!summary.ratings[r]?.trim()))return failure("summary","Mẫu 05/06 cần đủ tám cột và bốn mức xếp loại");
    for(const k of ["number","name","title","audience","signature"] as const)if(!summary[k]?.trim())return failure(k,"Nội dung bắt buộc");
    const t:Template={id:uuidv7(),kind:"summary",code,status,version:old.length+1,recorded_at:new Date().toISOString(),actor_ref:c.ref,summary:structuredClone(summary)};templates.set(key(ctx,kind,code),[...old,t]);return saved(t);
  }
  const all=periods.get(tenant(ctx))??[];
  if(path[0]!=="periods")return problem(404,"not_found","Không có đường dẫn");
  const periodID=path[1],p=all.find(x=>x.id===periodID&&nodeAllows(c,x));
  if(!periodID){if(!write)return json({items:all.filter(x=>nodeAllows(c,x)&&(center(c)||members(ctx,x.id).some(e=>canRead(c,e)))).map(x=>countPeriod(ctx,c,x))});
    if(!center(c))return problem(403,"forbidden","Chỉ HCTH/Admin mở đợt");const year=body?.year as number,quarter=body?.quarter as number,groups=body?.groups as string[]|undefined;
    if(!Number.isInteger(year)||year<2020||year>2100||!Number.isInteger(quarter)||quarter<0||quarter>4)return failure("year","Năm 2020–2100, quý 0–4");
    if(all.some(x=>x.workflow_version===V4&&x.year===year&&x.quarter===quarter&&nodeAllows(c,x)&&!x.closed))return problem(409,"hrm.appraisal_conflict","Đang có đợt mở cùng kỳ");
    if(body?.forms||body?.self_due_at||body?.summary_due_at||!Array.isArray(groups)||!groups.length||groups.length>3||new Set(groups).size!==groups.length||groups.some(g=>!GROUPS.includes(g)))return failure("groups","Chọn nhóm đối tượng, không còn bước tự chấm");
    const selected=body?.summaries as Record<string,string>|undefined,papers:Record<string,Template>={};
    for(const [kind,id] of Object.entries(selected??{})){const t=latestTemplates(ctx,"summary").find(x=>x.id===id&&x.status==="published"&&x.summary?.kind===kind);if(!t)return failure("summaries","Chọn phiên bản mẫu đã phát hành");papers[kind]=structuredClone(t);}
    const at=new Date().toISOString(),id=uuidv7(),cutoff=String(body?.cutoff_on),due=String(body?.manager_due_at);
    if(!/^\d{4}-\d{2}-\d{2}$/.test(cutoff)||cutoff.slice(0,4)!==String(year)||!Number.isFinite(Date.parse(due))||Date.parse(due)<=Date.parse(cutoff))return failure("manager_due_at","Hạn gửi sau ngày chốt, có múi giờ");
    const next:Period={workflow_version:V4,groups:groups as Period["groups"],id,version:1,year,quarter,node_ref:c.node,label:quarter?`Quý ${["","I","II","III","IV"][quarter]}/${year}`:`Năm ${year}`,cutoff_on:cutoff,known_at:at,self_due_at:"0001-01-01T00:00:00Z",manager_due_at:due,summary_due_at:"0001-01-01T00:00:00Z",forms:{},summaries:papers,self_locked:false,manager_locked:false,closed:false,signed_documents:{},warnings:[],counts:{},recorded_at:at,actor_ref:c.ref};
    const excluded=body?.excluded as {staff_id:string;reason:string}[]|undefined,seen=new Set<string>(),cohort:Item[]=[];
    for(const staff of H.staff()){
      if(staff.type==="hop_dong")continue;const snap=H.project(staff.events,cutoff);if(snap.status!=="active")continue;const position=hrmActiveItem("position",snap.position),leader=staff.type!=="can_bo_xa"&&!!position?.attrs?.leader,group=staff.type==="can_bo_xa"?"can_bo_xa":leader?"lanh_dao":"vien_chuc";if(!groups.includes(group))continue;
      const summaryKind=leader?"MS06":"MS05";if(!papers[summaryKind])return failure("summaries","Thiếu mẫu cho nhóm đối tượng");const exclusion=excluded?.find(x=>x.staff_id===staff.id);if(exclusion){if([...exclusion.reason.trim()].length<10)return failure("excluded","Loại trừ cần lý do 10 ký tự");seen.add(staff.id);}
      const item:Item={legacy_read_only:false,id:uuidv7(),period_id:id,version:1,actor_ref:c.ref,recorded_at:at,action:"open",name:staff.profile.full_name,member:{staff_id:staff.id,subject_ref:staff.id,account_ref:staff.account_ref,code:staff.code,staff_type:staff.type,group,unit:snap.unit,management_unit:snap.mgmt_unit||snap.unit,position:snap.position,position_name:position?.name??snap.position,position_order:position?.sort_order??0,unit_name:H.units().find(u=>u.code===(snap.mgmt_unit||snap.unit))?.name??(snap.mgmt_unit||snap.unit),summary_kind:summaryKind,...(exclusion?{excluded_reason:exclusion.reason}:{})},status:"not_started",self:{answers:[],total:"",suggested:"",complete:false},manager:{answers:[],total:"",suggested:"",complete:false},self_rating:"",collective_rating:"",official_rating:"",closed_rating:"",reason:"",personnel_proposal:"",note:"",correction_reason:"",returned_to:"",reopened:false,can_manage:false,can_center:false,self_late:false,manager_late:false};
      if(quarter===0)item.quarter_results=all.filter(q=>q.year===year&&q.quarter>0&&nodeAllows(c,q)).flatMap(q=>{const prior=entries.get(itemKey(ctx,q.id,staff.id))?.at(-1);if(!prior||prior.member.excluded_reason)return [];quarterMembers.set(prior.id,structuredClone(prior.member));return [{quarter:q.quarter,period_id:q.id,entry_version:prior.id,version:prior.version,status:prior.status,rating:prior.closed_rating}];}).sort((a,b)=>a.quarter-b.quarter);
      cohort.push(item);
    }
    if(!cohort.length||seen.size!==(excluded?.length??0)||cohort.every(x=>x.member.excluded_reason))return failure("excluded","Cần người tham gia và danh sách loại trừ hợp lệ");
    for(const item of cohort)entries.set(itemKey(ctx,id,item.member.staff_id),[item]);periods.set(tenant(ctx),[next,...all]);return saved(next);
  }
  if(!p)return problem(404,"hrm.appraisal_not_found","Không có đợt trong phạm vi");
  if(path[2]==="entries"){
    if(path.length>4&&(write||!(path.length===5&&path[4]==="history"||path.length===6&&path[4]==="versions")))return problem(404,"not_found","Không có đường dẫn");
    const staffID=path[3];if(!staffID){const xs=members(ctx,p.id).filter(x=>canRead(c,x));if(!xs.length&&!center(c))return problem(404,"hrm.appraisal_not_found","Không có đề xuất được phân công");return json({items:xs.map(x=>projected(c,x,p))});}
    const history=entries.get(itemKey(ctx,p.id,staffID)),current=history?.at(-1);if(!current||!canRead(c,current))return problem(404,"hrm.appraisal_not_found","Không có đề xuất trong phạm vi");
    if(!write){if(path[4]==="history"){const before=ctx.url.searchParams.get("before");if(before&&!/^[0-9]{1,18}$/.test(before))return failure("before","Mốc lịch sử không hợp lệ");const xs=[...history!].reverse().filter(x=>!before||BigInt(x.version)<BigInt(before));return json({items:xs.slice(0,50).map(x=>projected(c,x,p)),more:xs.length>50,next_before:xs.at(49)?.version??0});}if(path[4]==="versions"){const old=history!.find(x=>x.id===path[5]);return old?json(projected(c,old,p)):problem(404,"not_found","Không có phiên bản");}return json({item:projected(c,current,p),period:redacted(c,p),form:p.forms[current.member.group]??{},history:[...history!].reverse().slice(0,50).map(x=>projected(c,x,p)),history_more:history!.length>50});}
    if(p.workflow_version!==V4)return failure("action","Đợt và phiếu cũ chỉ đọc");
    const b=body as unknown as Change,x=structuredClone(current),caps=projected(c,x,p);
    if(b.version!==x.version)return problem(409,"hrm.appraisal_conflict","Đề xuất đã đổi");if(p.closed&&!x.reopened&&b.action!=="reopen")return failure("action","Đợt đã chốt");
    if(body&&("answers" in body||"proxy_document" in body||"return_to" in body))return failure("action","Không có writer phiếu điểm");
    if(b.action==="manager_submit"){
      if(!caps.can_manage)return problem(403,"forbidden","Không được nhập đề xuất");if(!["not_started","entered","change_requested"].includes(x.status))return failure("action","Đã gửi; cần HCTH yêu cầu điều chỉnh");
      x.self_rating=b.self_rating??"";x.collective_rating=b.collective_rating??"";x.reason=b.reason??"";x.note=b.note??"";x.personnel_proposal=b.personnel_proposal??"";const invalid=proposalFailure(x);if(invalid)return invalid;x.status="entered";x.returned_to="";x.manager_submitted_at=new Date().toISOString();
    }else if(b.action==="send"){if(!caps.can_manage)return problem(403,"forbidden","Không được gửi");if(x.status!=="entered")return failure("action","Cần nhập đủ đề xuất");const invalid=proposalFailure(x);if(invalid)return invalid;x.status="sent";}
    else if(b.action==="return"){if(!caps.can_center)return problem(403,"forbidden","Chỉ HCTH yêu cầu điều chỉnh");if(x.status!=="sent"||[...(b.reason?.trim()??"")].length<10)return failure("reason","Yêu cầu điều chỉnh cần đề xuất đã gửi và lý do");x.status="change_requested";x.returned_to="manager";x.correction_reason=b.reason!;x.official_rating="";}
    else if(b.action==="official"){if(!caps.can_center)return problem(403,"forbidden","Không được nhập kết luận");if(x.status!=="sent"||b.official_rating&&!APPRAISAL_RATINGS.some(r=>r===b.official_rating))return failure("official_rating","Chọn kết luận cho đề xuất đã gửi");x.official_rating=b.official_rating??"";}
    else if(b.action==="reopen"){if(!caps.can_center)return problem(403,"forbidden","Không được mở lại");if(x.status!=="closed"||[...(b.reason?.trim()??"")].length<10)return failure("reason","Mở lại cần HCTH và lý do");x.reopened=true;x.status="change_requested";x.returned_to="manager";x.correction_reason=b.reason!;x.official_rating="";x.closed_rating="";}
    else return failure("action","Thao tác không hợp lệ");
    return saved(projected(c,append(ctx,c,p,x,b.action),p));
  }
  if(!path[2]&&write){
    if(!center(c)&&!(c.group==="manager"&&c.manager&&body?.action==="send"))return problem(403,"forbidden","Không được thay đổi đợt");if(p.workflow_version!==V4)return failure("action","Đợt cũ chỉ đọc");if(body?.version!==p.version)return problem(409,"hrm.appraisal_conflict","Đợt đã đổi");const action=body?.action;
    if(action==="close"){const xs=members(ctx,p.id).filter(x=>!x.member.excluded_reason&&x.status!=="closed");if(!xs.length||xs.some(x=>x.status!=="sent"||proposalFailure(x)))return failure("entries","Còn người chưa nhập hoặc thiếu căn cứ");if(Object.keys(p.signed_documents).length<Object.keys(p.summaries).length&&!body?.acknowledge_unsigned)return failure("acknowledge_unsigned","Cần xác nhận thiếu bản ký");for(const x of xs)append(ctx,c,p,{...x,status:"closed",closed_rating:x.official_rating||x.collective_rating,reopened:false},"close");p.closed=true;}
    else if(action==="attach_signed")return problem(503,"hrm.appraisal_unavailable","Bản ký cần kiểm chứng tệp bằng API máy chủ");
    else if(action==="extend"){const due=body.manager_due_at;if(p.closed||typeof due!=="string"||due.length>40||!Number.isFinite(Date.parse(due))||Date.parse(due)<=Date.parse(p.manager_due_at))return failure("manager_due_at","Hạn mới phải sau hạn gửi hiện có");p.manager_due_at=due;}
    else if(action==="send"){if(!center(c)&&body.unit&&body.unit!==c.unit)return problem(403,"forbidden","Không gửi đơn vị khác");const xs=members(ctx,p.id).filter(x=>!x.member.excluded_reason&&projected(c,x,p).can_manage&&(!body.unit||x.member.management_unit===body.unit)&&!["sent","closed"].includes(x.status));if(!xs.length||xs.some(x=>x.status!=="entered"||proposalFailure(x)))return failure("entries","Còn người chưa hoàn tất đề xuất");for(const x of xs)append(ctx,c,p,{...x,status:"sent"},"send");}
    else if(action==="remind"){if(body.step!=="manager"||!members(ctx,p.id).some(x=>!x.member.excluded_reason&&!["sent","closed"].includes(x.status)))return failure("step","Không còn đơn vị chưa gửi");}
    else return failure("action","Không có thao tác đợt này");p.version++;return saved(redacted(c,p));
  }
  if(path[2]==="import"||path[2]==="import-template"){
    if(c.readOnly)return problem(403,"forbidden","Không có quyền nhập đề xuất");
    if(!members(ctx,p.id).some(x=>projected(c,x,p).can_manage))return problem(404,"not_found","Không có người được phân công");
    return problem(503,"hrm.appraisal_unavailable","Nhập Excel cần kiểm tệp bằng API máy chủ");
  }
  if(path[2]==="paper"&&!write){if(!center(c)||c.m.export!=="all")return problem(403,"forbidden","FN18 chỉ HCTH/Admin có quyền xuất");const kind=path[3]!,xs=members(ctx,p.id).filter(x=>x.member.summary_kind===kind&&!x.member.excluded_reason&&(!ctx.url.searchParams.get("unit")||x.member.management_unit===ctx.url.searchParams.get("unit")));const template=p.summaries[kind]?.summary;return template?json({period:p,template,items:xs}):problem(404,"not_found","Không có mẫu");}
  if(path[2]==="exports"){if(!center(c)||c.m.export!=="all")return problem(403,"forbidden","FN18 chỉ HCTH/Admin có quyền xuất");if(!write)return json({items:[]});if(!["MS05","MS06"].includes(String(body?.kind)))return problem(404,"not_found","Không có biểu xuất này");if(members(ctx,p.id).some(x=>!x.member.excluded_reason&&(!["sent","closed"].includes(x.status)||proposalFailure(x))))return failure("entries","Còn người chưa nhập hoặc thiếu căn cứ");}
  return problem(503,"hrm.appraisal_unavailable","Tệp xuất giả lập chưa sẵn sàng; kiểm tệp thật bằng API máy chủ");
}
function results(ctx:Ctx):Response {
  const c=actor(ctx);if(c instanceof Response)return c;if(!c.m["profile.view"]||c.m["profile.view"]==="none")return problem(403,"forbidden","Không có quyền xem hồ sơ");
  const staff=H.staff().find(x=>x.id===ctx.params[0]);if(!staff||!H.on(c,"profile.view",staff,H.project(staff.events,new Date().toISOString().slice(0,10))))return problem(404,"hrm.staff_not_found","Không có hồ sơ");
  const out:S["HrmAppraisalProfileResult"][]=[];for(const p of periods.get(tenant(ctx))??[]){if(!nodeAllows(c,p))continue;const x=entries.get(itemKey(ctx,p.id,staff.id))?.at(-1);if(x?.status==="closed"&&x.closed_rating&&!x.member.excluded_reason)out.push({period_id:p.id,entry_version:x.id,year:p.year,quarter:p.quarter,label:p.label,rating:x.closed_rating,recorded_at:x.recorded_at});}
  H.audit(c,"view",[staff.id],c.m["profile.view"],{appraisal_results:staff.id});return json({items:out});
}
export const hrmAppraisalRoutes:Route[]=[{method:"GET",pattern:/^\/v1\/hrm\/appraisals\/.+$/,handler:handle},{method:"POST",pattern:/^\/v1\/hrm\/appraisals\/.+$/,handler:handle},{method:"GET",pattern:/^\/v1\/hrm\/staff\/([^/]+)\/appraisal-results$/,handler:results}];
