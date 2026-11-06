import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsCampaigns } from "@/api/mock/kn-cnts-campaign";
import { ApiProblem } from "@/api/problem";
import { copyErrors, copyPayload, createCampaignApi, emptyForm, formErrors, formOf, payload, percent, reasonOk, type CampaignForm } from "./api";

function client(personaId: string) {
  return createCampaignApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => null, fetch: mockFetchNoDelay }));
}

const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)));

const filled = (patch: Partial<CampaignForm> = {}): CampaignForm => ({
  ...emptyForm(), name: "Điều tra tổng đàn 01/10/2026", template_code: "CNTS-DT-TONGDAN", communes: "91307, 91310 91313", from_date: "2026-10-01",
  to_date: "2026-10-10", due_at: "2026-10-10T17:00", owner_ref: "0193a000-0000-7000-8000-00000000c236", ...patch,
});

describe("Chiến dịch điều tra — hàm phía máy khách", () => {
  it("kiểm biểu nhập và dựng thân request", () => {
    expect(formErrors(filled())).toEqual({});
    expect(formErrors(filled({ communes: "" })).communes).toBeDefined();
    expect(formErrors(filled({ to_date: "2026-09-30" })).to_date).toBeDefined();
    expect(formErrors(filled({ template_code: "" })).template_code).toBeDefined();
    expect(formErrors(filled({ remind_before_hours: "200" })).remind_before_hours).toBeDefined();
    const p = payload(filled());
    expect(p).toMatchObject({ communes: ["91307", "91310", "91313"], regions: [], field: "chan_nuoi", remind_before_hours: 24 });
    expect(formErrors(filled({ owner_ref: "" })).owner_ref).toBeDefined();
    expect(p.owner_ref).toBe("0193a000-0000-7000-8000-00000000c236");
    const cf = { name: " ", from_date: "2026-11-01", to_date: "2026-10-01", due_at: "", owner_ref: "" };
    expect(copyErrors(cf)).toEqual({ to_date: "Đến ngày ≥ từ ngày", due_at: "Bắt buộc" });
    expect(copyPayload({ ...cf, to_date: "2026-11-05", due_at: "2026-11-05T17:00" })).not.toHaveProperty("name");
    // #192 FN-21 B1: phiên bản biểu đã chọn; bỏ trống = mới nhất lúc phát hành.
    expect(payload(filled({ template_version: "2" })).template_version).toBe(2);
    expect(payload(filled()).template_version).toBeNull();
    expect(reasonOk("ngắn")).toBe(false);
    expect(reasonOk("Xã mất điện nhiều ngày")).toBe(true);
  });
});

describe("Chiến dịch điều tra — backend giả lập /v1/extension/cnts/campaigns*", () => {
  beforeEach(() => resetCntsCampaigns());

  it("Nháp → Phát hành → nhắc, gia hạn → Đóng đợt → Mở lại; kết quả tách lũy kế", async () => {
    const tinh = client("kn-binh");
    const c = await tinh.create(payload(filled({ template_version: "2" })));
    expect(c).toMatchObject({ state: "draft", version: 1, tasks: 3, forms_period_id: null, template_version: 2 });
    expect(formOf(c).template_version).toBe("2");
    const v2 = await tinh.update(c.id, payload({ ...formOf(c), name: "Điều tra tổng đàn sau lũ" }));
    expect(v2.version).toBe(2);
    expect(await code(tinh.close(c.id))).toBe("extension.cnts_campaign_state");
    const pub = await tinh.publish(c.id);
    expect(pub.state).toBe("collecting");
    expect(pub.template_version).toBe(2); // #192: phát hành đúng phiên bản đã chọn
    expect(pub.period_key).toMatch(/^DT-20261001-[0-9a-f]{6}$/);
    expect(percent(pub)).toBeGreaterThan(0);
    expect(await code(tinh.update(c.id, payload(filled())))).toBe("extension.cnts_campaign_state");
    expect(((await tinh.remind(c.id)) as { reminded: number }).reminded).toBe(1);
    expect(await code(tinh.extend(c.id, "2026-10-20T10:00:00Z", "ngắn"))).toBe("validation_failed");
    expect((await tinh.extend(c.id, "2026-10-20T10:00:00Z", "Xã 91310 mất điện nhiều ngày")).due_at).toBe("2026-10-20T10:00:00Z");
    const closed = await tinh.close(c.id);
    expect(closed.state).toBe("closed");
    expect(closed.progress?.missed).toBe(1);
    const r = await tinh.results(c.id);
    expect(r).toMatchObject({ basis: "closed", separate_from_cumulative: true, period_kind: "adhoc" });
    const re = await tinh.reopen(c.id, "2026-10-25T10:00:00Z", "Bổ sung số liệu theo chỉ đạo");
    expect(re.state).toBe("collecting");
    expect(re.events.map((e) => e.action)).toEqual(["published", "reminded", "extended", "closed", "reopened"]);
    expect((await tinh.list()).map((x) => x.id)).toEqual([c.id]);
  });

  it("#236: người phụ trách bắt buộc, phải trong danh sách; sao chép đợt ra Nháp mới không mang tiến độ", async () => {
    const tinh = client("kn-binh");
    expect(await code(tinh.create(payload(filled({ owner_ref: "" }))))).toBe("validation_failed");
    expect(await code(tinh.create({ ...payload(filled()), owner_ref: "0193a000-0000-7000-8000-0000000000ff" }))).toBe("extension.cnts_campaign_owner");
    const owners = await tinh.owners();
    expect(owners.length).toBe(3);
    const c = await tinh.create(payload(filled()));
    expect(c).toMatchObject({ can_manage: true, owner: { display: "Trần Thị Hoa (giả lập)" } });
    await tinh.publish(c.id);
    const cp = await tinh.copy(c.id, { from_date: "2026-11-01", to_date: "2026-11-05", due_at: "2026-11-05T10:00:00Z" });
    expect(cp).toMatchObject({ state: "draft", version: 1, name: c.name, communes: c.communes, owner: c.owner, progress: null, events: [] });
    expect(cp.id).not.toBe(c.id);
  });
});
