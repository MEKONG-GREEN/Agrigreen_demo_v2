import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { Blob as NodeBlob, File as NodeFile } from "node:buffer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { KN_ACCOUNTS, KN_DEPARTMENTS, resetKnAccounts } from "@/api/mock/kn-accounts";
import { resetWeeklyPrograms, weeklyDigest } from "@/api/mock/kn-price-weekly";
import { readStoredXlsx } from "@/api/mock/forms/xlsx";
import type { SessionContext } from "@/kn/contract";
import { saveBlob } from "@/lib/files";
import type * as FilesModule from "@/lib/files";
import { createWeeklyApi, type WeeklyView, type WeeklyCommand } from "./api";
import { weeklyBalances, weeklyNumber } from "./model";
import { WeeklyProgramScreen } from "./WeeklyProgramScreen";

const originalAccounts = structuredClone(KN_ACCOUNTS), originalDepartmentStatus = KN_DEPARTMENTS.find(d => d.code === "thong_tin_truyen_thong")!.status;
const state = vi.hoisted(() => ({ api: null as ApiClient | null, context: null as SessionContext | null, mfa: true, subject: "kn-binh", status: "authenticated" }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: state.api!, status: state.status, principal: { subject: state.subject, tenantId: "weekly284", mfa: state.mfa }, kn: { status: "active", context: state.context, support: null } }) }));
vi.mock("@/lib/files", async importOriginal => ({ ...await importOriginal<typeof FilesModule>(), saveBlob: vi.fn() }));

async function fixture(fetcher: (request: Request) => Promise<Response> = mockFetchNoDelay) {
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-binh", "Content-Type": "application/json", "Idempotency-Key": `weekly-context-${Math.random()}` }, body: JSON.stringify({ assignment_id: "asg-binh-tinh", department_code: "thong_tin_truyen_thong" }) }));
  if (!response.ok) throw new Error(`fixture context ${response.status}: ${await response.text()}`);
  state.context = await response.json() as SessionContext;
  state.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => state.context?.context_id ?? null, fetch: fetcher });
  return createWeeklyApi(state.api);
}
const command = (v: WeeklyView, action: WeeklyCommand["action"]): WeeklyCommand => ({ version: v.version, week_id: v.report!.id, action });
const rowName = "Tập huấn nông dân chủ chốt (lớp)";
const input = (label: string, name = rowName) => screen.getByRole("textbox", { name: `${label} ${name}` });

beforeEach(() => {
  // Undici Request requires native File/Blob; jsdom supplies DOM fields, not streaming multipart bodies.
  vi.stubGlobal("Blob", NodeBlob); vi.stubGlobal("File", NodeFile);
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2027-02-18T04:00:00Z"));
  resetKnAccounts(structuredClone(originalAccounts)); resetWeeklyPrograms();
  const department = KN_DEPARTMENTS.find(d => d.code === "thong_tin_truyen_thong")!; department.status = "active";
  for (const account of KN_ACCOUNTS) for (const assignment of account.assignments) assignment.departments = assignment.departments.map(d => d.code === department.code ? department : d);
  KN_ACCOUNTS.find(a => a.id === "kn-binh")!.assignments.find(a => a.assignment_id === "asg-binh-tinh")!.departments.push(department);
  state.mfa = true; state.subject = "kn-binh"; state.status = "authenticated"; state.context = null; state.api = null; vi.mocked(saveBlob).mockClear();
});
afterEach(() => { cleanup(); resetKnAccounts(structuredClone(originalAccounts)); KN_DEPARTMENTS.find(d => d.code === "thong_tin_truyen_thong")!.status = originalDepartmentStatus; vi.useRealTimers(); vi.unstubAllGlobals(); });

