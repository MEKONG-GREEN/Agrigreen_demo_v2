import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, createAdminApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { COMMUNES, COOPERATIVES, ORGS, PROVINCES } from "@/api/mock/data";
import { NewAnchorForm } from "./NewAnchorForm";

let dropResponse = false;
const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockFetchNoDelay });
const adminApi = createAdminApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: async req => {
  const response = await mockAdminFetchNoDelay(req);
  if (dropResponse && req.url.endsWith("/v1/htx-anchors")) { dropResponse = false; throw new TypeError("network response lost"); }
  return response;
} });
const auth = { api, adminApi, principal: { roles: ["platform_admin"] as string[] } };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
const orgCount = ORGS.length;
const coopCount = COOPERATIVES.length;
beforeEach(() => { dropResponse = false; auth.principal.roles = ["platform_admin"]; });
afterEach(() => { ORGS.splice(orgCount); COOPERATIVES.splice(coopCount); });

async function fill(name: string) {
  await userEvent.type(screen.getByLabelText("Tên HTX mới"), name);
  const province = PROVINCES[1]!;
  const commune = COMMUNES.find(c => c.province_code === province.code)!;
  await screen.findByRole("option", { name: province.name });
  await userEvent.selectOptions(screen.getByLabelText("Tỉnh của HTX mới"), province.code);
  await screen.findByRole("option", { name: commune.name });
  await userEvent.selectOptions(screen.getByLabelText("Xã của HTX mới"), commune.code);
}

describe("Neo HTX: tạo Nút mới trong cùng luồng", () => {
  it("vai KN không thấy form tạo mới", () => {
    auth.principal.roles = ["kn_province_admin"];
    render(<NewAnchorForm onCreated={vi.fn()} />);
    expect(screen.queryByRole("form", { name: "Tạo HTX và lập neo" })).toBeNull();
  });
  it("chọn tỉnh/xã, tạo hồ sơ và neo mà không gọi POST tổ chức chung", async () => {
    const done = vi.fn();
    render(<NewAnchorForm onCreated={done} />);
    const name = "HTX Neo form thử nghiệm";
    await fill(name);
    await userEvent.click(screen.getByRole("button", { name: "Tạo HTX và lập neo" }));
    await waitFor(() => expect(done).toHaveBeenCalledOnce());
    expect(ORGS.filter(o => o.name === name)).toHaveLength(1);
    const made = COOPERATIVES.find(c => c.api.name === name)!;
    expect(made.api.confirmation_status).toBe("unconfirmed");
    expect(made.province_name).toBe(PROVINCES[1]!.name);
    expect(made.api.province_name).toBe(PROVINCES[1]!.name);
    expect(made.lnglat).toEqual(COMMUNES.find(c => c.code === made.api.admin_unit_code)!.center);
  });
  it("ORG-NEO-002: mất phản hồi sau khi máy chủ ghi; thử lại không tạo HTX trùng", async () => {
    const done = vi.fn();
    render(<NewAnchorForm onCreated={done} />);
    const name = "HTX Neo mất phản hồi thử nghiệm";
    await fill(name);
    dropResponse = true;
    await userEvent.click(screen.getByRole("button", { name: "Tạo HTX và lập neo" }));
    await screen.findByRole("button", { name: "Thử lại để hoàn tất" });
    expect(screen.getByLabelText("Tên HTX mới")).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Thử lại để hoàn tất" }));
    await waitFor(() => expect(done).toHaveBeenCalledOnce());
    expect(ORGS.filter(o => o.name === name)).toHaveLength(1);
  });
});
