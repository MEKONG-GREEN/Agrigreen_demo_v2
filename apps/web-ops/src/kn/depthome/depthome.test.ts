import { beforeEach, describe, expect, it } from "vitest";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetDeptHome } from "@/api/mock/kn-dept-home";
import { docFormErrors, docPayload, emptyDocForm, fileError, kpiLink } from "./api";

describe("W11.54 kho văn bản phòng — biểu nhập", () => {
  it("kiểm tên, loại, hiệu lực; đối tượng từ ô chọn", () => {
    const f = { ...emptyDocForm(), effective_from: "2026-02-01", effective_to: "2026-01-01" };
    expect(Object.keys(docFormErrors(f)).sort()).toEqual(["effective_to", "kind", "title"]);
    const ok = { ...emptyDocForm("f1"), title: " Quy trình ", kind: "quy_trinh_ky_thuat" as const, nong_ho: true };
    expect(docPayload(ok, 3)).toMatchObject({ title: "Quy trình", folder_ref: "f1", audience: ["xa", "nong_ho"], base_version: 3 });
  });
  it("tệp: bắt buộc, ≤ 20 MB, PDF/DOCX/XLSX (AC-02)", () => {
    expect(fileError(null)).toBe("Chọn tệp");
    expect(fileError(new File(["x"], "a.exe"))).toMatch(/PDF/);
    const big = { name: "a.pdf", size: 20 * 1024 * 1024 + 1 } as File;
    expect(fileError(big)).toMatch(/20 MB/);
    expect(fileError(new File(["%PDF-"], "a.PDF"))).toBeNull();
  });
  it("thẻ mở màn chi tiết (FN-17 B3)", () => {
    expect(kpiLink("alerts_open")).toBe("/kn/cnts/canh-bao");
    expect(kpiLink("docs_total", "chan_nuoi_thuy_san")).toBe("/kn/phong-ban/chan_nuoi_thuy_san/van-ban");
    expect(kpiLink("khong_co")).toBeNull();
  });
});

const req = (url: string) => new Request(url, { headers: { Authorization: "Bearer mock.setter" } });

describe("W11.54 giả lập /v1/extension/dept-*", () => {
  beforeEach(() => resetDeptHome());
  it("Admin phải chọn phòng; có phòng → trang chủ và kho văn bản", async () => {
    const no = await mockFetchNoDelay(req("http://x/v1/extension/dept-home"));
    expect(no.status).toBe(422);
    const home = await mockFetchNoDelay(req("http://x/v1/extension/dept-home?department=chan_nuoi_thuy_san"));
    expect(home.status).toBe(200);
    const h = (await home.json()) as { kpis: { key: string }[]; latest_docs: unknown[] };
    expect(h.kpis.map((k) => k.key)).toContain("outbreaks_active");
    const list = await mockFetchNoDelay(req("http://x/v1/extension/dept-docs?department=chan_nuoi_thuy_san&validity=expired"));
    const l = (await list.json()) as { items: { title: string; validity: string }[] };
    expect(l.items.length).toBeGreaterThan(0);
    expect(l.items.every((d) => d.validity === "expired")).toBe(true);
  });
});
