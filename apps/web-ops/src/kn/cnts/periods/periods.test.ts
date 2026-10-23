import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { ApiProblem } from "@/api/problem";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi, fieldErrors, groupFields, payloadValues, periodRows, type FormsField } from "./api";

async function contextOf(personaId: string, assignmentId: string, dept: string | null) {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: dept }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}

function client(personaId: string, ctx: string | null) {
  return createCntsPeriodApi(
    createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx, fetch: mockFetchNoDelay }),
  );
}

const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)));

describe("biểu kỳ CNTS — hàm phía máy khách", () => {
  const fs: FormsField[] = [
    { key: "heo", label: "Heo", type: "integer", required: true, non_negative: true, group: "A" },
    { key: "ca", label: "Cá", type: "decimal", required: true, non_negative: true, group: "B" },
    { key: "ghi_chu", label: "Ghi chú", type: "text", required: false },
  ];
  it("kiểm ô, báo không phát sinh, nhóm trường", () => {
    expect(fieldErrors(fs, { heo: "-1", ca: "abc" }, false)).toEqual({ heo: "Không được nhập số âm.", ca: "Không phải số hợp lệ." });
    expect(fieldErrors(fs, { heo: "1,5", ca: "-0,5" }, false)).toEqual({ heo: "Chỉ nhập số nguyên.", ca: "Không được nhập số âm." });
    expect(fieldErrors(fs, { heo: "12", ca: "1,5" }, false)).toEqual({});
    expect(fieldErrors(fs, {}, false)).toEqual({ heo: "Bắt buộc nhập.", ca: "Bắt buộc nhập." });
    expect(fieldErrors(fs, { heo: "3" }, true)).toEqual({ heo: "Báo không phát sinh: phải bằng 0." });
    expect(payloadValues(fs, { heo: " 1200 ", ca: "1.234,5" }, false)).toEqual({ heo: "1200", ca: "1234.5" });
    expect(payloadValues(fs, { heo: "", ghi_chu: " x " }, true)).toEqual({ heo: "0", ca: "0", ghi_chu: "x" });
    expect(groupFields(fs).map((g) => g.group)).toEqual(["A", "B", ""]);
  });
});

describe("biểu kỳ CNTS — backend giả lập /v1/extension/cnts/*, /v1/forms/*", () => {
  beforeEach(() => {
    resetKnAdmin();
    resetCntsPeriods();
  });

  it("cài biểu (Admin), phát hành BM-04 tuần Thứ Ba – Thứ Hai (lịch CNTS D8), Xã nộp có khóa lạc quan, sao chép kỳ trước, chốt kỳ", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
    const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
    await adm.departmentStatus(cn.id, "active");
    const admin = client("kn-admin", adminCtx);
    const tinh = client("kn-binh", null);
    const xa = client("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san"));

    expect((await admin.forms()).every((f) => f.template_id === null)).toBe(true);
    expect(await code(tinh.publish({ code: "CNTS-BM04", from_date: "2026-10-07", to_date: "2026-10-13", units: ["91301"] }))).toBe("forms.template_not_found");
    expect(await code(xa.install())).toBe("forbidden");
    expect((await admin.install()).every((f) => f.template_up_to_date)).toBe(true);

    const pv = await tinh.preview({ code: "CNTS-BM04", from_date: "2026-10-09", to_date: "2026-10-15", units: ["91301", "91302"] });
    expect(pv.periods.map((p) => [p.from_date, p.to_date])).toEqual([["2026-10-06", "2026-10-12"], ["2026-10-13", "2026-10-19"]]);
    expect(pv.periods.map((p) => p.key)).toEqual(["2026-W41", "2026-W42"]);
    expect(pv.periods[0]!.due_at).toBe("2026-10-13T05:00:00.000Z"); // 12:00 giờ VN Thứ Ba ngay sau kỳ
    expect(pv.periods[0]!.rule).toBe("kn.period_rule/v2;week_start=tue;week=year;month=week_end;cal=chan_nuoi_thuy_san:v3");
    expect([pv.calendar_id, pv.calendar_version]).toEqual(["chan_nuoi_thuy_san", 3]);
    const bm05 = await tinh.preview({ code: "CNTS-BM05-I", from_date: "2026-10-05", to_date: "2026-10-09", units: ["91301"] });
    expect([bm05.calendar_id, bm05.periods[0]!.rule, bm05.periods[0]!.from_date]).toEqual([null, "cnts.work_week/v1", "2026-10-05"]); // BM-05 không đổi
    expect(pv.tasks).toBe(4);
    const bm01 = await tinh.preview({ code: "CNTS-BM01", from_date: "2026-10-01", to_date: "2026-10-03", units: ["x"] });
    expect(bm01.periods).toHaveLength(3);
    await tinh.publish({ code: "CNTS-BM04", from_date: "2099-01-06", to_date: "2099-01-19", units: ["91301", "91302"] });

    const tasks = await xa.tasks("CNTS-BM04");
    expect(tasks).toHaveLength(4);
    const [later, first] = [tasks[0]!, tasks.find((t) => t.unit === "91301" && t.period_id !== tasks[0]!.period_id)!];
    expect(first.status).toBe("not_submitted");
    const vals = Object.fromEntries(first.fields.filter((f) => f.type !== "text").map((f) => [f.key, "1"]));
    await xa.submit(first.period_id, { unit: "91301", values: vals, base_version: 0, nil_report: false });
    expect(await code(xa.submit(first.period_id, { unit: "91301", values: vals, base_version: 0, nil_report: false }))).toBe("forms.submission_conflict");
    const prev = await xa.previous(later.period_id, "91301");
    expect(prev?.submission.values.tong_dan_heo).toBe("1");

    const rows = periodRows(await tinh.tasks("CNTS-BM04"));
    expect(rows.find((r) => r.periodId === first.period_id)).toMatchObject({ total: 2, submitted: 1 });
    expect(await code(xa.close(first.period_id))).toBe("forbidden");
    await tinh.close(first.period_id);
    expect(await code(xa.submit(first.period_id, { unit: "91302", values: vals, base_version: 0, nil_report: false }))).toBe("forms.period_closed");
    expect((await xa.tasks("CNTS-BM04")).find((t) => t.period_id === first.period_id && t.unit === "91302")!.status).toBe("missed");
  });
});
