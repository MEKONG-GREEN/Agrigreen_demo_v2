import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsCal } from "@/api/mock/kn-cnts-cal";
import { DutyRosterScreen } from "./DutyRosterScreen";
const auth: {api: ApiClient|null} = {api:null};
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({api:auth.api,kn:{context:{role:"admin"}}})}));
vi.mock("next/navigation",()=>({usePathname:()=>"/kn/cnts/lich-truc",useRouter:()=>({push:vi.fn(),replace:vi.fn()})}));
beforeEach(()=>resetCntsCal());
it("CNTS-ROSTER-SCOPE-002: Admin chọn tỉnh trước khi tải; lịch chung cũ chỉ xem",async()=>{
 const calls:string[]=[];
 auth.api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.setter",fetch:async(req)=>{
  calls.push(req.url);return mockFetchNoDelay(req);
 }});
 const user=userEvent.setup();
 render(<DutyRosterScreen/>);
 const selector=screen.getByRole("combobox",{name:"Tỉnh của lịch trực"});
 await screen.findByRole("option",{name:/An Giang/});
 expect(calls.filter(x=>x.includes("duty-rosters"))).toHaveLength(0);
 await user.selectOptions(selector,"91");
 await screen.findByRole("button",{name:"Thêm hàng"});
 expect(calls.some(x=>x.includes("province_code=91"))).toBe(true);
 await user.selectOptions(selector,"legacy");
 await waitFor(()=>expect(calls.some(x=>x.includes("province_code=legacy"))).toBe(true));
 await waitFor(()=>expect(screen.queryByRole("button",{name:"Thêm hàng"})).not.toBeInTheDocument());
 expect(screen.queryByRole("button",{name:/^Lưu/})).not.toBeInTheDocument();
});
