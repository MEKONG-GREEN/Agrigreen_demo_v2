"use client";

import Link from "next/link";
import { KN_DEPARTMENTS } from "@/api/mock/kn-accounts";
import { Icon } from "@/components/Icon";
import { AlertList, SectionTitle, type DashboardAlert } from "@/components/patterns/dashboard";
import { DemoNotice, Greeting, RequiresMock } from "@/kn/ui";

const STATS = [
  { label: "Tài khoản hoạt động", value: "132", foot: "4 tài khoản đang khóa" },
  { label: "Phòng ban đang áp dụng", value: `${KN_DEPARTMENTS.filter((d) => d.status === "active").length} / ${KN_DEPARTMENTS.length}`, foot: "Phòng Trồng trọt" },
  { label: "Mẫu báo cáo", value: "14", foot: "1 bản nháp chưa phát hành" },
  { label: "Báo cáo đã nộp tuần 36", value: "87 / 102", foot: "15 xã chưa nộp" },
];

const SYSTEM_ALERTS: DashboardAlert[] = [
  { id: "s1", level: "attention", title: "1 tài khoản Cấp Xã chưa gán xã/phường", description: "Phạm Văn Lộc · Phòng Trồng trọt", at: "2026-09-08T01:00:00Z", href: "/quan-tri/tai-khoan" },
  { id: "s2", level: "attention", title: "3 xã chưa có cán bộ phụ trách Phòng Trồng trọt", description: "Khánh Bình, Nhơn Hội, Phú Hữu", at: "2026-09-08T01:00:00Z", href: "/quan-tri/to-chuc" },
  { id: "s3", level: "info", title: "Mẫu CC01 (Báo cáo gửi Chi cục) đang ở bản nháp", description: "Phòng Trồng trọt › Mẫu báo cáo", at: "2026-09-07T09:20:00Z", href: "/quan-tri/mau-bao-cao" },
];

/** Tổng quan hệ thống Admin (Hình 3c) — mẫu trang đợt 1, số liệu minh họa. */
export default function KnAdminOverviewPage() {
  return (
    <RequiresMock title="Tổng quan hệ thống">
      <Greeting />
      <DemoNotice />
      <div className="hot-grid">
        {STATS.map((s) => (
          <div className="card hot" key={s.label} style={{ minHeight: 0, paddingBottom: 14 }}>
            <div className="lb">{s.label}</div>
            <div className="vl">{s.value}</div>
            <div className="dl neutral">{s.foot}</div>
          </div>
        ))}
      </div>
      <SectionTitle>Phòng ban</SectionTitle>
      <div className="dept-grid">
        {KN_DEPARTMENTS.slice(0, 3).map((d) =>
          d.status === "active" ? (
            <div className="card dept-card is-on" key={d.code}>
              <h3>
                <span className="row" style={{ gap: 8 }}>
                  <Icon name="sprout" /> {d.name}
                </span>
                <span className="pill pill-ok">Đang áp dụng</span>
              </h3>
              <dl>
                <dt>Tài khoản</dt>
                <dd>118</dd>
                <dt>Mẫu báo cáo</dt>
                <dd>14</dd>
                <dt>Đợt phát hành đang mở</dt>
                <dd>3</dd>
                <dt>Xã chưa nộp tuần 36</dt>
                <dd>15</dd>
              </dl>
              <p style={{ marginTop: 10 }}>
                <Link className="link-all" href={`/kn/phong-ban/${d.code}/theo-doi-nop`}>
                  Mở menu phòng ban →
                </Link>
              </p>
            </div>
          ) : (
            <div className="card dept-card is-off" key={d.code}>
              <h3>
                <span className="row" style={{ gap: 8 }}>
                  <Icon name="building" /> {d.name}
                </span>
                <span className="pill">Chưa mở</span>
              </h3>
              <p className="small">Module chưa mở. Admin bật &quot;Đang áp dụng&quot; ở Quản trị hệ thống › Phòng ban.</p>
            </div>
          ),
        )}
      </div>
      <AlertList title="Cảnh báo hệ thống" alerts={SYSTEM_ALERTS} />
    </RequiresMock>
  );
}
