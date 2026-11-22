"use client";

// FN-AD-04.01, BRD KHTC v2.3 / BF-SWEEP-KHTC: danh sách 7 loại; thêm/sửa/xem mở trang riêng.
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { DataTable, PageHeader, RowActions, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { CATALOG_KINDS, KIND_ATTRS, KIND_LABEL, TEMPLATED, TEMPLATE_LABEL, dmy, useKhtcApi, useKhtcRights, type KhtcCatalogItem, type KhtcCatalogKind } from "./api";
import { catalogCreateHref, catalogEditHref, catalogItemHref, catalogListHref, isCatalogKind } from "./catalog";

export function KhtcCatalogScreen() {
  const query = useSearchParams();
  const raw = query.get("kind") ?? "";
  const kind = isCatalogKind(raw) ? raw : "unit";
  const retired = query.get("retired") === "1";
  return <CatalogList key={kind + ":" + retired} kind={kind} retired={retired} />;
}

function CatalogList({ kind, retired }: { kind: KhtcCatalogKind; retired: boolean }) {
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const router = useRouter();
  const s = useLoad(() => api.catalog(kind, retired), [api, kind, retired]);
  const act = useAction();
  const toggle = async (item: KhtcCatalogItem) => {
    const r = await act.run(() => api.reviseCatalog(item.kind, item.code, {
      name: item.name, sort_order: item.sort_order, active: !item.active, default_for: item.default_for, attrs: item.attrs,
    }), item.active ? `Đã ngừng dùng ${item.code}` : `Đã dùng lại ${item.code}`);
    if (r) s.reload();
  };
  const columns: Column<KhtcCatalogItem>[] = [
    { key: "code", header: "Mã", cell: (item) => <Link href={catalogItemHref(item.kind, item.code, retired)}>{item.code}</Link> },
    { key: "name", header: kind === "signer" ? "Họ tên" : "Tên", cell: (item) => item.name },
    ...(KIND_ATTRS[kind] ?? []).map<Column<KhtcCatalogItem>>((f) => ({ key: f.key, header: f.label, cell: (item) => item.attrs[f.key] || "—" })),
  ];
  if (TEMPLATED.includes(kind)) columns.push({ key: "defaults", header: "Mặc định cho mẫu", cell: (item) => item.default_for.map((t) => TEMPLATE_LABEL[t] ?? t).join(", ") || "—" });
  columns.push(
    { key: "status", header: "Trạng thái", cell: (item) => <StatusPill tone={item.active ? "ok" : "neutral"}>{item.active ? "Đang dùng" : "Ngừng dùng"}</StatusPill> },
    { key: "updated", header: "Cập nhật", cell: (item) => `v${item.version} · ${dmy(item.recorded_at)}` },
    { key: "actions", header: "Thao tác", cell: (item) => <div className="row">
      <RowActions label={item.name} viewHref={catalogItemHref(item.kind, item.code, retired)} editHref={rights.catalog ? catalogEditHref(item.kind, item.code, retired) : undefined} />
      {rights.catalog ? <button type="button" className="btn btn-o btn-sm" disabled={act.busy || s.loading} onClick={() => void toggle(item)}>{item.active ? "Ngừng dùng" : "Dùng lại"}</button> : null}
    </div> },
  );
  return (
    <div>
      <PageHeader title="Danh mục KHTC" actions={rights.catalog ? <Link className="btn btn-p btn-sm" href={catalogCreateHref(kind, retired)}>Thêm</Link> : null}>
        Loại báo cáo, ĐVT, lĩnh vực, nguồn kinh phí, người ký, nơi nhận, căn cứ (mặt hàng giá cả quản lý ở Báo giá). Đổi mặc định chỉ áp dụng cho bản soạn mới.
      </PageHeader>
      {!rights.catalog ? <ReadOnlyNotice>Chỉ xem — Admin quản lý Danh mục KHTC.</ReadOnlyNotice> : null}
      {act.view}
      <LoadState s={s}>{(data) => (
        <div className="grid2" style={{ gridTemplateColumns: "220px 1fr", alignItems: "start" }}>
          <div className="card" role="tablist" aria-label="Loại danh mục">
            {CATALOG_KINDS.map((k) => <button key={k} type="button" role="tab" aria-selected={k === kind} className={k === kind ? "btn btn-p btn-sm" : "btn btn-o btn-sm"}
              style={{ display: "flex", justifyContent: "space-between", width: "100%", marginBottom: 4 }} onClick={() => router.replace(catalogListHref(k, retired), { scroll: false })}>
              <span>{KIND_LABEL[k]}</span><span>{data.counts.find((x) => x.kind === k)?.active ?? 0}</span>
            </button>)}
          </div>
          <div>
            <label className="row small" style={{ marginBottom: 8 }}><input type="checkbox" checked={retired} onChange={(e) => router.replace(catalogListHref(kind, e.target.checked), { scroll: false })} /> Hiện cả mục đã ngừng dùng</label>
            <DataTable caption={KIND_LABEL[kind]} columns={columns} rows={data.items} rowKey={(item) => item.code} empty="Chưa có mục nào." />
          </div>
        </div>
      )}</LoadState>
    </div>
  );
}
