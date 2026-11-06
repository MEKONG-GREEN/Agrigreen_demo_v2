import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { PNProgressScreen } from "./PNProgressScreen";
import type { PNPeriod, PNProgress } from "./api";
import type * as Files from "@/lib/files";
import { saveBlob } from "@/lib/files";

const state = vi.hoisted(() => ({ api: {}, status: "authenticated", knStatus: "active", principal: { actor_ref: "actor-a", tenant_id: "tenant-a", amr: ["mfa"] }, context: { context_id: "ctx-a", role: "kn_tinh", department_code: "trong_trot", read_only: false } }));
const calls = vi.hoisted(() => ({ periods: vi.fn(), get: vi.fn(), excel: vi.fn() }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: state.api, status: state.status, principal: state.principal, kn: { status: state.knStatus, context: state.context } }) }));
vi.mock("./api", async original => ({ ...await original<typeof import("./api")>(), createPNProgressApi: () => calls }));
vi.mock("@/shell/Breadcrumb", () => ({ useBreadcrumbTail: () => undefined }));
vi.mock("@/lib/files", async original => ({ ...await original<typeof Files>(), saveBlob: vi.fn() }));
const A = "01900000-0000-7000-8000-000000000215", B = "01900000-0000-7000-8000-000000000216", S = "01900000-0000-7000-8000-000000000217", P = "01900000-0000-7000-8000-000000000218";
const period: PNPeriod = { id: A, name: "Kỳ A", year: 2030, from: "2030-01-01T00:00:00Z", to: "2030-06-04T00:00:00Z", cutoff: "2030-06-04T00:00:00Z" };
function view(name = "Hạng mục riêng A"): PNProgress {
  return { department: "trong_trot", period, status: "snapshot", snapshot: { id: S, version: 2, captured_at: "2030-06-05T03:00:00Z" }, programs: [{ ref: P, name: "Chương trình chung", roman: "I" }], rows: [{ ref: P, parent_ref: null, kind: "program", roman: "I", name: "Chương trình chung", unit: "", planned: null, year_to_date: "", percent: null, level: "", missing: false }, { ref: B, parent_ref: P, kind: "item", roman: "", name, department: "trong_trot", unit: "ha", planned: "9007199254740993.25", year_to_date: "25.125", percent: "0.125", level: "slow", missing: false }] };
}
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { promise, resolve }; }
beforeEach(() => {
  vi.clearAllMocks(); vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-06-05T03:00:00Z"));
  Object.assign(state, { api: {}, status: "authenticated", knStatus: "active", principal: { actor_ref: "actor-a", tenant_id: "tenant-a", amr: ["mfa"] }, context: { context_id: "ctx-a", role: "kn_tinh", department_code: "trong_trot", read_only: false } });
  calls.periods.mockReset().mockResolvedValue({ department: "trong_trot", items: [period], has_more: false, next_after: null });
  calls.get.mockReset().mockResolvedValue(view()); calls.excel.mockReset().mockResolvedValue({ blob: new Blob(["xlsx"]), name: "phong.xlsx" });
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

it("shows exact M5 quantities, source version and read-only controls", async () => {
  render(<PNProgressScreen />); const table = await screen.findByRole("table", { name: "Tiến độ chương trình của phòng" });
  expect(within(table).getByText("9007199254740993.25")).toBeInTheDocument(); expect(within(table).getByText("25.125")).toBeInTheDocument();
  expect(screen.getByText("Mẫu 5 v2")).toBeInTheDocument(); expect(screen.getByText("Chỉ xem")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^Lưu|^Gửi/ })).not.toBeInTheDocument(); expect(calls.periods).toHaveBeenCalledWith("trong_trot", "2030");
});
it("keeps the displayed snapshot for program/level filters and Excel, then explicitly reloads latest", async () => {
  render(<PNProgressScreen />); await screen.findByText("Hạng mục riêng A");
  fireEvent.change(screen.getByRole("combobox", { name: "Chương trình" }), { target: { value: P } });
  await waitFor(() => expect(calls.get).toHaveBeenLastCalledWith("trong_trot", A, P, "", S));
  fireEvent.change(screen.getByRole("combobox", { name: "Mức đạt" }), { target: { value: "slow" } });
  await waitFor(() => expect(calls.get).toHaveBeenLastCalledWith("trong_trot", A, P, "slow", S));
  fireEvent.click(await screen.findByRole("button", { name: "Xuất Excel" }));
  await waitFor(() => expect(saveBlob).toHaveBeenCalledTimes(1)); expect(calls.excel).toHaveBeenCalledWith("trong_trot", A, S, P, "slow");
  fireEvent.click(screen.getByRole("button", { name: "Đọc bản Mẫu 5 mới" }));
  await waitFor(() => expect(calls.get).toHaveBeenLastCalledWith("trong_trot", A, P, "slow", undefined));
});
it("shows an honest unprepared state and missing percentage without offering export of invented data", async () => {
  calls.get.mockResolvedValue({ ...view(), status: "not_prepared", snapshot: null, rows: [], programs: [] });
  const mounted = render(<PNProgressScreen />); await screen.findByText(/KHTC chưa chuẩn bị bản Mẫu 5/);
  expect(screen.queryByRole("button", { name: "Xuất Excel" })).not.toBeInTheDocument();
  mounted.unmount(); const data = view(); data.rows[1] = { ...data.rows[1]!, missing: true, percent: "0" }; calls.get.mockResolvedValue(data);
  render(<PNProgressScreen />); const row = (await screen.findByText("Hạng mục riêng A")).closest("tr")!;
  expect(within(row).getByText("—")).toBeInTheDocument(); expect(within(row).queryByText("0")).not.toBeInTheDocument();
});
it("reads the next choices page and drops old rows synchronously on period or year selection", async () => {
  calls.periods.mockResolvedValueOnce({ department: "trong_trot", items: [period], has_more: true, next_after: A }).mockResolvedValueOnce({ department: "trong_trot", items: [{ ...period, id: B, name: "Kỳ B" }], has_more: false, next_after: null });
  render(<PNProgressScreen />); await screen.findByText("Hạng mục riêng A"); fireEvent.click(screen.getByRole("button", { name: "Xem thêm kỳ" }));
  await screen.findByRole("option", { name: "Kỳ B" }); expect(calls.periods).toHaveBeenLastCalledWith("trong_trot", "2030", A);
  const pending = deferred<PNProgress>(); calls.get.mockReturnValue(pending.promise);
  fireEvent.change(screen.getByRole("combobox", { name: "Kỳ báo cáo" }), { target: { value: B } }); expect(screen.queryByText("Hạng mục riêng A")).not.toBeInTheDocument();
  const next = { ...view("Hạng mục riêng B"), period: { ...period, id: B } }; await act(async () => pending.resolve(next)); await screen.findByText("Hạng mục riêng B");
  calls.periods.mockReturnValue(new Promise(() => {})); fireEvent.change(screen.getByRole("combobox", { name: /Năm kế hoạch/ }), { target: { value: "2029" } });
  expect(screen.queryByText("Hạng mục riêng B")).not.toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Xuất Excel" })).not.toBeInTheDocument();
});
it.each(["api", "actor", "tenant", "context", "department", "role", "logout", "revoke"])("clears scoped data and discards a late download after %s changes", async kind => {
  const mounted = render(<PNProgressScreen />); await screen.findByText("Hạng mục riêng A");
  const file = deferred<{ blob: Blob; name: string }>(); calls.excel.mockReturnValue(file.promise); fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
  calls.periods.mockReturnValue(new Promise(() => {}));
  if (kind === "api") state.api = {};
  if (kind === "actor") state.principal = { ...state.principal, actor_ref: "actor-b" };
  if (kind === "tenant") state.principal = { ...state.principal, tenant_id: "tenant-b" };
  if (kind === "context") state.context = { ...state.context, context_id: "ctx-b" };
  if (kind === "department") state.context = { ...state.context, department_code: "chan_nuoi_thuy_san" };
  if (kind === "role") state.context = { ...state.context, role: "kn_xa" };
  if (kind === "logout") state.status = "anonymous";
  if (kind === "revoke") state.knStatus = "revoked";
  mounted.rerender(<PNProgressScreen />); expect(screen.queryByText("Hạng mục riêng A")).not.toBeInTheDocument();
  await act(async () => file.resolve({ blob: new Blob(["OLD PRIVATE FILE"]), name: "old.xlsx" })); expect(saveBlob).not.toHaveBeenCalled();
});
it("drops late filtered responses and late Excel when returning A to another filter to A", async () => {
  const mounted = render(<PNProgressScreen />); await screen.findByText("Hạng mục riêng A");
  const file = deferred<{ blob: Blob; name: string }>(); calls.excel.mockReturnValue(file.promise); fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
  const old = deferred<PNProgress>(), fresh = deferred<PNProgress>(); calls.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
  fireEvent.change(screen.getByRole("combobox", { name: "Mức đạt" }), { target: { value: "slow" } });
  fireEvent.change(screen.getByRole("combobox", { name: "Mức đạt" }), { target: { value: "" } });
  await act(async () => { old.resolve(view("OLD FILTER")); file.resolve({ blob: new Blob(["old"]), name: "old.xlsx" }); });
  expect(screen.queryByText("OLD FILTER")).not.toBeInTheDocument(); expect(saveBlob).not.toHaveBeenCalled();
  await act(async () => fresh.resolve(view("Fresh response"))); await screen.findByText("Fresh response"); mounted.unmount();
});
it("Admin route binds the requested professional department and links back to it", async () => {
  state.context = { ...state.context, role: "admin", department_code: "ke_hoach_tai_chinh" };
  render(<PNProgressScreen department="trong_trot" />); await screen.findByText("Hạng mục riêng A");
  expect(screen.getByRole("link", { name: "Báo cáo cho KHTC" })).toHaveAttribute("href", "/kn/phong-ban/trong_trot/bao-cao-khtc");
});
it("refuses a forced foreign department before any data request", () => {
  render(<PNProgressScreen department="chan_nuoi_thuy_san" />); expect(screen.getByRole("alert")).toHaveTextContent("phòng chuyên môn");
  expect(calls.periods).not.toHaveBeenCalled(); expect(calls.get).not.toHaveBeenCalled();
});
