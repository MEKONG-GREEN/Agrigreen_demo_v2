import { useState } from "react";
import { contextStore } from "@/kn/contextStore";
import { PromotionSelectionProvider, usePromotionSelection } from "./promotion-selection";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmPromotion } from "@/api/mock/kn-hrm-promotion";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { ApiProblem } from "@/api/problem";
import { HrmDecisionListScreen } from "./DecisionScreens";
import { createPromotionApi } from "./promotion-api";
import { HrmPromotionExportScreen, HrmPromotionScreen } from "./PromotionScreens";
import { createStaffApi } from "./staff-api";

const nav = vi.hoisted(() => ({ params: {} }));
vi.mock("next/navigation", () => ({ usePathname: () => "/kn/nhan-su/nang-bac", useRouter: () => ({ replace: vi.fn(), push: vi.fn() }), useParams: () => nav.params }));
const auth: { api: ApiClient | null; context: string; actor: string; tenant:string } = { api: null, context:"",actor:"actor278",tenant:"tenant278" };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, principal:{subject:auth.actor,tenantId:auth.tenant}, kn: { context: { context_id:auth.context,role: "admin" } } }) }));
vi.mock("@/lib/files", async orig => ({ ...(await orig<object>()), saveBlob: vi.fn() }));

async function clientFor(personaId: string, assignmentId: string, fetcher = mockFetchNoDelay) {
  const res = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${personaId}-${Math.random()}` }, body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }) }));
  const ctx = (await res.json()) as { context_id: string };
  auth.context=ctx.context_id;contextStore.set(auth.context);
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx.context_id, fetch: fetcher });
  return { api, context: ctx.context_id, pa: createPromotionApi(api), sa: createStaffApi(api) };
}

async function problemCode(p: Promise<unknown>) {
  try { await p; } catch (e) { return e instanceof ApiProblem ? e.problem.code : String(e); }
  return "ok";
}

function SelectionProbe(){const s=usePromotionSelection();return <output aria-label="selection count">{s.keys.length}</output>}
function PromotionHarness(){
  const [exporting,setExporting]=useState(false);
  return <PromotionSelectionProvider><SelectionProbe/><div onClickCapture={e=>{if((e.target as Element).closest("a"))e.preventDefault()}} onClick={e=>{const a=(e.target as Element).closest("a");if(a?.getAttribute("href")==="/kn/nhan-su/nang-bac/xuat"){e.preventDefault();setExporting(true)}else if(a?.getAttribute("href")==="/kn/nhan-su/nang-bac"){e.preventDefault();setExporting(false)}}}>{exporting?<HrmPromotionExportScreen/>:<HrmPromotionScreen/>}</div></PromotionSelectionProvider>;
}

describe("FN09 HCTH v4.0", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-10-12T03:00:00Z"));
    resetHrmStaff(); resetHrmPromotion(); auth.actor="actor278";auth.tenant="tenant278";
  });
  afterEach(() => vi.useRealTimers());

  it("chỉ thường xuyên, dùng NB07 cá nhân, xuất trực tiếp rồi chỉ đổi người có tên", async () => {
    const { pa, sa } = await clientFor("kn-admin", "asg-admin");
    const l = await pa.list();
    expect(l.items).toHaveLength(2);
    expect(l.items.every(x => x.kind === "nltx")).toBe(true);
    expect(l).not.toHaveProperty("summary");
    expect(l).not.toHaveProperty("rules");
    const a = l.items.find(x => x.code === "VC-00001")!;
    const b = l.items.find(x => x.code === "VC-00002")!;
    expect(a.due).toBe("2027-01-01");
    expect(b.due).toBe("2018-01-01");
    await pa.setRating(b.staff_id, { year: 2017, rating: "KHTNV" });
    expect((await pa.list()).items.find(x => x.key === b.key)?.cond).toBe("ok");
    expect((await pa.list({ year: 2027, month: 1, unit: a.unit, q: "Mẫu" })).items.map(x => x.key)).toEqual([a.key]);
    const ex = await pa.exportLists({ keys: [a.key, b.key], review_month: "2026-11", preparer_id:a.staff_id });
    expect(ex.items.map(x => x.form)).toEqual(["mau1"]);
    expect((await pa.file(ex.items[0]!.id)).drift).toBe(false);
    expect((await pa.list()).items.filter(x => x.status === "exported")).toHaveLength(2);
    const d = await pa.createDecision({ export_id: ex.items[0]!.id, decision_no: "540/QĐ-SNNMT", decided_on: "2026-10-10", issuer: "Sở NN&MT", people: [{ case_key: a.key }] });
    expect(d.returned).toBe(0);
    expect((await sa.get(a.staff_id, a.due)).current.step).toBe(a.to_step);
    const after = await pa.list();
    expect(after.items.find(x => x.key === b.key)?.status).toBe("exported");
    expect(after.items.find(x => x.key === a.key)?.status).toBe("decided");
    await pa.cancelDecision(d.id, "Nhập sai số quyết định");
    expect((await sa.get(a.staff_id, a.due)).current.step).toBe(a.from_step);
    expect((await pa.decision(d.id)).status).toBe("cancelled");
    const future=await pa.createDecision({kind:"nltx",decision_no:"QĐ-future",decided_on:"2026-10-01",issuer:"Sở NN&MT",people:[{staff_id:a.staff_id,to_step:a.to_step,effective_on:a.due}]});
    const futureRows=(await pa.list({year:2027,month:1})).items.filter(x=>x.staff_id===a.staff_id);
    expect(futureRows).toHaveLength(1);expect(futureRows[0]?.status).toBe("decided");
    await pa.cancelDecision(future.id,"Hủy quyết định ngày hưởng tương lai");
    expect((await pa.list()).items.find(x=>x.key===a.key)?.status).toBe("exported");

  });

  it("các đường đưa vào/đề xuất ngoài BRD trả404 và khác phòng không có quyền", async () => {
    const { pa } = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(pa.list())).toBe("forbidden");
    const { api, context } = await clientFor("kn-admin", "asg-admin");
    const admin = await api.GET("/v1/hrm/promotion", {});
    expect(admin.response.ok).toBe(true);
    // Direct HTTP verifies removed routes independently of the generated client's type surface.
    for (const [method, path] of [["POST", "cases/include"], ["POST", "cases/exclude"], ["GET", "nlth-pool"], ["POST", "proposals"], ["POST", "proposals/withdraw"]]) {
      const response = await mockFetchNoDelay(new Request(`http://mock/v1/hrm/promotion/${path}`, { method, headers: { Authorization: "Bearer mock.kn-admin", "X-AG-Context": context, "Idempotency-Key": crypto.randomUUID() } }));
      expect(response.status).toBe(404);
    }
  });

  it("màn danh sách có 3 trạng thái, lọc bất kỳ năm và không có khối v3", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    const { unmount } = render(<PromotionSelectionProvider><HrmPromotionScreen /></PromotionSelectionProvider>);
    expect(await screen.findByText("Nguyễn Thị Mẫu")).toBeTruthy();
    expect(screen.queryByText("Tổng số đến kỳ")).toBeNull();
    expect(screen.queryByText("Lập danh sách trước hạn")).toBeNull();
    expect(screen.queryByText("Điều kiện")).toBeNull();
    const select = screen.getByRole("combobox", { name: "Trạng thái" });
    expect(Array.from((select as HTMLSelectElement).options).map(o => o.text)).toEqual(["Tất cả", "Chưa xử lý", "Chờ quyết định", "Đã có quyết định"]);
    const year = screen.getByRole("textbox", { name: "Năm" });
    fireEvent.change(year, { target: { value: "2027" } });
    expect(screen.getByText("Nguyễn Thị Mẫu")).toBeTruthy();
    expect(screen.queryByText("Trần Văn Thử")).toBeNull();
    for (const raw of ["1e100000", "1e-100000", "9".repeat(10000)]) { fireEvent.change(year, { target: { value: raw } }); expect(screen.getByRole("alert")).toBeTruthy(); }
    fireEvent.change(year, { target: { value: "" } });
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn Nguyễn Thị Mẫu" }));
    expect(screen.getByRole("link", { name: "Xuất danh sách (1)" }).getAttribute("href")).toBe("/kn/nhan-su/nang-bac/xuat");
    unmount(); render(<HrmDecisionListScreen />);
    expect(await screen.findByText("Sổ quyết định nâng bậc")).toBeTruthy();
  });

  it("xem trước và xuất chỉ Mẫu1; số mũ/chuỗi dài không gọi API; retry dùng lại khóa", async () => {
    const requests: Request[] = [];
    let failed = false;
    const intercept: typeof mockFetchNoDelay = async (request) => {
      if (request.method === "POST" && request.url.endsWith("/v1/hrm/promotion/exports")) {
        requests.push(request.clone());
        if (!failed) { failed = true; return new Response(JSON.stringify({ type: "about:blank", title: "Thử lại", status: 503, code: "service_unavailable" }), { status: 503, headers: { "Content-Type": "application/problem+json" } }); }
      }
      return mockFetchNoDelay(request);
    };
    const { api, pa } = await clientFor("kn-admin", "asg-admin", intercept);
    auth.api = api;
    await pa.list();
    render(<PromotionHarness/>);
    fireEvent.click(await screen.findByRole("checkbox",{name:"Chọn Nguyễn Thị Mẫu"}));
    fireEvent.click(screen.getByRole("link",{name:"Xuất danh sách (1)"}));
    const months = await screen.findByRole("textbox", { name: "Số tháng được hưởng của Nguyễn Thị Mẫu" });
    const option = await screen.findByRole("option", {name:/VC-00001/});
    fireEvent.change(screen.getByRole("combobox",{name:"Người lập"}),{target:{value:(option as HTMLOptionElement).value}});
    for (const raw of ["1e100000", "1e-100000", "9".repeat(10000)]) {
      fireEvent.change(months, { target: { value: raw } }); fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
      expect(requests).toHaveLength(0); expect(screen.getByRole("button", { name: "Xuất và tải Mẫu số 1" })).toBeDisabled();
    }
    fireEvent.change(months, { target: { value: "1" } });
    const retained = screen.getByRole("textbox", { name: "HS bảo lưu hiện có của Nguyễn Thị Mẫu" });
    for (const raw of ["1e100000", "1e-100000", "9".repeat(10000)]) {
      fireEvent.change(retained, { target: { value: raw } }); fireEvent.click(screen.getByRole("button", { name: "Xem trước" })); expect(requests).toHaveLength(0);
    }
    fireEvent.change(retained, { target: { value: "0,2" } });
    expect((screen.getByRole("textbox",{name:"HS bảo lưu sau nâng của Nguyễn Thị Mẫu"}) as HTMLInputElement).value).toBe("0,2");
    fireEvent.click(screen.getByRole("button", { name: "Xem trước" }));
    expect(await screen.findByRole("heading", {name:"Xem trước Mẫu số 1"})).toBeTruthy();
    const send = screen.getByRole("button", { name: "Xuất và tải Mẫu số 1" });
    fireEvent.click(send); await waitFor(() => expect(requests).toHaveLength(1));
    await waitFor(() => expect(send).not.toBeDisabled()); fireEvent.click(send);
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[0]!.headers.get("Idempotency-Key")).toBe(requests[1]!.headers.get("Idempotency-Key"));
    expect(await screen.findByText(/Đã xuất Mẫu số 1/)).toBeTruthy();
  });
  it("batch500 giữ URL ngắn, gửi đủ500keys và xóa lựa chọn sau thành công",async()=>{
    const seed=await clientFor("kn-admin","asg-admin");
    const source=(await seed.pa.list()).items[0]!;
    const cases=Array.from({length:500},(_,i)=>{const id=crypto.randomUUID();return {...source,staff_id:id,key:`${id}:nltx:b7-20270101`,code:`VC-${i+1}`,name:`Người ${i+1}`}});
    let sent:string[]=[];
    auth.api=createApiClient({baseUrl:"http://batch",getAccessToken:()=>"mock.kn-admin",getContextId:()=>auth.context,fetch:async request=>{
      const path=new URL(request.url).pathname;
      if(path==="/v1/hrm/promotion/exports"&&request.method==="POST"){sent=(await request.json() as {keys:string[]}).keys;return new Response(JSON.stringify({items:[]}),{status:201,headers:{"Content-Type":"application/json"}})}
      const body=path==="/v1/hrm/promotion"?{as_of:"2026-10-12",items:cases,missing:[]}:{items:[{id:source.staff_id,label:"Người lập",code:"VC-00001"}]};
      return new Response(JSON.stringify(body),{status:200,headers:{"Content-Type":"application/json"}});
    }});
    render(<PromotionHarness/>);
    fireEvent.click(await screen.findByRole("button",{name:"Chọn tất cả đang hiển thị"}));
    const link=screen.getByRole("link",{name:"Xuất danh sách (500)"});
    expect(link.getAttribute("href")).toBe("/kn/nhan-su/nang-bac/xuat");
    fireEvent.click(link);
    const preparer=await screen.findByRole("combobox",{name:"Người lập"},{timeout:10_000,interval:100});
    await waitFor(()=>expect(Array.from((preparer as HTMLSelectElement).options).some(o=>o.value===source.staff_id)).toBe(true),{timeout:10_000,interval:100});
    fireEvent.change(preparer,{target:{value:source.staff_id}});
    fireEvent.click(screen.getByRole("button",{name:"Xem trước"}));
    fireEvent.click(screen.getByRole("button",{name:"Xuất và tải Mẫu số 1"}));
    await waitFor(()=>expect(sent).toEqual(cases.map(x=>x.key)),{timeout:10_000,interval:100});
    expect(screen.getByLabelText("selection count").textContent).toBe("0");
  },30_000);

  it("hủy/rời màn, đổi context/profile và reload không tái sử dụng keys cũ",async()=>{
    auth.api=(await clientFor("kn-admin","asg-admin")).api;
    const ui=render(<PromotionHarness/>);
    const pick=async()=>{fireEvent.click(await screen.findByRole("button",{name:"Chọn tất cả đang hiển thị"}));fireEvent.click(screen.getByRole("link",{name:"Xuất danh sách (2)"}));await screen.findByRole("heading",{name:"Xuất danh sách nâng bậc"})};
    await pick();expect(screen.getByLabelText("selection count").textContent).toBe("2");
    fireEvent.click(screen.getByRole("link",{name:"Danh sách nâng bậc"}));
    expect(screen.getByLabelText("selection count").textContent).toBe("0");
    await pick();
    auth.api=(await clientFor("kn-admin","asg-admin")).api;
    ui.rerender(<PromotionHarness/>);
    expect(await screen.findByText(/Chưa chọn người đến kỳ/)).toBeTruthy();
    expect(screen.getByLabelText("selection count").textContent).toBe("0");
    ui.unmount();
    const next=render(<PromotionHarness/>);await pick();
    auth.actor="new-actor";auth.tenant="new-tenant";next.rerender(<PromotionHarness/>);
    expect(await screen.findByText(/Chưa chọn người đến kỳ/)).toBeTruthy();
    next.unmount();
    render(<PromotionSelectionProvider><HrmPromotionExportScreen/></PromotionSelectionProvider>);
    expect(await screen.findByText(/Chưa chọn người đến kỳ/)).toBeTruthy();
  });

});
