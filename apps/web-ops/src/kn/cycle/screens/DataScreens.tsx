"use client";

// Cấp Xã › Dữ liệu (FN-XA-03.03..03.06) — XEM/DANH SÁCH trên API đang có (pf/party, pf/mechanization, pf/geo).
// Thêm/sửa HTX, máy móc theo HTX, thuộc tính thửa v7.7, vật tư: API phía máy chủ thuộc P7 (migration 000077–000079)
// — chưa có ở Application API; màn ghi rõ khoảng trống, không tự dựng backend.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { BackLink, DetailLayout, LabelValueBlock, LogPanel, RelatedTabs } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, RowActions, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import type { components } from "@/api/generated/application";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { LoadState, num, ymdLocal, useLoad } from "../common";

type Coop = components["schemas"]["Cooperative"];

export function GapNotice({ what }: { what: string }) {
  return (
    <div className="alert alert-warn" role="note" style={{ marginBottom: 12 }}>
      <Icon name="info" />
      <span>
        <b>Chỉ xem.</b> {what} chưa có API ghi ở Application API — phần máy chủ thuộc gói P7 (KN v7.7). Danh sách dưới đây đọc từ API hiện có.
      </span>
    </div>
  );
}

function useCoops(commune: string) {
  const { api, kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  return useLoad(async () => {
    const codes = commune ? [commune] : communes.map((c) => c.code);
    const pages = await Promise.all(codes.map((c) => unwrap(api.GET("/v1/cooperatives", { params: { query: { admin_unit: c, include_inactive: true, limit: 200 } } }))));
    return pages.flatMap((p) => p.items);
  }, [commune, communes.map((c) => c.code).join()]);
}

/** FN-XA-03.03 Danh sách HTX (Hình 16a). */
export function CoopsScreen() {
  const { kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const [commune, setCommune] = useState("");
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const s = useCoops(commune);
  const columns: Column<Coop>[] = [
    { key: "name", header: "Tên HTX", cell: (c) => c.name },
    { key: "commune", header: "Xã/phường", cell: (c) => c.commune_name ?? c.admin_unit_code },
    { key: "area", header: "Diện tích (ha)", cell: (c) => num(c.area_ha as string | null), className: "num" },
    { key: "members", header: "Nông hộ", cell: (c) => c.member_households, className: "num" },
    { key: "status", header: "Trạng thái", cell: (c) => <StatusPill tone={c.active ? "ok" : "neutral"}>{c.active ? "Hoạt động" : "Ngừng hoạt động"}</StatusPill> },
    { key: "act", header: "Thao tác", cell: (c) => <RowActions label={`HTX ${c.name}`} viewHref={`/kn/du-lieu/htx/${c.ref}`} /> },
  ];
  return (
    <>
      <PageHeader title="Danh sách HTX" />
      <GapNotice what="Thêm, sửa, vô hiệu hóa HTX và nông hộ của Cấp Xã (FN-XA-03.03)" />
      <FilterBar onReset={() => (setCommune(""), setStatus(""), setQ(""), setPage(1))}>
        <FilterChip label="Xã/phường" value={commune} onChange={(v) => (setCommune(v), setPage(1))} options={[{ value: "", label: "Tất cả" }, ...communes.map((c) => ({ value: c.code, label: c.name }))]} />
        <FilterChip
          label="Trạng thái"
          value={status}
          onChange={(v) => (setStatus(v), setPage(1))}
          options={[
            { value: "", label: "Tất cả" },
            { value: "active", label: "Hoạt động" },
            { value: "inactive", label: "Ngừng hoạt động" },
          ]}
        />
        <SearchBox value={q} onChange={(v) => (setQ(v), setPage(1))} placeholder="Tìm theo tên HTX" />
      </FilterBar>
      <LoadState s={s}>
        {(items) => {
          const rows = items.filter((c) => (!status || (status === "active") === c.active) && (!q || c.name.toLowerCase().includes(q.toLowerCase())));
          return <DataTable caption="Danh sách HTX" columns={columns} rows={rows.slice((page - 1) * 10, page * 10)} rowKey={(c) => c.ref} page={page} pageSize={10} total={rows.length} onPage={setPage} empty="Xã phụ trách chưa có HTX." />;
        }}
      </LoadState>
    </>
  );
}

/** FN-XA-03.03 Chi tiết HTX (Hình 16b) — chỉ xem. */
export function CoopDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const { api } = useAuth();
  const s = useLoad(() => unwrap(api.GET("/v1/cooperatives/{ref}", { params: { path: { ref: id } } })), [id]);
  useBreadcrumbTail(s.data ? [{ label: s.data.name }] : []);
  return (
    <>
      <BackLink href="/kn/du-lieu/htx">Danh sách HTX</BackLink>
      <LoadState s={s}>
        {(c) => (
          <>
            <PageHeader title={c.name} />
            <GapNotice what="Sửa HTX, thêm nông hộ, vô hiệu hóa" />
            <DetailLayout
              main={
                <>
                  <LabelValueBlock title="Thông tin chung" items={[{ label: "Mã HTX", value: c.code }, { label: "Mã số thuế", value: c.tax_code }, { label: "Ngày thành lập", value: c.established_on }, { label: "Trạng thái", value: c.active ? "Hoạt động" : "Ngừng hoạt động" }]} />
                  <LabelValueBlock title="Địa bàn – sản xuất" items={[{ label: "Xã/phường", value: c.commune_name }, { label: "Tỉnh", value: c.province_name }, { label: "Diện tích (ha)", value: num(c.area_ha as string | null) }, { label: "Địa chỉ", value: c.address }]} />
                  <RelatedTabs
                    tabs={[
                      { id: "hh", label: `Nông hộ (${c.member_households})`, content: <p className="mute">Danh sách nông hộ (SĐT che một phần) đọc qua kho định danh — màn quản lý nông hộ ở App HTX.</p> },
                      { id: "mm", label: "Máy móc", content: <Link href="/kn/du-lieu/may-moc">Xem máy móc HTX →</Link> },
                      { id: "th", label: `Thửa ruộng (${c.digitized_plots ?? "—"})`, content: <Link href="/kn/du-lieu/ve-thua">Xem thửa ruộng →</Link> },
                    ]}
                  />
                </>
              }
              log={<LogPanel entries={[]} empty="Log chỉnh sửa HTX của Cấp Xã có khi P7 mở API ghi." />}
            />
          </>
        )}
      </LoadState>
    </>
  );
}

function CoopPicker({ value, onChange, coops }: { value: string; onChange: (v: string) => void; coops: Coop[] }) {
  return <FilterChip label="HTX" value={value} onChange={onChange} options={[{ value: "", label: "Chọn HTX" }, ...coops.map((c) => ({ value: c.ref, label: `${c.name} · ${c.commune_name ?? ""}` }))]} />;
}

/** FN-XA-03.04 Máy móc HTX — đọc số máy theo loại (pf/mechanization). */
export function MachinesScreen() {
  const { api } = useAuth();
  const coops = useCoops("");
  const [ref, setRef] = useState("");
  const s = useLoad(ref ? () => unwrap(api.GET("/v1/mechanization/cooperatives/{ref}/machine-count", { params: { path: { ref }, query: { at: ymdLocal(new Date()) } } })) : null, [ref]);
  return (
    <>
      <PageHeader title="Máy móc HTX" />
      <GapNotice what="Khai báo loại máy và số lượng theo HTX của Cấp Xã (FN-XA-03.04, migration 000078)" />
      <FilterBar>
        <CoopPicker value={ref} onChange={setRef} coops={coops.data ?? []} />
      </FilterBar>
      {!ref ? (
        <p className="mute">Chọn một HTX để xem máy theo loại.</p>
      ) : (
        <LoadState s={s}>
          {(m) => (
            <table className="dt" aria-label="Máy theo loại">
              <thead>
                <tr>
                  <th scope="col">Khâu</th>
                  <th scope="col">Loại máy</th>
                  <th scope="col">Số lượng</th>
                </tr>
              </thead>
              <tbody>
                {m.by_stage.map((b) => (
                  <tr key={`${b.farm_stage}-${b.machine_type}`}>
                    <td>{b.farm_stage}</td>
                    <td>{b.machine_type}</td>
                    <td>{b.total}</td>
                  </tr>
                ))}
                <tr>
                  <td colSpan={2}>
                    <b>Tổng</b>
                  </td>
                  <td>
                    <b>{m.total}</b> (hoạt động {m.active})
                  </td>
                </tr>
              </tbody>
            </table>
          )}
        </LoadState>
      )}
    </>
  );
}

/** FN-XA-03.05 Vẽ thửa ruộng — danh sách thửa đã số hóa của HTX (pf/geo). */
export function PlotsScreen() {
  const { api } = useAuth();
  const coops = useCoops("");
  const [ref, setRef] = useState("");
  const s = useLoad(ref ? () => unwrap(api.GET("/v1/cooperatives/{ref}/plots", { params: { path: { ref } } })) : null, [ref]);
  return (
    <>
      <PageHeader title="Vẽ thửa ruộng" />
      <GapNotice what="Vẽ thửa v7.7 (tên nông dân, hình tròn/vuông, diện tích khai báo, migration 000079) trên web; vẽ ngoại tuyến ở ứng dụng di động" />
      <FilterBar>
        <CoopPicker value={ref} onChange={setRef} coops={coops.data ?? []} />
      </FilterBar>
      {!ref ? (
        <p className="mute">Chọn một HTX để xem thửa đã số hóa.</p>
      ) : (
        <LoadState s={s}>
          {(fc) => (
            <ul className="list-plain" aria-label="Thửa ruộng">
              {fc.features.map((f, i) => (
                <li key={i}>{JSON.stringify((f as { properties?: Record<string, unknown> }).properties ?? {}).slice(0, 160)}</li>
              ))}
              {!fc.features.length ? <li className="mute">HTX chưa có thửa đã số hóa.</li> : null}
            </ul>
          )}
        </LoadState>
      )}
    </>
  );
}

/** FN-XA-03.06 Vật tư — chưa có API (P7, migration 000077 material_record). */
export function MaterialsScreen() {
  return (
    <>
      <PageHeader title="Vật tư" />
      <GapNotice what="Ghi nhận vật tư theo xã (FN-XA-03.06, bảng material_record)" />
      <div className="card sec">
        <p>Danh mục Vật tư (loại, ĐVT) quản lý ở Admin › Phòng Trồng trọt › Danh mục nông nghiệp. Sổ ghi nhận vật tư của Cấp Xã chờ API P7.</p>
      </div>
    </>
  );
}
