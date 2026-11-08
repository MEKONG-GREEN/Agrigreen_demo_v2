// #277 — FN-21 B3, FN-10 HCN-01…03: consumer thật, hợp đồng HTTP công việc và sổ hộ.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import type { FormsTask } from "./api";
import { CntsEntryScreen } from "./CntsEntryScreen";

const H1 = "0193a000-0000-7000-8000-0000000a0001";
const H2 = "0193a000-0000-7000-8000-0000000a0002";
const auth = { api: null as ApiClient | null, principal: { subject: "officer", tenantId: "tenant-a", scopeUnits: ["30001"] }, kn: { context: { context_id: "ctx-a", assignment_id: "asg-a", role: "kn_xa", communes: [{ code: "30001", name: "Xã thử" }] } } };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
const task = (unit = H1): FormsTask => ({
  id: "period:" + unit, profile: "kn_cnts", period_id: "period", unit, unit_kind: "household", parent: "30001",
  template_id: "template", template_version: 1, period_key: "DT-277", period_from: "2099-10-01", period_to: "2099-10-05", submission_id: null, held_at: null, template_code: "CNTS-DT", template_title: "Điều tra hộ", label: "Đợt thử", fields: [], period_kind: "adhoc",
  period_status: "open", due_at: "2099-10-20T00:00:00Z", open: true, late: false, status: "not_submitted",
  version: 0, submitted_at: null, values: {}, nil_report: false, note: "", explanation: "", return_note: null,
});
const household = (id = H1, owner_name: string | null = "Tên hộ được phép") => ({ id, code: id === H1 ? "HCN0000001" : "HCN0000002", commune_code: "30001", owner_name, owner_label: "Nhãn hộ được phép", owner_masked_phone: "0912•••123", status: "active" });
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
let tasks: FormsTask[];
let getHousehold: (req: Request) => Promise<Response>;
let requests: Request[];
let submissions: unknown[];

beforeEach(() => {
  localStorage.clear();
  tasks = [task()]; requests = []; submissions = [];
  auth.principal = { subject: "officer", tenantId: "tenant-a", scopeUnits: ["30001"] };
  auth.kn.context = { context_id: "ctx-a", assignment_id: "asg-a", role: "kn_xa", communes: [{ code: "30001", name: "Xã thử" }] };
  getHousehold = async () => json(household());
  auth.api = createApiClient({ baseUrl: "http://test", getAccessToken: () => "test-token", getContextId: () => auth.kn.context.context_id,
    fetch: async (request: Request) => {
      requests.push(request.clone());
      const path = new URL(request.url).pathname;
      if (path === "/v1/forms/tasks") return json({ items: tasks });
      if (path === "/v1/extension/cnts/period-forms") return json({ items: [] });
      if (path.startsWith("/v1/masterdata/catalogs/")) return json({ items: [{ name: "Cơ sở thử", attrs: { facility_ref: "facility" } }] });
      if (path.startsWith("/v1/extension/cnts/livestock-households/")) return getHousehold(request);
      if (path.startsWith("/v1/extension/cnts/period-drafts/")) return json({ draft: null });
      if (path === "/v1/forms/periods/period/submissions") { submissions.push(await request.json()); return json({}); }
      throw new Error("Unexpected request " + request.method + " " + path);
    },
  });
});

