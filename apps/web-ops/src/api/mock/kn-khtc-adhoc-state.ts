import type {components} from "../generated/application";
type S=components["schemas"];
export type MockAdhocRequest=S["KHTCAdhocRequest"]&{tenant:string;signature:string;key:string};
export const ADHOC_REQUESTS:MockAdhocRequest[]=[];
export function activeAdhoc(period:string,department:string){return ADHOC_REQUESTS.findLast(r=>r.period_id===period&&r.department===department&&!["failed","cancelled"].includes(r.state));}
export function publicAdhoc(r:MockAdhocRequest):S["KHTCAdhocRequest"]{
 const {tenant:_t,signature:_s,key:_k,...view}=r;void _t;void _s;void _k;
 return structuredClone({...view,overdue:["intent","issued"].includes(r.state)&&Date.now()>Date.parse(r.due_at)});
}
export function validateMockAdhoc(content:S["KHTCFormContent"],request:S["KHTCAdhocRequest"],submit:boolean):S["KHTCFormIssue"][]{
 const issues:S["KHTCFormIssue"][]=[];
 const add=(section:string,message:string)=>issues.push({rule:"template",severity:"error",adjustment:-1,section,message});
 if(Object.keys(content.cells).length||content.adjustments.length||Object.keys(content.texts).length)add("adhoc_values","Chỉ nhập câu trả lời của yêu cầu mới");
 if(Object.keys(content.adhoc_values??{}).length>20){add("adhoc_values","Tối đa20 chỉ tiêu");return issues;}
 for(const [code,raw]of Object.entries(content.adhoc_values??{})){
 if(!request.indicators.some(d=>d.code===code)){add(code,"Chỉ tiêu không được giao");continue;}
 if(raw && (raw.length>23 || !/^\d{1,15}(\.\d{1,6})?$/.test(raw)))add(code,"Số không âm, tối đa15 chữ số và6 chữ số thập phân");
 }
 if(submit)for(const d of request.indicators)if(!content.adhoc_values?.[d.code]?.trim())add(d.code,"Bắt buộc nhập");
 if((content.adhoc_text?.length??0)>32000||[...content.adhoc_text??""].length>8000)add("adhoc_text","Tối đa8000 ký tự");
 if(submit&&!request.indicators.length&&!content.adhoc_text?.trim())add("adhoc_text","Bắt buộc nhập nội dung bổ sung");
 return issues;
}
