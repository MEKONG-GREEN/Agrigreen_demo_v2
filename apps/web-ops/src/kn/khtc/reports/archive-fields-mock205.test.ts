import {expect,it} from "vitest";
import {archiveFields205,archiveMatches205} from "@/api/mock/kn-khtc-reports";
type Frozen=Parameters<typeof archiveFields205>[0];
const content=(entries:Record<string,unknown>[],rows:Record<string,unknown>[])=>({data:{template:"mau_1",plan:{Entries:entries},rows}} as unknown as Frozen);
const rows=[{ref:"root",kind:"program",department:"trong_trot",parent_ref:null},{ref:"a",kind:"item",department:"trong_trot",parent_ref:"root",communes:[]},{ref:"b",kind:"item",department:"chan_nuoi",parent_ref:"root",communes:[]}];
it("projects only frozen selected item fields with nearest inheritance and explicit partial metadata",()=>{
 const frozen=content([{Ref:"root",Kind:"program",FieldCode:"trong_trot"},{Ref:"a",ParentRef:"root",Kind:"item",FieldCode:"chan_nuoi"},{Ref:"b",ParentRef:"root",Kind:"item",FieldCode:""}],rows);
 expect(archiveFields205(frozen,{department:"trong_trot"})).toEqual({field_codes:["chan_nuoi"],field_status:"complete"});
 expect(archiveFields205(frozen)).toEqual({field_codes:["chan_nuoi","trong_trot"],field_status:"complete"});
 expect(archiveMatches205(frozen,{department:"trong_trot"},{field:"trong_trot"})).toBe(false);
 const partial=content([{Ref:"a",Kind:"item",FieldCode:"chan_nuoi"}],rows);
 expect(archiveFields205(partial)).toEqual({field_codes:["chan_nuoi"],field_status:"partial"});
});
it("never infers fields from department, numeric values or a live catalog",()=>{
 const frozen=content([],rows.map(r=>({...r,planned:"1e100000",budget:"9".repeat(100000)})));
 expect(archiveFields205(frozen)).toEqual({field_codes:[],field_status:"unknown"});
 expect(archiveMatches205(frozen,{},{})).toBe(true);
 expect(archiveMatches205(frozen,{},{field_status:"unknown"})).toBe(true);
 expect(archiveMatches205(frozen,{},{field:"trong_trot"})).toBe(false);
});
