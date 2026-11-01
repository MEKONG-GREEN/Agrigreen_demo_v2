import type { components } from "@/api/generated/application";
type S=components["schemas"];
export function validAdhocSelection(raw:unknown,data:S["KHTCReportData"]):raw is S["KHTCReportAdhocSelection"]{
 if(!raw||typeof raw!=="object")return false;
 const x=raw as Record<string,unknown>;
 if(Object.keys(x).some(k=>!["rows","metrics"].includes(k))||!Array.isArray(x.rows)||!Array.isArray(x.metrics))return false;
 if(x.rows.length+x.metrics.length<1||x.rows.length+x.metrics.length>200)return false;
 const rows=new Set(data.rows.filter(r=>r.kind==="item").map(r=>r.ref)),metrics=new Set(data.metrics.map(m=>m.key));
 return x.rows.every(k=>typeof k==="string"&&k.length===36&&rows.has(k))&&new Set(x.rows).size===x.rows.length&&
 x.metrics.every(k=>typeof k==="string"&&k.length<=512&&!k.includes("\0")&&metrics.has(k))&&new Set(x.metrics).size===x.metrics.length;
}
export function selectedAdhocContent(input:S["KHTCReportContent"]):S["KHTCReportContent"]{
 const result=structuredClone(input);
 if(result.data.template!=="adhoc")return result;
 const s=result.adhoc??{rows:[],metrics:[]};
 result.data.rows=result.data.rows.filter(r=>s.rows.includes(r.ref));
 result.data.metrics=result.data.metrics.filter(m=>s.metrics.includes(m.key));
 delete result.data.external;
 result.data.provisional=[
 ...(result.data.supplements??[]).filter(r=>r.state==="intent"||r.state==="issued").map(r=>"Yêu cầu bổ sung chưa hoàn tất: "+r.department),
 ...result.data.rows.filter(r=>r.missing).map(r=>"Hạng mục chưa có số liệu: "+r.name),
 ...result.data.metrics.filter(m=>!m.value.Closed).map(m=>"Nguồn chưa chốt: "+m.label),
 ];
 if(!s.rows.length&&!s.metrics.length)result.data.provisional.push("Chưa chọn chỉ tiêu");
 return result;
}