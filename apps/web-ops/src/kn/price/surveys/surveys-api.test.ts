// @vitest-environment node
// Môi trường node: FormData/File/Request của undici (jsdom File không đi qua Request của Node) — nhập tệp nhiều phần.
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { ApiProblem } from "@/api/problem";
import { createSurveyApi } from "./api";

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
  return createSurveyApi(api);
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

const CSV =
  "Tên cán bộ điều tra,Xã,Nhóm cây,Mặt hàng,Họ và tên nông hộ,Số điện thoại,Diện tích (ha),Sản lượng (kg),Giá bán (đ/kg),Chi phí giống\n" +
  "Lê Văn Hải,An Châu,Rau màu,Dưa leo,Nguyễn Văn A,0901999001,0.5,10000,9000,500000\n" +
  "Lê Văn Hải,An Châu,Rau màu,dưa leo,Nguyễn Văn B,0901999002,0.5,10000,9500,500000\n" +
  "Lê Văn Hải,An Châu,Rau màu,DƯA LEO,Nguyễn Văn C,0901999003,0.5,10000,9200,500000\n" +
  "Lê Văn Hải,An Châu,Rau màu,Dưa leo,Nguyễn Văn E,0901999005,0,10000,9000,500000\n" +
  "Lê Văn Hải,An Châu,Rau màu,Dưa leo,Nguyễn Văn A,0901999001,0.5,10000,9100,500000\n";

describe("điều tra giá thành W11.78 — nhập tệp hai bước (giả lập)", () => {
  it("BG-FN-TI-02.11: chuẩn hóa tên mặt hàng (AC-01), diện tích 0 → lỗi (AC-02), trùng SĐT + mặt hàng → bỏ dòng sau (AC-03); ghi nhận đúng tệp đã xem", async () => {
    const sa = await clientFor("kn-binh", "asg-binh-tinh");
    const rounds = await sa.rounds();
    const ht = rounds.items.find((r) => r.name === "Hè Thu 2026")!;
    const file = new File([CSV], "DieuTra.csv", { type: "text/csv" });
    const pv = await sa.preview(ht.id, { file });
    expect(pv.lines.slice(0, 3).map((l) => l.item)).toEqual(["MH00001", "MH00001", "MH00001"]);
    expect(pv.lines[3]!.level).toBe("error");
    expect(pv.lines[4]!.skip).toBe(true);
    expect(pv.ok).toBe(3);
    expect(await problemCode(sa.commit(ht.id, { file, sha256: "x".repeat(64) }))).toBe("price.survey_import_changed");
    const res = await sa.commit(ht.id, { file, sha256: pv.sha256 });
    expect(res.created).toBe(3);
    expect(res.errors).toBe(1);
  });

  it("Cấp TW không vào đợt điều tra; Cấp Tỉnh không chốt được khi còn phiếu chờ rà soát", async () => {
    const tw = await clientFor("kn-quan", "asg-quan-tw");
    expect(await problemCode(tw.rounds())).toBe("forbidden");
    const sa = await clientFor("kn-binh", "asg-binh-tinh");
    const ht = (await sa.rounds()).items.find((r) => r.name === "Hè Thu 2026")!;
    expect(await problemCode(sa.act(ht.id, { op: "close" }))).toBe("price.survey_round_pending");
  });
});
