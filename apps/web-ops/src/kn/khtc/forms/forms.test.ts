import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { ApiProblem } from "@/api/problem";
import { createKhtcPeriodApi } from "../periods/api";
import {
  createKhtcFormApi,
  emptyContent,
  liveCells,
  normalizeForSend,
  pct,
  taskAction,
  type KhtcFormRow,
} from "./api";

const code = async (p: Promise<unknown>) =>
  p.then(
    () => "ok",
    (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)),
  );
const api = () =>
  createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => "mock.tinh",
    getContextId: () => null,
    fetch: mockFetchNoDelay,
  });

const row = (over: Partial<KhtcFormRow> = {}): KhtcFormRow => ({
  entry_ref: "r1",
  no: 1,
  code: "",
  name: "Mô hình lúa",
  unit: "ha",
  planned_qty: "100",
  scale_note: "",
  module_sourced: false,
  source_key: "",
  cells: { E: "95" },
  over_plan: false,
  ...over,
});

describe("W11.63 phiếu KHTC — tính trước trên màn (BR-DL-05, BR-DL-12)", () => {
  it("H = E + F + G (chỉ điều chỉnh cùng năm), I làm tròn 1 chữ số, vượt KH", () => {
    const c = emptyContent();
    c.cells.r1 = { done: "3,5" };
    c.adjustments = [
      { entry_ref: "r1", period_id: "w40", amount: "1", reason: "x" },
      {
        entry_ref: "r1",
        period_id: "w52-2025",
        amount: "-9",
        reason: "năm trước",
      },
    ];
    const l = liveCells(row(), c, { w40: 2026, "w52-2025": 2025 }, 2026);
    expect(l).toMatchObject({ G: 1, H: 99.5, I: "99,5", over: false });
    c.cells.r1 = { done: "10" };
    expect(liveCells(row(), c, { w40: 2026 }, 2026)).toMatchObject({
      H: 106,
      I: "106",
      over: true,
    });
    expect(liveCells(row({ module_sourced: true }), c, {}, 2026).H).toBeNull();
    expect(pct(110.66)).toBe("110,7");
    expect(
      normalizeForSend({
        cells: { r1: { done: " 2,5 " }, r2: {} },
        adjustments: [],
        texts: { a: " " },
      }),
    ).toEqual({
      cells: {
        r1: { done: "2.5", status: "", content: "", people: "", budget: "" },
      },
      adjustments: [],
      texts: {},
    });
    expect(taskAction("draft")).toBe("Tiếp tục");
    expect(taskAction("supplement")).toBe("Sửa, gửi lại");
  });
});

describe("W11.63 phiếu KHTC — giả lập theo hợp đồng", () => {
  it("KH-FN-PN-03.01/03.02: phiếu kỳ này ở 4 phòng; nháp → Tiếp tục; nguồn module không nhập; gửi lại chỉ khi yêu cầu bổ sung", async () => {
    const forms = createKhtcFormApi(api());
    const periods = createKhtcPeriodApi(api());
    const p = await periods.open({
      report_type: "week",
      year: new Date().getFullYear() + 1,
      no: 2,
      departments: [
        "trong_trot",
        "chan_nuoi_thuy_san",
        "thong_tin_truyen_thong",
        "hanh_chinh_tong_hop",
      ],
    });
    for (const d of [
      "trong_trot",
      "chan_nuoi_thuy_san",
      "thong_tin_truyen_thong",
      "hanh_chinh_tong_hop",
    ]) {
      expect((await forms.tasks(d)).some((t) => t.period_id === p.id)).toBe(
        true,
      );
    }
    const dept = "thong_tin_truyen_thong";
    let f = await forms.get(p.id, dept);
    expect(f.template.code).toBe("KH-TTTT");
    const kinds = f.template.columns.map((c) => c.kind);
    expect(kinds).toContain("from_plan");
    expect(kinds).toContain("prior_cumulative");
    const mod = f.tabs[0]!.rows.find((r) => r.module_sourced)!;
    const news = f.tabs[0]!.rows.find((r) => !r.module_sourced)!;
    const c = emptyContent();
    c.cells[mod.entry_ref] = { done: "5" };
    c.cells[news.entry_ref] = { done: "2" };
    f = await forms.draft(p.id, dept, c, 0);
    expect(f.state).toBe("draft");
    expect(
      (await forms.tasks(dept)).find((t) => t.period_id === p.id)?.state,
    ).toBe("draft");
    expect(
      (await periods.get(p.id)).forms.find((x) => x.department === dept)?.state,
    ).toBe("draft");
    expect(await code(forms.submit(p.id, dept, c, 0))).toBe(
      "extension.khtc_form_invalid",
    );
    delete c.cells[mod.entry_ref];
    c.texts.cong_tac_khac = "Đăng 2 tin";
    f = await forms.submit(p.id, dept, c, 0);
    expect(f).toMatchObject({
      state: "submitted",
      attempt: 1,
      editable: false,
    });
    expect(await code(forms.draft(p.id, dept, c, 1))).toBe(
      "extension.khtc_form_locked",
    );
    expect(await code(forms.review(p.id, dept, "returned", ""))).toBe(
      "validation_failed",
    );
    f = await forms.review(p.id, dept, "returned", "Bổ sung nội dung tin");
    expect(f.state).toBe("supplement");
    expect((await forms.get(p.id, dept)).request_note).toBe(
      "Bổ sung nội dung tin",
    );
    expect(await code(forms.submit(p.id, dept, c, 0))).toBe(
      "forms.submission_conflict",
    );
    f = await forms.submit(p.id, dept, c, 1);
    expect(f.attempt).toBe(2);
    f = await forms.review(p.id, dept, "accepted");
    expect(f.state).toBe("confirmed");
  });
});
