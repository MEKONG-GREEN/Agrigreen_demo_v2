import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
type S=components["schemas"];
export type InitialDraft225=S["KhtcInitialDraft225"];
export type InitialDraftState225=S["KhtcInitialDraftState225"];
export type InitialDraftEvent225=S["KhtcInitialDraftEvent225"];
export type InitialDraftSave225=S["KhtcInitialDraftSave225"];
export function createDraftApi225(api:ApiClient){return {
 current:(year:number):Promise<InitialDraftState225>=>unwrap(api.GET("/v1/kn/khtc/plans/{year}/draft",{params:{path:{year}}})),
 history:(year:number,before_revision?:number)=>unwrap(api.GET("/v1/kn/khtc/plans/{year}/draft/history",{params:{path:{year},query:{limit:100,...(before_revision?{before_revision}:{})}}})),
 save:(year:number,body:InitialDraftSave225,key:string):Promise<InitialDraft225>=>unwrap(api.POST("/v1/kn/khtc/plans/{year}/draft",{params:{path:{year},header:{"Idempotency-Key":key}},body})),
 discard:(year:number,body:S["KhtcInitialDraftDiscard225"],key:string):Promise<InitialDraft225>=>unwrap(api.POST("/v1/kn/khtc/plans/{year}/draft/discard",{params:{path:{year},header:{"Idempotency-Key":key}},body})),
 };}
