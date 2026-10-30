import {unwrap,type ApiClient} from "@/api/client";
import type {components} from "@/api/generated/application";
import {createRetryingMutation} from "./retry";
type S=components["schemas"];
export type AdhocRequest=S["KHTCAdhocRequest"];
export type AdhocRequestInput=S["KHTCAdhocRequestInput"];
export function createAdhocApi(api:ApiClient){
 const mutate=createRetryingMutation();
 return {
 resolvedCreate:(id:string)=>mutate.resolved("create:"+id),
 opens:async()=>(await unwrap(api.GET("/v1/extension/khtc/adhoc-open-intents"))).items,
 recover:(id:string,generation:number,reason:string)=>mutate("recover:"+id+":"+generation,{expected_generation:generation,reason},key=>unwrap(api.POST("/v1/extension/khtc/adhoc-open-intents/{id}/reconcile",{params:{path:{id},header:{"Idempotency-Key":key}},body:{expected_generation:generation,reason}}))),
 list:async(id:string)=>(await unwrap(api.GET("/v1/extension/khtc/periods/{id}/adhoc-requests",{params:{path:{id}}}))).items,
 inbox:async()=>(await unwrap(api.GET("/v1/extension/khtc/adhoc-requests/inbox"))).items,
 create:(id:string,body:AdhocRequestInput)=>mutate("create:"+id,body,key=>unwrap(api.POST("/v1/extension/khtc/periods/{id}/adhoc-requests",{params:{path:{id},header:{"Idempotency-Key":key}},body}))),
 reconcile:(id:string,requestID:string)=>mutate("reconcile:"+requestID,{},key=>unwrap(api.POST("/v1/extension/khtc/periods/{id}/adhoc-requests/{requestID}/reconcile",{params:{path:{id,requestID},header:{"Idempotency-Key":key}},body:{}})))
 };
}
