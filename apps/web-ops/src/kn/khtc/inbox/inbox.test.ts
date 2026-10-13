import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { ApiProblem } from "@/api/problem";
import {
  attemptText,
  createDeptFormsApi,
  filterRows,
  latestSupplement,
  type DeptForm,
} from "./api";

const client = () =>
  createDeptFormsApi(
    createApiClient({
      baseUrl: "http://mock",
      getAccessToken: () => "mock.tinh",
      getContextId: () => null,
      fetch: mockFetchNoDelay,
    }),
  );
const code = async (p: Promise<unknown>) =>
  p.then(
    () => "ok",
    (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)),
  );

describe("Báo cáo cho KHTC phía phòng (W11.11, FN-PN-03.01)", () => {
  it("phiếu kỳ này của từng phòng: Nhập / Sửa, gửi lại / Xem, đường dẫn màn phiếu W11.63", async () => {
    const api = client();
    const tt = await api.list("current", "trong_trot");
    expect(tt.department_name).toBe("Phòng Trồng trọt");
    expect(tt.items[0]).toMatchObject({
      state: "submitted",
      action: "view",
      attempt: 1,
      needs_action: false,
    });
    expect(tt.items[0]!.entry_route).toBe(
      `/kn/khtc/phieu/${tt.items[0]!.period_id}?department=trong_trot`,
    );
    const cn = await api.list("current", "chan_nuoi_thuy_san");
    expect(cn.items[0]).toMatchObject({
      state: "supplement",
      action: "resubmit",
      needs_action: true,
    });
    expect(cn.items[0]!.supplement_note).toBeTruthy();
    expect(cn.counts).toMatchObject({ needs_action: 1, supplement: 1 });
    const tttt = await api.list("current", "thong_tin_truyen_thong");
    expect(tttt.items[0]!.action).toBe("enter");
    expect((await api.list("sent", "thong_tin_truyen_thong")).items).toEqual(
      [],
    );
    expect((await api.list("sent", "chan_nuoi_thuy_san")).items).toHaveLength(
      1,
    );
  });

  it("Admin phải chọn phòng", async () => {
    expect(await code(client().list("current"))).toBe("validation_failed");
  });

  it("lọc Cần xử lý / loại báo cáo, lần gửi, yêu cầu bổ sung mới nhất", () => {
    const row = (o: Partial<DeptForm>) =>
      ({
        report_type: "week",
        state: "submitted",
        needs_action: false,
        attempt: 1,
        max_submissions: 3,
        ...o,
      }) as DeptForm;
    const rows = [
      row({ state: "not_started", needs_action: true, attempt: 0 }),
      row({}),
      row({
        report_type: "month",
        state: "supplement",
        needs_action: true,
        supplement_note: "Bổ sung",
        reviewed_at: "2026-10-14T01:00:00Z",
        name: "Tháng 10",
      }),
    ];
    expect(filterRows(rows, "", "needs_action")).toHaveLength(2);
    expect(filterRows(rows, "month", "")).toHaveLength(1);
    expect(filterRows(rows, "week", "submitted")).toHaveLength(1);
    expect(attemptText(rows[0]!)).toBe("—");
    expect(attemptText(rows[1]!)).toBe("1/3");
    expect(attemptText(row({ attempt: 4, max_submissions: 0 }))).toBe("4");
    expect(latestSupplement(rows)?.name).toBe("Tháng 10");
  });
});