describe("284 ordinary decimals and mock digest", () => {
  it("bounds all hostile forms before parsing and preserves exact AC arithmetic", async () => {
    for (const bad of ["1e100000", "1e-100000", "9".repeat(100000), "NaN", "Infinity", "-1", ".5", "1.00000"]) { expect(weeklyNumber(bad)).toBeNull(); expect(weeklyNumber(bad, true)).toBeNull(); }
    expect(weeklyNumber("1.5", true)).toBeNull(); expect(weeklyNumber("999999999999999999", true)?.toString()).toBe("999999999999999999");
    const api = await fixture(), v = await api.read("training"), row = v.report!.rows[3]!;
    const balances = weeklyBalances({ ...row, opening_progress: "10", opening_money: "803458000" }, { progress: "2", money: "10000000" });
    expect(balances.progress?.toString()).toBe("12"); expect(balances.rate?.toString()).toBe("100"); expect(balances.money?.toString()).toBe("813458000");
    expect(weeklyBalances({ ...row, plan: "6552", adjusted_plan: "3822" }, { progress: "1934" }).rate?.toString()).toBe("50.6");
    expect(weeklyBalances({ ...row, plan: "0" }).rate).toBeNull();
    expect(weeklyDigest(new TextEncoder().encode("abc"))).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    expect(weeklyDigest(new Uint8Array())).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
  });
});

