import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { ApiProblem } from "@/api/problem";
import { createStaffApi, vnDate } from "./staff-api";
import {
  HrmPermissionScreen,
  HrmStaffDetailScreen,
  HrmStaffListScreen,
} from "./StaffScreens";

const params: { id: string } = { id: "" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/ho-so",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useParams: () => params,
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
  return { api, sa: createStaffApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("hồ sơ nhân sự — tiện ích", () => {
  it("ngày theo mức chính xác (BR-05)", () => {
    expect(vnDate("1988")).toBe("1988");
    expect(vnDate("2010-05")).toBe("05/2010");
    expect(vnDate("2024-01-02")).toBe("02/01/2024");
    expect(vnDate(null)).toBe("—");
  });
});

describe("hồ sơ nhân sự — backend giả lập theo hợp đồng /v1/hrm/staff*", () => {
  beforeEach(() => {
    resetHrm();
    resetHrmStaff();
  });

  it("Admin: danh sách as-of, bậc = phép chiếu + hệ số tra bảng (AC-04), tạo hồ sơ mã tự sinh, thẻ TK, nhật ký xem/xuất", async () => {
    const { sa } = await clientFor("kn-admin", "asg-admin");
    expect((await sa.access()).group).toBe("admin");
    const l = await sa.list({});
    expect(l.total).toBe(3);
    const vc1 = l.items.find((x) => x.code === "VC-00001")!;
    expect(vc1.label).toBe("Nguyễn T. M.");
    expect(vc1.current).toMatchObject({
      grade: "01.003",
      step: 6,
      steps: 9,
      coefficient: "3.99",
      status: "active",
    });
    expect(
      (await sa.list({ at: "2023-06-01" })).items.find(
        (x) => x.code === "VC-00001",
      )!.current.coefficient,
    ).toBe("3.66");
    expect((await sa.list({ unit: "trong_trot" })).total).toBe(2); // gồm cán bộ xã do phòng quản lý (HS-37)

    const created = await sa.create({
      type: "hop_dong",
      profile: {
        full_name: "Phạm Văn Mới",
        gender: "nam",
        birth_date: "1990",
        phone: "0901000099",
      },
      events: [
        { kind: "work_start", valid_from: "2026-01-05", payload: {} },
        {
          kind: "unit",
          valid_from: "2026-01-05",
          payload: { unit: "chan_nuoi" },
        },
        {
          kind: "position",
          valid_from: "2026-01-05",
          payload: { position: "vien_chuc" },
        },
      ],
    });
    expect(created.code).toBe("HD-00001");
    expect(
      await problemCode(
        sa.create({
          type: "vien_chuc",
          profile: {
            full_name: "Trùng Số",
            gender: "nam",
            birth_date: "1990",
            phone: "0901000099",
          },
          events: [
            {
              kind: "unit",
              valid_from: "2026-01-05",
              payload: { unit: "chan_nuoi" },
            },
            {
              kind: "position",
              valid_from: "2026-01-05",
              payload: { position: "vien_chuc" },
            },
          ],
        }),
      ),
    ).toBe("hrm.duplicate_person");
    expect(
      await problemCode(
        sa.addEvent(vc1.id, {
          kind: "step",
          valid_from: "2025-01-01",
          payload: { step: "10" },
        }),
      ),
    ).toBe("hrm.step_out_of_range");
    expect(
      await problemCode(
        sa.addEvent(vc1.id, {
          kind: "work_end",
          valid_from: "2026-08-15",
          payload: {},
        }),
      ),
    ).toBe("validation_failed");
    await sa.addEvent(vc1.id, {
      kind: "work_end",
      valid_from: "2026-08-15",
      payload: { reason: "nghi_huu" },
    });
    expect((await sa.get(vc1.id, "2026-08-14")).current.status).toBe("active");
    expect((await sa.get(vc1.id, "2026-08-15")).current.status).toBe("left");

    const sens = await sa.sensitive(vc1.id);
    expect(sens.fields.hometown).toBe("Xã Mỹ Hòa Hưng");
    const xa = l.items.find((x) => x.code === "XA-00001")!;
    expect(await problemCode(sa.sensitive(xa.id))).toBe("hrm.not_applicable");
    const ex = await sa.exportCsv({});
    expect(await ex.blob.text()).toContain("VC-00001");
    const log = (await sa.accessLog(vc1.id)).items.map((x) => x.action);
    expect(log).toEqual(
      expect.arrayContaining(["view", "view_sensitive", "export"]),
    );
  });

  it("Cấp Tỉnh ngoài HCTH không xem hồ sơ (BR-04); Admin cấp 'bản thân' theo nhóm; Admin cố định", async () => {
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect((await tt.sa.access()).perms["profile.view"]).toBe("none");
    expect(await problemCode(tt.sa.list({}))).toBe("forbidden");
    const { sa } = await clientFor("kn-admin", "asg-admin");
    expect(
      await problemCode(
        sa.setPermission({
          grantee_kind: "role",
          grantee: "admin",
          perms: { "profile.view": "none" },
        }),
      ),
    ).toBe("hrm.permission_locked");
    expect(
      await problemCode(
        sa.setPermission({
          grantee_kind: "role",
          grantee: "manager",
          perms: { "sensitive.view": "all" },
        }),
      ),
    ).toBe("validation_failed");
    await sa.setPermission({
      grantee_kind: "role",
      grantee: "tinh",
      perms: { "profile.view": "self" },
    });
    expect((await tt.sa.access()).perms["profile.view"]).toBe("self");
    expect((await tt.sa.list({})).total).toBe(0); // chưa gắn tài khoản
    expect(await problemCode(tt.sa.permissions())).toBe("forbidden");
    expect((await sa.permissionHistory()).items).toHaveLength(1);
  });
});

describe("màn Hồ sơ nhân sự", () => {
  beforeEach(() => {
    resetHrm();
    resetHrmStaff();
  });

  it("danh sách → chi tiết theo thẻ (Ngạch - Bậc: hệ số 3,99); thẻ TK chỉ hiện khi bấm xem", async () => {
    const c = await clientFor("kn-admin", "asg-admin");
    auth.api = c.api;
    const user = userEvent.setup();
    const list = render(<HrmStaffListScreen />);
    expect(await screen.findByText("VC-00001")).toBeInTheDocument();
    expect(screen.getByText("Nguyễn T. M.")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Thêm hồ sơ" }),
    ).toBeInTheDocument();
    list.unmount();

    params.id = (await c.sa.list({ q: "VC-00001" })).items[0]!.id;
    render(<HrmStaffDetailScreen />);
    expect(
      await screen.findByText(/Nguyễn Thị Mẫu · VC-00001/),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Ngạch - Bậc" }));
    expect(await screen.findByText("3,99")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Thống kê BC Sở" }));
    expect(screen.queryByText("Xã Mỹ Hòa Hưng")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Xem thẻ" }));
    expect(await screen.findByText("Xã Mỹ Hòa Hưng")).toBeInTheDocument();
  });

  it("thêm hồ sơ qua hộp thoại", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    const user = userEvent.setup();
    render(<HrmStaffListScreen />);
    await user.click(await screen.findByRole("button", { name: "Thêm hồ sơ" }));
    const dlg = await screen.findByRole("dialog");
    await user.type(within(dlg).getByLabelText("Họ và tên"), "Đỗ Thị Thêm");
    await user.type(within(dlg).getByLabelText(/^Ngày sinh/), "1992-02-02");
    await user.type(within(dlg).getByLabelText("Số điện thoại"), "0901000777");
    await user.selectOptions(
      within(dlg).getByLabelText("Đơn vị công tác"),
      "hcth",
    );
    await user.click(within(dlg).getByRole("button", { name: "Lưu" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(
      await screen.findByText(/Đã tạo hồ sơ VC-00003/),
    ).toBeInTheDocument();
  });

  it("Admin: ma trận phân quyền, nhóm Admin cố định", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    render(<HrmPermissionScreen />);
    expect(await screen.findByText("Cố định")).toBeInTheDocument();
    expect(screen.getByText("Lãnh đạo các phòng")).toBeInTheDocument();
  });
});
