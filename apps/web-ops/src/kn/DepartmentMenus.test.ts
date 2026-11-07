import { describe, expect, it } from "vitest";
import { knMenu } from "@/api/mock/kn";
import { fromKnMenu } from "./menuAdapter";
import { menuAllows, menuLinks } from "@/shell/menu";
import type { KnRole } from "./contract";

describe("S-4 department route parity", () => {
 const departments=["trong_trot","chan_nuoi_thuy_san","ke_hoach_tai_chinh","thong_tin_truyen_thong","hanh_chinh_tong_hop"];
 for(const department of departments) for(const role of ["kn_tw","kn_tinh","kn_xa"] as KnRole[]) {
  it(`${role}/${department} denies foreign TT and Q4 routes`,()=>{
   const menu=fromKnMenu(knMenu(role,department,{adHoc:true}));
   for(const link of menuLinks(menu)) expect(menuAllows(menu,link.node.href!)).toBe(true);
   expect(menuAllows(menu,"/kn/bao-cao/dot-xuat")).toBe(false);
   expect(menuAllows(menu,"/kn/van-ban")).toBe(department==="chan_nuoi_thuy_san" && role!=="kn_tw");
   if(department!=="trong_trot") for(const path of ["/kn/bao-cao/tuan","/kn/bao-cao/thang","/kn/bao-cao/vu","/kn/bao-cao/nam","/kn/bao-cao/tong-hop","/kn/nhiem-vu/phat-hanh","/kn/nhiem-vu/trang-thai-nop","/kn/nhiem-vu/ra-soat","/kn/nhiem-vu/cong-viec"]) expect(menuAllows(menu,path),path).toBe(false);
   expect(menuAllows(menu,"/kn/dung-chung/contact")).toBe(true);
  });
 }
});