describe("284 two program screens with actual generated client + mock contract", () => {
  it("reloads the displayed new draft after an explicitly selected closed period and a concurrent writer", async () => {
    const api = await fixture(); let previous = await api.read("training");
    previous = await api.write("training", { ...command(previous, "close"), confirm: true });
    render(<WeeklyProgramScreen program="training" />);
    await screen.findByRole("button", { name: "Tạo báo cáo tuần mới" });
    fireEvent.change(screen.getByRole("combobox", { name: "Kỳ báo cáo" }), { target: { value: previous.report!.id } });
    await screen.findByRole("button", { name: "Tạo báo cáo tuần mới" });
    fireEvent.click(screen.getByRole("button", { name: "Tạo báo cáo tuần mới" }));
    await screen.findByRole("button", { name: "Lưu nháp" });
    const latest = await api.read("training"), row = latest.report!.rows.find(r => r.name === rowName)!;
    await api.write("training", { ...command(latest, "save"), rows: [{ id: row.id, progress: "4" }] });
    fireEvent.change(input("Tiến độ tuần"), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByRole("button", { name: "Tải lại số liệu mới" });
    fireEvent.click(screen.getByRole("button", { name: "Tải lại số liệu mới" }));
    await waitFor(() => expect(input("Tiến độ tuần")).toHaveValue("4"));
    expect(screen.getByRole("combobox", { name: "Kỳ báo cáo" })).toHaveValue(latest.report!.id);
  });

  it("previews one matching period or explicitly reports fallback before committing; cumulative columns are ignored", async () => {
    const api = await fixture();
    for (const program of ["training", "information"] as const) {
      const v = await api.read(program), row = v.report!.rows.find(r => !r.group)!, week = v.report!.number;
      const first = program === "training" ? week : week + 3, last = first + 1;
      const csv = `Tên hạng mục,Tiến độ thực hiện tuần ${first},Kinh phí tuần ${first},Đánh giá tuần ${first},Tiến độ thực hiện tuần ${last},Kinh phí tuần ${last},Đánh giá tuần ${last},Lũy kế kinh phí tuần ${week}\r\n"${row.name}",2,100,Fixture chọn đầu,3,200,Fixture chọn cuối,1e100000\r\n`;
      const file = new File([csv], "284-multiple-weeks.csv", { type: "text/csv" });
      const preview = await api.preview(program, { file, version: v.version, week_id: v.report!.id });
      expect(preview.valid_rows).toBe(1); expect(preview.error_rows).toBe(0);
      expect(preview.source_columns).toContain(program === "training" ? `Số liệu tuần ${week}.` : "dùng cặp tuần cuối trong tệp");
      expect((await api.read(program)).report!.rows.find(r => r.id === row.id)!.progress).toBe("0");
      const result = await api.commit(program, { file, version: preview.version, week_id: preview.week_id, sha256: preview.sha256 });
      const saved = result.report!.rows.find(r => r.id === row.id)!;
      expect([saved.progress, saved.money, saved.evaluation]).toEqual(program === "training" ? ["2", "100", "Fixture chọn đầu"] : ["3", "200", "Fixture chọn cuối"]);
    }
  });

  it("AC01/03 updates quantities, percentage and program money immediately before saving", async () => {
    const api = await fixture(); let v = await api.read("training"); const row = v.report!.rows.find(r => r.name === rowName)!;
    v = await api.write("training", { ...command(v, "save"), rows: [{ id: row.id, progress: "10", money: "803458000" }] });
    v = await api.write("training", { ...command(v, "close"), confirm: true }); await api.write("training", command(v, "new"));
    render(<WeeklyProgramScreen program="training" />); await screen.findByRole("button", { name: "Lưu nháp" });
    fireEvent.change(input("Tiến độ tuần"), { target: { value: "2" } }); fireEvent.change(input("Kinh phí tuần"), { target: { value: "10000000" } });
    const r = input("Tiến độ tuần").closest("tr")!;
    expect(within(r).getByText("12 / 12")).toBeInTheDocument(); expect(within(r).getByText("đạt 100,00% KH")).toBeInTheDocument();
    expect(screen.getByTestId("weekly-total-money")).toHaveTextContent("813.458.000");
    expect((await api.read("training")).report!.rows.find(r => r.name === rowName)!.progress).toBe("0");
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await screen.findByText("Đã lưu nháp.");
    expect((await api.read("training")).report!.total_money).toBe("813458000");
  });

  it("AC02 adjusts plan with a reason; cancel never writes; invalid fields block save and close", async () => {
    const requests: Request[] = []; const api = await fixture(async request => { requests.push(request.clone()); return mockFetchNoDelay(request); });
    const name = "Thu thập thông tin giá cả nông sản (phiếu)";
    render(<WeeklyProgramScreen program="information" />); await screen.findByRole("button", { name: `Điều chỉnh kế hoạch ${name}` });
    fireEvent.click(screen.getByRole("button", { name: `Điều chỉnh kế hoạch ${name}` })); fireEvent.change(screen.getByLabelText("Quy mô điều chỉnh"), { target: { value: "3822" } });
    fireEvent.change(screen.getByLabelText("Lý do điều chỉnh"), { target: { value: "Theo điều chỉnh kế hoạch đã duyệt" } }); fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(); expect(requests.filter(r => r.method === "POST")).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: `Điều chỉnh kế hoạch ${name}` })); fireEvent.change(screen.getByLabelText("Quy mô điều chỉnh"), { target: { value: "3822" } });
    fireEvent.change(screen.getByLabelText("Lý do điều chỉnh"), { target: { value: "Theo điều chỉnh kế hoạch đã duyệt" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu điều chỉnh" })); await screen.findByText("Đã điều chỉnh kế hoạch.");
    for (const bad of ["1e100000", "1e-100000", "9".repeat(100000), "-1"]) { fireEvent.change(input("Tiến độ tuần", name), { target: { value: bad } }); expect(input("Tiến độ tuần", name)).toHaveAttribute("aria-invalid", "true"); expect(screen.getByRole("button", { name: "Lưu nháp" })).toBeDisabled(); }
    fireEvent.change(input("Tiến độ tuần", name), { target: { value: "1934" } }); expect(within(input("Tiến độ tuần", name).closest("tr")!).getByText("đạt 50,60% KH")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await screen.findByText("Đã lưu nháp.");
    const r = (await api.read("information")).report!.rows.find(r => r.name === name)!; expect(r.plan).toBe("6552"); expect(r.adjusted_plan).toBe("3822"); expect(r.rate).toBe("50.6");
  });

  it("chốt is confirmed atomically and old weeks expose no edit controls", async () => {
    const api = await fixture(); render(<WeeklyProgramScreen program="training" />); const close = await screen.findByRole("button", { name: /^Chốt báo cáo tuần/ });
    fireEvent.change(input("Tiến độ tuần"), { target: { value: "2" } }); fireEvent.click(close); expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận chốt" })); await screen.findByText("Đã chốt báo cáo tuần.");
    const closed = await api.read("training"); expect(closed.report!.status).toBe("closed"); expect(screen.queryByRole("button", { name: "Lưu nháp" })).not.toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Thêm hạng mục" })).not.toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Nhập từ Excel" })).not.toBeInTheDocument();
    await expect(api.write("training", { ...command(closed, "save"), rows: [] })).rejects.toMatchObject({ status: 409, code: "price.weekly_closed" });
    fireEvent.click(screen.getByRole("button", { name: "Tạo báo cáo tuần mới" })); await screen.findByRole("button", { name: "Lưu nháp" });
    const next = await api.read("training"); expect(next.report!.rows.find(r => r.name === rowName)!.opening_progress).toBe("2");
    fireEvent.change(screen.getByRole("combobox", { name: "Kỳ báo cáo" }), { target: { value: closed.report!.id } }); await screen.findByText(/Báo cáo đã chốt — chỉ xem/);
    expect(screen.queryByRole("button", { name: "Lưu nháp" })).not.toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Tạo báo cáo tuần mới" })).not.toBeInTheDocument();
  });

  it("adds a group with children and deletes the whole group after warning/confirmation", async () => {
    const api = await fixture(); render(<WeeklyProgramScreen program="information" />); fireEvent.click(await screen.findByRole("button", { name: "Thêm hạng mục" }));
    fireEvent.change(screen.getByLabelText("Tên hạng mục (kèm đơn vị tính)"), { target: { value: "Nhóm hoạt động bổ sung" } }); fireEvent.click(screen.getByLabelText("Mục nhóm gồm các mục con"));
    fireEvent.change(screen.getByLabelText("Tên mục con 1"), { target: { value: "Mục con bổ sung (lớp)" } }); fireEvent.change(screen.getByLabelText("Quy mô mục con 1"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Thêm vào báo cáo" })); await screen.findByText("Đã thêm hạng mục.");
    fireEvent.change(input("Tiến độ tuần", "Mục con bổ sung (lớp)"), { target: { value: "1" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await screen.findByText("Đã lưu nháp.");
    fireEvent.click(screen.getByRole("button", { name: "Xóa Nhóm hoạt động bổ sung" })); expect(screen.getByRole("dialog")).toHaveTextContent("kể cả tuần đã chốt"); fireEvent.click(screen.getByRole("button", { name: "Đóng" }));
    expect((await api.read("information")).report!.rows.some(r => r.name === "Nhóm hoạt động bổ sung")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Xóa Nhóm hoạt động bổ sung" })); fireEvent.click(screen.getByRole("button", { name: "Xác nhận xóa" })); await screen.findByText("Đã xóa hạng mục và cập nhật lũy kế các tuần.");
    expect((await api.read("information")).report!.rows.some(r => r.name.includes("bổ sung"))).toBe(false);
  });

  it("Excel preview accepts valid rows alongside errors and real template/export files open as OOXML", async () => {
    const api = await fixture(); const v = await api.read("training"), name = v.report!.rows[0]!.name;
    render(<WeeklyProgramScreen program="training" />); fireEvent.click(await screen.findByRole("button", { name: "Nhập từ Excel" }));
    const content = `Tên hạng mục,Tiến độ tuần,Kinh phí tuần,Đánh giá tuần,Lũy kế\r\n"${name}",2,10000000,Đã thực hiện,1e100000\r\n"${rowName}",-1,5,,\r\nTên chưa có,1,2,,\r\n`;
    const file = new File([content], "bao-cao.csv", { type: "text/csv" });
    if (!file.arrayBuffer) Object.defineProperty(file, "arrayBuffer", { value: async () => new TextEncoder().encode(content).buffer });
    fireEvent.change(screen.getByLabelText(/Chọn tệp Excel hoặc CSV/), { target: { files: [file] } }); fireEvent.click(screen.getByRole("button", { name: "Kiểm tra tệp" }));
    await within(screen.getByRole("dialog")).findByText("1 dòng hợp lệ · 0 cảnh báo ghi đè · 2 dòng lỗi"); fireEvent.click(screen.getByRole("button", { name: "Tải danh sách dòng lỗi" })); expect(saveBlob).toHaveBeenCalledWith(expect.any(Blob), "dong-loi-nhap-tuan.csv");
    fireEvent.click(screen.getByRole("button", { name: "Ghi nhận dòng hợp lệ" })); await screen.findByText("Đã ghi các dòng hợp lệ từ Excel.");
    const imported = await api.read("training"); expect(imported.report!.rows[0]!.progress).toBe("2"); expect(imported.report!.total_money).toBe("10000000"); expect(imported.report!.rows[3]!.progress).toBe("0");
    const exported = await api.export("training", imported.report!.id), template = await api.template("training", imported.report!.id);
    const parsed = readStoredXlsx(new Uint8Array(await exported.blob.arrayBuffer())); const empty = readStoredXlsx(new Uint8Array(await template.blob.arrayBuffer()));
    expect(parsed?.merges).toContain("D4:E4"); expect(parsed?.merges).toContain("G4:H4"); expect(parsed?.cells.get("D6")?.text).toBe("2"); expect(parsed?.cells.get("A2")?.text).toContain(imported.report!.to); expect(empty?.cells.get("D6")?.text).toBe("");
    const issues = await api.issues("training", imported.report!.id); expect(readStoredXlsx(new Uint8Array(await issues.blob.arrayBuffer()))?.cells.size).toBeGreaterThan(3);
  });

  it("lost ACK retries the exact same body/key and CAS conflict keeps editable fields until reload", async () => {
    let lost = true; const requests: Request[] = [];
    const api = await fixture(async request => { const matches = request.method === "POST" && request.url.includes("/weekly-programs/") && request.url.endsWith("/actions"); if (matches) requests.push(request.clone()); const response = await mockFetchNoDelay(request); if (matches && lost) { lost = false; throw new TypeError("lost ACK after commit"); } return response; });
    render(<WeeklyProgramScreen program="training" />); await screen.findByRole("button", { name: "Lưu nháp" }); fireEvent.change(input("Tiến độ tuần"), { target: { value: "2" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(requests).toHaveLength(1)); await waitFor(() => expect(screen.getByRole("button", { name: "Lưu nháp" })).not.toBeDisabled()); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await screen.findByText("Đã lưu nháp.");
    expect(requests).toHaveLength(2); expect(requests[0]!.headers.get("Idempotency-Key")).toBe(requests[1]!.headers.get("Idempotency-Key")); expect(await requests[0]!.text()).toBe(await requests[1]!.text());
    let v = await api.read("training"); expect(v.version).toBe(2); expect(v.log).toHaveLength(2);
    fireEvent.change(input("Tiến độ tuần"), { target: { value: "3" } }); const row = v.report!.rows.find(r => r.name === rowName)!; v = await api.write("training", { ...command(v, "save"), rows: [{ id: row.id, progress: "4" }] });
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await screen.findByRole("button", { name: "Tải lại số liệu mới" }); expect(input("Tiến độ tuần")).toHaveValue("3");
    fireEvent.click(screen.getByRole("button", { name: "Tải lại số liệu mới" })); await waitFor(() => expect(input("Tiến độ tuần")).toHaveValue("4"));
  });

  it.each(["kn_xa", "kn_tw", "readonly", "admin"])("%s never reads or writes the screen", async mode => {
    const requests: Request[] = []; await fixture(async request => { requests.push(request); return mockFetchNoDelay(request); });
    state.context = { ...state.context!, ...(mode === "readonly" ? { read_only: true } : { role: mode as "kn_xa" | "kn_tw" | "admin" }) };
    render(<WeeklyProgramScreen program="information" />); expect(screen.getByRole("alert")).toHaveTextContent("Cấp Tỉnh Phòng TTTT"); expect(requests).toHaveLength(0); expect(screen.queryByRole("button", { name: "Lưu nháp" })).not.toBeInTheDocument();
  });

  it("authority change destroys fields and a late success cannot restore editable content", async () => {
    let release: (() => void) | undefined; const held = new Promise<void>(resolve => { release = resolve; });
    await fixture(async request => { const response = await mockFetchNoDelay(request); if (request.method === "POST" && request.url.endsWith("/actions")) await held; return response; });
    const rendered = render(<WeeklyProgramScreen program="training" />); await screen.findByRole("button", { name: "Lưu nháp" }); fireEvent.change(input("Tiến độ tuần"), { target: { value: "2" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu nháp" })).toBeDisabled()); state.context = { ...state.context!, read_only: true }; rendered.rerender(<WeeklyProgramScreen program="training" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Cấp Tỉnh Phòng TTTT"); await act(async () => { release?.(); await held; }); expect(screen.queryByRole("button", { name: "Lưu nháp" })).not.toBeInTheDocument();
  });
});
