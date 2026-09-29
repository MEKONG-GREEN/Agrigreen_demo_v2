import { describe, expect, it } from "vitest";
import { deepLinkHref } from "./deepLink";

const ID = "01927b3e-0ff1-7000-8000-00000000abcd";

describe("deepLinkHref — liên kết thông báo sang trang web", () => {
  it("các loại có sẵn: chỉ đạo, tài liệu, nhiệm vụ leo thang, xung đột, tổ chức", () => {
    expect(deepLinkHref(`/extension/directives/${ID}`, "directive")).toBe(`/chi-dao?id=${ID}`);
    expect(deepLinkHref(`/documents/${ID}`, "document")).toBe(`/thu-vien?doc=${ID}`);
    expect(deepLinkHref(`/extension/tasks/${ID}`, "task_escalated")).toBe(`/nhiem-vu?task=${ID}`);
    expect(deepLinkHref(`/sync/conflicts/${ID}`, "sync_conflict")).toBe(`/quan-tri/xung-dot?id=${ID}`);
    expect(deepLinkHref(`/orgs/${ID}`, "org_deactivated")).toBe(`/quan-tri/to-chuc?org=${ID}`);
  });

  it("HTX US-LOG-05: farm_log_pending → trang phê duyệt nhật ký", () => {
    expect(deepLinkHref(`/cropcycle/farm-logs/${ID}`, "farm_log_pending")).toBe("/htx/phe-duyet");
  });

  it("HTX US-LOG-05 AC-3: farm_log_reviewed → trang nhật ký canh tác", () => {
    expect(deepLinkHref(`/cropcycle/farm-logs/${ID}`, "farm_log_reviewed")).toBe("/htx/nhat-ky");
  });

  it("HTX US-SEASON-01: season_opened → trang mùa vụ", () => {
    expect(deepLinkHref(`/cropcycle/seasons/${ID}`, "season_opened")).toBe("/htx/mua-vu");
  });

  it("KN US-HTX-05: household_registered / household_membership_changed → danh sách HTX & nông hộ", () => {
    expect(deepLinkHref(`/households/${ID}`, "household_registered")).toBe("/htx");
    expect(deepLinkHref(`/households/${ID}`, "household_membership_changed")).toBe("/htx");
  });

  it("KN US-TASK-01: task_assigned → trang nhiệm vụ mở sẵn nhiệm vụ", () => {
    expect(deepLinkHref(`/extension/tasks/${ID}`, "task_assigned")).toBe(`/nhiem-vu?task=${ID}`);
  });

  it("không có kind vẫn nhận dạng theo đường dẫn (nhật ký mặc định → phê duyệt)", () => {
    expect(deepLinkHref(`/cropcycle/farm-logs/${ID}`)).toBe("/htx/phe-duyet");
    expect(deepLinkHref(`/extension/tasks/${ID}?x=1#y`)).toBe(`/nhiem-vu?task=${ID}`);
  });

  it("deep_link trống/lạ: dùng trang mặc định theo kind của App HTX", () => {
    expect(deepLinkHref("", "farm_log_pending")).toBe("/htx/phe-duyet");
    expect(deepLinkHref(null, "farm_log_reviewed")).toBe("/htx/nhat-ky");
    expect(deepLinkHref("/unknown/path", "season_opened")).toBe("/htx/mua-vu");
    expect(deepLinkHref(undefined, "household_registered")).toBe("/htx");
    expect(deepLinkHref("/x", "household_membership_changed")).toBe("/htx");
  });

  it("kind lạ và đường dẫn lạ → null (chỉ mở hộp thư)", () => {
    expect(deepLinkHref("/unknown/path", "unknown_kind")).toBeNull();
    expect(deepLinkHref("https://evil.example/extension/tasks/abc", "task_assigned")).toBeNull();
    expect(deepLinkHref(`/extension/tasks/${ID}/../../x`, "task_assigned")).toBeNull();
    expect(deepLinkHref(null)).toBeNull();
    expect(deepLinkHref("", "toString")).toBeNull();
  });
});
