// @vitest-environment node
// W11.53c (#176, FN-19 luồng phụ): nạp Excel điểm, hộ — kiểm tệp phía trình duyệt, lỗi theo dòng giữ được từ problem 422,
// thử lại sau mất phản hồi giữ cùng Idempotency-Key và đúng từng byte (#183), tệp khác byte (cùng tên/cỡ/kiểu/thời điểm
// sửa) nhận khóa mới. Môi trường node: thân multipart như trình duyệt thật.
import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { IdempotentOperation, withIdempotentOperation } from "@/api/idempotency";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import { IMPORT_MAX_BYTES, ImportRejected, createCntsModelApi, importFileError, issueWhere, snapshotFile, type ImportResult } from "./api";

const P = "0190f1c4-7d3e-7000-8000-0000000000d1";
const ok: ImportResult = {
  project_ref: P, project_code: "DA-X", valid: true, committed: true, replayed: false, site_count: 1, household_count: 0,
  sites: [], households: [], issues: [], issues_truncated: false, site_refs: ["0190f1c4-7d3e-7000-8000-0000000000e1"],
};

/** Client thật trên máy chủ giả ghi lại khóa + băm thân của mỗi POST; `lose` → máy chủ đã xử lý nhưng phản hồi mất. */
function recorder(reply: () => Response = () => new Response(JSON.stringify(ok), { status: 201, headers: { "Content-Type": "application/json" } })) {
  const sent: { key: string | null; body: string; type: string | null }[] = [];
  let lose = false;
  const api = createApiClient({
    baseUrl: "http://srv",
    getAccessToken: () => "t",
    getContextId: () => "0190f1c4-7d3e-7000-8000-0000000000c0",
    fetch: async (req: Request) => {
      const b = new Uint8Array(await req.clone().arrayBuffer());
      sent.push({ key: req.headers.get("Idempotency-Key"), body: Buffer.from(b).toString("base64"), type: req.headers.get("Content-Type") });
      if (lose) {
        lose = false;
        throw new TypeError("Failed to fetch");
      }
      return reply();
    },
  });
  return { mapi: createCntsModelApi(api), sent, loseNext: () => (lose = true) };
}

const xlsx = (bytes: number[]) => new File([new Uint8Array([0x50, 0x4b, ...bytes])], "nap.xlsx", { type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", lastModified: 7 });

describe("nạp Excel điểm, hộ — kiểm tệp và lỗi theo dòng", () => {
  it("chỉ .xlsx, 1 byte – 2 MB", () => {
    expect(importFileError(null)).toMatch(/Chọn tệp/);
    expect(importFileError(new File(["x"], "a.xls"))).toMatch(/xlsx/);
    expect(importFileError(new File([], "a.xlsx"))).toMatch(/2 MB/);
    expect(importFileError(new File([new Uint8Array(IMPORT_MAX_BYTES + 1)], "a.xlsx"))).toMatch(/2 MB/);
    expect(importFileError(xlsx([1]))).toBeNull();
    expect(issueWhere({ sheet: "Hộ tham gia", row: 5, column: "C", message: "x" })).toBe("Hộ tham gia · dòng 5 · cột C");
    expect(issueWhere({ sheet: null, row: null, column: null, message: "x" })).toBe("Tệp");
  });

  it("422 extension.cnts_import_invalid giữ danh sách lỗi theo dòng/cột", async () => {
    const body = { type: "x", title: "Tệp nạp không hợp lệ", status: 422, code: "extension.cnts_import_invalid",
      issues: [{ sheet: "Điểm trình diễn", row: 4, column: "E", message: "Quy mô: không dùng dạng số mũ" }, { bad: true }] };
    const { mapi } = recorder(() => new Response(JSON.stringify(body), { status: 422, headers: { "Content-Type": "application/problem+json" } }));
    const err = await mapi.commitImport(P, xlsx([1])).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ImportRejected);
    const r = err as ImportRejected;
    expect(r.code).toBe("extension.cnts_import_invalid");
    expect(r.issues[0]).toEqual({ sheet: "Điểm trình diễn", row: 4, column: "E", message: "Quy mô: không dùng dạng số mũ" });
    expect(r.issues).toHaveLength(2);
  });
});

describe("#183: thử lại nạp sau mất phản hồi (client thật + middleware thật)", () => {
  it("cùng tệp → cùng khóa, cùng từng byte, Content-Type xác định; máy chủ phát lại", async () => {
    const { mapi, sent, loseNext } = recorder();
    const file = await snapshotFile(xlsx([1, 2, 3]));
    const op = new IdempotentOperation();
    loseNext();
    await expect(withIdempotentOperation(op, () => mapi.commitImport(P, file))).rejects.toBeTruthy();
    const r = await withIdempotentOperation(op, () => mapi.commitImport(P, file));
    expect(r.committed).toBe(true);
    expect(sent).toHaveLength(2);
    expect(sent[0]!.key).toBeTruthy();
    expect(sent[1]!.key).toBe(sent[0]!.key);
    expect(sent[1]!.body).toBe(sent[0]!.body);
    expect(sent[1]!.type).toBe(sent[0]!.type);
    expect(sent[0]!.type).toMatch(/^multipart\/form-data; boundary=----AgFormBoundary/);
  });

  it("tệp khác byte dù cùng tên/cỡ/kiểu/thời điểm sửa → khóa mới; xem trước và ghi là hai thao tác", async () => {
    const { mapi, sent, loseNext } = recorder();
    const op = new IdempotentOperation();
    loseNext();
    await expect(withIdempotentOperation(op, () => mapi.commitImport(P, xlsx([1, 2, 3])))).rejects.toBeTruthy();
    loseNext();
    await expect(withIdempotentOperation(op, () => mapi.commitImport(P, xlsx([9, 9, 9])))).rejects.toBeTruthy();
    loseNext();
    await expect(withIdempotentOperation(op, () => mapi.previewImport(P, xlsx([1, 2, 3])))).rejects.toBeTruthy();
    expect(new Set(sent.map((s) => s.key)).size).toBe(3);
  });
});

describe("giả lập nạp điểm, hộ", () => {
  beforeEach(() => resetCntsModel());
  const mapi = createCntsModelApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", getContextId: () => null, fetch: mockFetchNoDelay }));

  it("tải tệp mẫu; xem trước báo cần máy chủ thật", async () => {
    const p = await mapi.createProject({ code: "da-nap", name: "Dự án nạp", kind: "knqg", start_date: "2027-03-01" });
    const b = await mapi.importTemplate(p.ref);
    expect(new Uint8Array(await b.arrayBuffer()).slice(0, 2)).toEqual(new Uint8Array([0x50, 0x4b]));
    await expect(mapi.previewImport(p.ref, xlsx([1]))).rejects.toMatchObject({ code: "extension.cnts_import_invalid" });
    await expect(mapi.importTemplate("0190f1c4-7d3e-7000-8000-00000000ffff")).rejects.toMatchObject({ code: "extension.cnts_project_not_found" });
  });
});
