import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { KhtcPendingInboxScreen } from "./KhtcPendingInboxScreen";
import { readPending243 } from "./api";

const route = vi.hoisted(() => ({ pathname: "/kn/khtc/phieu-cho-ra-soat" }));
vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));
const auth = {
  status: "authenticated", principal: { subject: "actor-A", tenantId: "tenant-A", mfa: true }, scopeWarning: null as string | null,
  kn: { status: "active", assignments: [] as unknown[], support: null as {target:string;reason:string}|null,
    context: { context_id: "context-A", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", expires_at: "2030-01-01T00:00:00Z", read_only:false } },
  api: null as unknown as ApiClient,
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
const ids = { period: "01990000-0000-7000-8000-000000000009", second: "01990000-0000-7000-8000-000000000008", submission: "01990000-0000-7000-8000-000000000001" };
const row = (name = "Tuần đã gửi") => ({ period_id: ids.period, period_name: name, department: "trong_trot", department_name: "Phòng Trồng trọt", submission_id: ids.submission, attempt: 2, submitted_at: "2026-10-13T02:00:00Z" });
const page = (name = "Tuần đã gửi") => ({ rows:[row(name)], next_cursor:null });
const makeApi = (fetch: (request: Request) => Promise<Response>) => createApiClient({ baseUrl: "http://mock", getAccessToken: () => "test", fetch });
function delayed() { let resolve!: (response: Response) => void; const promise = new Promise<Response>(r => { resolve = r; }); return {promise,resolve}; }

beforeEach(() => {
  vi.useFakeTimers({toFake:["Date"]}); vi.setSystemTime(new Date("2026-10-13T03:00:00Z"));
  route.pathname="/kn/khtc/phieu-cho-ra-soat";
  auth.status="authenticated"; auth.principal={subject:"actor-A",tenantId:"tenant-A",mfa:true}; auth.scopeWarning=null;
  auth.kn={status:"active",assignments:[],support:null,context:{context_id:"context-A",role:"kn_tinh",department_code:"ke_hoach_tai_chinh",expires_at:"2030-01-01T00:00:00Z",read_only:false}};
  auth.api=makeApi(async()=>Response.json(page()));
});
afterEach(() => vi.useRealTimers());

describe("Hộp phiếu chờ native243", () => {
  it("shows recorded latest attempt, STT, and existing reviewer link with only a private GET", async () => {
    const requests:Request[]=[]; auth.api=makeApi(async request=>{requests.push(request);return Response.json(page());});
    render(<KhtcPendingInboxScreen/>); await screen.findByText("Tuần đã gửi");
    expect(screen.getByRole("columnheader",{name:"STT"})).toBeInTheDocument();
    expect(screen.getByRole("link",{name:"Xem phiếu"})).toHaveAttribute("href",`/kn/khtc/ra-soat/${ids.period}?department=trong_trot`);
    expect(requests).toHaveLength(1); expect(requests[0]!.method).toBe("GET"); expect(requests[0]!.cache).toBe("no-store");
    expect(screen.queryByRole("button",{name:"Xác nhận phiếu"})).not.toBeInTheDocument();
  });
  it("keeps next-page navigation on an empty group and clears old rows while paging", async () => {
    const requests:Request[]=[]; const second=delayed();
    auth.api=makeApi(async r=>{requests.push(r);return new URL(r.url).searchParams.has("before")?second.promise:Response.json({rows:[],next_cursor:ids.period});});
    render(<KhtcPendingInboxScreen/>); await screen.findByText(/Chưa có phiếu chờ rà soát trong nhóm kỳ này/);
    fireEvent.click(screen.getByRole("button",{name:"Trang tiếp"}));
    expect(screen.getByText("Đang đọc hộp phiếu…")).toBeInTheDocument();
    await act(async()=>second.resolve(Response.json({rows:[{...row("Kỳ cũ hơn"),period_id:ids.second}],next_cursor:null})));
    await screen.findByText("Kỳ cũ hơn"); expect(requests[1]!.url).toContain(`before=${ids.period}`);
    expect(screen.getByRole("button",{name:"Trang tiếp"})).toBeDisabled();
    fireEvent.click(screen.getByRole("button",{name:"Trang trước"}));
    expect(screen.queryByText("Kỳ cũ hơn")).not.toBeInTheDocument();
  });
  it("clears results on reload failure and never converts a denial to an empty count", async () => {
    let fail=false;auth.api=makeApi(async()=>fail?Response.json({status:403,code:"forbidden",title:"Denied"},{status:403}):Response.json(page()));
    render(<KhtcPendingInboxScreen/>);await screen.findByText("Tuần đã gửi");fail=true;
    fireEvent.click(screen.getByRole("button",{name:"Làm mới"}));
    expect(screen.queryByText("Tuần đã gửi")).not.toBeInTheDocument();
    await screen.findByRole("alert");expect(screen.queryByText(/Chưa có phiếu/)).not.toBeInTheDocument();
  });
  it.each(["context","tenant","mfa","assignment","support","scope","role","readonly","route"])("rejects the old response after a committed %s authority change",async part=>{
    const old=delayed();let calls=0;auth.api=makeApi(async()=>++calls===1?old.promise:Response.json(page("Phiếu hiện tại")));
    const view=render(<KhtcPendingInboxScreen/>);await waitFor(()=>expect(calls).toBe(1));
    if(part==="context")auth.kn.context={...auth.kn.context,context_id:"context-B"};
    if(part==="tenant")auth.principal={...auth.principal,tenantId:"tenant-B"};
    if(part==="mfa")auth.principal={...auth.principal,mfa:false};
    if(part==="assignment")auth.kn.assignments=[{assignment_id:"replacement"}];
    if(part==="support")auth.kn.support={target:"B",reason:"Changed support session"};
    if(part==="scope")auth.scopeWarning="Changed scope";
    if(part==="role")auth.kn.context={...auth.kn.context,role:"admin"};
    if(part==="readonly")auth.kn.context={...auth.kn.context,read_only:true};
    if(part==="route")route.pathname+="/replacement";
    view.rerender(<KhtcPendingInboxScreen/>);await screen.findByText("Phiếu hiện tại");
    await act(async()=>old.resolve(Response.json(page("Phiếu cũ"))));
    expect(screen.queryByText("Phiếu cũ")).not.toBeInTheDocument();
  });
  it("separates a committed client A to B to A lifetime",async()=>{
    const old=delayed();let calls=0;const a=makeApi(async()=>++calls===1?old.promise:Response.json(page("A mới")));
    auth.api=a;const view=render(<KhtcPendingInboxScreen/>);await waitFor(()=>expect(calls).toBe(1));
    auth.api=makeApi(async()=>Response.json(page("B")));view.rerender(<KhtcPendingInboxScreen/>);await screen.findByText("B");
    auth.api=a;view.rerender(<KhtcPendingInboxScreen/>);await screen.findByText("A mới");
    await act(async()=>old.resolve(Response.json(page("A cũ"))));expect(screen.queryByText("A cũ")).not.toBeInTheDocument();
  });
  it("hides cached facts when the session expires without a rerender",async()=>{
    auth.kn.context.expires_at="2026-10-13T03:00:00.100Z";
    vi.useFakeTimers();vi.setSystemTime(new Date("2026-10-13T03:00:00Z"));
    render(<KhtcPendingInboxScreen/>);await act(async()=>{await vi.advanceTimersByTimeAsync(0);});
    expect(screen.getByText("Tuần đã gửi")).toBeInTheDocument();
    await act(async()=>{await vi.advanceTimersByTimeAsync(101);});
    expect(screen.queryByText("Tuần đã gửi")).not.toBeInTheDocument();expect(screen.getByRole("alert")).toHaveTextContent("Ngữ cảnh đã hết hạn");
  });
  it("does not request native data for an already expired session",async()=>{
    auth.kn.context.expires_at="2026-10-13T02:59:59Z";
    let calls=0;auth.api=makeApi(async()=>{calls++;return Response.json(page());});
    render(<KhtcPendingInboxScreen/>);
    expect(screen.getByRole("alert")).toHaveTextContent("Ngữ cảnh đã hết hạn");
    expect(calls).toBe(0);expect(screen.queryByText("Tuần đã gửi")).not.toBeInTheDocument();
  });
  it("rejects malformed, duplicated, unsafe or non-progressing native page facts",async()=>{
    for(const invalid of [{rows:null,next_cursor:null},{rows:[],next_cursor:ids.period},{rows:[row(),row()],next_cursor:null},
      {rows:[{...row(),attempt:1e100}],next_cursor:null},{rows:[{...row(),submission_id:"wrong"}],next_cursor:null},{rows:[{...row(),submitted_at:"invalid"}],next_cursor:null}]){
      const api=makeApi(async()=>Response.json(invalid));
      await expect(readPending243(api,ids.period,new AbortController().signal)).rejects.toThrow();
    }
  });
});
