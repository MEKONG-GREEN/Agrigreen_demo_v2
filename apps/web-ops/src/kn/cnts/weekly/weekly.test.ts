// BF-CN-WEEKLY (PRD UP-06): quy tắc thuần của màn Báo cáo công việc tuần — bảng T2–CN, kiểm trước khi gửi, chọn kỳ mặc
// định, gợi ý Phần I từ KSGM / Nhập liệu tuần / Lịch trực; mock: kế hoạch đã đăng ký → Lịch công tác chờ duyệt.
import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { resetWorkSchedules } from "@/api/mock/kn-schedules";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi, type FormsTask } from "../periods/api";
import {
  PART_I,
  PART_II,
  WORK_OTHER,
  WORK_TYPES,
  applySuggestions,
  buildSuggestions,
  partKeys,
  pickTask,
  validateWeekly,
  weekDays,
  weeklyPayload,
  type MyDuty,
} from "./weekly";

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

function task(code: string, from: string, extra: Partial<FormsTask> = {}): FormsTask {
  const to = new Date(Date.parse(from + "T00:00:00Z") + 6 * 86_400_000).toISOString().slice(0, 10);
  return {
    id: `${code}:${from}:${extra.unit ?? "30001"}`, profile: "kn_cnts", period_id: `p-${code}-${from}`, unit: "30001", unit_kind: "commune", parent: "30001",
    template_id: "t", template_code: code, template_title: code, template_version: 1, fields: [], label: `Tuần ${from}`, period_kind: "week", period_key: from,
    period_from: from, period_to: to, due_at: "2026-09-21T01:00:00Z", period_status: "open", open: true, status: "not_submitted", version: 0, submission_id: null,
    values: {}, nil_report: false, note: "", explanation: "", late: false, submitted_at: null, return_note: null, held_at: null, ...extra,
  };
}

const fullWeek = (part: "I" | "II") => {
  const v: Record<string, string> = {};
  for (const k of ["t2", "t3", "t4", "t5", "t6"]) {
    v[`${k}_loai`] = WORK_TYPES[0];
    if (part === "I") v[`${k}_ket_qua`] = "Kiểm tra đàn";
    else Object.assign(v, { [`${k}_ke_hoach`]: "Kiểm tra đàn", [`${k}_ket_qua_du_kien`]: "Nắm số đàn" });
  }
  return v;
};

describe("bảng T2–CN", () => {
  it("bảy ngày từ Thứ Hai, 5 ngày đầu bắt buộc", () => {
    const d = weekDays("2026-09-21");
    expect(d.map((x) => x.key)).toEqual(["t2", "t3", "t4", "t5", "t6", "t7", "cn"]);
    expect(d[6]!.date).toBe("2026-09-27");
    expect(d.filter((x) => x.required)).toHaveLength(5);
  });

  it("khóa trường mỗi phần khớp máy chủ (7 ngày)", () => {
    expect(partKeys("I")).toHaveLength(7 * 3);
    expect(partKeys("II")).toContain("cn_dia_diem");
    expect(partKeys("I")).not.toContain("t2_dia_diem");
  });
});

describe("kiểm trước khi gửi (UP-06)", () => {
  it("đủ T2–T6 thì hợp lệ; T7/CN bỏ trống được", () => {
    expect(validateWeekly("I", fullWeek("I"), false, "")).toEqual({});
    expect(validateWeekly("II", fullWeek("II"), false, "")).toEqual({});
  });

  it("thiếu ngày làm việc, T7 đã chọn loại thì bắt buộc nội dung", () => {
    const v = { ...fullWeek("I"), t3_ket_qua: "", t7_loai: WORK_TYPES[0] };
    const e = validateWeekly("I", v, false, "");
    expect(Object.keys(e).sort()).toEqual(["t3_ket_qua", "t7_ket_qua"]);
  });

  it("Phần II đi công tác: bắt buộc ghi rõ và Địa điểm", () => {
    const v = { ...fullWeek("II"), t4_loai: WORK_OTHER };
    const e = validateWeekly("II", v, false, "");
    expect(e.t4_cong_tac_khac).toBeTruthy();
    expect(e.t4_dia_diem).toBe("Đi công tác: bắt buộc Địa điểm.");
    // Phần I không đòi Địa điểm.
    expect(validateWeekly("I", { ...fullWeek("I"), t4_loai: WORK_OTHER, t4_cong_tac_khac: "Họp tỉnh" }, false, "")).toEqual({});
  });

  it("mỗi ô ≤ 500 ký tự; sau hạn bắt buộc lý do", () => {
    const e = validateWeekly("I", { ...fullWeek("I"), t2_ket_qua: "x".repeat(501) }, true, " ");
    expect(e.t2_ket_qua).toBe("Tối đa 500 ký tự.");
    expect(e.explanation).toMatch(/lý do/);
  });

  it("giá trị gửi: chỉ khóa của phần, bỏ ô trống", () => {
    expect(weeklyPayload("I", { t2_loai: " " + WORK_TYPES[0] + " ", t2_ket_qua: "A", la: "x", t3_ket_qua: "" })).toEqual({ t2_loai: WORK_TYPES[0], t2_ket_qua: "A" });
  });
});

