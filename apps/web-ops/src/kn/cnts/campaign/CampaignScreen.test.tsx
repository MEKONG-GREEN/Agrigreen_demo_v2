// #236 CNTS-CAMPAIGN-SCOPE (BRD CNTS v4.0 FN-21): màn "Tạo chiến dịch" (người phụ trách bắt buộc, lưu Nháp rồi mở đợt ở màn
// quản lý) và "Sao chép đợt" (đợt Nháp mới, không mang tiến độ/số liệu của đợt nguồn) trên backend giả lập.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsCampaigns } from "@/api/mock/kn-cnts-campaign";
import { createCampaignApi, emptyForm, payload, type FormsTemplate } from "./api";
import type * as CampaignApiModule from "./api";
import { CampaignCreateScreen, CampaignScreen } from "./CampaignScreen";

const nav = vi.hoisted(() => ({ push: vi.fn(), search: "" }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/chien-dich",
  useRouter: () => ({ push: nav.push, replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(nav.search),
}));

const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => null, fetch: mockFetchNoDelay });
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api, kn: { context: { role: "kn_tinh", read_only: false, department_code: "chan_nuoi_thuy_san", communes: [] } } }),
}));

// Giả lập web không có /v1/forms/templates của cap/forms: trả một biểu điều tra kn_cnts cho ô "Biểu điều tra".
vi.mock("./api", async (importOriginal) => {
  const m = await importOriginal<typeof CampaignApiModule>();
  return {
    ...m,
    createCampaignApi: (a: Parameters<typeof m.createCampaignApi>[0]) => ({
      ...m.createCampaignApi(a),
      templates: async () => [{ code: "CNTS-DT-TONGDAN", title: "Điều tra tổng đàn", version: 1 }] as unknown as FormsTemplate[],
    }),
  };
});

beforeEach(() => {
  resetCntsCampaigns();
  nav.push.mockReset();
  nav.search = "";
});

describe("Tạo chiến dịch (FN-21 B1)", () => {
  it("bắt buộc người phụ trách; lưu Nháp rồi chuyển sang đợt ở màn quản lý", async () => {
    const u = userEvent.setup();
    render(<CampaignCreateScreen />);
    await u.type(screen.getByLabelText("Tên đợt"), "Điều tra tổng đàn 01/10/2026");
    const tpl = screen.getByLabelText("Biểu điều tra") as HTMLSelectElement;
    await waitFor(() => expect(tpl.options.length).toBeGreaterThan(1));
    await u.selectOptions(tpl, tpl.options[1]!.value);
    await u.type(screen.getByLabelText("Mã xã"), "91307, 91310");
    await u.type(screen.getByLabelText("Từ ngày"), "2026-10-01");
    await u.type(screen.getByLabelText("Đến ngày"), "2026-10-10");
    await u.type(screen.getByLabelText("Hạn nộp"), "2026-10-10T17:00");
    await u.click(screen.getByRole("button", { name: "Lưu nháp" }));
    expect(await screen.findByText("Chọn người phụ trách")).toBeInTheDocument();
    expect(nav.push).not.toHaveBeenCalled();

    const owner = screen.getByLabelText("Người phụ trách") as HTMLSelectElement;
    await waitFor(() => expect(owner.options.length).toBe(4));
    await u.selectOptions(owner, owner.options[2]!.value);
    await u.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledTimes(1));
    const [created] = await createCampaignApi(api).list();
    expect(created).toMatchObject({ state: "draft", owner: { ref: owner.options[2]!.value, display: "Trần Thị Hoa (giả lập)" } });
    expect(nav.push).toHaveBeenCalledWith(`/kn/cnts/chien-dich?dot=${created!.id}`);
  });
});

describe("Sao chép đợt cũ (FN-21 luồng phụ)", () => {
  it("đợt Nháp mới giữ biểu, phạm vi, người phụ trách; không mang tiến độ của đợt nguồn", async () => {
    const capi = createCampaignApi(api);
    const [me] = await capi.owners();
    const src = await capi.create(payload({ ...emptyForm(), name: "Điều tra tổng đàn 01/10/2026", template_code: "CNTS-DT-TONGDAN",
      communes: "91307 91310 91313", from_date: "2026-10-01", to_date: "2026-10-10", due_at: "2026-10-10T17:00", owner_ref: me!.ref }));
    await capi.publish(src.id);
    await capi.close(src.id);
    nav.search = `dot=${src.id}`;

    const u = userEvent.setup();
    render(<CampaignScreen />);
    const detail = await screen.findByRole("region", { name: "Chi tiết đợt điều tra" });
    expect(await within(detail).findByText(/Tiến độ:/)).toBeInTheDocument();
    await u.click(within(detail).getByRole("button", { name: "Sao chép đợt" }));
    const panel = within(detail).getByLabelText("Sao chép đợt");
    await u.click(within(panel).getByRole("button", { name: "Tạo đợt sao chép" }));
    expect(within(panel).getAllByText("Bắt buộc").length).toBe(3);
    await u.type(within(panel).getByLabelText("Tên đợt mới"), "Điều tra tổng đàn 01/04/2027");
    await u.type(within(panel).getByLabelText("Từ ngày mới"), "2027-04-01");
    await u.type(within(panel).getByLabelText("Đến ngày mới"), "2027-04-10");
    await u.type(within(panel).getByLabelText("Hạn nộp mới"), "2027-04-10T17:00");
    await u.click(within(panel).getByRole("button", { name: "Tạo đợt sao chép" }));

    await waitFor(async () => expect(await capi.list()).toHaveLength(2));
    const copy = (await capi.list()).find((c) => c.id !== src.id)!;
    expect(copy).toMatchObject({ state: "draft", version: 1, name: "Điều tra tổng đàn 01/04/2027", template_code: "CNTS-DT-TONGDAN",
      communes: ["91307", "91310", "91313"], owner: { ref: me!.ref }, progress: null, events: [], forms_period_id: null });
    const opened = await screen.findByRole("region", { name: "Chi tiết đợt điều tra" });
    expect(await within(opened).findByText("Điều tra tổng đàn 01/04/2027", { exact: false })).toBeInTheDocument();
    expect(within(opened).queryByText(/Tiến độ:/)).toBeNull();
    expect(within(opened).getByRole("button", { name: "Phát hành" })).toBeInTheDocument();
  });
});
