import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, unwrap } from "@/api/client";
import { mockAdminFetchNoDelay } from "@/api/mock/handler";
import { mockDeliveryHealth } from "@/api/mock/notify-health";
import { mockToken } from "@/api/mock/personas";
import { DeliveryHealthBanner } from "./DeliveryHealthBanner";
import { channelAlert, deliveryHealthAlerts, type ChannelHealth } from "./deliveryHealth";

const admin = (persona: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });
const auth = { adminApi: admin("setter"), principal: { roles: ["platform_admin"] as string[], mfa: true } };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const ok: ChannelHealth = { status: "ok", sent: 3, failed: 0, dropped: 0, pending: 0, disabled: 0, disabled_reason: null, last_disabled_at: null, last_sent_at: "2026-10-24T08:00:00Z", warning: null };

describe("P0-NOTIFY-FAILCLOSED: cảnh báo kênh gửi trên màn quản trị", () => {
  beforeEach(() => {
    auth.adminApi = admin("setter");
    auth.principal = { roles: ["platform_admin"], mfa: true };
  });

  it("kênh TẮT → câu tiếng Việt nêu lý do; kênh bình thường/chưa có lần gửi → không cảnh báo", () => {
    expect(channelAlert("Email", { ...ok, status: "disabled", disabled_reason: "smtp_not_configured" })).toBe(
      "Email đang TẮT: chưa cấu hình SMTP — thông báo không được gửi",
    );
    expect(channelAlert("Thông báo đẩy", { ...ok, status: "disabled", disabled_reason: "fcm_not_configured" })).toBe(
      "Thông báo đẩy đang TẮT: chưa cấu hình FCM — thông báo không được gửi",
    );
    expect(channelAlert("Email", { ...ok, status: "disabled", disabled_reason: "la" })).toContain("chưa cấu hình kênh gửi");
    expect(channelAlert("Email", { ...ok, status: "failing", warning: "Email đang gửi lỗi" })).toBe("Email đang gửi lỗi");
    expect(channelAlert("Email", ok)).toBeNull();
    expect(channelAlert("Email", { ...ok, status: "idle" })).toBeNull();
    expect(deliveryHealthAlerts(null)).toEqual([]);
    expect(deliveryHealthAlerts({ window_days: 7, email: ok, push: ok })).toEqual([]);
  });

  it("mock Admin API: quản trị đọc được, vai thường bị từ chối", async () => {
    const h = await unwrap(admin("setter").GET("/v1/notify/delivery-health"));
    expect(deliveryHealthAlerts(h)).toEqual([{ channel: "push", text: "Thông báo đẩy đang TẮT: chưa cấu hình FCM — thông báo không được gửi" }]);
    await expect(unwrap(admin("api-admin").GET("/v1/notify/delivery-health"))).rejects.toMatchObject({ status: 403 });
    expect(mockDeliveryHealth().push.status).toBe("disabled");
  });

  it("banner hiện cho quản trị đã qua MFA, ẩn với vai không được xem", async () => {
    const { unmount } = render(<DeliveryHealthBanner />);
    const banner = await screen.findByTestId("delivery-health-banner");
    expect(banner.textContent).toContain("Thông báo đẩy đang TẮT: chưa cấu hình FCM — thông báo không được gửi");
    expect(banner.textContent).not.toContain("Email đang TẮT");
    unmount();

    auth.principal = { roles: ["kn_officer"], mfa: true };
    render(<DeliveryHealthBanner />);
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId("delivery-health-banner")).toBeNull();
  });
});
