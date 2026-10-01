"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { config } from "@/config";
import { findActive } from "@/shell/menu";
import { contextShortLabel } from "./session";

/** Cấp Quốc gia chỉ xem: ẩn mọi nút Thêm/Sửa/Xóa (BRD §4.2). */
export function useKnReadOnly(): boolean {
  const { kn } = useAuth();
  return kn.context?.role === "kn_tw";
}

/** Nhãn mục menu ứng với trang hiện tại (tiêu đề trang giữ chỗ). */
export function useMenuLabel(): string | null {
  const { kn } = useAuth();
  const pathname = usePathname() ?? "";
  return kn.menu ? (findActive(kn.menu, pathname)?.node.label ?? null) : null;
}

/** "Xin chào, <tên>" + nhãn vai trò · phòng ban (Hình 3c–3e). */
export function Greeting() {
  const { principal, kn } = useAuth();
  return (
    <div className="pg-head">
      <h1>
        Xin chào, {principal?.displayName}{" "}
        {kn.context ? <span className="role-pill">{contextShortLabel(kn.context)}</span> : null}
      </h1>
    </div>
  );
}

/** Ghi chú mẫu trang đợt 1: dữ liệu minh họa, chỉ ở chế độ giả lập. */
export function DemoNotice({ children }: { children?: ReactNode }) {
  return (
    <div className="alert alert-info" role="note" style={{ marginBottom: 16 }}>
      <Icon name="info" />
      <span>
        <b>Mẫu trang khung P6 — dữ liệu minh họa.</b>
        {children ?? " Màn hình đầy đủ dựng ở đợt sau trên hợp đồng API P4 (tạm, chờ khách)."}
      </span>
    </div>
  );
}

/** Ngoài chế độ giả lập: chưa có API → không hiện số minh họa. */
export function RequiresMock({ children, title }: { children: ReactNode; title: string }) {
  if (config.mock) return <>{children}</>;
  return <ComingSoon title={title} />;
}

export function ComingSoon({ title }: { title: string }) {
  return (
    <>
      <div className="pg-head">
        <h1>{title}</h1>
      </div>
      <div className="card sec">
        <p>
          <b>Màn hình đang được xây dựng.</b>
        </p>
        <p className="mute">Khung giao diện, menu và phân quyền đã sẵn sàng; nội dung màn hình dựng ở đợt tiếp theo của module Phòng Trồng trọt v7.7.</p>
      </div>
    </>
  );
}
