// W11.53 (BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01): mẫu biểu dự án KNQG — hàm thuần (lỗi ô, nội dung gửi lên bỏ ô tự
// điền) và màn soạn biểu (lập Mẫu 01, lưu, trình duyệt). Luồng ký có tệp: knqg-api.test.ts (môi trường node).
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsKnqg } from "@/api/mock/kn-cnts-knqg";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import { createCntsModelApi } from "@/kn/cnts/model/api";
import { createKnAdminApi } from "@/kn/cycle/api";
import { deepLinkHref } from "@/lib/deepLink";
import { KnqgScreen } from "./KnqgScreen";
import { contentErrors, contentForSave, createErrors, createKnqgApi, emptyRow, periodLabel, type Spec } from "./api";

// W11.53b: tham số URL đổi được giữa các lần vẽ (bấm thông báo khi đang ở chính màn này).
const nav = vi.hoisted(() => ({ search: "", replace: [] as string[] }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/du-an-mau-bieu",
  useRouter: () => ({ push: vi.fn(), replace: (u: string) => nav.replace.push(u) }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(nav.search),
}));

const auth: { api: ApiClient | null; role: string } = { api: null, role: "admin" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role, communes: [], department_code: "chan_nuoi_thuy_san" } } }),
}));

async function contextOf(personaId: string, assignmentId: string, dept: string | null) {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: dept }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}

const client = (persona: string, ctx: string) =>
  createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => ctx, fetch: (req: Request) => mockFetchNoDelay(req) });

let adminApi: ApiClient;
beforeEach(async () => {
  resetKnAdmin();
  resetCntsModel();
  resetCntsKnqg();
  const adminCtx = await contextOf("kn-admin", "asg-admin", null);
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
  const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (cn.status !== "active") await adm.departmentStatus(cn.id, "active");
  adminApi = client("kn-admin", adminCtx);

  auth.api = adminApi;
  auth.role = "admin";
  nav.search = "";
  nav.replace = [];
});

describe("hàm thuần mẫu biểu KNQG", () => {
  const spec = {
    form: "01", code: "Mẫu 01/TĐ-DA", bm: "BM-12", title: "Tiến độ", lifecycle: "approve", per_site: false, period: "month", unique: true,
    fields: [{ key: "ten_du_an", label: "Tên dự án", kind: "text", auto: true }, { key: "danh_gia", label: "Đánh giá", kind: "long", required: true }],
    tables: [{ key: "vat_tu", label: "Vật tư", max_rows: 2, columns: [{ key: "ten", label: "Tên", kind: "text" }, { key: "da_cap", label: "Đã cấp", kind: "number" },
      { key: "nk", label: "Tự điền", kind: "text", auto: true }] }],
    sections: [{ title: "A", fields: ["ten_du_an", "danh_gia"], tables: ["vat_tu"] }],
  } as Spec;

  it("lỗi ô số, quá số dòng; nội dung gửi lên bỏ ô/cột tự điền, ô rỗng", () => {
    expect(contentErrors(spec, { fields: {}, tables: { vat_tu: [{ da_cap: "-1" }, {}, {}] } })).toEqual({
      "content.tables.vat_tu": "Tối đa 2 dòng.",
      "content.tables.vat_tu.0.da_cap": "Số không âm, dấu chấm thập phân.",
    });
    expect(contentForSave(spec, { fields: { ten_du_an: "giả", danh_gia: " Tốt " }, tables: { vat_tu: [{ ten: "Gà", nk: "x", da_cap: "" }, { ten: " " }] } })).toEqual({
      fields: { danh_gia: "Tốt" },
      tables: { vat_tu: [{ ten: "Gà" }] },
    });
    expect(emptyRow(spec.tables[0]!)).toEqual({ ten: "", da_cap: "" });
    expect(createErrors(spec, { project_ref: "p", period: "2027-13" })).toEqual({ period: "Chọn tháng." });
    expect(periodLabel("2027-10")).toBe("Tháng 10/2027");
    expect(periodLabel("2027")).toBe("Năm 2027");
  });
});

