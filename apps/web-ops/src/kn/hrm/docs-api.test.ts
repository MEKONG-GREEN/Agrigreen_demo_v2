// @vitest-environment node
// Môi trường node: FormData/File/Request của undici (jsdom File không đi qua Request của Node) — tải lên nhiều phần.
import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetHrm } from "@/api/mock/kn-hrm";
import { resetHrmDocs } from "@/api/mock/kn-hrm-docs";
import { resetHrmStaff } from "@/api/mock/kn-hrm-staff";
import { ApiProblem } from "@/api/problem";
import { createDocApi, fileSize } from "./doc-api";

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

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

const pdf = () =>
  new File(
    [new TextEncoder().encode("%PDF-1.7\nhello\n%%EOF\n")],
    "QĐ 45.pdf",
    { type: "application/pdf" },
  );

describe("kho tài liệu nhân sự — tiện ích", () => {
  it("dung lượng tệp", () => {
    expect(fileSize(512)).toBe("512 B");
    expect(fileSize(2048)).toBe("2 KB");
    expect(fileSize(3 * 1024 * 1024)).toBe("3,0 MB");
  });
});

describe("kho tài liệu nhân sự — backend giả lập theo hợp đồng /v1/hrm/docs*", () => {
  beforeEach(() => {
    resetHrm();
    resetHrmStaff();
    resetHrmDocs();
  });

  it("Admin: cây mặc định, tải lên (định dạng), lọc, xem/tải ghi nhật ký, sửa phân loại, xóa = ẩn", async () => {
    const { da } = await clientFor("kn-admin", "asg-admin");
    const t = await da.tree();
    expect(t.items).toHaveLength(7);
    expect(t.can_create_root).toBe(true);
    expect((await da.installDefaults()).created).toBe(0);
    const nangBac = t.items.find((f) => f.system_key === "nang_bac")!;
    expect((await da.list({})).total).toBe(2);

    const sub = await da.createFolder({
      parent_id: nangBac.folder_id,
      name: "Năm 2026",
    });
    expect(
      await problemCode(
        da.createFolder({ parent_id: nangBac.folder_id, name: "năm 2026" }),
      ),
    ).toBe("hrm.folder_exists");
    const up = await da.upload(
      {
        folder_id: sub.folder_id,
        title: "QĐ nâng bậc đợt 2",
        doc_type: "nang_bac",
        doc_no: "45/QĐ-TTKN",
        issued_on: "2026-09-01",
      },
      pdf(),
    );
    expect(
      await problemCode(
        da.upload(
          { folder_id: sub.folder_id, title: "Lạ", doc_type: "khac" },
          new File([new Uint8Array([0x4d, 0x5a])], "a.exe"),
        ),
      ),
    ).toBe("hrm.doc_format");
    const l = await da.list({
      folder: nangBac.folder_id,
      recursive: "1",
      q: "45/qđ",
    });
    expect(l.items.map((x) => x.title)).toEqual(["QĐ nâng bậc đợt 2"]);
    expect(l.items[0]!.year).toBe(2026);
    expect(l.items[0]!.folder_path).toEqual([
      "Quyết định nâng bậc",
      "Năm 2026",
    ]);

    const c = await da.content(up.doc_id, true);
    expect(c.name).toBe("QĐ 45.pdf");
    expect(await c.blob.text()).toContain("%PDF-1.7");
    await da.classify(up.doc_id, {
      title: "QĐ nâng bậc đợt 2/2026",
      doc_type: "nang_bac",
      doc_no: "45/QĐ-TTKN",
      issued_on: "2026-09-01",
    });
    const d = await da.get(up.doc_id);
    expect(d.access?.map((a) => a.action)).toEqual(["download"]);
    expect(d.history.map((h) => h.change)).toEqual(["created", "updated"]);
    expect(d.history[1]!.changed_fields).toEqual(["title"]);

    expect(await problemCode(da.hide(up.doc_id, true, "ngắn"))).toBe(
      "validation_failed",
    );
    await da.hide(up.doc_id, true, "Tải nhầm bản chưa ký");
    expect((await da.list({ folder: sub.folder_id })).total).toBe(0);
    expect(
      (await da.list({ folder: sub.folder_id, include_hidden: "1" })).total,
    ).toBe(1);
    expect(
      await problemCode(
        da.hideFolder(nangBac.folder_id, true, "Dọn thư mục cũ"),
      ),
    ).toBe("hrm.folder_not_empty");
  });

  it("Cấp Tỉnh ngoài HCTH không thấy (ma trận FN-20); quyền thư mục mở cho nhóm; gốc chỉ Admin", async () => {
    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect((await tt.da.tree()).items).toHaveLength(0);
    expect((await tt.da.list({})).total).toBe(0);
    expect(await problemCode(tt.da.createFolder({ name: "Gốc" }))).toBe(
      "forbidden",
    );
    const { da } = await clientFor("kn-admin", "asg-admin");
    const khac = (await da.tree()).items.find((f) => f.system_key === "khac")!;
    expect(
      await problemCode(
        da.setACL(khac.folder_id, { "role:admin": { "docs.view": "none" } }),
      ),
    ).toBe("validation_failed");
    await da.setACL(
      khac.folder_id,
      { "role:tinh": { "docs.view": "all" } },
      "Mở tài liệu chung",
    );
    const t = await tt.da.tree();
    expect(t.items.map((f) => f.name)).toEqual(["Tài liệu nhân sự khác"]);
    expect(t.items[0]!.can_manage).toBe(false);
    expect((await tt.da.list({})).items.map((x) => x.title)).toEqual([
      "Biên bản họp giao ban phòng Chăn nuôi",
    ]);
    expect(
      (await da.folderHistory(khac.folder_id)).items.map((h) => h.change),
    ).toEqual(["created", "acl"]);
  });
});
