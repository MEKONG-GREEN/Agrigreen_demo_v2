import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, unwrap, type ApiClient } from "@/api/client";
import { uuidv7 } from "@/api/idempotency";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { personaForContext } from "@/api/mock/kn";
import { knSupportAdminMock, resetKNSupport } from "@/api/mock/kn-support";
import { findPersona, TENANT } from "@/api/mock/personas";
import ContactAdminForm from "@/auth/AccountSupportForm";
import SupportQueueScreen from "./SupportQueueScreen";

const auth: { api: ApiClient | null; principal: { subject: string; tenantId: string; roles: string[]; mfa: boolean } | null; kn: { context: { context_id: string; role: string } | null } } = { api: null, principal: null, kn: { context: null } };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const input = { name: "Fixture hỗ trợ", contact: "274@example.test", message: "Tài khoản chưa có vai trò áp dụng <script>alert(274)</script>" };
function publicClient(fetch = mockFetchNoDelay) { return createApiClient({ baseUrl: "http://mock", getAccessToken: () => null, fetch }); }
async function adminClient(fetch = mockFetchNoDelay) {
  const res = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": uuidv7() }, body: JSON.stringify({ assignment_id: "asg-admin" }) }));
  const context = await res.json() as { context_id: string };
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context.context_id, fetch });
  return { api, context: { context_id: context.context_id, role: "admin" } };
}
async function submit(api: ApiClient) { await unwrap(api.POST("/v1/public/contacts/support-requests", { params: { header: { "Idempotency-Key": uuidv7() } }, body: input })); }
async function fill(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText(/Họ tên/), input.name);
  await user.type(screen.getByLabelText(/Email hoặc số điện thoại liên hệ/), input.contact);
  await user.type(screen.getByLabelText(/Nội dung cần hỗ trợ/), input.message);
}