describe("#277 — nhãn hộ của công việc FN-21", () => {
  it("hiện HCN và tên được phép ở danh sách và biểu; gửi đúng unit, không lưu nhãn vào nháp", async () => {
    const u = userEvent.setup();
    render(<CntsEntryScreen />);
    const row = await screen.findByRole("row", { name: /HCN0000001.*Tên hộ được phép/ });
    expect(within(row).getByText(/xã Xã thử/)).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent(H1);
    await u.click(within(row).getByRole("button", { name: "Nhập" }));
    const entry = screen.getByRole("region", { name: "Nhập báo cáo" });
    expect(await within(entry).findByText(/HCN0000001.*Tên hộ được phép/)).toBeInTheDocument();
    await u.type(screen.getByLabelText("H08 Ghi chú chung"), "Ghi chú số liệu");
    expect(Object.values(localStorage).join("")).not.toContain("Tên hộ được phép");
    await u.click(within(entry).getByRole("button", { name: "Gửi báo cáo" }));
    await waitFor(() => expect(submissions).toHaveLength(1));
    expect(submissions[0]).toMatchObject({ unit: H1, values: {}, base_version: 0, note: "Ghi chú số liệu" });
    expect(JSON.stringify(submissions)).not.toContain("Tên hộ được phép");
    const reads = requests.filter((r) => new URL(r.url).pathname.includes("/livestock-households/"));
    expect(reads.length).toBeGreaterThan(0);
    expect(reads.every((r) => r.headers.get("X-AG-Context") === "ctx-a")).toBe(true);
    expect(requests.some((r) => new URL(r.url).pathname === "/v1/extension/cnts/livestock-households")).toBe(false);
  });

  it("dùng owner_label khi không có quyền tên đầy đủ; không hiện SĐT", async () => {
    getHousehold = async () => json(household(H1, null));
    render(<CntsEntryScreen />);
    expect(await screen.findByText("HCN0000001 · Nhãn hộ được phép")).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("0912");
    expect(document.body).not.toHaveTextContent("Tên hộ được phép");
  });

  it.each([[403, "Không có quyền xem hộ"], [404, "Không có hộ trong phạm vi hiện tại"], [503, "Chưa tải được thông tin hộ"]])("phản ánh HTTP %s, không đoán mã/tên từ ref", async (status, message) => {
    getHousehold = async () => json({ status, code: "test.error", title: "Lỗi" }, Number(status));
    render(<CntsEntryScreen />);
    expect(await screen.findByText(String(message))).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent(H1);
    expect(document.body).not.toHaveTextContent("HCN0000001");
  });

  it.each([{ id: H2 }, { commune_code: "30002" }])("không nhận nhãn của hộ/xã khác công việc: %j", async (patch) => {
    getHousehold = async () => json({ ...household(), ...patch });
    render(<CntsEntryScreen />);
    expect(await screen.findByText("Thông tin hộ không khớp công việc")).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("HCN0000001");
    expect(document.body).not.toHaveTextContent("Tên hộ được phép");
  });

  it("không bịa tên khi kho chỉ trả mã; giữ tên dài dưới dạng văn bản an toàn", async () => {
    getHousehold = async () => json({ ...household(H1, null), owner_label: null });
    const view = render(<CntsEntryScreen />);
    expect(await screen.findByText("HCN0000001 · Tên hộ chưa khả dụng")).toBeInTheDocument();
    view.unmount();
    const longName = '<img src=x onerror="alert(1)">' + "ộ".repeat(170);
    getHousehold = async () => json(household(H1, longName));
    render(<CntsEntryScreen />);
    expect(await screen.findByText("HCN0000001 · " + longName)).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });

  it.each(["context", "tenant"])("bỏ biểu/nhãn cũ và phản hồi muộn khi đổi %s với cùng ApiClient", async (change) => {
    let finish!: (r: Response) => void;
    let first = true;
    getHousehold = async () => {
      if (first) { first = false; return new Promise<Response>((resolve) => { finish = resolve; }); }
      return json(household(H2, "Tên hộ phiên mới"));
    };
    const view = render(<CntsEntryScreen />);
    await waitFor(() => expect(finish).toBeDefined());
    fireEvent.click(screen.getByRole("button", { name: "Nhập" }));
    expect(screen.getByRole("region", { name: "Nhập báo cáo" })).toBeInTheDocument();
    tasks = [task(H2)];
    if (change === "context") auth.kn.context = { ...auth.kn.context, context_id: "ctx-b", assignment_id: "asg-b" };
    else auth.principal = { ...auth.principal, tenantId: "tenant-b" };
    view.rerender(<CntsEntryScreen />);
    expect(screen.queryByRole("region", { name: "Nhập báo cáo" })).toBeNull();
    expect(await screen.findByText("HCN0000002 · Tên hộ phiên mới")).toBeInTheDocument();
    await act(async () => { finish(json(household(H1, "Tên cũ đến muộn"))); });
    expect(document.body).not.toHaveTextContent("Tên cũ đến muộn");
    expect(document.body).not.toHaveTextContent("HCN0000001");
  });

  it("đọc lại quyền khi mở và tải lại công việc, không giữ nhãn trước đó", async () => {
    render(<CntsEntryScreen />);
    await screen.findByText("HCN0000001 · Tên hộ được phép");
    getHousehold = async () => json({ status: 403, code: "forbidden", title: "Đã thu hồi" }, 403);
    // Gửi xong/tải lại công việc cũng phải đọc lại quyền của nhãn hiện tại.
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "Nhập" }));
    expect(await within(screen.getByRole("region", { name: "Nhập báo cáo" })).findByText("Không có quyền xem hộ")).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "Gửi báo cáo" }));
    await waitFor(() => expect(document.body).not.toHaveTextContent("Tên hộ được phép"));
  });

  it("giữ nhãn xã/cơ sở hiện có và không gọi sổ hộ cho các đơn vị đó", async () => {
    tasks = [{ ...task("30001"), unit_kind: "commune" }, { ...task("facility"), unit_kind: "facility" }];
    render(<CntsEntryScreen />);
    expect(await screen.findByText("Cơ sở thử")).toBeInTheDocument();
    expect(screen.getByText("Xã thử")).toBeInTheDocument();
    expect(requests.some((r) => new URL(r.url).pathname.includes("livestock-households"))).toBe(false);
  });
});
