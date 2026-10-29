import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { ApiProblem } from "@/api/problem";
import { createHrmApi, normDecimal, normGradeCode, vnDecimal } from "./api";
import { HrmCatalogScreen } from "./CatalogScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/danh-muc",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
}));

async function clientFor(
  personaId: string,
  assignmentId: string,
  department = "trong_trot",
) {
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
        department_code: department,
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
  return { api, ha: createHrmApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("danh mục nhân sự — tiện ích", () => {
  it("chuẩn hóa mã ngạch (AC-03), hệ số kiểu Việt Nam", () => {
    expect(normGradeCode(" V.03.04.12 \n")).toBe("V.03.04.12");
    expect(normDecimal(" 2,34 ")).toBe("2.34");
    expect(vnDecimal("4.98")).toBe("4,98");
  });
});

describe("danh mục nhân sự — backend giả lập theo hợp đồng /v1/hrm/*", () => {
  beforeEach(() => resetHrm());

  it("Admin thêm ngạch (mã chuẩn hóa), trùng → 409; Cấp Tỉnh phòng khác → 403", async () => {
    const { ha } = await clientFor("kn-admin", "asg-admin");
    const w = await ha.create("grade", {
      code: " V.03.04.12 ",
      name: "Kỹ thuật viên",
      attrs: { equiv_group: "CS", grade_type: "vien_chuc" },
    });
    expect(w).toEqual({ code: "V.03.04.12", version: 1 });
    expect(
      await problemCode(
        ha.create("grade", {
          code: "V.03.04.12",
          name: "X",
          attrs: { equiv_group: "CS", grade_type: "vien_chuc" },
        }),
      ),
    ).toBe("hrm.item_exists");
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await problemCode(tt.ha.list("grade"))).toBe("forbidden");
  });

  it("ngừng sử dụng là phiên bản mới; bảng hệ số mới có hiệu lực từ ngày (AC-02)", async () => {
    const { ha } = await clientFor("kn-admin", "asg-admin");
    await ha.revise("position", "vien_chuc", {
      name: "Viên chức",
      active: false,
    });
    expect(
      (await ha.list("position")).items.some((x) => x.code === "vien_chuc"),
    ).toBe(false);
    expect((await ha.history("position", "vien_chuc")).versions).toHaveLength(
      2,
    );
    await ha.addScale("01.003", {
      valid_from: "2026-07-01",
      hold_months: 36,
      coefficients: ["2.40", "2.75"],
    });
    const before = (await ha.scales("2026-06-30")).grades.find(
      (g) => g.grade.code === "01.003",
    );
    const after = (await ha.scales("2026-07-01")).grades.find(
      (g) => g.grade.code === "01.003",
    );
    expect(before?.scale?.coefficients[0]).toBe("2.34");
    expect(after?.scale?.coefficients).toEqual(["2.40", "2.75"]);
    expect(
      await problemCode(
        ha.addScale("01.003", {
          valid_from: "2026-07-01",
          hold_months: 36,
          coefficients: ["2.40", "2.30"],
        }),
      ),
    ).toBe("validation_failed");
    expect(
      await problemCode(
        ha.addRule("achievement_conversion", {
          valid_from: "2027-01-01",
          payload: { rules: [{ award_code: "khong_co", months: 9 }] },
        }),
      ),
    ).toBe("hrm.item_reference");
  });
});

describe("màn Danh mục nhân sự", () => {
  beforeEach(() => resetHrm());

  it("Admin: dữ liệu mẫu, bảng hệ số, STT và liên kết Thêm/Sửa/Lịch sử trên trang riêng", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    const user = userEvent.setup();
    render(<HrmCatalogScreen />);
    expect(await screen.findByText(/dữ liệu mẫu/)).toBeInTheDocument();
    expect(await screen.findByText("01.002")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Chuyên viên chính/ }));
    expect(
      await screen.findByText(/Bảng hệ số — Chuyên viên chính/),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^Chức vụ/ }));
    expect(await screen.findByRole("link", { name: "Thêm" })).toHaveAttribute("href", "/kn/nhan-su/danh-muc/position/them-moi");
    expect(screen.getByRole("link", { name: "Sửa vien_chuc" })).toHaveAttribute("href", "/kn/nhan-su/danh-muc/position/vien_chuc/sua");
    expect(screen.getByRole("link", { name: "vien_chuc" })).toHaveAttribute("href", "/kn/nhan-su/danh-muc/position/vien_chuc");
    expect(screen.getByRole("columnheader", { name: "STT" })).toBeVisible();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