describe("Q10/FN-CH-00.01 support 274", () => {
  beforeEach(() => { resetKNSupport(); auth.api = publicClient(); auth.principal = null; auth.kn = { context: null }; });

  it("anonymous validation, lost ACK retry uses one key and commits one KN request", async () => {
    const keys: string[] = []; let loseACK = true;
    auth.api = publicClient(async (request) => {
      keys.push(request.headers.get("Idempotency-Key") ?? "");
      const response = await mockFetchNoDelay(request);
      if (loseACK) { loseACK = false; throw new TypeError("synthetic lost ACK"); }
      return response;
    });
    const user = userEvent.setup(); render(<ContactAdminForm />);
    await user.click(screen.getByRole("button", { name: "Gửi yêu cầu" }));
    expect(keys).toHaveLength(0); expect(screen.getAllByRole("alert").length).toBeGreaterThan(0);
    await fill(user); await user.click(screen.getByRole("button", { name: "Gửi yêu cầu" }));
    await screen.findByText(/Chưa xác nhận được việc tiếp nhận/);
    expect(screen.getByLabelText(/Họ tên/)).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Thử lại" }));
    await screen.findByText(/Yêu cầu đã được tiếp nhận/);
    expect(keys).toHaveLength(2); expect(keys[0]).toBe(keys[1]); expect(screen.queryByDisplayValue(input.contact)).not.toBeInTheDocument();
    const admin = await adminClient(); const list = await unwrap(admin.api.GET("/v1/contacts/support-requests")); expect(list.items).toHaveLength(1);
    expect(JSON.stringify(list)).not.toContain(input.contact);
  });

  it("Admin opens escaped text, handles lost resolve ACK with same key and refreshes the queue", async () => {
    await submit(auth.api!); const keys: string[] = []; let loseACK = true;
    const admin = await adminClient(async (request) => {
      const isResolve = request.method === "PUT"; if (isResolve) keys.push(request.headers.get("Idempotency-Key") ?? "");
      const response = await mockFetchNoDelay(request);
      if (isResolve && loseACK) { loseACK = false; throw new TypeError("synthetic lost resolve ACK"); }
      return response;
    });
    auth.api = admin.api; auth.principal = { subject: "admin", tenantId: TENANT, roles: ["platform_admin"], mfa: true }; auth.kn.context = admin.context;
    const user = userEvent.setup(); const rendered = render(<SupportQueueScreen />);
    await user.click(await screen.findByRole("button", { name: "Xem yêu cầu" }));
    await screen.findByText(input.message); expect(rendered.container.querySelector("script")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Đánh dấu đã xử lý" }));
    await screen.findByText(/Chưa xác nhận được việc xử lý/);
    await user.click(screen.getByRole("button", { name: "Đánh dấu đã xử lý" }));
    await screen.findByText("Chưa có yêu cầu ở trạng thái này.");
    expect(keys).toHaveLength(2); expect(keys[0]).toBe(keys[1]); expect(screen.queryByText(input.contact)).not.toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText("Trạng thái"), "resolved");
    expect(await screen.findByRole("button", { name: "Xem yêu cầu" })).toBeEnabled();
  });

  it("drops late decrypted content when actor/context authority changes", async () => {
    await submit(auth.api!); let release: ((r: Response) => void) | undefined;
    const admin = await adminClient(async (request) => {
      const response = await mockFetchNoDelay(request);
      if (/\/contacts\/support-requests\/[^/]+$/u.test(new URL(request.url).pathname) && request.method === "GET") return new Promise<Response>((resolve) => { release = resolve; void response; });
      return response;
    });
    auth.api = admin.api; auth.principal = { subject: "admin", tenantId: TENANT, roles: ["platform_admin"], mfa: true }; auth.kn.context = admin.context;
    const user = userEvent.setup(); const rendered = render(<SupportQueueScreen />);
    await user.click(await screen.findByRole("button", { name: "Xem yêu cầu" }));
    await waitFor(() => expect(release).toBeTypeOf("function"));
    auth.principal = { subject: "another", tenantId: TENANT, roles: ["kn_user"], mfa: false }; auth.kn.context = null;
    rendered.rerender(<SupportQueueScreen />);
    release!(new Response(JSON.stringify({ ...input, id: uuidv7(), state: "new", version: 1, created_at: new Date().toISOString() }), { status: 200, headers: { "Content-Type": "application/json" } }));
    await screen.findByText(/Chỉ Admin Khuyến nông/); expect(screen.queryByText(input.contact)).not.toBeInTheDocument();
  });

  it("mock direct API checks tenant, MFA and role before replay, no public read API", async () => {
    await submit(auth.api!); const admin = await adminClient(); const list = await unwrap(admin.api.GET("/v1/contacts/support-requests")); const row = list.items[0]!;
    const persona = personaForContext(findPersona("kn-admin")!, admin.context.context_id);
    const headers = { "X-AG-Context": admin.context.context_id, "Content-Type": "application/json", "Idempotency-Key": uuidv7() };
    const path = `http://mock/v1/contacts/support-requests/${row.id}/resolution`;
    const request = () => new Request(path, { method: "PUT", headers, body: JSON.stringify({ expected_version: row.version }) });
    expect((await knSupportAdminMock(request(), new URL(path), persona))?.status).toBe(200);
    expect((await knSupportAdminMock(request(), new URL(path), { ...persona, amr: ["pwd"] }))?.status).toBe(403);
    expect((await knSupportAdminMock(request(), new URL(path), { ...persona, me: { ...persona.me, roles: ["kn_user"] } }))?.status).toBe(403);
    expect((await knSupportAdminMock(request(), new URL(path), { ...persona, me: { ...persona.me, tenant_id: uuidv7() } }))?.status).toBe(404);
    const publicRead = await mockFetchNoDelay(new Request("http://mock/v1/contacts/support-requests")); expect(publicRead.status).toBe(401);
  });
});
