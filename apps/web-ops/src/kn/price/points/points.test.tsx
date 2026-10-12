import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { addDay, vnToday } from "@/api/mock/kn-price-items";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { ApiProblem } from "@/api/problem";
import { createPricePointApi, officerLabel, pointProblems } from "./api";
import { PointsScreen } from "./PointsScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/xa-bao-gia",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
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
      body: JSON.stringify({
        assignment_id: assignmentId,
        department_code: "trong_trot",
      }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, pa: createPricePointApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("xã báo giá — backend giả lập theo hợp đồng /v1/price/points*", () => {
  beforeEach(() => resetPricePoints());

  it("thêm xã (mã tự sinh, ĐVHC trùng → 409), phân công theo ngày, chỉ Admin ghi", async () => {
    const { pa } = await clientFor("kn-admin", "asg-admin");
    const tomorrow = addDay(vnToday(), 1);
    const w = await pa.createPoint({ admin_unit_code: "30286", region_code: "an_giang" });
    expect(w).toMatchObject({ code: "XBG0004", valid_from: tomorrow });
    expect(await problemCode(pa.createPoint({ admin_unit_code: "30280", region_code: "an_giang" }))).toBe(
      "price.admin_unit_taken",
    );
    expect(await problemCode(pa.createPoint({ admin_unit_code: "30286", region_code: "x", valid_from: "2020-01-01" }))).toBe(
      "price.valid_from",
    );
    const { officers } = await pa.officers("XA");
    await pa.assignOfficer("XBG0004", { role: "xa", officer_ref: officers[2]!.ref, valid_from: addDay(vnToday(), 2) });
    const d = await pa.point("XBG0004", addDay(vnToday(), 2));
    expect(d.point.officer_name).toBe("Lê Văn Cường");
    expect(d.officers).toHaveLength(1);
    expect((await pa.point("XBG0004", tomorrow)).point.officer_ref).toBeNull();

    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(tinh.pa.createPoint({ admin_unit_code: "31126", region_code: "an_giang" }))).toBe("forbidden");
    expect((await tinh.pa.status()).can_manage).toBe(false);
  });

  it("kiểm biểu mẫu và nhãn cán bộ", () => {
    expect(pointProblems({}, "create")).toEqual(["Chọn ĐVHC (xã)", "Chọn nhóm vùng"]);
    expect(pointProblems({ reason: " " }, "retire")).toEqual(["Nhập lý do ngưng"]);
    expect(officerLabel(null, null, "Chưa có cán bộ Xã")).toBe("Chưa có cán bộ Xã");
    expect(officerLabel("0190c0de-0000-7000-8000-000000000001", null, "")).toBe("0190c0de…");
  });

  it("màn Admin: danh sách xã, thêm xã qua hộp thoại", async () => {
    const { api } = await clientFor("kn-admin", "asg-admin");
    auth.api = api;
    render(<PointsScreen />);
    const table = await screen.findByRole("table", { name: "Danh mục xã báo giá" });
    expect(within(table).getByText("XBG0001")).toBeInTheDocument();
    expect(within(table).getByText("Nguyễn Văn An")).toBeInTheDocument();
    expect(within(table).getAllByText("Chưa phân công").length).toBeGreaterThan(0);
    await userEvent.click(await screen.findByRole("button", { name: "Thêm xã báo giá" }));
    const dlg = screen.getByRole("dialog", { name: "Thêm xã báo giá" });
    await waitFor(() => expect(within(dlg).getByLabelText("ĐVHC (xã)")).toBeInTheDocument());
    await userEvent.selectOptions(within(dlg).getByLabelText("ĐVHC (xã)"), "31126");
    await userEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});
