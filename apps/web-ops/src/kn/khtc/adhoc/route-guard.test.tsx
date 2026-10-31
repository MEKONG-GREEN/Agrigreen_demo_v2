import {render,screen} from "@testing-library/react";
import {afterEach,expect,it,vi} from "vitest";
import {KhtcAdhocScreen} from "./KhtcAdhocScreen";

const auth=vi.hoisted(()=>({context:null as null|{role:string;department_code:string|null;read_only:boolean}}));
const pane=vi.hoisted(()=>vi.fn());
vi.mock("@/auth/AuthProvider",()=>({useAuth:()=>({kn:{context:auth.context}})}));
vi.mock("./AdhocOpenRecovery",()=>({AdhocOpenRecovery:()=>{pane("recovery");return <p>Recovery pane</p>}}));
vi.mock("../periods/KhtcPeriodsScreen",()=>({KhtcPeriodsScreen:()=>{pane("periods");return <p>Period pane</p>}}));
vi.mock("../reports/KhtcReportsScreen",()=>({KhtcReportsScreen:()=>{pane("reports");return <p>Report pane</p>}}));
afterEach(()=>{pane.mockClear();auth.context=null});

it.each([
 ["no selected session",null],
 ["readonly Admin",{role:"admin",department_code:null,read_only:true}],
 ["readonly KHTC",{role:"kn_tinh",department_code:"ke_hoach_tai_chinh",read_only:true}],
 ["TW KHTC",{role:"kn_tw",department_code:"ke_hoach_tai_chinh",read_only:false}],
 ["commune",{role:"kn_xa",department_code:"trong_trot",read_only:false}],
 ["another department",{role:"kn_tinh",department_code:"trong_trot",read_only:false}],
] as const)("W11.68 draft panes do not mount for %s",(_name,context)=>{
 auth.context=context;render(<KhtcAdhocScreen/>);
 expect(pane).not.toHaveBeenCalled();
 expect(screen.getByRole("link",{name:"Xem báo cáo đã xuất"})).toHaveAttribute("href","/kn/khtc/bao-cao-da-xuat");
});

it.each([
 {role:"admin",department_code:null,read_only:false},
 {role:"kn_tinh",department_code:"ke_hoach_tai_chinh",read_only:false},
])("W11.68 renders the draft panes for an authorized context %j",(context)=>{
 auth.context=context;render(<KhtcAdhocScreen/>);
 expect(pane.mock.calls.map(([name])=>name)).toEqual(["recovery","periods","reports"]);
});