describe("màn Mẫu biểu dự án KNQG", () => {
  it("lập Mẫu 01 từ dự án, soạn, lưu, trình duyệt", async () => {
    const user = userEvent.setup();
    render(<KnqgScreen />);
    await screen.findByText("Chưa có biểu nào.");
    await user.click(screen.getByRole("button", { name: "Lập biểu" }));
    const form = await screen.findByRole("region", { name: "Lập biểu" });
    await waitFor(() => expect(within(form).getAllByRole("option").length).toBeGreaterThan(5));
    const selects = within(form).getAllByRole("combobox");
    await user.selectOptions(selects[1]!, within(selects[1]!).getAllByRole("option")[1]!);
    await user.click(within(form).getByRole("button", { name: "Lập biểu" }));
    expect(await within(form).findByText("Chọn tháng.")).toBeTruthy();
    const month = form.querySelector('input[type="month"]') as HTMLInputElement;
    await user.type(month, "2027-10");
    await user.click(within(form).getByRole("button", { name: "Lập biểu" }));
    const editor = await screen.findByRole("region", { name: /Mẫu 01\/TĐ-DA/ });
    expect(within(editor).getAllByText("Đang lập").length).toBeGreaterThan(0);
    expect((within(editor).getByLabelText(/Tên dự án/) as HTMLInputElement).readOnly).toBe(true);
    await user.type(within(editor).getByLabelText(/5\. Đánh giá chung/), "Đúng tiến độ");
    await user.click(within(editor).getByRole("button", { name: "Lưu" }));
    expect(await within(await screen.findByRole("region", { name: /Mẫu 01\/TĐ-DA/ })).findByText("Đã lưu.")).toBeTruthy();
    const ed2 = screen.getByRole("region", { name: /Mẫu 01\/TĐ-DA/ });
    await user.click(within(ed2).getByRole("button", { name: "Trình duyệt" }));
    await waitFor(() => expect(within(screen.getByRole("region", { name: /Mẫu 01\/TĐ-DA/ })).getAllByText("Chờ duyệt").length).toBeGreaterThan(0));
  });
});

describe("W11.53b: thông báo → bộ định tuyến → mở đúng biểu", () => {
  it("deep_link của thông báo mở thẳng biểu, kể cả khi đang ở chính màn này; đóng biểu thì bỏ ?doc=", async () => {
    const k = createKnqgApi(adminApi);
    const project = (await createCntsModelApi(adminApi).projects()).items.find((p) => p.kind === "knqg")!;
    const d7 = await k.create({ form: "07", project_ref: project.ref, period: "2027" });
    const d1 = await k.create({ form: "01", project_ref: project.ref, period: "2027-10" });
    // Chuông/hộp thư dùng deepLinkHref để ra URL đích; màn đọc ?doc= từ URL đó.
    const href7 = deepLinkHref(`/kn/cnts/du-an-mau-bieu?doc=${d7.ref}`, "cnts_knqg_sent")!;
    expect(href7).toBe(`/kn/cnts/du-an-mau-bieu?doc=${d7.ref}`);
    nav.search = href7.split("?")[1]!;
    const { rerender } = render(<KnqgScreen />);
    expect(await screen.findByRole("region", { name: /Mẫu 07\/NTKQ-DA/ })).toBeTruthy();
    // Đang ở màn này, bấm thông báo của biểu khác → URL đổi → mở biểu mới.
    const href1 = deepLinkHref(`/kn/cnts/du-an-mau-bieu?doc=${d1.ref}`, "cnts_knqg_submitted")!;
    nav.search = href1.split("?")[1]!;
    rerender(<KnqgScreen />);
    expect(await screen.findByRole("region", { name: /Mẫu 01\/TĐ-DA/ })).toBeTruthy();
    expect(screen.queryByRole("region", { name: /Mẫu 07\/NTKQ-DA/ })).toBeNull();
    // Đóng biểu đang mở theo liên kết → bỏ ?doc= để bấm lại cùng thông báo vẫn mở được.
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Mẫu 01 — Tiến độ tháng", expanded: true }));
    expect(nav.replace).toEqual(["/kn/cnts/du-an-mau-bieu"]);
    expect(screen.queryByRole("region", { name: /Mẫu 01\/TĐ-DA/ })).toBeNull();
  });
});