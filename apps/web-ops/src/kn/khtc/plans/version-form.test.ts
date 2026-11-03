import { describe, expect, it } from "vitest";
import { PLAN_BASE, parsePlanRoute, planHref } from "./routes";
import { calendarDate, newVersionHead, validateVersionHead } from "./version-form";

describe("kế hoạch — địa chỉ có giới hạn", () => {
  it.each(["1e100000", "1e-100000", "9".repeat(100000), "1999", "2101", "02026", "2026x"])("năm sai không tải API", year => {
    expect(parsePlanRoute(PLAN_BASE, new URLSearchParams({ year }), 2030)).toBeNull();
  });
  it.each(["1e100000", "1e-100000", "9".repeat(100000), "0", "01", "2147483648", "2/extra"])("phiên bản sai", version => {
    expect(parsePlanRoute(`${PLAN_BASE}/phien-ban/${version}`, new URLSearchParams("year=2026"), 2030)).toBeNull();
  });
  it("chỉ đúng đường dẫn và năm, không rơi về phiên bản mới nhất", () => {
    expect(parsePlanRoute(`${PLAN_BASE}/phien-ban/2`, new URLSearchParams("year=2026"), 2030)).toMatchObject({year:2026,version:2,view:"version"});
    expect(parsePlanRoute(`${PLAN_BASE}/other`, new URLSearchParams(), 2030)).toBeNull();
    expect(parsePlanRoute(PLAN_BASE, new URLSearchParams("year=2026&year=2027"),2030)).toBeNull();
    expect(planHref(PLAN_BASE,2026,"save")).toBe(`${PLAN_BASE}/luu-phien-ban?year=2026`);
    const alias="/kn/phong-ban/ke_hoach_tai_chinh/ke-hoach-nam";
    expect(parsePlanRoute(`${alias}/lich-su`,new URLSearchParams("year=2000"),2030)).toMatchObject({base:alias,year:2000,view:"history"});
    expect(parsePlanRoute(PLAN_BASE,new URLSearchParams("year=2100"),2030)?.year).toBe(2100);
  });
});
describe("quyết định kế hoạch — lỗi theo trường",()=>{
  it("điều chỉnh cần đủ ba trường, ngày thật và đúng năm hiệu lực",()=>{
    expect(validateVersionHead(2028,true,newVersionHead(2028,true)).errors).toHaveProperty("decision_no");
    const head={decision_no:"QĐ 1",decision_date:"2028-02-29",effective_from:"2028-03-01",note:"Ghi chú"};
    expect(validateVersionHead(2028,true,head).valid).toBe(true);
    expect(validateVersionHead(2028,true,{...head,decision_date:"2027-02-29"}).errors).toHaveProperty("decision_date");
    expect(validateVersionHead(2028,true,{...head,effective_from:"2029-01-01"}).errors).toHaveProperty("effective_from");
    expect(validateVersionHead(2028,true,{...head,note:"x".repeat(1001)}).errors).toHaveProperty("note");
    expect(validateVersionHead(2028,true,{...head,decision_no:"x".repeat(101)}).errors).toHaveProperty("decision_no");
  });
  it("phiên bản đầu giữ mặc định01/01; không parse ngày mũ hoặc chuỗi dài",()=>{
    expect(validateVersionHead(2030,false,newVersionHead(2030,false)).valid).toBe(true);
    for(const raw of ["1e100000","1e-100000","9".repeat(100000),"2030-02-30"]){expect(calendarDate(raw)).toBe(false);}
  });
});
