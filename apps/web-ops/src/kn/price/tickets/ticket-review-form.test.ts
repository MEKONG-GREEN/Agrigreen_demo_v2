import { describe, expect, it } from "vitest";
import type { PriceSheet } from "./api";
import { fieldKey, reviewInitial, validateReview } from "./ticket-review-form";
const sheet={day:"2026-10-12",lines:[{item_code:"MH00001",min:"9000",max:"10000",no_goods:false},{item_code:"MH00002",min:null,max:null,no_goods:true}]} as PriceSheet;
const fields=(lo="12.5",hi="13.5")=>({...reviewInitial(sheet),reason:"Đối chiếu",[fieldKey("MH00001","min")]:lo,[fieldKey("MH00001","max")]:hi});
describe("204 explicit review validation",()=>{
 it("normalizes valid prices; sends only selected lines with required reason",()=>{
  const v=validateReview(sheet,"correct",fields("00001.2300","9000.00"));expect(v.valid).toBe(true);expect(v.lines).toEqual([{item_code:"MH00001",min:"1.23",max:"9000"}]);
  expect(validateReview(sheet,"correct",{...fields(),reason:" "}).errors.reason).toBeDefined();expect(validateReview(sheet,"correct",{...reviewInitial(sheet),reason:"ok"}).errors.lines).toBeDefined();
 });
 it.each(["min","max"] as const)("bounds hostile %s before numeric normalization",side=>{
  for(const bad of ["1e100000","1e-100000","9".repeat(100000),"1000000000000","-1","0","1.234"]){const f=fields();f[fieldKey("MH00001",side)]=bad;expect(validateReview(sheet,"correct",f).errors[fieldKey("MH00001",side)]).toBeDefined();}
 });
 it("rejects incomplete/reversed/no-goods pairs and invalid dates, preserving other errors",()=>{
  expect(validateReview(sheet,"correct",fields("12","")).errors["MH00001.max"]).toBeDefined();expect(validateReview(sheet,"correct",fields("14","13")).errors["MH00001.max"]).toMatch(/lớn hơn/);
  expect(validateReview(sheet,"correct",{...fields(),"MH00002.min":"1","MH00002.max":"2"}).errors["MH00002.min"]).toMatch(/Xã gửi/);
  expect(validateReview(sheet,"redate",{...fields(),day:"2026-02-30",reason:""}).errors).toMatchObject({reason:"Bắt buộc nhập.",day:"Ngày không hợp lệ."});
 });
 it.each(["correct","redate","delete"] as const)("%s reason follows native Unicode rune/Go-space contract",action=>{
  for(const reason of ["😀".repeat(500),"\u0085"+"😀".repeat(500)+"\u0085","\uFEFF"]){const v=validateReview(sheet,action,{...fields(),reason});expect(v.valid).toBe(true);expect(v.reason).toBe(reason.replace(/^\u0085|\u0085$/g,""));}
  expect(validateReview(sheet,action,{...fields(),reason:"\u0085\u0085"}).errors.reason).toBeDefined();expect(validateReview(sheet,action,{...fields(),reason:"😀".repeat(501)}).errors.reason).toBeDefined();
 });

});
