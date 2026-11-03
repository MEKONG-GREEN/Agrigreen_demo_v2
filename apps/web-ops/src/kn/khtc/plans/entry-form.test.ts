import { describe,expect,it } from "vitest";
import { blankEntry } from "../api";
import { checkEntry, entryDraft } from "./entry-form";
import { entryHref,newEntryHref,parsePlanRoute,PLAN_BASE,programHref } from "./routes";
const parent=blankEntry("programs","program",null,10);
const item={...blankEntry("programs","item",parent,20),name:"Mục thử",department_code:"trong_trot"};
describe("206 bounded entry form",()=>{
  for(const field of ["sort_order","points_count","planned_qty","budget"] as const){
    for(const [name,raw] of [["largeExponent","1e100000"],["smallExponent","1e-100000"],["longDigits","9".repeat(100000)],["hugeOrdinary","9".repeat(40)],["negative","-1"],["precision","1.00001"]]){
      it(`${field} rejects ${name} before any conversion`,()=>{const checked=checkEntry({...entryDraft(item),[field]:raw},2030);expect(checked.valid).toBe(false);expect(checked.errors).toHaveProperty(field);expect(checked.entry).toBeNull();});
    }
  }
  it("keeps exact decimal upper bounds and validates integer resource bounds",()=>{
    const checked=checkEntry({...entryDraft(item),planned_qty:"9999999999999999.9999",budget:"999999999999999999.99",points_count:"100000",sort_order:"100000"},2030);
    expect(checked.valid).toBe(true);expect(checked.entry).toMatchObject({planned_qty:"9999999999999999.9999",budget:"999999999999999999.99",points_count:100000,sort_order:100000});
    expect(checkEntry({...entryDraft(item),planned_qty:"10000000000000000"},2030).errors).toHaveProperty("planned_qty");
    expect(checkEntry({...entryDraft(item),budget:"1000000000000000000"},2030).errors).toHaveProperty("budget");
  });
  it("preserves field snapshot and validates fields/date/module without inventing metadata",()=>{
    const draft=entryDraft({...item,field_code:"other"});expect(checkEntry(draft,2030).entry?.field_code).toBe("other");
    expect(checkEntry({...draft,entry:{...draft.entry,tracking:"stopped",stopped_from:"2030-02-30"}},2030).errors).toHaveProperty("stopped_from");
    expect(checkEntry({...draft,entry:{...draft.entry,department_code:null}},2030).errors).toHaveProperty("department_code");
  });
});
describe("206 canonical operation routes",()=>{
  for(const href of [entryHref(PLAN_BASE,2030,item.ref),entryHref(PLAN_BASE,2030,item.ref,true),newEntryHref(PLAN_BASE,2030,"programs",parent.ref,"item"),programHref(PLAN_BASE,2030,"research"),programHref(PLAN_BASE,2030,"advisory",true)]){
    it(href,()=>{const u=new URL(href,"https://local.invalid");expect(parsePlanRoute(u.pathname,u.searchParams,2030)).not.toBeNull();});
  }
  it("fails closed for malformed operation identity, duplicate params and invalid parent/kind",()=>{
    for(const href of [`${PLAN_BASE}/noi-dung/1e100000/sua?year=2030`,`${PLAN_BASE}/them-noi-dung?year=2030&parent=${parent.ref}&kind=program`,`${PLAN_BASE}/chon-chuong-trinh?year=2030&tab=programs&tab=research`]){const u=new URL(href,"https://local.invalid");expect(parsePlanRoute(u.pathname,u.searchParams,2030)).toBeNull();}
  });
});
