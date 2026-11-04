"use client";
import Link from "next/link";
import { DetailPage, LabelValueBlock } from "@/components/patterns/detail";
import { StatusPill } from "@/components/patterns/list";
import { ReadOnlyNotice, dt } from "@/kn/cycle/common";
import { FORM_TONE, vnDate } from "../periods/api";
import type { KhtcFormSheet } from "../forms/api";
import { inboxBack, inboxHref, type InboxRoute } from "./inbox-routes";

export function InboxDetail({ route, sheet: f }: { route: InboxRoute; sheet: KhtcFormSheet }) {
  const entry = `/kn/khtc/phieu/${route.id}?${new URLSearchParams({ department: f.department })}`;
  return <DetailPage title={`Chi tiết phiếu · ${f.period.name}`} back={{ href: inboxBack(route), label: "Danh sách phiếu" }} badge={<StatusPill tone={FORM_TONE[f.state]}>{f.state_label}</StatusPill>} actions={<>
    <Link className="btn btn-o" href={inboxHref(route.base, route.id, route.department, route.view, true)}>Lịch sử gửi và rà soát</Link>
    <Link className="btn btn-p" href={entry}>{f.editable ? "Mở phiếu để nhập" : "Xem nội dung phiếu"}</Link>
  </>}>
    <ReadOnlyNotice>Trang này chỉ đọc thông tin phiếu. Nhập và gửi tại màn phiếu hiện hữu.</ReadOnlyNotice>
    <LabelValueBlock title="Thông tin phiếu" items={[
      { label: "Phòng", value: f.department_name }, { label: "Kỳ", value: f.period.name },
      { label: "Khoảng thời gian", value: `${vnDate(f.period.from_date)} – ${vnDate(f.period.to_date)}` },
      { label: "Hạn nộp", value: dt(f.period.due_at) }, { label: "Lần gửi", value: `${f.attempt}/${f.period.max_submissions || "∞"}` },
      { label: "Mẫu phiếu", value: `${f.template.code} v${f.template.version}` },
      { label: "Kế hoạch năm", value: f.plan ? `Phiên bản ${f.plan.version}${f.plan.decision_no ? ` · ${f.plan.decision_no}` : ""}` : "Chưa có thông tin" },
      { label: "Nội dung hiện tại", value: f.content_source === "draft" ? "Nháp trên máy chủ" : f.content_source === "submitted" ? "Lần gửi gần nhất" : "Chưa nhập" },
      { label: "Lưu nháp lúc", value: f.draft_at ? dt(f.draft_at) : "—" },
    ]} />
    {f.request_note ? <p role="status">Yêu cầu bổ sung: {f.request_note}</p> : null}
    {f.period.terminal === "closed" ? <p>Kỳ đã đóng — số liệu chỉ đọc.</p> : null}
    <p className="mute">Lịch sử chỉ gồm các lần gửi và kết quả rà soát do máy chủ cung cấp; không phải log đầy đủ các lần sửa nháp.</p>
  </DetailPage>;
}
