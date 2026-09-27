import { Icon } from "./Icon";

/** Huy hiệu khi số liệu chưa đầy đủ (còn đơn vị chưa gửi báo cáo). */
export function ProvisionalBadge({ missing, total }: { missing: number; total: number }) {
  return (
    <span className="badge-prov" role="status" title={`Còn ${missing}/${total} đơn vị chưa gửi số liệu kỳ này`}>
      <Icon name="info" size={15} />
      Số liệu tạm tính
      <span className="sr-only">
        : còn {missing} trên {total} đơn vị chưa gửi số liệu
      </span>
    </span>
  );
}