describe("kỳ mặc định", () => {
  const ts = [task(PART_I, "2026-09-14"), task(PART_I, "2026-09-21"), task(PART_II, "2026-09-21"), task(PART_II, "2026-09-28"), task(PART_II, "2026-10-05")];
  it("Phần I: tuần đã kết thúc gần nhất; Phần II: tuần sắp tới gần nhất", () => {
    expect(pickTask(ts, "I", "2026-09-24")?.period_from).toBe("2026-09-14");
    expect(pickTask(ts, "II", "2026-09-24")?.period_from).toBe("2026-09-28");
    expect(pickTask(ts, "II", "2026-10-30")?.period_from).toBe("2026-10-05");
    expect(pickTask([], "I", "2026-09-24")).toBeNull();
  });
});

describe("gợi ý Phần I từ KSGM, Nhập liệu tuần, Lịch trực", () => {
  const days = weekDays("2026-09-14");
  const ksgm = [
    task("CNTS-BM01", "2026-09-15", { unit: "fac-1", parent: "30001", version: 1, values: { trau_bo: "2", heo: "15", gia_cam: "", de: "0" } }),
    task("CNTS-BM01", "2026-09-15", { unit: "fac-9", parent: "30009", version: 1, values: { heo: "99" } }),
    task("CNTS-BM01", "2026-09-16", { unit: "fac-1", parent: "30001", version: 0 }),
  ];
  // Kỳ ngày: period_from = ngày KSGM.
  ksgm.forEach((t) => (t.period_to = t.period_from));
  const intake = [task("CNTS-BM04", "2026-09-09", { version: 1, submitted_at: "2026-09-16T05:00:00Z", label: "Tuần 38/2026" })];
  const duties: MyDuty[] = [{ date: "2026-09-17", shift: "D", facility: { ref: "fac-1", code: "GM01", name: "Lò mổ Hòa Thuận", commune_code: "30001" }, colleagues: [] }];

  it("đúng ngày, đúng xã; bỏ bài chưa nộp và xã khác", () => {
    const s = buildSuggestions(days, "30001", ksgm, intake, duties, (t) => (t.unit === "fac-1" ? "Lò mổ Hòa Thuận" : t.unit));
    expect(s.t3).toEqual(["KSGM Lò mổ Hòa Thuận: trâu bò 2, heo 15, dê 0"]);
    expect(s.t4).toEqual(["Gửi Nhập liệu tuần chăn nuôi (Tuần 38/2026)"]);
    expect(s.t5).toEqual(["Trực KSGM ca đêm tại Lò mổ Hòa Thuận"]);
    expect(s.t2).toBeUndefined();
  });

  it("chỉ điền ô Kết quả còn trống, chọn sẵn loại công việc", () => {
    const { values, filled } = applySuggestions({ t3_ket_qua: "Đã nhập tay" }, { t3: ["KSGM"], t5: ["Trực", "KSGM"] });
    expect(filled).toBe(1);
    expect(values.t3_ket_qua).toBe("Đã nhập tay");
    expect(values.t5_ket_qua).toBe("Trực; KSGM");
    expect(values.t5_loai).toBe(WORK_TYPES[0]);
  });
});

describe("giả lập: kế hoạch Phần II đã đăng ký → Lịch công tác chờ duyệt", () => {
  beforeEach(() => {
    resetKnAdmin();
    resetCntsPeriods();
    resetWorkSchedules();
  });

  it("đăng ký kế hoạch rồi tạo lịch; chưa đăng ký → 409; Log có phiên bản", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
    const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
    await adm.departmentStatus(cn.id, "active");
    const xaCtx = await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san");
    const raw = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-an", getContextId: () => xaCtx, fetch: mockFetchNoDelay });
    const admin = createCntsPeriodApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockFetchNoDelay }));
    const tinh = createCntsPeriodApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => null, fetch: mockFetchNoDelay }));
    const xa = createCntsPeriodApi(raw);
    await admin.install();
    // Tuần kế hoạch kế tiếp (Thứ Hai sau hôm nay) để lịch chưa bị khóa.
    const now = new Date();
    const mon = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate() + ((8 - now.getDay()) % 7 || 7)));
    const from = mon.toISOString().slice(0, 10);
    const pv = await tinh.preview({ code: PART_II, from_date: from, to_date: from, units: ["91301"] });
    expect(pv.periods[0]!.to_date).toBe(new Date(mon.getTime() + 6 * 86_400_000).toISOString().slice(0, 10)); // T2–CN
    await tinh.publish({ code: PART_II, from_date: from, to_date: from, units: ["91301"] });
    const t = (await xa.tasks(PART_II)).find((x) => x.period_from === from)!;
    const plan = (body: object) => raw.POST("/v1/extension/cnts/weekly-plan/schedule", { params: { header: { "Idempotency-Key": `k-${Math.random()}` } }, body: body as never });

    expect((await plan({ period_id: t.period_id, unit: "91301" })).response.status).toBe(409);
    const values = weeklyPayload("II", { ...fullWeek("II"), t3_loai: WORK_OTHER, t3_cong_tac_khac: "Họp tỉnh", t3_dia_diem: "Sở NN&MT" });
    await xa.submit(t.period_id, { unit: "91301", values, base_version: 0, nil_report: false, explanation: "Đăng ký" });
    const r = await plan({ period_id: t.period_id, unit: "91301" });
    expect(r.response.status).toBe(201);
    const d = r.data as { schedule: { status: string }; items: { day: string; location: string | null }[] };
    expect(d.schedule.status).toBe("pending");
    expect(d.items).toHaveLength(5);
    expect(d.items.find((i) => i.day === weekDays(from)[1]!.date)?.location).toBe("Sở NN&MT");
    expect((await xa.submissions(t.period_id, "91301")).map((s) => s.version)).toEqual([1]);
  });
});
