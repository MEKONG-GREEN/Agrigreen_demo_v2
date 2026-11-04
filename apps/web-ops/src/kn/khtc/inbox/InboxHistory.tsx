"use client";
import Link from "next/link";
import { DetailPage } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { dt } from "@/kn/cycle/common";
import type { KhtcFormSheet } from "../forms/api";
import { inboxHref, type InboxRoute } from "./inbox-routes";

export function InboxHistory({ route, sheet }: { route: InboxRoute; sheet: KhtcFormSheet }) {
  return <DetailPage title={`Lịch sử gửi và rà soát · ${sheet.period.name}`} back={{ href: inboxHref(route.base, route.id, route.department, route.view), label: "Chi tiết phiếu" }}>
    <p>Phiếu {sheet.department_name}. Chỉ hiển thị dữ liệu do máy chủ trả về; chưa có thông tin người thực hiện hoặc nội dung riêng của từng lần gửi.</p>
    <DataTable caption="Các lần gửi phiếu" rows={sheet.submissions} rowKey={r => r.id} empty="Chưa gửi lần nào." columns={[
      { key: "attempt", header: "Lần gửi", cell: r => r.attempt },
      { key: "submitted", header: "Thời điểm gửi", cell: r => dt(r.submitted_at) },
      { key: "late", header: "Nộp trễ", cell: r => r.late ? "Có" : "Không" },
      { key: "decision", header: "Kết quả rà soát", cell: r => r.decision === "returned" ? "Yêu cầu bổ sung" : r.decision === "accepted" ? "Đã xác nhận" : "Chưa có kết quả" },
      { key: "reviewed", header: "Thời điểm rà soát", cell: r => r.reviewed_at ? dt(r.reviewed_at) : "—" },
      { key: "note", header: "Nội dung rà soát", cell: r => r.review_note || "—" },
    ]} />
    <Link className="btn btn-o" href={inboxHref(route.base, route.id, route.department, route.view)}>Quay lại chi tiết</Link>
  </DetailPage>;
}
