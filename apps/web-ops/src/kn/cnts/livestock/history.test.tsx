// #276 Q9: UI/mock contract only; native vault authorization is covered by Go/PG tests.
import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { newIdempotencyKey } from "@/api/idempotency";
import { createKnAdminApi } from "@/kn/cycle/api";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsLivestock } from "@/api/mock/kn-cnts-livestock";
import { BreadcrumbTailProvider } from "@/shell/Breadcrumb";
import { createLivestockApi, type HouseholdSave } from "./api";
import { LivestockDetailScreen } from "./LivestockScreens";

const selected = { context: "", id: "" };
const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => selected.context, fetch: mockFetchNoDelay });
vi.mock("next/navigation", () => ({ useParams: () => ({ id: selected.id }), useRouter: () => ({ push: vi.fn(), replace: vi.fn() }), usePathname: () => "/kn/cnts/so-lieu-xa/ho-chan-nuoi" }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api, kn: { context: { role: "admin", communes: [] } } }) }));


async function contextOf(persona: string, assignment: string, department: string | null) {
  const r = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": newIdempotencyKey() }, body: JSON.stringify({ assignment_id: assignment, department_code: department }) }));
  expect(r.status).toBe(201);
  return ((await r.json()) as { context_id: string }).context_id;
}

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-08T05:00:00Z"));
  resetKnAdmin(); resetCntsLivestock();
  selected.context = await contextOf("kn-admin", "asg-admin", null);
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => selected.context, fetch: mockAdminFetchNoDelay });
  const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (cn.status !== "active") await adm.departmentStatus(cn.id, "active");
});
afterEach(() => vi.useRealTimers());

it("keeps exact old → new names after a later rename, masks phones, and renders names as text", async () => {
  const lapi = createLivestockApi(api);
  const oldName = "Hộ <img src=x onerror=alert(1)> An";
  const nextName = "Trại <script>unsafe276</script> Bình";
  const body: HouseholdSave = { owner_name: oldName, owner_phone: "0990000123", commune_code: "91301", hamlet: "Ấp kiểm thử", livestock_type: "nong_ho", animals: [{ species_code: "GA", head_count: 12 }], checkin: { lat: 10.3, lon: 105.4, accuracy_m: "9", mocked: false, captured_at: new Date().toISOString() } };
  const h = await lapi.create(body); selected.id = h.id;
  await lapi.update(h.id, { ...body, base_version: 1, owner_name: nextName, owner_phone: "0991000456" });
  await lapi.update(h.id, { ...body, base_version: 2, owner_name: "Hộ kiểm thử Cường", owner_phone: "0992000789" });
  const detail = await lapi.get(h.id);
  expect(detail.history.find((v) => v.version === 2)?.changes).toEqual([
    { field: "owner_name", old: oldName, new: nextName },
    { field: "owner_phone", old: "0990•••123", new: "0991•••456" },
  ]);
  const { container } = render(<BreadcrumbTailProvider><LivestockDetailScreen /></BreadcrumbTailProvider>);
  const history = await screen.findByRole("complementary", { name: "Lịch sử thay đổi" });
  expect(within(history).getByText(`Họ tên chủ hộ / tên trang trại: ${oldName} → ${nextName}`)).toBeInTheDocument();
  expect(within(history).getByText("Số điện thoại: 0990•••123 → 0991•••456")).toBeInTheDocument();
  expect(history).not.toHaveTextContent("0990000123"); expect(history).not.toHaveTextContent("0991000456"); expect(history).not.toHaveTextContent("0992000789");
  expect(container.querySelector("img")).toBeNull(); expect(container.querySelector("script")).toBeNull();
});

it("does not reveal history names through legacy, wrong-department, national or missing-cohort contexts", async () => {
  const lapi = createLivestockApi(api);
  const body: HouseholdSave = { owner_name: "Hộ kiểm thử An", owner_phone: "0990000123", commune_code: "91301", hamlet: "Ấp kiểm thử", livestock_type: "nong_ho", animals: [{ species_code: "GA", head_count: 12 }], checkin: { lat: 10.3, lon: 105.4, accuracy_m: "9", mocked: false, captured_at: new Date().toISOString() } };
  const h = await lapi.create(body);
  await lapi.update(h.id, { ...body, base_version: 1, owner_name: "Hộ kiểm thử Bình", owner_phone: "0991000456" });
  const contexts = [
    { persona: "kn-admin", id: null },
    { persona: "kn-an", id: await contextOf("kn-an", "asg-an-xa", "trong_trot") },
    { persona: "kn-quan", id: await contextOf("kn-quan", "asg-quan-tw", "trong_trot") },
    // The legacy mock provider omits province_group: never manufacture a cohort from its level.
    { persona: "kn-cnts", id: await contextOf("kn-cnts", "asg-cnts-tinh", "chan_nuoi_thuy_san") },
  ];
  for (const c of contexts) {
    const reader = createLivestockApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${c.persona}`, getContextId: () => c.id, fetch: mockFetchNoDelay }));
    const detail = await reader.get(h.id);
    expect(detail.history.find((v) => v.version === 2)?.changes).toEqual([
      { field: "owner_name", old: "(không đọc được lịch sử định danh)", new: "(không đọc được lịch sử định danh)" },
      { field: "owner_phone", old: "0990•••123", new: "0991•••456" },
    ]);
  }
  const outside = await contextOf("kn-loc", "asg-loc-xa", "chan_nuoi_thuy_san");
  const response = await mockFetchNoDelay(new Request(`http://mock/v1/extension/cnts/livestock-households/${h.id}`, { headers: { Authorization: "Bearer mock.kn-loc", "X-AG-Context": outside } }));
  expect(response.status).toBe(404);
  expect(await response.text()).not.toContain("Hộ kiểm thử");
});
