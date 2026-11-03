import { expect, it } from "vitest";
import type { MenuTree } from "@/kn/contract";
import { MAIN_KINDS, FIXED_KINDS } from "./api";
import { crumbsFor, findActive, menuAllows } from "../../shell/menu";
const alias="/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su";
const canonical="/kn/nhan-su/danh-muc";
const tree=():MenuTree=>({version:"197",role:"admin",department_code:null,groups:[{key:"hcth",label:"HCTH",items:[{key:"catalog",label:"Danh mục",icon:"catalog",href:canonical,status:"active"}]}]});
it("197 aliases preserve the full supported FN19 path and existing menu leaf",()=>{
 const paths=["","/he-so","/he-so/01.003","/he-so/V.03.04.12/them-moi","/quy-tac/promotion","/quy-tac/promotion/them-moi","/quy-tac/achievement_conversion","/quy-tac/achievement_conversion/them-moi"];
 for(const kind of [...MAIN_KINDS,...FIXED_KINDS])paths.push(`/${kind}`,`/${kind}/them-moi`,`/${kind}/muc/${kind==="grade"?"V.03.04.12":"muc_moi"}`,`/${kind}/muc/${kind==="grade"?"01.003":"muc_moi"}/sua`);
 for(const tail of paths){expect(menuAllows(tree(),alias+tail),tail).toBe(true);expect(findActive(tree(),alias+tail)?.node.href).toBe(canonical);expect(crumbsFor(tree(),alias+tail)).toEqual(["HCTH","Danh mục"]);}
 // Mapping must retain a full path, not grant the whole alias via a root flag.
 const restricted=tree();restricted.groups[0]!.items[0]!.href=canonical+"/quy-tac/promotion";
 expect(menuAllows(restricted,alias+"/quy-tac/promotion/them-moi")).toBe(true);
 expect(menuAllows(restricted,alias+"/quy-tac/achievement_conversion")).toBe(false);
 expect(menuAllows(restricted,alias+"/he-so")).toBe(false);
});
it("197 aliases cannot add disabled/missing module, other role/department or unknown route permission",()=>{
 for(const status of ["not_open","coming_soon"] as const){const t=tree();t.groups[0]!.items[0]!.status=status;expect(menuAllows(t,alias+"/quy-tac/promotion")).toBe(false);}
 const missing=tree();missing.groups=[];expect(menuAllows(missing,alias+"/quy-tac/promotion")).toBe(false);
 const other=tree();other.role="kn_tinh";other.department_code="trong_trot";expect(menuAllows(other,alias+"/quy-tac/promotion")).toBe(false);
 for(const role of [null,"kn_tw","kn_xa"] as const){const t=tree();t.role=role;t.department_code="hanh_chinh_tong_hop";expect(menuAllows(t,alias+"/quy-tac/promotion")).toBe(false);}
 const hcth=tree();hcth.role="kn_tinh";hcth.department_code="hanh_chinh_tong_hop";expect(menuAllows(hcth,alias+"/quy-tac/promotion")).toBe(true);
 for(const path of [alias+"/quy-tac/unknown",alias+"/quy-tac/promotion/sua",alias+"/quy-tac/promotion/them-moi/extra",alias+"/unknown",alias+"/position/muc/%2F",alias+"/grade/muc/"+"9".repeat(41),alias+"/he-so/%2F",alias+"/he-so/01.003/sua",alias+"/position/muc/x/unknown",alias.replace("hanh_chinh_tong_hop","trong_trot")+"/quy-tac/promotion"]){expect(menuAllows(tree(),path),path).toBe(false);}
});
