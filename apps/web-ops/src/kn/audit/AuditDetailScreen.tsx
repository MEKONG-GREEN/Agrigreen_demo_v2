"use client";

// AUDIT-LOG-KN (#73): Chi tiết một dòng nhật ký hệ thống (DetailPage, R-A2). Dòng lấy từ trang danh sách vừa tải (bộ nhớ,
// đã được máy chủ ghi lượt xem) — không gọi lại máy chủ, không có API đọc một dòng. Không còn trong bộ nhớ (tải lại trang)
// → yêu cầu mở lại từ danh sách.
import { useParams } from "next/navigation";
import { DetailPage, LabelValueBlock } from "@/components/patterns/detail";
import { DataTable, StatusPill, type Column } from "@/components/patterns/list";
import { formatDateTime } from "@/lib/format";
import type { AuditChange } from "../cycle/api";
import { AUDIT_BASE } from "./AuditLogScreen";
import { OUTCOME, actionLabel, fieldLabel, moduleLabel, sourceLabel, valueText } from "./labels";
import { findAuditItem } from "./selection";

export function AuditDetailScreen() {
  const params = useParams<{ id: string }>();
  const found = findAuditItem(String(params?.id ?? ""));
  const back = { href: AUDIT_BASE, label: "Nhật ký hệ thống" };
  if (!found) {
    return (
      <DetailPage back={back} title="Chi tiết nhật ký">
        <p className="mute">Không còn dữ liệu của dòng này trong phiên làm việc. Mở lại từ danh sách Nhật ký hệ thống.</p>
      </DetailPage>
    );
  }
  const { item, names } = found;
  const who = (id: string) => names.get(id) ?? id;
  const cols: Column<AuditChange>[] = [
    { key: "field", header: "Trường", cell: (c) => fieldLabel(c.field) },
    { key: "before", header: "Trước", cell: (c) => valueText(c.before, names) },
    { key: "after", header: "Sau", cell: (c) => valueText(c.after, names) },
  ];
  return (
    <DetailPage back={back} title={actionLabel(item.action)} badge={<StatusPill tone={OUTCOME[item.outcome].tone}>{OUTCOME[item.outcome].label}</StatusPill>}>
      <LabelValueBlock
        title="Thông tin chung"
        items={[
          { label: "Thời điểm ghi sổ", value: formatDateTime(item.recorded_at) },
          { label: "Thời điểm thao tác", value: formatDateTime(item.occurred_at) },
          { label: "Người thao tác", value: who(item.actor_ref) },
          { label: "Phòng ban", value: item.department ?? "" },
          { label: "Module", value: moduleLabel(item.module) },
          { label: "Nguồn", value: sourceLabel(item.source) },
          { label: "Mã thao tác", value: item.action },
          { label: "Mã đối tượng", value: item.entity_ref },
          { label: "Mã sự kiện", value: item.event_ref },
        ]}
      />
      <h2>Dữ liệu ghi nhận</h2>
      <DataTable caption="Dữ liệu ghi nhận" columns={cols} rows={item.changes} rowKey={(c) => c.field} empty="Sổ không ghi trường nào cho thao tác này." />
    </DetailPage>
  );
}
