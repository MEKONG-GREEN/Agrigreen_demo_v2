import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { KhtcPendingInboxScreen } from "./KhtcPendingInboxScreen";
import { readPending243 } from "./api";

vi.mock("next/navigation", () => ({ usePathname: () => "/kn/khtc/phieu-cho-ra-soat" }));
const auth = { status: "authenticated", principal: { subject: "actor245", tenantId: "tenant245", mfa: true }, scopeWarning: null,
  kn: { status: "active", assignments: [] as unknown[], support: null,
    context: { context_id: "context245", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", expires_at: "2030-01-01T00:00:00Z", read_only: false } }, api: null as unknown as ApiClient };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
const period = "01990000-0000-7000-8000-000000000009", cursor = "01990000-0000-7000-8000-000000000008";
const row = (name: string) => ({ period_id: period, period_name: name, department: "trong_trot", department_name: "Phòng Trồng trọt",
  submission_id: "01990000-0000-7000-8000-000000000001", attempt: 1, submitted_at: "2026-10-13T02:00:00Z" });
const page = (name: string) => ({ rows: [row(name)], next_cursor: null });
const api = (fetch: (request: Request) => Promise<Response>) => createApiClient({ baseUrl: "http://mock", getAccessToken: () => "test", fetch });
function delayed() { let resolve!: (response: Response) => void; const promise = new Promise<Response>(r => { resolve = r; }); return { promise, resolve }; }
beforeEach(() => { vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2026-10-13T03:00:00Z"));
  auth.kn.context = { context_id: "context245", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", expires_at: "2030-01-01T00:00:00Z", read_only: false };
  auth.principal.mfa = true;
});
afterEach(() => vi.useRealTimers());

it("resets cursor and discards an old response when regular changes to adhoc", async () => {
  const old = delayed(), next = delayed(), requests: Request[] = [];
  auth.api = api(async request => { requests.push(request); const q = new URL(request.url).searchParams;
    return q.get("report_type") === "adhoc" ? next.promise : q.has("before") ? old.promise : Response.json({ rows: [], next_cursor: cursor }); });
  render(<KhtcPendingInboxScreen />); await screen.findByText(/Chưa có phiếu chờ rà soát/);
  fireEvent.click(screen.getByRole("button", { name: "Trang tiếp" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  fireEvent.change(screen.getByRole("combobox", { name: "Loại phiếu" }), { target: { value: "adhoc" } });
  await waitFor(() => expect(requests).toHaveLength(3));
  expect(new URL(requests[2]!.url).searchParams.get("report_type")).toBe("adhoc");
  expect(new URL(requests[2]!.url).searchParams.has("before")).toBe(false);
  await act(async () => old.resolve(Response.json(page("Phiếu thường niên cũ"))));
  expect(screen.queryByText("Phiếu thường niên cũ")).not.toBeInTheDocument();
  await act(async () => next.resolve(Response.json(page("Phát sinh hiện tại"))));
  await screen.findByRole("cell", { name: "Phát sinh hiện tại" });
  expect(screen.getByRole("button", { name: "Trang trước" })).toBeDisabled();
  expect(screen.getByRole("link", { name: "Xem phiếu" })).toHaveAttribute("href", `/kn/khtc/ra-soat/${period}?department=trong_trot`);
});

it("sends department and period filters, resets the page, and clears stale rows on denial", async () => {
  const requests: Request[] = []; let deny = false;
  auth.api = api(async request => { requests.push(request); return deny ? Response.json({ status: 503, title: "Chưa đối chiếu", code: "extension.dependency_unavailable" }, { status: 503 }) : Response.json(page("Kỳ cần đối chiếu")); });
  render(<KhtcPendingInboxScreen />); await screen.findByRole("cell", { name: "Kỳ cần đối chiếu" });
  fireEvent.change(screen.getByRole("combobox", { name: "Loại phiếu" }), { target: { value: "adhoc" } });
  await screen.findByRole("cell", { name: "Kỳ cần đối chiếu" });
  fireEvent.change(screen.getByRole("combobox", { name: "Phòng" }), { target: { value: "trong_trot" } });
  await screen.findByRole("cell", { name: "Kỳ cần đối chiếu" });
  fireEvent.change(screen.getByRole("combobox", { name: "Kỳ báo cáo trong nhóm" }), { target: { value: period } });
  await waitFor(() => expect(requests).toHaveLength(4));
  const query = new URL(requests[3]!.url).searchParams;
  expect(query.get("report_type")).toBe("adhoc"); expect(query.get("department")).toBe("trong_trot"); expect(query.get("period_id")).toBe(period); expect(query.has("before")).toBe(false);
  await screen.findByRole("link", { name: "Xem phiếu" }); deny = true;
  fireEvent.click(screen.getByRole("button", { name: "Làm mới" }));
  expect(screen.queryByRole("link", { name: "Xem phiếu" })).not.toBeInTheDocument();
  await screen.findByRole("alert"); expect(screen.queryByText(/Chưa có phiếu chờ/)).not.toBeInTheDocument();
});

it("drops the adhoc selection and stale data when current authority becomes read-only", async () => {
  const response = delayed(); let calls = 0;
  auth.api = api(async request => { calls++; return new URL(request.url).searchParams.get("report_type") === "adhoc" ? response.promise : Response.json(page("Phiếu thường niên")); });
  const view = render(<KhtcPendingInboxScreen />); await screen.findByRole("link", { name: "Xem phiếu" });
  fireEvent.change(screen.getByRole("combobox", { name: "Loại phiếu" }), { target: { value: "adhoc" } });
  await waitFor(() => expect(calls).toBe(2));
  auth.kn.context = { ...auth.kn.context, read_only: true }; view.rerender(<KhtcPendingInboxScreen />);
  await screen.findByRole("link", { name: "Xem phiếu" });
  expect(screen.getByRole("combobox", { name: "Loại phiếu" })).toHaveValue("regular");
  expect(screen.queryByRole("option", { name: "Phiếu phát sinh" })).not.toBeInTheDocument();
  await act(async () => response.resolve(Response.json(page("Phiếu phát sinh phiên cũ"))));
  expect(screen.queryByText("Phiếu phát sinh phiên cũ")).not.toBeInTheDocument();
});

it("rejects response rows outside the requested filters and raw malformed filter identities", async () => {
  const client = api(async () => Response.json(page("Kỳ"))), signal = new AbortController().signal;
  await expect(readPending243(client, null, signal, { department: "chan_nuoi_thuy_san" })).rejects.toThrow();
  await expect(readPending243(client, null, signal, { period_id: cursor })).rejects.toThrow();
  const mixedId = "0199ab00-0000-7000-8000-000000000009";
  const mixed = api(async () => Response.json({ rows: [{ ...row("Kỳ chữ hoa"), period_id: mixedId }], next_cursor: null }));
  expect((await readPending243(mixed, null, signal, { period_id: mixedId.toUpperCase() })).rows).toHaveLength(1);
  await expect(readPending243(client, null, signal, { period_id: "1e100000" })).rejects.toThrow();
  await expect(readPending243(client, null, signal, { department: "a".repeat(100000) })).rejects.toThrow();
});
