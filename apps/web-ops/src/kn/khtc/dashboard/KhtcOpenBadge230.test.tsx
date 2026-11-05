import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import type { AuthContextValue } from "@/auth/AuthProvider";
import type { MenuTree } from "@/kn/contract";
import { KhtcOpenBadgeSidebar } from "./KhtcOpenBadgeSidebar";

type Auth = Pick<AuthContextValue, "status" | "principal" | "kn" | "scopeWarning" | "api">;
const menu: MenuTree = { version: "kn.menu/v14", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", groups: [{ key: "main", label: "Điều hành", items: [
  { key: "periods", label: "Kỳ báo cáo", icon: "task", href: "/kn/khtc/ky-bao-cao", badge_source: "extension.khtc_periods.open" },
  { key: "other", label: "Việc khác", icon: "task", href: "/kn/other", badge_source: "other.pending" },
] }] };
const snapshot = (): Omit<Auth, "api"> => ({ status: "authenticated", scopeWarning: null,
  principal: { subject: "actor-A", displayName: "Fixture A", tenantId: "tenant-A", orgRef: "org-A", roles: ["kn_officer"], mfa: true, level: "tinh", unitName: null, orgName: null, scopeUnits: ["X1"], scopeAll: false },
  kn: { status: "active", assignments: [{ assignment_id: "asg-A", role: "kn_tinh", org_ref: "org-A", org_name: "Fixture", mgmt_level: null, communes: [], departments: [{ code: "ke_hoach_tai_chinh", name: "KHTC", description: null, status: "active" }] }],
    context: { context_id: "ctx-A", assignment_id: "asg-A", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", department_name: "KHTC", org_ref: "org-A", org_name: "Fixture", communes: [], default_route: "/kn/khtc/trang-chu", expires_at: "2099-12-31T00:00:00Z" }, menu, badges: null } });
const auth: Auth = { ...snapshot(), api: createApiClient({ baseUrl: "http://badge.test", getAccessToken: () => null }) };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
type Pending = { request: Request; resolve: (response: Response) => void };
function client() {
  const pending: Pending[] = [];
  const api = createApiClient({ baseUrl: "http://badge.test", getAccessToken: () => "test", getContextId: () => auth.kn.context?.context_id ?? null,
    fetch: request => new Promise<Response>(resolve => pending.push({ request, resolve })) });
  return { api, pending };
}
let main: ReturnType<typeof client>;
const props = () => ({ menu, pathname: "/kn/khtc/trang-chu", badges: { counts: { "extension.khtc_periods.open": 999, "other.pending": 8 } } });
async function flush() { await act(async () => {}); }
async function queued(pending: Pending[], count: number) { await flush(); expect(pending).toHaveLength(count); }
async function reply(pending: Pending[], index: number, count: unknown, status = 200) {
  await flush(); expect(pending[index]).toBeDefined();
  await act(async () => { pending[index]!.resolve(Response.json(status === 200 ? { open_count: count } : { code: "forbidden", detail: "Denied" }, { status })); });
}
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2030-01-02T03:00:00Z"));
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  Object.assign(auth, snapshot()); main = client(); auth.api = main.api;
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });

describe("KHTC open badge230 authority and request lifetime", () => {
  it("reads a real no-store GET and preserves other sources; never uses an old cached KHTC number", async () => {
    render(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    expect(screen.queryByText("999")).not.toBeInTheDocument(); expect(screen.getByText("8")).toBeInTheDocument();
    await queued(main.pending, 1);
    const request = main.pending[0]!.request;
    expect(request.method).toBe("GET"); expect(new URL(request.url).pathname).toBe("/v1/extension/khtc/open-period-count");
    expect(request.headers.get("X-AG-Context")).toBe("ctx-A"); expect(request.cache).toBe("no-store");
    await reply(main.pending, 0, 118); await screen.findByText("118"); expect(screen.getByText("8")).toBeInTheDocument();
  });
  it("hides a confirmed zero", async () => { render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); await reply(main.pending, 0, 0); expect(screen.queryByText("0")).not.toBeInTheDocument(); expect(screen.queryByText("999")).not.toBeInTheDocument(); });
  it.each([null, -1, 1.2, Number.MAX_SAFE_INTEGER + 1, "1e100000", "1e-100000", "9".repeat(100000)])("hides malformed or hostile counter data %#", async value => {
    render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); await reply(main.pending, 0, value); expect(screen.queryByText("999")).not.toBeInTheDocument(); expect(screen.getByText("8")).toBeInTheDocument();
  });
  it("removes a previously loaded number after a fresh denial", async () => {
    render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); await reply(main.pending, 0, 118); await screen.findByText("118");
    fireEvent.focus(window); await queued(main.pending, 2); await reply(main.pending, 1, null, 403);
    await waitFor(() => expect(screen.queryByText("118")).not.toBeInTheDocument());
  });
  it("does not request for a missing/disabled advertised source or anonymous authority", async () => {
    const disabled = structuredClone(menu); disabled.groups[0]!.items[0]!.status = "not_open";
    const view = render(<KhtcOpenBadgeSidebar {...props()} menu={disabled} />); await flush(); expect(main.pending).toHaveLength(0);
    auth.status = "anonymous"; auth.principal = null; view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    expect(main.pending).toHaveLength(0); expect(screen.queryByText("999")).not.toBeInTheDocument();
  });
  const mutations: [string, () => void][] = [
    ["actor", () => { auth.principal = { ...auth.principal!, subject: "actor-B" }; }],
    ["tenant", () => { auth.principal = { ...auth.principal!, tenantId: "tenant-B" }; }],
    ["MFA", () => { auth.principal = { ...auth.principal!, mfa: false }; }],
    ["scope", () => { auth.principal = { ...auth.principal!, scopeUnits: ["X2"] }; }],
    ["selected context", () => { auth.kn = { ...auth.kn, context: { ...auth.kn.context!, context_id: "ctx-B" } }; }],
    ["role", () => { auth.kn = { ...auth.kn, context: { ...auth.kn.context!, role: "kn_tw" } }; }],
    ["readonly", () => { auth.kn = { ...auth.kn, context: { ...auth.kn.context!, read_only: true } }; }],
    ["assignment", () => { auth.kn = { ...auth.kn, assignments: [] }; }],
    ["support", () => { auth.kn = { ...auth.kn, support: { target: "delegate", reason: "Hỗ trợ kiểm tra" } }; }],
  ];
  it.each(mutations)("drops a late response after %s changes", async (_name, change) => {
    const view = render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); change(); view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    await queued(main.pending, 2); await reply(main.pending, 0, 118); expect(screen.queryByText("118")).not.toBeInTheDocument();
    await reply(main.pending, 1, 231); await screen.findByText("231");
  });
  it("does not resurrect cached A after a committed context A→B→A", async () => {
    const initial = snapshot(); const view = render(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    await reply(main.pending, 0, 118); await screen.findByText("118");
    auth.kn = { ...auth.kn, context: { ...auth.kn.context!, context_id: "ctx-B" } }; view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    expect(screen.queryByText("118")).not.toBeInTheDocument(); Object.assign(auth, initial); view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    expect(screen.queryByText("118")).not.toBeInTheDocument(); await queued(main.pending, 3);
    await reply(main.pending, 1, 231); expect(screen.queryByText("231")).not.toBeInTheDocument();
    await reply(main.pending, 2, 312); await screen.findByText("312");
  });
  it("drops pending old A after A→B→A even though the final serialized authority equals A", async () => {
    const initial = snapshot(); const view = render(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    auth.kn = { ...auth.kn, context: { ...auth.kn.context!, context_id: "ctx-B" } }; view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    Object.assign(auth, initial); view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    await reply(main.pending, 0, 118); await reply(main.pending, 1, 231); expect(screen.queryByText("118")).not.toBeInTheDocument(); expect(screen.queryByText("231")).not.toBeInTheDocument();
    await reply(main.pending, 2, 312); await screen.findByText("312");
  });
  it("gives a restored raw API client a fresh lifetime", async () => {
    const other = client(); const view = render(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    auth.api = other.api; view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    auth.api = main.api; view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush();
    await queued(main.pending, 2); await queued(other.pending, 1);
    await reply(main.pending, 0, 118); await reply(other.pending, 0, 231);
    expect(screen.queryByText("118")).not.toBeInTheDocument(); expect(screen.queryByText("231")).not.toBeInTheDocument();
    await reply(main.pending, 1, 312); await screen.findByText("312");
  });
  it("keeps only the latest overlapping refresh", async () => {
    render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); fireEvent.focus(window); fireEvent.focus(window);
    await queued(main.pending, 3); await reply(main.pending, 2, 312); await screen.findByText("312");
    await reply(main.pending, 0, 118); await reply(main.pending, 1, 231);
    expect(screen.queryByText("118")).not.toBeInTheDocument(); expect(screen.queryByText("231")).not.toBeInTheDocument(); expect(screen.getByText("312")).toBeInTheDocument();
  });
  it("starts a fresh request on route change and ignores old route responses", async () => {
    const view = render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); view.rerender(<KhtcOpenBadgeSidebar {...props()} pathname="/kn/khtc/ky-bao-cao" />); await flush();
    await reply(main.pending, 0, 118); expect(screen.queryByText("118")).not.toBeInTheDocument(); await reply(main.pending, 1, 231); await screen.findByText("231");
  });
  it("refreshes visible tabs and expires a context without waiting for another render", async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date("2030-01-02T03:00:00Z"));
    auth.kn.context = { ...auth.kn.context!, expires_at: "2030-01-02T03:01:10Z" };
    render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); await reply(main.pending, 0, 118);
    expect(screen.getByText("118")).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(30_000)); await queued(main.pending, 2); await reply(main.pending, 1, 231);
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    act(() => vi.advanceTimersByTime(30_000)); await queued(main.pending, 2);
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible"); fireEvent(document, new Event("visibilitychange")); await queued(main.pending, 3);
    act(() => vi.advanceTimersByTime(10_000)); expect(screen.queryByText("231")).not.toBeInTheDocument();
    await reply(main.pending, 2, 312); expect(screen.queryByText("312")).not.toBeInTheDocument();
    fireEvent.focus(window); await queued(main.pending, 3);
  });
  it("never starts with an expired context and ignores a response after unmount", async () => {
    auth.kn.context = { ...auth.kn.context!, expires_at: "2029-01-01T00:00:00Z" };
    const view = render(<KhtcOpenBadgeSidebar {...props()} />); await flush(); expect(main.pending).toHaveLength(0);
    Object.assign(auth, snapshot()); view.rerender(<KhtcOpenBadgeSidebar {...props()} />); await flush(); await queued(main.pending, 1);
    view.unmount(); await reply(main.pending, 0, 118); expect(screen.queryByText("118")).not.toBeInTheDocument();
  });
});
