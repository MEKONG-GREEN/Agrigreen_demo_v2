// GIẢ LẬP W11.26, cùng kho nhân sự/ma trận FN20; nghiệp vụ thật ở pf/hrm.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { hrmStaffInternals as H } from "./kn-hrm-staff";
import { APPRAISAL_RATINGS, appraisalPointGuard, appraisalSuggestion, validateAppraisalAnswers } from "@/kn/hrm/appraisal-form";
type S=components["schemas"];
type Template=S["HrmAppraisalTemplate"];type Period=S["HrmAppraisalPeriod"];type Item=S["HrmAppraisalItem"];
type Change=S["HrmAppraisalEntryChange"];type Form=S["HrmAppraisalForm"];
const quarterMembers=new Map<string,Item["member"]>();
const templates=new Map<string,Template[]>(),periods=new Map<string,Period[]>(),entries=new Map<string,Item[]>(),replays=new Map<string,{body:string;value:unknown}>();
export function resetHrmAppraisals(){quarterMembers.clear();templates.clear();periods.clear();entries.clear();replays.clear();}
function ckey(ctx:Ctx){return ctx.persona.me.tenant_id;}
function actor(ctx:Ctx){const c=H.caller(ctx);return c instanceof Response?c:{...c,manager:!!knContext(ctx.request.headers.get("X-AG-Context")??"")?.manager};}
function center(c:Exclude<ReturnType<typeof actor>,Response>){return ["admin","hcth"].includes(c.group)&&c.m.assessment==="all";}
function canRead(c:Exclude<ReturnType<typeof actor>,Response>,x:Item){const self=x.member.account_ref===c.ref,unit=x.member.management_unit===c.unit;return (c.m.assessment==="all"||c.m.assessment==="unit"&&(unit||self)||c.m.assessment==="self"&&self)&&(center(c)||self||c.group==="manager"&&unit&&x.member.summary_kind==="MS05");}
function projected(c:Exclude<ReturnType<typeof actor>,Response>,x:Item):Item {const out=structuredClone(x);out.can_self=!c.readOnly&&x.member.account_ref===c.ref;out.can_manage=!c.readOnly&&(x.member.summary_kind==="MS06"?center(c):c.group==="admin"||c.manager&&["manager","hcth"].includes(c.group)&&x.member.management_unit===c.unit&&x.member.account_ref!==c.ref);out.can_center=center(c)&&!c.readOnly;out.quarter_results=out.quarter_results?.filter(q=>{const member=quarterMembers.get(q.entry_version);return !!member&&canRead(c,{...x,member});});return out;}
function key(ctx:Ctx,kind:string,code:string){return `${ckey(ctx)}:${kind}:${code}`;}
function itemKey(ctx:Ctx,period:string,staff:string){return key(ctx,period,staff);}
function latestTemplates(ctx:Ctx,kind:string){return [...templates.entries()].filter(([k])=>k.startsWith(`${ckey(ctx)}:${kind}:`)).map(([,v])=>v.at(-1)!);}
function failure(field:string,reason:string){return validation([{field,reason}]);}
// Giữ nháp rỗng và giới hạn phát hành giống API; chỉ tính số sau kiểm cú pháp/độ dài.
function formGuard(form:Form|undefined,code:string,publish:boolean):Response|null {
 if(!form||form.code!==code||!form.name.trim()||[...form.name].length>200||!["vien_chuc","lanh_dao","can_bo_xa"].includes(form.group))return failure("form","Tên hoặc nhóm phiếu không hợp lệ");
 if(!Array.isArray(form.criteria)||form.criteria.length>200||publish&&!form.criteria.length)return failure("criteria","Phiếu phát hành cần 1–200 tiêu chí");
 const points=(raw:string)=>{const [whole="0",fraction=""]=raw.trim().replace(",",".").split(".");return BigInt(whole)*100n+BigInt(fraction.padEnd(2,"0"));};
 const seen=new Set<string>();let total=0n;
 for(const criterion of form.criteria){
  if(!/^[a-z][a-z0-9_]{0,39}$/.test(criterion.code)||seen.has(criterion.code))return failure("criteria","Mã tiêu chí không hợp lệ hoặc trùng");seen.add(criterion.code);
  if(!criterion.title.trim()||[...criterion.title].length>500||[...criterion.group].length>200||[...criterion.guide].length>2000)return failure("criteria","Tên, nhóm hoặc hướng dẫn tiêu chí không hợp lệ");
  if(appraisalPointGuard(criterion.maximum,"1000000"))return failure("maximum","Điểm tối đa không hợp lệ");
  const maximum=points(criterion.maximum);if(maximum===0n||total+maximum>100000000n)return failure("maximum","Tổng phiếu không vượt 1.000.000 điểm và từng tiêu chí phải lớn hơn 0");total+=maximum;
 }
 if(!Array.isArray(form.bands)||form.bands.length!==0&&form.bands.length!==4||publish&&form.bands.length!==4)return failure("bands","Bảng quy đổi cần đủ bốn mức");
 let previous=total+1n;
 for(const [i,band] of form.bands.entries()){
  if(appraisalPointGuard(band.minimum,"1000000"))return failure("bands","Cận quy đổi không hợp lệ");
  const minimum=points(band.minimum);if(minimum>total||minimum>=previous||band.rating!==APPRAISAL_RATINGS[i])return failure("bands","Cận giảm dần trong tổng điểm theo đúng thứ tự xếp loại");previous=minimum;
 }
 if(form.bands.length&&previous!==0n)return failure("bands","Cận cuối bằng 0 để phủ hết thang điểm");
 if((form.questions?.length??0)>20||form.questions?.some(q=>!q.trim()||[...q].length>1000))return failure("questions","Tối đa 20 câu hỏi nhận xét, mỗi câu 1–1.000 ký tự");
 return null;
}
function handle(ctx:Ctx):Response {
 const c=actor(ctx);if(c instanceof Response)return c;if(c.group==="tw"||c.m.assessment==="none"||!c.m.assessment)return problem(403,"forbidden","Không có quyền đánh giá");
 const path=new URL(ctx.request.url).pathname.replace("/v1/hrm/appraisals","").split("/").filter(Boolean),write=ctx.request.method==="POST";
 if(write&&c.readOnly)return problem(403,"forbidden","Ngữ cảnh chỉ xem");
 const body=ctx.body as Record<string,unknown>|null,idem=ctx.request.headers.get("Idempotency-Key")??"",fp=JSON.stringify([c.ref,path,body]),rk=key(ctx,"request",idem);
 if(write){if(idem.length<8||idem.length>200)return failure("Idempotency-Key","Khóa từ 8 đến 200 ký tự");const prior=replays.get(rk);if(prior)return prior.body===fp?json(structuredClone(prior.value),201):problem(409,"hrm.appraisal_conflict","Khóa đã dùng cho dữ liệu khác");}
 const saved=(value:unknown)=>{replays.set(rk,{body:fp,value:structuredClone(value)});return json(value,201);};
 if(path[0]==="templates"){
  if(!center(c))return problem(403,"forbidden","Chỉ HCTH/Admin quản lý mẫu");const kind=path[1]!,code=path[2];if(!["summary","form"].includes(kind))return problem(404,"not_found","Không có loại mẫu");
  if(!write)return json({items:code?structuredClone(templates.get(key(ctx,kind,code))??[]):latestTemplates(ctx,kind)});
  if(!code||!/^[a-z][a-z0-9_]{0,39}$/.test(code))return failure("code","Mã mẫu không hợp lệ");const old=templates.get(key(ctx,kind,code))??[];
  if(Number(body?.version??0)!==(old.at(-1)?.version??0))return problem(409,"hrm.appraisal_conflict","Phiên bản mẫu đã đổi");
  const status=body?.status as Template["status"],form=body?.form as Form|undefined,summary=body?.summary as S["HrmAppraisalSummary"]|undefined;
  if(!["draft","published","retired"].includes(status))return failure("status","Chọn trạng thái mẫu");
  if(kind==="form"){const invalid=formGuard(form,code,status==="published");if(invalid)return invalid;}
  if(kind==="summary"&&(!summary||summary.columns.length!==8))return failure("summary","MS05/MS06 cần tám cột");
  const t:Template={id:uuidv7(),kind:kind as Template["kind"],code,status,version:old.length+1,recorded_at:new Date().toISOString(),actor_ref:c.ref,...(form?{form:structuredClone(form)}:{summary:structuredClone(summary!)})};templates.set(key(ctx,kind,code),[...old,t]);return saved(t);
 }
 const all=periods.get(ckey(ctx))??[];
 if(path[0]!=="periods")return problem(404,"not_found","Không có đường dẫn");
 const periodID=path[1],p=all.find(x=>x.id===periodID);
 const members=(id:string)=>[...entries.entries()].filter(([k])=>k.startsWith(`${ckey(ctx)}:${id}:`)).map(([,xs])=>xs.at(-1)!);
 if(!periodID){if(!write)return json({items:all.filter(x=>center(c)||members(x.id).some(e=>canRead(c,e))).map(x=>center(c)?x:{...x,forms:{},summaries:{},counts:{},warnings:[],signed_documents:{}})});
  if(!center(c))return problem(403,"forbidden","Chỉ HCTH/Admin mở đợt");const year=body?.year as number,quarter=body?.quarter as number;
  if(!Number.isInteger(year)||year<2020||year>2100||!Number.isInteger(quarter)||quarter<0||quarter>4)return failure("year","Năm 2020–2100, quý 0–4");if(all.some(x=>x.year===year&&x.quarter===quarter))return problem(409,"hrm.appraisal_conflict","Đã có đợt cùng kỳ");
  const selectedForms=body?.forms as Record<string,string>,selectedSummaries=body?.summaries as Record<string,string>,frozen:Record<string,Template>={},papers:Record<string,Template>={};
  for(const [group,id] of Object.entries(selectedForms??{})){const t=latestTemplates(ctx,"form").find(x=>x.id===id&&x.status==="published"&&x.form?.group===group);if(!t)return failure("forms","Chọn phiên bản phiếu đã phát hành đúng nhóm");frozen[group]=structuredClone(t);}
  for(const [kind,id] of Object.entries(selectedSummaries??{})){const t=latestTemplates(ctx,"summary").find(x=>x.id===id&&x.status==="published"&&x.summary?.kind===kind);if(!t)return failure("summaries","Chọn phiên bản mẫu tổng hợp đã phát hành");papers[kind]=structuredClone(t);}
  if(!Object.keys(frozen).length)return failure("forms","Chọn nhóm đánh giá");
  const at=new Date().toISOString(),id=uuidv7(),next:Period={id,version:1,year,quarter,node_ref:ctx.persona.me.tenant_id,label:quarter?`Quý ${quarter}/${year}`:`Năm ${year}`,cutoff_on:String(body?.cutoff_on),known_at:at,self_due_at:String(body?.self_due_at),manager_due_at:String(body?.manager_due_at),summary_due_at:String(body?.summary_due_at),forms:frozen,summaries:papers,self_locked:false,manager_locked:false,closed:false,signed_documents:{},warnings:[],counts:{},recorded_at:at,actor_ref:c.ref};
  if(!(next.cutoff_on<next.self_due_at&&next.self_due_at<next.manager_due_at&&next.manager_due_at<next.summary_due_at))return failure("summary_due_at","Ngày chốt < hạn tự chấm < hạn quản lý < hạn tổng hợp");
  for(const staff of H.staff()){const snap=H.project(staff.events,next.cutoff_on);if(snap.status!=="active")continue;const leader=/truong|pho_|giam_doc/.test(snap.position),group=staff.type==="can_bo_xa"?"can_bo_xa":leader?"lanh_dao":"vien_chuc";if(!frozen[group])continue;const summaryKind=leader?"MS06":"MS05";if(!papers[summaryKind])return failure("summaries","Thiếu mẫu cho nhóm đối tượng");const item:Item={id:uuidv7(),period_id:id,version:1,actor_ref:c.ref,recorded_at:at,action:"open",member:{staff_id:staff.id,subject_ref:staff.id,account_ref:staff.account_ref,code:staff.code,staff_type:staff.type,group,unit:snap.unit,management_unit:snap.mgmt_unit||snap.unit,position:snap.position,position_name:snap.position,position_order:1,unit_name:snap.mgmt_unit||snap.unit,summary_kind:summaryKind},status:"not_started",self:{answers:[],total:"",suggested:"",complete:false},manager:{answers:[],total:"",suggested:"",complete:false},self_rating:"",collective_rating:"",official_rating:"",closed_rating:"",reason:"",personnel_proposal:"",note:"",correction_reason:"",returned_to:"",reopened:false,can_self:false,can_manage:false,can_center:false,self_late:false,manager_late:false};if(quarter===0){item.quarter_results=all.filter(q=>q.year===year&&q.quarter>0).flatMap(q=>{const prior=entries.get(itemKey(ctx,q.id,staff.id))?.at(-1);if(!prior||prior.member.excluded_reason)return [];quarterMembers.set(prior.id,structuredClone(prior.member));return [{quarter:q.quarter,period_id:q.id,entry_version:prior.id,version:prior.version,status:prior.status,rating:prior.closed_rating}];}).sort((a,b)=>a.quarter-b.quarter);}entries.set(itemKey(ctx,id,staff.id),[item]);}
  periods.set(ckey(ctx),[next,...all]);return saved(next);
 }
 if(!p)return problem(404,"hrm.appraisal_not_found","Không có đợt trong phạm vi");
 if(path[2]==="entries"){
  const staffID=path[3];if(!staffID)return json({items:members(p.id).filter(x=>canRead(c,x)).map(x=>projected(c,x))});
  const history=entries.get(itemKey(ctx,p.id,staffID)),current=history?.at(-1);if(!current||!canRead(c,current))return problem(404,"hrm.appraisal_not_found","Không có phiếu trong phạm vi");
  if(!write){if(path[4]==="history"){const before=ctx.url.searchParams.get("before"),xs=[...history!].reverse().filter(x=>!before||x.version<Number(before));return json({items:xs.slice(0,50).map(x=>projected(c,x)),more:xs.length>50,next_before:xs.at(49)?.version??0});}if(path[4]==="versions"){const old=history!.find(x=>x.id===path[5]);return old?json(projected(c,old)):problem(404,"not_found","Không có phiên bản");}return json({item:projected(c,current),period:center(c)?p:{...p,forms:{},summaries:{},signed_documents:{},warnings:[],counts:{}},form:p.forms[current.member.group],history:[...history!].reverse().slice(0,50).map(x=>projected(c,x)),history_more:history!.length>50});}
  if(path[4]==="guest-link")return problem(503,"hrm.appraisal_guest_unavailable","Guest OTP chưa cấu hình");
  const b=body as unknown as Change,x=structuredClone(current),caps=projected(c,x);if(b.version!==x.version)return problem(409,"hrm.appraisal_conflict","Phiếu đã đổi");if(p.closed&&!x.reopened&&b.action!=="reopen")return failure("action","Đợt đã chốt");
  if(["self_draft","self_submit","proxy_self","manager_draft","manager_submit"].includes(b.action)){
   const self=b.action.startsWith("self_")||b.action==="proxy_self";if((b.action==="proxy_self"?!caps.can_manage:self&&!caps.can_self)||!self&&!caps.can_manage)return problem(403,"forbidden","Không được chấm bước này");if(b.action==="proxy_self")return problem(503,"hrm.appraisal_unavailable","Nhập thay cần kiểm chứng tệp scan bằng API máy chủ");if(self&&((p.self_locked&&!x.reopened)||!(x.status==="not_started"||x.status==="change_requested"&&x.returned_to==="self"))||!self&&((p.manager_locked&&!x.reopened)||!(x.status==="self_submitted"||x.status==="manager_submitted"||x.status==="change_requested"&&x.returned_to==="manager")))return failure("action","Bước chưa mở hoặc đã khóa");const checked=validateAppraisalAnswers(p.forms[x.member.group]!.form!,b.answers??[],!b.action.endsWith("draft"));if(!checked.valid)return validation(Object.entries(checked.errors).map(([field,reason])=>({field,reason})));const suggestion=appraisalSuggestion(p.forms[x.member.group]!.form!,checked.answers);const score={answers:checked.answers,total:suggestion?.total??"",suggested:suggestion?.rating??"",complete:!!suggestion};if(self){x.self=score;x.self_rating=b.self_rating||suggestion?.rating||"";if(b.action!=="self_draft"){x.status="self_submitted";x.returned_to="";x.manager={answers:[],total:"",suggested:"",complete:false};x.collective_rating="";x.official_rating="";}}else{x.manager=score;x.collective_rating=b.collective_rating||suggestion?.rating||"";x.status=b.action==="manager_draft"?"self_submitted":"manager_submitted";if(b.action==="manager_submit")x.returned_to="";}x.reason=b.reason??"";x.note=b.note??"";x.personnel_proposal=b.personnel_proposal??"";
  }else if(b.action==="send"){if(!caps.can_manage||x.status!=="manager_submitted")return failure("action","Cần hoàn tất đề xuất");x.status="sent";}
  else if(b.action==="return"){if((b.reason?.trim().length??0)<10||!(b.return_to==="manager"?caps.can_center&&x.status==="sent":caps.can_manage))return failure("reason","Trả phiếu cần quyền và lý do");x.status="change_requested";x.returned_to=b.return_to!;x.correction_reason=b.reason!;}
  else if(b.action==="official"){if(!caps.can_center||x.status!=="sent")return problem(403,"forbidden","Không được nhập kết luận");x.official_rating=b.official_rating??"";x.reason=b.reason??x.reason;}
  else if(b.action==="reopen"){if(!caps.can_center||x.status!=="closed"||(b.reason?.trim().length??0)<10)return failure("reason","Mở lại cần HCTH và lý do");x.reopened=true;x.status="change_requested";x.returned_to="manager";x.official_rating="";}
  else return failure("action","Thao tác không hợp lệ");
  if(["HTXSNV","KHTNV"].some(r=>r===x.self_rating||r===x.collective_rating)&&x.reason.trim().length<10)return failure("reason","Căn cứ bắt buộc khi đề xuất xuất sắc hoặc không hoàn thành");x.id=uuidv7();x.version++;x.recorded_at=new Date().toISOString();x.actor_ref=c.ref;x.action=b.action;history!.push(x);return saved(projected(c,x));
 }
 if(!path[2]&&write){if(!center(c)&&!(c.group==="manager"&&body?.action==="send"))return problem(403,"forbidden","Không được thay đổi đợt");if(body?.version!==p.version)return problem(409,"hrm.appraisal_conflict","Đợt đã đổi");const a=body?.action;
  if(a==="close"){const xs=members(p.id);if(xs.some(x=>x.status!=="sent"&&x.status!=="closed"))return failure("entries","Còn đề xuất chưa gửi");if(Object.keys(p.signed_documents).length<Object.keys(p.summaries).length&&!body?.acknowledge_unsigned)return failure("acknowledge_unsigned","Cần xác nhận thiếu bản ký");for(const x of xs.filter(x=>x.status!=="closed")){const next={...x,id:uuidv7(),version:x.version+1,status:"closed" as const,closed_rating:x.official_rating||x.collective_rating,action:"close"};entries.get(itemKey(ctx,p.id,x.member.staff_id))!.push(next);}p.closed=true;}
  else if(a==="lock_self"||a==="unlock_self")p.self_locked=a==="lock_self";else if(a==="lock_manager"||a==="unlock_manager")p.manager_locked=a==="lock_manager";else if(a==="attach_signed")return problem(503,"hrm.appraisal_unavailable","Bản ký cần kiểm chứng tệp bằng API máy chủ");else if(a==="extend"){for(const k of ["self_due_at","manager_due_at","summary_due_at"] as const)if(body?.[k])p[k]=String(body[k]);}else if(a==="send"){const xs=members(p.id).filter(x=>center(c)||x.member.management_unit===c.unit);if(xs.some(x=>x.status!=="manager_submitted"))return failure("entries","Còn người chưa hoàn tất đề xuất");for(const x of xs)entries.get(itemKey(ctx,p.id,x.member.staff_id))!.push({...x,id:uuidv7(),version:x.version+1,status:"sent",action:"send"});}else if(a!=="remind")return failure("action","Không có thao tác");p.version++;return saved(p);
 }
 if(path[2]==="paper"&&!write){if(!center(c)&&c.group!=="manager")return problem(403,"forbidden","Không có quyền xuất");const kind=path[3]!,xs=members(p.id).filter(x=>x.member.summary_kind===kind&&canRead(c,x));const template=p.summaries[kind]?.summary;return template?json({period:p,template,items:xs}):problem(404,"not_found","Không có mẫu");}
 if(path[2]==="exports"&&!path[3]&&!write)return json({items:[]});
 return problem(503,"hrm.appraisal_unavailable","Tệp xuất giả lập chưa sẵn sàng; kiểm tệp thật bằng API máy chủ");
}
export const hrmAppraisalRoutes:Route[]=[{method:"GET",pattern:/^\/v1\/hrm\/appraisals\/.+$/,handler:handle},{method:"POST",pattern:/^\/v1\/hrm\/appraisals\/.+$/,handler:handle}];