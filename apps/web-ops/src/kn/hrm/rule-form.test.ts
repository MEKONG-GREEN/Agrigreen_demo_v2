import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import type { HrmPromotionRules, HrmRule, HrmItem, HrmRuleKey } from "./api";
import { effectiveRule, initialRuleValues, parseRuleVersion, PROMOTION_INPUTS, rulePath, validRuleDay, validateRule, type RuleValues } from "./rule-form";

const day="2026-10-12";
const promotion:HrmPromotionRules={ min_rating:"HTTNV",rating_years:3,achievement_years:5,discipline_extension_months:{canh_cao:6},nlth_quota_pct:"10",nlth_retirement_months:12,nlth_retirement_within_months:12,tnvk_start_pct:"5",tnvk_step_pct:"1",tnvk_cycle_months:12,tnvk_first_months:36,remind_months:[1,3,6],retirement_age_male_months:738,retirement_age_female_months:684 };
const refs=[{code:"canh_cao",active:true},{code:"cstd_co_so",active:true},{code:"bang_khen",active:true}] as HrmItem[];
function rule(key:HrmRuleKey):HrmRule{return {key,valid_from:"2026-01-01",payload:(key==="promotion"?promotion:{rules:[{award_code:"cstd_co_so",months:9},{award_code:"bang_khen",months:12}]}) as unknown as HrmRule["payload"],note:"",placeholder:true,version:1,recorded_at:"2026-01-01T01:00:00Z"};}
function values(key:HrmRuleKey){return initialRuleValues(key,rule(key),day);}
function replaceNumber(v:RuleValues,name:string,raw:string){
 if(name.startsWith("reminder_"))v.reminders[Number(name.slice(9))]=raw;
 else if(name.startsWith("row_"))v.rows[Number(name.split("_")[1]) ]!.months=raw;
 else v.fields[name as keyof RuleValues["fields"]]=raw;
}
beforeEach(()=>{vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date(`${day}T03:00:00Z`));});
afterEach(()=>vi.useRealTimers());
describe("FN19 validation before numeric conversion",()=>{
 for(const key of ["promotion","achievement_conversion"] as const){
  const fields=key==="promotion"?[...PROMOTION_INPUTS.map((f)=>f.key),"reminder_0","reminder_1","reminder_2","row_0_months"]:["row_0_months","row_1_months"];
  for(const name of fields) for(const [label,raw] of [["exponent+","1e100000"],["exponent-","1e-100000"],["long","9".repeat(100000)],["large","99999999999999999999999999999999"]]){
   it(`${key}/${name}/${label} rejected at the field`,()=>{const v=values(key);replaceNumber(v,name,raw!);const checked=validateRule(key,v,refs);expect(checked.valid).toBe(false);expect(checked.errors[name]).toBeTruthy();expect(checked.body).toBeNull();});
  }
 }
 it("domain boundaries and Vietnamese decimal normalization",()=>{
  for(const field of PROMOTION_INPUTS) for(const bound of [field.min,field.max]){
   const v=values("promotion");v.fields[field.key]=String(bound);expect(validateRule("promotion",v,refs).valid,field.key).toBe(true);
  }
  const v=values("promotion");v.fields.nlth_quota_pct="12,34";v.fields.tnvk_start_pct="0";v.rows[0]!.months="0";
  const checked=validateRule("promotion",v,refs);expect(checked.valid).toBe(true);
  expect(checked.body?.payload).toMatchObject({nlth_quota_pct:"12.34",tnvk_start_pct:"0",discipline_extension_months:{canh_cao:0},remind_months:[1,3,6]});
  for(const field of PROMOTION_INPUTS){const below=values("promotion");below.fields[field.key]=String(field.min-1);expect(validateRule("promotion",below,refs).valid).toBe(false);const above=values("promotion");above.fields[field.key]=String(field.max+1);expect(validateRule("promotion",above,refs).valid).toBe(false);}
 });
 it("required values, row codes, references and ordering are not silently dropped",()=>{
  const v=values("promotion");v.fields.rating_years="";expect(validateRule("promotion",v,refs).errors.rating_years).toBeTruthy();
  v.fields.rating_years="3";v.reminders=["3","1"];expect(validateRule("promotion",v,refs).errors.reminder_1).toBeTruthy();
  v.reminders=["1","3"];v.rows.push({...v.rows[0]!});expect(validateRule("promotion",v,refs).errors.row_1_code).toBeTruthy();
  v.rows=[{code:"",months:"6"}];expect(validateRule("promotion",v,refs).errors.row_0_code).toBeTruthy();
  v.rows=[{code:"canh_cao",months:"6"}];expect(validateRule("promotion",v,[{...refs[0]!,active:false}]).errors.row_0_code).toBeTruthy();
  const c=values("achievement_conversion");c.rows=[];expect(validateRule("achievement_conversion",c,refs).errors.rows).toBeTruthy();
 });
 it("date, note and payload copies preserve the selected immutable version",()=>{
  const base=rule("promotion");const before=JSON.stringify(base);const v=initialRuleValues("promotion",base,day);v.fields.rating_years="4";v.rows[0]!.months="12";v.reminders[0]="2";
  expect(JSON.stringify(base)).toBe(before);
  v.from="2026-02-30";expect(validateRule("promotion",v,refs).errors.from).toBeTruthy();v.from=day;v.note="x".repeat(501);expect(validateRule("promotion",v,refs).errors.note).toBeTruthy();
 });
});
it("bounded URL selectors and version-as-of selection",()=>{
 expect(parseRuleVersion(null)).toBeUndefined();expect(parseRuleVersion("1")).toBe(1);expect(parseRuleVersion("9007199254740991")).toBe(Number.MAX_SAFE_INTEGER);
 for(const value of ["0","01","-1","1e100000","1e-100000","NaN","Infinity","9".repeat(100000),"9007199254740992"])expect(parseRuleVersion(value)).toBeNull();
 for(const value of ["2026-02-30","0001-01-01","1e100000","9".repeat(100000)])expect(validRuleDay(value)).toBe(false);
 const a=rule("promotion"),b={...a,version:2,valid_from:"2027-01-01"},c={...a,version:3};const versions=[a,b,c];const old=JSON.stringify(versions);
 expect(effectiveRule(versions,day)?.version).toBe(3);expect(effectiveRule(versions,"2027-01-01")?.version).toBe(2);expect(JSON.stringify(versions)).toBe(old);
 expect(rulePath("/kn/phong-ban/hanh_chinh/danh-muc-nhan-su","promotion",day,true,3)).toBe("/kn/phong-ban/hanh_chinh/danh-muc-nhan-su/quy-tac/promotion/them-moi?at=2026-10-12&version=3");
});
