import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { planOf, weeksInYear } from "@/api/mock/kn-khtc-periods";
import { ApiProblem } from "@/api/problem";
import {
  createKhtcPeriodApi,
  numberChoices,
  progressText,
  scopeChoices,
  vnDate,
} from "./api";

const code = async (p: Promise<unknown>) =>
  p.then(
    () => "ok",
    (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)),
  );
const client = () =>
  createKhtcPeriodApi(
    createApiClient({
      baseUrl: "http://mock",
      getAccessToken: () => "mock.tinh",
      getContextId: () => null,
      fetch: mockFetchNoDelay,
    }),
  );

describe("Kỳ KHTC — lịch và tên kỳ (BRD Mục II.4)", () => {
  it("tuần 41/2026 = 07/10 – 13/10 (Thứ Tư – Thứ Ba); 2026 có 52 tuần", () => {
    expect(planOf({ report_type: "week", year: 2026, no: 41 })).toMatchObject({
      key: "2026-W41",
      name: "Tuần 41 năm 2026 (từ ngày 07/10/2026 đến ngày 13/10/2026)",
    });
    expect(weeksInYear(2026)).toBe(52);
    expect(planOf({ report_type: "week", year: 2026, no: 53 })).toMatchObject({
      field: "no",
    });
  });
  it("tháng/quý/năm theo Thứ Ba cuối; 9 tháng chỉ với quý III; Mẫu 5 bắt buộc hạn nộp", () => {
    expect(planOf({ report_type: "month", year: 2026, no: 9 })).toMatchObject({
      name: "Tháng 9 năm 2026 (số liệu đến ngày 29/09/2026)",
    });
    expect(
      planOf({
        report_type: "quarter",
        year: 2026,
        no: 3,
        scope: "nine_months",
      }),
    ).toMatchObject({ key: "2026-9M" });
    expect(
      planOf({ report_type: "quarter", year: 2026, no: 1, scope: "half" }),
    ).toMatchObject({ field: "scope" });
    expect(planOf({ report_type: "year", year: 2026 })).toMatchObject({
      name: "Năm 2026 (số liệu đến ngày 29/12/2026)",
    });
    expect(
      planOf({ report_type: "program", year: 2026, as_of_date: "2026-10-02" }),
    ).toMatchObject({
      name: "Đến ngày 02/10/2026 (số liệu đến ngày 29/09/2026)",
      dueRequired: true,
    });
  });
  it("ô chọn và hiển thị", () => {
    expect(numberChoices("month", [])).toHaveLength(12);
    expect(
      numberChoices("week", [
        { no: 1, from_date: "2025-12-31", to_date: "2026-01-06" },
      ])[0]!.label,
    ).toBe("Tuần 1 (31/12/2025 – 06/01/2026)");
    expect(scopeChoices(2).map((c) => c.value)).toEqual(["quarter", "half"]);
    expect(scopeChoices(1).map((c) => c.value)).toEqual(["quarter"]);
    expect(vnDate("2026-10-13")).toBe("13/10/2026");
    expect(
      progressText({
        counts: { forms: 4, forms_sent: 1, sources: 0, sources_closed: 0 },
      }),
    ).toBe("1/4 (25%)");
  });
});

describe("Kỳ KHTC — mở / trùng / hủy / đóng (mock theo hợp đồng)", () => {
  it("mở kỳ sinh một phiếu mỗi phòng, không mở trùng, hủy rồi mở lại được", async () => {
    const api = client();
    const draft = await api.preview({
      report_type: "week",
      year: 2030,
      no: 10,
    });
    expect(draft.max_submissions).toBe(3);
    expect(draft.duplicate).toBeNull();
    const p = await api.open({
      report_type: "week",
      year: 2030,
      no: 10,
      departments: ["trong_trot", "chan_nuoi_thuy_san"],
    });
    expect(p.forms.map((f) => f.state)).toEqual(["not_started", "not_started"]);
    expect(p.state).toBe("opened");
    expect(
      await code(api.open({ report_type: "week", year: 2030, no: 10 })),
    ).toBe("extension.khtc_period_duplicate");
    expect(await code(api.close(p.id))).toBe(
      "extension.khtc_period_not_closable",
    );
    expect(await code(api.cancel(p.id, "nhầm"))).toBe("validation_failed");
    const c = await api.cancel(p.id, "Mở nhầm tuần báo cáo");
    expect(c.state).toBe("cancelled");
    expect(c.forms[0]!.state).toBe("withdrawn");
    expect(await code(api.cancel(p.id, "Mở nhầm tuần báo cáo"))).toBe(
      "extension.khtc_period_terminal",
    );
    expect(
      await code(api.open({ report_type: "week", year: 2030, no: 10 })),
    ).toBe("ok");
  });
  it("phát sinh: phải nhập hạn nộp", async () => {
    const response=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":crypto.randomUUID()},body:JSON.stringify({assignment_id:"asg-admin"})}));
    expect(response.ok).toBe(true);
    const selected=await response.json() as {context_id:string};
    const api=createKhtcPeriodApi(createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>selected.context_id,fetch:mockFetchNoDelay}));
    expect(
      await code(
        api.open({
          report_type: "adhoc",
          name: "Báo cáo hạn mặn",
          from_date: "2030-03-01",
          to_date: "2030-03-10",
        }),
      ),
    ).toBe("validation_failed");
  });
});
