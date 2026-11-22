import { describe,expect,it } from "vitest";
import type { PriceTicketLog } from "./api";
import { reviewLogEntries } from "./ticket-review-view";
describe("204 ledger Log projection",()=>{
 it("uses each preceding effective line price across corrections, keeping original price and real provenance",()=>{
  const line=(version:number,adj:string|null,at:string)=>({item_code:"MH00001",version,min:"38000",max:"38000",no_goods:false,note:null,adj_min:adj,adj_max:adj,adj_reason:adj?"Đối chiếu":null,actor_ref:`actor${version}`,actor_name:version===3?"Cán bộ Tỉnh":null,at});
  const log={ticket:[],alerts:[],lines:[line(3,"57000","2026-10-12T06:00:00Z"),line(1,null,"2026-10-12T03:00:00Z"),line(2,"56000","2026-10-12T05:00:00Z")]} satisfies PriceTicketLog;
  const entries=reviewLogEntries(log);expect(entries[0]).toMatchObject({event:"MH00001 · dòng v3 · Hiệu chỉnh",at:"2026-10-12T06:00:00Z",actor_display:"Cán bộ Tỉnh",reason:"Đối chiếu"});
  expect(entries[0]!.changes).toEqual([{field:"Giá sử dụng",old:"56.000 – 56.000",new:"57.000 – 57.000"},{field:"Giá Xã gửi (lưu nguyên)",old:"38.000 – 38.000",new:"38.000 – 38.000"}]);
  expect(entries[1]!.actor_display).toBe("actor2");expect(entries[2]!.at).toBe("2026-10-12T03:00:00Z");
 });
});
