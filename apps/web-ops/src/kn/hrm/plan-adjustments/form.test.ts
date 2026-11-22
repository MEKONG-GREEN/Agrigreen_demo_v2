import { describe, expect, it } from "vitest";
import { trimReason, validateAdjustment } from "./form";
import { adjustmentHref, parseAdjustmentRoute } from "./routes";
const normal={leaving:"0",additional:"9999",reason:"Điều chỉnh hợp lệ"};
describe("adjustment203 bounded inputs and exact routes",()=>{
  for(const field of ["leaving","additional"] as const){
    for(const raw of ["1e3","1e100000","1e-100000","9".repeat(100000),"99999999999999999999999999999999","-1","0.5","10000",""]){
      it(`${field} rejects ${raw.slice(0,16)}`,()=>expect(validateAdjustment({...normal,[field]:raw}).errors[field]).toBeTruthy());
    }
    for(const raw of ["0","9999","1"]){it(`${field} accepts ${raw}`,()=>expect(validateAdjustment({...normal,[field]:raw}).valid).toBe(true));}
  }
  it("reason matches Go TrimSpace/RuneCount, including NEL and FEFF",()=>{
    expect(validateAdjustment({...normal,reason:"😀".repeat(9)}).errors.reason).toBeTruthy();
    expect(validateAdjustment({...normal,reason:"😀".repeat(10)}).valid).toBe(true);
    expect(validateAdjustment({...normal,reason:"\u0085\u00a0"+"😀".repeat(500)+"\u0085"}).valid).toBe(true);
    expect(validateAdjustment({...normal,reason:"😀".repeat(501)}).errors.reason).toBeTruthy();
    expect(trimReason("\ufeff"+"😀".repeat(9))).toBe("\ufeff"+"😀".repeat(9));
    expect(validateAdjustment({...normal,reason:"\ufeff"+"😀".repeat(9)}).valid).toBe(true);
  });
  it("malformed year/unit/seq rejects before numeric conversion and URL construction",()=>{
    for(const year of ["1e100000","1e-100000","9".repeat(100000),"2019","2101"]){expect(parseAdjustmentRoute(year,"trong_trot","edit")).toBeNull();}
    for(const seq of ["0","01","-1","1e100000","1e-100000","9".repeat(100000),"9007199254740992"]){expect(parseAdjustmentRoute("2027","trong_trot","detail",seq)).toBeNull();}
    expect(parseAdjustmentRoute("2027","x/y","edit")).toBeNull();
    expect(parseAdjustmentRoute("2027","trong_trot","detail","2")).toEqual({year:2027,unit:"trong_trot",mode:"detail",seq:2});
    expect(adjustmentHref(2027,"trong_trot",2)).toBe("/kn/nhan-su/ke-hoach/2027/trong_trot/dieu-chinh/2");
  });
});
