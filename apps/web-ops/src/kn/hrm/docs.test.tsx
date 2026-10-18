import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { resetHrmDocs } from "@/api/mock/kn-hrm-docs";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { createDocApi } from "./doc-api";
import {
  HrmDocDetailScreen,
  HrmDocFoldersScreen,
  HrmDocsScreen,
} from "./DocScreens";

const params: { id: string } = { id: "" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/nhan-su/kho-tai-lieu",
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
  return { api, da: createDocApi(api) };
}

describe("màn Kho tài liệu nhân sự", () => {
  beforeEach(() => {
    resetHrm();
    resetHrmStaff();
    resetHrmDocs();
  });

  it("cây thư mục + danh sách; chọn thư mục lọc tài liệu; nút Tải lên theo quyền", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    render(<HrmDocsScreen />);
    expect(
      await screen.findByRole("heading", { name: "Hồ sơ dữ liệu" }),
    ).toBeTruthy();
    expect(
      await screen.findByText("QĐ nâng bậc lương tháng 7-2026"),
    ).toBeTruthy();
    expect(
      screen.getByText("Biên bản họp giao ban phòng Chăn nuôi"),
    ).toBeTruthy();
    await userEvent.click(
      screen.getByRole("button", { name: /Tài liệu nhân sự khác/ }),
    );
    await waitFor(() =>
      expect(screen.queryByText("QĐ nâng bậc lương tháng 7-2026")).toBeNull(),
    );
    expect(
      screen.getByText("Biên bản họp giao ban phòng Chăn nuôi"),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Tải lên" })).toBeTruthy();
  });

  it("chi tiết tệp: thông tin, lịch sử, xóa = ẩn có lý do", async () => {
    const c = await clientFor("kn-admin", "asg-admin");
    auth.api = c.api;
    const doc = (await c.da.list({ q: "Biên bản" })).items[0]!;
    params.id = doc.doc_id;
    render(<HrmDocDetailScreen />);
    expect(
      await screen.findByRole("heading", {
        name: "Biên bản họp giao ban phòng Chăn nuôi",
      }),
    ).toBeTruthy();
    expect(screen.getByText("Lịch sử")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Xóa (ẩn)" }));
    await userEvent.type(
      screen.getByLabelText("Lý do"),
      "Trùng với bản đã lưu",
    );
    await userEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    expect(await screen.findByText(/Tài liệu đã ẩn/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Hiện lại" })).toBeTruthy();
  });

  it("Admin — Thư mục tài liệu: chọn thư mục, sửa quyền thư mục", async () => {
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    render(<HrmDocFoldersScreen />);
    expect(
      await screen.findByRole("button", { name: "Cài thư mục mặc định" }),
    ).toBeTruthy();
    await userEvent.click(
      await screen.findByRole("button", { name: /Hồ sơ đánh giá/ }),
    );
    expect(
      await screen.findByRole("region", { name: "Quyền thư mục" }),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Thêm dòng" }));
    await userEvent.selectOptions(
      screen.getByLabelText("Xem, tải xuống 1"),
      "none",
    );
    await userEvent.click(screen.getByRole("button", { name: "Lưu quyền" }));
    expect(await screen.findByText("Đã lưu quyền thư mục.")).toBeTruthy();
    expect(await screen.findByText("Đổi quyền thư mục")).toBeTruthy();
    // HRM-DOC-STATUS-001: thông báo còn sau khi cây/quyền/lịch sử đã tải lại.
    expect(screen.getByText("Đã lưu quyền thư mục.")).toBeTruthy();
  });
});
