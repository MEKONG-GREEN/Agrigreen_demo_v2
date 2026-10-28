import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrmPromotion } from "@/api/mock/kn-hrm-promotion";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { ApiProblem } from "@/api/problem";
import { HrmDecisionListScreen } from "./DecisionScreens";
import { createPromotionApi } from "./promotion-api";
import { HrmPromotionScreen } from "./PromotionScreens";
import { createStaffApi } from "./staff-api";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/nang-bac",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useParams: () => ({}),
}));
const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
}));
vi.mock("@/lib/files", async (orig) => ({
  ...(await orig<object>()),
  saveBlob: vi.fn(),
}));

async function clientFor(personaId: string, assignmentId: string) {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, pa: createPromotionApi(api), sa: createStaffApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("nâng bậc lương FN-09, FN-10 (W11.25) — giả lập", () => {
  beforeEach(() => {
    resetHrmStaff();
    resetHrmPromotion();
  });

  it("danh sách → đưa vào (lý do) → xuất Mẫu số 1 → quyết định cập nhật bậc → hủy khôi phục", async () => {
    const { pa, sa } = await clientFor("kn-admin", "asg-admin");
    const l = await pa.list(3);
    const a = l.items.find((x) => x.code === "VC-00001")!;
    const b = l.items.find((x) => x.code === "VC-00002")!;
    expect(a.kind).toBe("nltx");
    expect(a.cond).toBe("ok");
    expect(a.to_step).toBe(a.from_step + 1);
    expect(b.cond).toBe("review"); // thiếu xếp loại → Cần xem xét
    expect(await problemCode(pa.include([b.key]))).toBe("validation_failed");
    await pa.include([b.key], "Đang bổ sung kết quả xếp loại");
    await pa.include([a.key]);
    const ex = await pa.exportLists({ keys: [a.key, b.key], review_month: "2026-11" });
    expect(ex.items.map((x) => x.form)).toEqual(["mau1"]);
    expect(ex.items[0]!.file_name).toBe("MauSo1_T11_2026.xlsx");
    expect((await pa.file(ex.items[0]!.id)).drift).toBe(false);
    expect((await pa.list(3)).items.filter((x) => x.status === "exported")).toHaveLength(2);

    const src = await pa.exportDetail(ex.items[0]!.id);
    expect(src.items).toHaveLength(2);
    const d = await pa.createDecision({
      export_id: src.id,
      decision_no: "540/QĐ-SNNMT",
      decided_on: "2026-10-10",
      issuer: "Sở NN&MT",
      people: [{ case_key: a.key }],
    });
    expect(d.returned).toBe(1);
    const st = await sa.get(a.staff_id, a.due);
    expect(st.current.step).toBe(a.to_step);
    const after = await pa.list(3);
    expect(after.items.find((x) => x.key === b.key)?.note).toMatch(/540\/QĐ-SNNMT/);
    expect(after.items.find((x) => x.key === a.key)?.status).toBe("decided");
    expect(await problemCode(pa.createDecision({ kind: "nltx", decision_no: "540/qđ-snnmt", decided_on: "2026-01-01", issuer: "Sở NN&MT",
      people: [{ staff_id: b.staff_id, to_step: 5, effective_on: "2026-01-01" }] }))).toBe("hrm.decision_warnings");
    expect((await pa.decisions({ q: "540" })).items).toHaveLength(1);

    expect(await problemCode(pa.cancelDecision(d.id, "sai"))).toBe("validation_failed");
    await pa.cancelDecision(d.id, "Nhập sai số quyết định");
    expect((await sa.get(a.staff_id, a.due)).current.step).toBe(a.from_step);
    expect((await pa.decision(d.id)).status).toBe("cancelled");
    expect(await problemCode(pa.cancelDecision(d.id, "Hủy lần thứ hai"))).toBe("hrm.decision_cancelled");
  });

  it("Cấp Tỉnh ngoài HCTH không có màn nâng bậc", async () => {
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(tt.pa.list(3))).toBe("forbidden");
  });

  it("màn danh sách nâng bậc và sổ quyết định hiển thị", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    const { unmount } = render(<HrmPromotionScreen />);
    expect(await screen.findByText("Tổng số đến kỳ")).toBeTruthy();
    expect(await screen.findByText("Nguyễn Thị Mẫu")).toBeTruthy();
    expect(screen.getByText("Lập danh sách trước hạn")).toBeTruthy();
    unmount();
    render(<HrmDecisionListScreen />);
    expect(await screen.findByText("Sổ quyết định nâng bậc")).toBeTruthy();
    expect(await screen.findByText("Chưa có quyết định phù hợp bộ lọc.")).toBeTruthy();
  });
});
