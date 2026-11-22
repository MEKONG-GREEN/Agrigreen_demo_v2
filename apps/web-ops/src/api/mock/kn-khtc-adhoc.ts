// GIẢ LẬP W11.68. Synthetic data only; native/sealed provenance is proven by real PG tests.
import {json,problem,type Ctx,type Route} from "./http";
import {knContext} from "./kn";
import {khtcMockStore} from "./kn-khtc-periods";
import {khtcAdhocNativeReturn} from "./kn-khtc-forms";
import {ADHOC_REQUESTS,publicAdhoc,type MockAdhocRequest} from "./kn-khtc-adhoc-state";
import {uuidv7} from "../idempotency";
import type {components} from "../generated/application";
type Input=components["schemas"]["KHTCAdhocRequestInput"];
const denied=()=>problem(403,"forbidden","Không có quyền");
const owner=(ctx:Ctx)=>{const c=knContext(ctx.request.headers.get("X-AG-Context")??"");return !!c&&!c.read_only&&(c.role==="admin"||c.role==="kn_tinh"&&c.department_code==="ke_hoach_tai_chinh")};
const view=(ctx:Ctx,r:MockAdhocRequest)=>r.tenant===ctx.persona.me.tenant_id;
function handle(ctx:Ctx):Response{
 const [periodID,requestID]=ctx.params;
 if(!owner(ctx))return denied();
 const period=khtcMockStore().find(x=>x.p.id===periodID&&x.p.report_type==="adhoc"&&x.adhocTenant===ctx.persona.me.tenant_id);
 if(!period)return problem(404,"extension.khtc_form_not_found","Không có kỳ phát sinh");
 if(ctx.request.method==="GET")return json({items:ADHOC_REQUESTS.filter(r=>r.period_id===periodID&&view(ctx,r)).map(publicAdhoc)});
 if(requestID){
 const r=ADHOC_REQUESTS.find(x=>x.id===requestID&&x.period_id===periodID&&view(ctx,x));if(!r)return problem(404,"extension.khtc_form_not_found","Không có yêu cầu");
 if(period.terminal&&r.state!=="fulfilled")r.state="cancelled";return json(publicAdhoc(r));
 }
 const body=ctx.body as Input;const key=ctx.request.headers.get("Idempotency-Key")??"";
 if(!key)return problem(400,"idempotency_key_required","Thiếu khóa thao tác");
 const signature=JSON.stringify(body);
 const old=ADHOC_REQUESTS.find(r=>r.period_id===periodID&&r.key===key&&view(ctx,r));
 if(old)return old.signature===signature?json(publicAdhoc(old),201):problem(409,"extension.khtc_adhoc_request_idempotency","Khóa thao tác đã dùng cho nội dung khác");
 if(period.terminal||period.reportFinal)return problem(409,"extension.khtc_form_locked","Kỳ đã kết thúc");
 if(!body||!period.p.departments.some(d=>d.code===body.department)||typeof body.instruction!=="string"||!body.instruction.trim()||body.instruction.length>16000||[...body.instruction].length>4000||!Array.isArray(body.indicators)||body.indicators.length>20||typeof body.due_at!=="string"||body.due_at.length>35||!Number.isFinite(Date.parse(body.due_at))||Date.parse(body.due_at)<=Date.now()||Date.parse(body.due_at)>Date.now()+366*86400000)return problem(422,"validation_failed","Nội dung, phòng, hạn hoặc chỉ tiêu không hợp lệ");
 const seen=new Set<string>();for(const d of body.indicators){if(!/^[A-Z][A-Z0-9_]{0,39}$/.test(d.code)||seen.has(d.code)||!d.name.trim()||[...d.name].length>200||[...d.unit].length>50)return problem(422,"validation_failed","Chỉ tiêu không hợp lệ");seen.add(d.code);}
 if(ADHOC_REQUESTS.some(r=>r.period_id===periodID&&r.department===body.department&&["intent","issued"].includes(r.state)))return problem(409,"extension.khtc_adhoc_request_pending","Phòng còn yêu cầu chưa hoàn tất");
 const effect=khtcAdhocNativeReturn(period,body.department,ctx.persona.me.actor_ref);if(effect instanceof Response)return effect;
 const r:MockAdhocRequest={...structuredClone(body),id:uuidv7(),period_id:periodID!,state:"issued",overdue:false,return_applied:effect,actor_ref:ctx.persona.me.actor_ref,at:new Date().toISOString(),tenant:ctx.persona.me.tenant_id,signature,key};
 ADHOC_REQUESTS.push(r);return json(publicAdhoc(r),201);
}
export const khtcAdhocRoutes:Route[]=[
 // Mock open commits synchronously; partial native/KN commits are exercised on
 // private PostgreSQL. A completed mock open has no incomplete recovery intent.
 {method:"GET",pattern:/^\/v1\/extension\/khtc\/adhoc-open-intents$/,handler:ctx=>owner(ctx)?json({items:[]}):denied()},
 {method:"POST",pattern:/^\/v1\/extension\/khtc\/adhoc-open-intents\/([^/]+)\/reconcile$/,handler:ctx=>owner(ctx)?problem(404,"extension.khtc_period_not_found","Không có ý định phát hành dang dở"):denied()},

 {method:"GET",pattern:/^\/v1\/extension\/khtc\/periods\/([^/]+)\/adhoc-requests$/,handler:handle},
 {method:"POST",pattern:/^\/v1\/extension\/khtc\/periods\/([^/]+)\/adhoc-requests(?:\/([^/]+)\/reconcile)?$/,handler:handle},
 {method:"GET",pattern:/^\/v1\/extension\/khtc\/adhoc-requests\/inbox$/,handler:ctx=>{
 const c=knContext(ctx.request.headers.get("X-AG-Context")??"");
 if(!c||c.read_only||c.role!=="kn_tinh"||!c.department_code)return denied();
 return json({items:ADHOC_REQUESTS.filter(r=>view(ctx,r)&&r.department===c.department_code&&["intent","issued"].includes(r.state)&&khtcMockStore().some(p=>p.p.id===r.period_id&&!p.terminal)).slice(0,100).map(publicAdhoc)});
 }}
];
