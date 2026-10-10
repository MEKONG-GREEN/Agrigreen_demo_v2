"use client";

// FN-AD-04.03 — Danh mục mặt hàng, nhóm hàng theo thời kỳ (BRD Báo giá nông sản v1.3; W11.73, cap/price).
// Tab Mặt hàng · Nhóm hàng · Gán cho xã · Bộ theo vụ · Đề xuất từ xã. Quyền lấy từ máy chủ (can_edit, can_steward);
// Cấp Quốc gia chỉ xem; Cấp Xã chỉ thấy tab Đề xuất (đề xuất của mình). Mọi thay đổi có ngày hiệu lực — vắng = ngày báo
// giá kế tiếp (BR-02).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import {
  DataTable,
  FilterBar,
  FilterChip,
  PageHeader,
  SearchBox,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import {
  createPriceItemApi,
  dmy,
  itemLabel,
  KIND_LABEL,
  PROPOSAL_LABEL,
  STATE_LABEL,
  validRange,
  type PriceCatalog,
  type PriceItem,
  type PriceItemApi,
  type PriceItemInput,
  type PriceItemSet,
  type PriceProposal,
} from "./api";

type Tab = "items" | "groups" | "assign" | "sets" | "proposals";
const PAGE = 20;

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

export function usePriceItemApi(): PriceItemApi {
  const { api } = useAuth();
  return useMemo(() => createPriceItemApi(api), [api]);
}

export function ItemsScreen() {
  const { kn } = useAuth();
  const pa = usePriceItemApi();
  const role = kn.context?.role;
  const [tab, setTab] = useState<Tab>(role === "kn_xa" ? "proposals" : "items");
  const [at, setAt] = useState(vnToday());
  const [nonce, setNonce] = useState(0);
  const bump = () => setNonce((n) => n + 1);
  const catalog = useLoad(() => pa.catalog(at), [at, nonce]);
  const list = useLoad(role === "kn_xa" ? null : () => pa.items({ at }), [
    at,
    nonce,
  ]);
  const canEdit = list.data?.can_edit ?? false;
  const steward = list.data?.can_steward ?? false;
  const tabs: { id: Tab; label: string }[] =
    role === "kn_xa"
      ? [{ id: "proposals", label: "Đề xuất mặt hàng" }]
      : [
          { id: "items", label: "Mặt hàng" },
          { id: "groups", label: "Nhóm hàng" },
          { id: "assign", label: "Gán cho xã" },
          { id: "sets", label: "Bộ theo vụ" },
          {
            id: "proposals",
            label: `Đề xuất từ xã${list.data ? ` (${list.data.pending_proposals})` : ""}`,
          },
        ];
  return (
    <>
      <PageHeader title="Mặt hàng theo thời kỳ">
        Phòng Thông tin – Truyền thông là chủ danh mục. Mọi thay đổi áp dụng từ
        ngày báo giá kế tiếp, không ảnh hưởng phiếu đã có.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12 }}>
        <label htmlFor="pi-at">Hiệu lực tại ngày</label>
        <input
          id="pi-at"
          type="date"
          className="input"
          style={{ width: 170 }}
          value={at}
          onChange={(e) => e.target.value && setAt(e.target.value)}
        />
      </div>
      <div className="dtabs" role="tablist" aria-label="Danh mục mặt hàng">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={t.id === tab}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <LoadState s={catalog}>
        {(cat) => (
          <>
            {tab === "items" ? (
              <LoadState s={list}>
                {(d) => (
                  <ItemsTab
                    pa={pa}
                    at={at}
                    items={d.items}
                    cat={cat}
                    canEdit={canEdit}
                    steward={steward}
                    onChanged={bump}
                  />
                )}
              </LoadState>
            ) : null}
            {tab === "groups" ? (
              <GroupsTab
                pa={pa}
                at={at}
                steward={steward}
                nonce={nonce}
                onChanged={bump}
              />
            ) : null}
            {tab === "assign" ? (
              <LoadState s={list}>
                {(d) => (
                  <AssignTab
                    pa={pa}
                    at={at}
                    items={d.items}
                    steward={steward}
                    nonce={nonce}
                    onChanged={bump}
                  />
                )}
              </LoadState>
            ) : null}
            {tab === "sets" ? (
              <LoadState s={list}>
                {(d) => (
                  <SetsTab
                    pa={pa}
                    at={at}
                    items={d.items}
                    steward={steward}
                    nonce={nonce}
                    onChanged={bump}
                  />
                )}
              </LoadState>
            ) : null}
            {tab === "proposals" ? (
              <ProposalsTab
                pa={pa}
                cat={cat}
                steward={steward}
                propose={role === "kn_xa" || role === "kn_tinh"}
                nonce={nonce}
                onChanged={bump}
              />
            ) : null}
          </>
        )}
      </LoadState>
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Tab Mặt hàng
// ---------------------------------------------------------------------------------------------------------------------

type Dlg =
  | { kind: "create"; base?: Partial<PriceItemInput> }
  | { kind: "revise"; item: PriceItem }
  | { kind: "retire"; item: PriceItem }
  | { kind: "history"; code: string };

function stateTone(s: PriceItem["state"]) {
  return s === "active" ? "ok" : s === "pending" ? "info" : "neutral";
}

function ItemsTab({
  pa,
  at,
  items,
  cat,
  canEdit,
  steward,
  onChanged,
}: {
  pa: PriceItemApi;
  at: string;
  items: PriceItem[];
  cat: PriceCatalog;
  canEdit: boolean;
  steward: boolean;
  onChanged: () => void;
}) {
  const [group, setGroup] = useState("");
  const [kind, setKind] = useState("");
  const [state, setState] = useState("");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const act = useAction();
  const groupName = (c: string) =>
    cat.groups.find((g) => g.code === c)?.name ?? c;
  const unitName = (c: string) =>
    cat.units.find((u) => u.code === c)?.name ?? c;
  const rows = items.filter(
    (i) =>
      (!group || i.group_code === group) &&
      (!kind || i.kind === kind) &&
      (!state || i.state === state) &&
      (!q ||
        `${i.code} ${i.name} ${i.variety}`
          .toLowerCase()
          .includes(q.toLowerCase())),
  );
  const move = (i: PriceItem, d: "up" | "down") =>
    act
      .run(
        () => pa.moveItem(i.code, d),
        "Đã đổi thứ tự — áp dụng từ ngày báo giá kế tiếp",
      )
      .then((r) => r && onChanged());
  const columns: Column<PriceItem>[] = [
    {
      key: "ord",
      header: "Thứ tự",
      cell: (i) =>
        steward && i.state === "active" ? (
          <span className="row">
            <button
              type="button"
              className="btn btn-o btn-sm"
              aria-label={`Đưa ${i.code} lên`}
              disabled={act.busy}
              onClick={() => move(i, "up")}
            >
              ▲
            </button>
            <button
              type="button"
              className="btn btn-o btn-sm"
              aria-label={`Đưa ${i.code} xuống`}
              disabled={act.busy}
              onClick={() => move(i, "down")}
            >
              ▼
            </button>
          </span>
        ) : (
          i.sort_order
        ),
    },
    { key: "code", header: "Mã", cell: (i) => i.code },
    {
      key: "name",
      header: "Mặt hàng",
      cell: (i) => (
        <>
          {itemLabel(i)}{" "}
          {i.new_price ? <StatusPill tone="info">Giá mới</StatusPill> : null}{" "}
          {i.gated ? <StatusPill tone="warn">Bộ đang tắt</StatusPill> : null}
          {i.replaces_code ? (
            <div className="mute small">Thay thế cho {i.replaces_code}</div>
          ) : null}
        </>
      ),
    },
    { key: "group", header: "Nhóm hàng", cell: (i) => groupName(i.group_code) },
    { key: "kind", header: "Loại", cell: (i) => KIND_LABEL[i.kind] },
    { key: "unit", header: "ĐVT", cell: (i) => unitName(i.unit_code) },
    { key: "spec", header: "Quy cách", cell: (i) => i.spec ?? "—" },
    { key: "valid", header: "Hiệu lực", cell: validRange },
    {
      key: "points",
      header: "Số xã",
      cell: (i) => (i.point_count === null ? "Mọi xã" : i.point_count),
    },
    {
      key: "state",
      header: "Trạng thái",
      cell: (i) => (
        <StatusPill tone={stateTone(i.state)}>
          {STATE_LABEL[i.state]}
        </StatusPill>
      ),
    },
    {
      key: "acts",
      header: "Thao tác",
      cell: (i) => (
        <span className="row">
          <button
            type="button"
            className="btn btn-o btn-sm"
            onClick={() => setDlg({ kind: "history", code: i.code })}
          >
            Lịch sử
          </button>
          {canEdit && i.state !== "retired" && (!i.has_price || steward) ? (
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setDlg({ kind: "revise", item: i })}
            >
              Sửa
            </button>
          ) : null}
          {steward && i.state !== "retired" ? (
            <button
              type="button"
              className="btn btn-d btn-sm"
              onClick={() => setDlg({ kind: "retire", item: i })}
            >
              Ngưng
            </button>
          ) : null}
        </span>
      ),
    },
  ];
  return (
    <>
      <div
        className="alert alert-info"
        role="note"
        style={{ marginBottom: 12 }}
      >
        <Icon name="info" />
        <span>
          Mặt hàng đã có giá không sửa đơn vị tính, quy cách, nhóm hàng — dùng
          “Tạo mặt hàng thay thế”. Mặt hàng Cơ bản áp dụng mọi xã; mặt hàng Địa
          phương gán theo xã.
        </span>
      </div>
      {act.view}
      <FilterBar
        onReset={() => (
          setGroup(""),
          setKind(""),
          setState(""),
          setQ(""),
          setPage(1)
        )}
      >
        <FilterChip
          label="Nhóm hàng"
          value={group}
          onChange={(v) => (setGroup(v), setPage(1))}
          options={[
            { value: "", label: "Tất cả" },
            ...cat.groups.map((g) => ({ value: g.code, label: g.name })),
          ]}
        />
        <FilterChip
          label="Loại"
          value={kind}
          onChange={(v) => (setKind(v), setPage(1))}
          options={[
            { value: "", label: "Tất cả" },
            { value: "basic", label: "Cơ bản" },
            { value: "local", label: "Địa phương" },
          ]}
        />
        <FilterChip
          label="Trạng thái"
          value={state}
          onChange={(v) => (setState(v), setPage(1))}
          options={[
            { value: "", label: "Tất cả" },
            ...(["active", "pending", "retired"] as const).map((s) => ({
              value: s,
              label: STATE_LABEL[s],
            })),
          ]}
        />
        <SearchBox
          value={q}
          onChange={(v) => (setQ(v), setPage(1))}
          placeholder="Tìm theo mã, tên mặt hàng"
        />
        {canEdit ? (
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => setDlg({ kind: "create" })}
          >
            Thêm mặt hàng
          </button>
        ) : null}
      </FilterBar>
      <DataTable
        caption="Danh mục mặt hàng"
        columns={columns}
        rows={rows.slice((page - 1) * PAGE, page * PAGE)}
        rowKey={(i) => i.code}
        page={page}
        pageSize={PAGE}
        total={rows.length}
        onPage={setPage}
        empty="Chưa có mặt hàng phù hợp."
      />
      {dlg?.kind === "create" ? (
        <ItemDialog
          title={
            dlg.base?.replaces_code
              ? `Tạo mặt hàng thay thế cho ${dlg.base.replaces_code}`
              : "Thêm mặt hàng"
          }
          cat={cat}
          base={dlg.base}
          create
          onClose={() => setDlg(null)}
          onSave={(b) => pa.createItem(b)}
          onDone={onChanged}
        />
      ) : null}
      {dlg?.kind === "revise" ? (
        <ItemDialog
          title={`Sửa mặt hàng ${dlg.item.code}`}
          cat={cat}
          base={dlg.item}
          locked={dlg.item.has_price}
          onClose={() => setDlg(null)}
          onSave={(b) => pa.reviseItem(dlg.item.code, b)}
          onDone={onChanged}
          onReplace={
            canEdit
              ? () =>
                  setDlg({
                    kind: "create",
                    base: {
                      ...toInput(dlg.item),
                      replaces_code: dlg.item.code,
                    },
                  })
              : undefined
          }
        />
      ) : null}
      {dlg?.kind === "retire" ? (
        <RetireDialog
          pa={pa}
          item={dlg.item}
          onClose={() => setDlg(null)}
          onDone={onChanged}
        />
      ) : null}
      {dlg?.kind === "history" ? (
        <HistoryDialog
          pa={pa}
          code={dlg.code}
          at={at}
          cat={cat}
          onClose={() => setDlg(null)}
        />
      ) : null}
    </>
  );
}

function toInput(i: PriceItem): PriceItemInput {
  return {
    name: i.name,
    variety: i.variety,
    group_code: i.group_code,
    unit_code: i.unit_code,
    spec: i.spec ?? "",
    sale_place: i.sale_place ?? "",
    kind: i.kind,
  };
}

/** Thêm / sửa / duyệt đề xuất: thông tin, loại, hiệu lực, "Thay thế cho", xã áp dụng; ĐVT, quy cách khóa khi đã có giá. */
export function ItemDialog({
  title,
  cat,
  base,
  create = false,
  locked = false,
  onClose,
  onSave,
  onDone,
  onReplace,
}: {
  title: string;
  cat: PriceCatalog;
  base?: Partial<PriceItemInput> | PriceItem;
  create?: boolean;
  locked?: boolean;
  onClose: () => void;
  onSave: (b: PriceItemInput) => Promise<unknown>;
  onDone: () => void;
  onReplace?: () => void;
}) {
  const b = base ?? {};
  const [f, setF] = useState({
    name: b.name ?? "",
    variety: b.variety ?? "",
    group_code: b.group_code ?? cat.groups[0]?.code ?? "",
    unit_code: b.unit_code ?? cat.units[0]?.code ?? "",
    spec: b.spec ?? "",
    sale_place: b.sale_place ?? "",
    kind: (b.kind ?? "basic") as "basic" | "local",
    valid_from: "",
    valid_to: "",
    replaces_code: ("replaces_code" in b && b.replaces_code) || "",
    points: ("points" in b && b.points?.join(", ")) || "",
  });
  const act = useAction();
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const save = async () => {
    const body: PriceItemInput = {
      name: f.name.trim(),
      variety: f.variety.trim(),
      group_code: f.group_code,
      unit_code: f.unit_code,
      spec: f.spec.trim(),
      sale_place: f.sale_place.trim(),
      kind: f.kind,
    };
    if (f.valid_from) body.valid_from = f.valid_from;
    if (create) {
      if (f.valid_to) body.valid_to = f.valid_to;
      if (f.replaces_code) body.replaces_code = f.replaces_code;
      const pts = f.points.split(/[\s,;]+/).filter(Boolean);
      if (pts.length) body.points = pts;
    }
    const r = await act.run(() => onSave(body));
    if (r) {
      onDone();
      onClose();
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <>
          {onReplace && locked ? (
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={onReplace}
            >
              Tạo mặt hàng thay thế
            </button>
          ) : null}
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy}
            onClick={save}
          >
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      {locked ? (
        <p className="mute small">
          Mặt hàng đã có giá: chỉ sửa tên hiển thị và nơi bán.
        </p>
      ) : null}
      <div className="grid2">
        <div className="field">
          <label htmlFor="pi-name" className="req">
            Tên loại
          </label>
          <input
            id="pi-name"
            className="input"
            maxLength={100}
            value={f.name}
            onChange={(e) => set({ name: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pi-variety" className="req">
            Giống / phẩm cấp
          </label>
          <input
            id="pi-variety"
            className="input"
            maxLength={100}
            disabled={locked}
            value={f.variety}
            onChange={(e) => set({ variety: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pi-group" className="req">
            Nhóm hàng
          </label>
          <select
            id="pi-group"
            className="select"
            disabled={locked}
            value={f.group_code}
            onChange={(e) => set({ group_code: e.target.value })}
          >
            {cat.groups.map((g) => (
              <option key={g.code} value={g.code}>
                {g.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="pi-unit" className="req">
            Đơn vị tính
          </label>
          <select
            id="pi-unit"
            className="select"
            disabled={locked}
            value={f.unit_code}
            onChange={(e) => set({ unit_code: e.target.value })}
          >
            {cat.units.map((u) => (
              <option key={u.code} value={u.code}>
                {u.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="pi-spec">Quy cách</label>
          <input
            id="pi-spec"
            className="input"
            maxLength={100}
            disabled={locked}
            value={f.spec}
            onChange={(e) => set({ spec: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pi-place">Nơi bán</label>
          <input
            id="pi-place"
            className="input"
            maxLength={200}
            value={f.sale_place}
            onChange={(e) => set({ sale_place: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pi-kind" className="req">
            Loại
          </label>
          <select
            id="pi-kind"
            className="select"
            disabled={locked}
            value={f.kind}
            onChange={(e) => set({ kind: e.target.value as "basic" | "local" })}
          >
            <option value="basic">Cơ bản — mọi xã</option>
            <option value="local">Địa phương — gán theo xã</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor="pi-from">Hiệu lực từ</label>
          <input
            id="pi-from"
            type="date"
            className="input"
            value={f.valid_from}
            onChange={(e) => set({ valid_from: e.target.value })}
          />
          <span className="mute small">Để trống: ngày báo giá kế tiếp</span>
        </div>
        {create ? (
          <>
            <div className="field">
              <label htmlFor="pi-to">Hiệu lực đến</label>
              <input
                id="pi-to"
                type="date"
                className="input"
                value={f.valid_to}
                onChange={(e) => set({ valid_to: e.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor="pi-replaces">Thay thế cho (mã)</label>
              <input
                id="pi-replaces"
                className="input"
                placeholder="MH00001"
                value={f.replaces_code}
                onChange={(e) =>
                  set({ replaces_code: e.target.value.trim().toUpperCase() })
                }
              />
            </div>
            {f.kind === "local" ? (
              <div className="field">
                <label htmlFor="pi-points">
                  Xã áp dụng (mã xã, cách nhau bởi dấu phẩy)
                </label>
                <input
                  id="pi-points"
                  className="input"
                  value={f.points}
                  onChange={(e) => set({ points: e.target.value })}
                />
              </div>
            ) : null}
          </>
        ) : null}
      </div>
    </Dialog>
  );
}

function RetireDialog({
  pa,
  item,
  onClose,
  onDone,
}: {
  pa: PriceItemApi;
  item: PriceItem;
  onClose: () => void;
  onDone: () => void;
}) {
  const [to, setTo] = useState("");
  const [reason, setReason] = useState("");
  const act = useAction();
  const save = async () => {
    const r = await act.run(() =>
      pa.retireItem(item.code, {
        reason: reason.trim(),
        ...(to ? { valid_to: to } : {}),
      }),
    );
    if (r) {
      onDone();
      onClose();
    }
  };
  return (
    <Dialog
      title={`Ngưng ${item.code} — ${itemLabel(item)}`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-d btn-sm"
          disabled={act.busy || !reason.trim()}
          onClick={save}
        >
          Ngưng
        </button>
      }
    >
      {act.view}
      <div className="field">
        <label htmlFor="pr-to">Hiệu lực đến</label>
        <input
          id="pr-to"
          type="date"
          className="input"
          value={to}
          onChange={(e) => setTo(e.target.value)}
        />
        <span className="mute small">
          Để trống: đến hết hôm nay. Báo cáo các ngày trước vẫn hiển thị mặt
          hàng.
        </span>
      </div>
      <div className="field">
        <label htmlFor="pr-reason" className="req">
          Lý do
        </label>
        <textarea
          id="pr-reason"
          className="input"
          maxLength={500}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
      </div>
    </Dialog>
  );
}

function HistoryDialog({
  pa,
  code,
  at,
  cat,
  onClose,
}: {
  pa: PriceItemApi;
  code: string;
  at: string;
  cat: PriceCatalog;
  onClose: () => void;
}) {
  const s = useLoad(() => pa.item(code, at), [code, at]);
  const unitName = (c: string) =>
    cat.units.find((u) => u.code === c)?.name ?? c;
  return (
    <Dialog title={`Lịch sử thay đổi ${code}`} onClose={onClose}>
      <LoadState s={s}>
        {(d) => (
          <>
            {d.replaced_by.length ? (
              <p>Được thay thế bởi: {d.replaced_by.join(", ")}</p>
            ) : null}
            <table className="dt">
              <thead>
                <tr>
                  <th scope="col">Phiên bản</th>
                  <th scope="col">Hiệu lực từ</th>
                  <th scope="col">Nội dung</th>
                  <th scope="col">Trạng thái</th>
                </tr>
              </thead>
              <tbody>
                {d.history.map((h) => (
                  <tr key={h.version}>
                    <td>{h.version}</td>
                    <td>{dmy(h.valid_from)}</td>
                    <td>
                      {itemLabel(h)} · {unitName(h.unit_code)}
                      {h.spec ? ` · ${h.spec}` : ""} · thứ tự {h.sort_order}
                    </td>
                    <td>
                      {h.status === "retired"
                        ? `Ngưng${h.note ? `: ${h.note}` : ""}`
                        : "Hoạt động"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </LoadState>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Tab Nhóm hàng
// ---------------------------------------------------------------------------------------------------------------------

function GroupsTab({
  pa,
  at,
  steward,
  nonce,
  onChanged,
}: {
  pa: PriceItemApi;
  at: string;
  steward: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const s = useLoad(() => pa.groups(at), [at, nonce]);
  const act = useAction();
  const [edit, setEdit] = useState<{ code?: string; name: string } | null>(
    null,
  );
  const save = async () => {
    if (!edit) return;
    const r = await act.run(() =>
      edit.code
        ? pa.reviseGroup(edit.code, { name: edit.name.trim() })
        : pa.createGroup({ name: edit.name.trim() }),
    );
    if (r) {
      setEdit(null);
      onChanged();
    }
  };
  const remove = (code: string) =>
    act
      .run(() => pa.retireGroup(code), "Đã xóa nhóm hàng")
      .then((r) => r && onChanged());
  return (
    <>
      {act.view}
      {steward ? (
        <div className="row" style={{ marginBottom: 12 }}>
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => setEdit({ name: "" })}
          >
            Thêm nhóm hàng
          </button>
        </div>
      ) : null}
      <LoadState s={s}>
        {(d) => (
          <DataTable
            caption="Nhóm hàng"
            columns={[
              { key: "code", header: "Mã", cell: (g) => g.code },
              { key: "name", header: "Tên nhóm", cell: (g) => g.name },
              { key: "items", header: "Số mặt hàng", cell: (g) => g.items },
              {
                key: "acts",
                header: "Thao tác",
                cell: (g) =>
                  steward ? (
                    <span className="row">
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        onClick={() => setEdit({ code: g.code, name: g.name })}
                      >
                        Sửa tên
                      </button>
                      <button
                        type="button"
                        className="btn btn-d btn-sm"
                        disabled={act.busy}
                        onClick={() => remove(g.code)}
                      >
                        Xóa
                      </button>
                    </span>
                  ) : null,
              },
            ]}
            rows={d.groups}
            rowKey={(g) => g.code}
            page={1}
            pageSize={100}
            total={d.groups.length}
            onPage={() => undefined}
          />
        )}
      </LoadState>
      {edit ? (
        <Dialog
          title={edit.code ? `Sửa nhóm ${edit.code}` : "Thêm nhóm hàng"}
          onClose={() => setEdit(null)}
          actions={
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy || !edit.name.trim()}
              onClick={save}
            >
              Lưu
            </button>
          }
        >
          <div className="field">
            <label htmlFor="pg-name" className="req">
              Tên nhóm
            </label>
            <input
              id="pg-name"
              className="input"
              maxLength={100}
              value={edit.name}
              onChange={(e) => setEdit({ ...edit, name: e.target.value })}
            />
          </div>
        </Dialog>
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Tab Gán cho xã — mã xã báo giá (mặc định mã ĐVHC xã; danh mục xã báo giá ở W11.72)
// ---------------------------------------------------------------------------------------------------------------------

function AssignTab({
  pa,
  at,
  items,
  steward,
  nonce,
  onChanged,
}: {
  pa: PriceItemApi;
  at: string;
  items: PriceItem[];
  steward: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [point, setPoint] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  const [from, setFrom] = useState("");
  const act = useAction();
  const s = useLoad(point ? () => pa.pointItems(point, at) : null, [
    point,
    at,
    nonce,
  ]);
  const locals = items.filter(
    (i) => i.kind === "local" && i.state !== "retired",
  );
  const assigned = new Set(
    (s.data?.items ?? []).filter((i) => i.kind === "local").map((i) => i.code),
  );
  const assign = () =>
    act
      .run(
        () => pa.assign(point, chosen, from || undefined),
        (r) => `Đã gán ${r.written} mặt hàng từ ${dmy(r.valid_from)}`,
      )
      .then((r) => r && (setChosen([]), onChanged()));
  const unassign = (code: string) =>
    act
      .run(
        () => pa.unassign(point, [code], from || undefined),
        (r) => `Đã bỏ gán từ ${dmy(r.valid_from)}`,
      )
      .then((r) => r && onChanged());
  return (
    <>
      {act.view}
      <div className="card" style={{ padding: 16, marginBottom: 12 }}>
        <div className="grid2">
          <div className="field">
            <label htmlFor="pa-point">Mã xã báo giá</label>
            <input
              id="pa-point"
              className="input"
              placeholder="vd 91301"
              value={point}
              onChange={(e) => setPoint(e.target.value.trim())}
            />
          </div>
          <div className="field">
            <label htmlFor="pa-from">Ngày áp dụng</label>
            <input
              id="pa-from"
              type="date"
              className="input"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
            <span className="mute small">Để trống: ngày báo giá kế tiếp</span>
          </div>
        </div>
      </div>
      {point ? (
        <LoadState s={s}>
          {(d) => (
            <div className="grid2">
              <section
                className="card"
                style={{ padding: 16 }}
                aria-label="Mặt hàng của xã"
              >
                <h2>
                  Mặt hàng của xã {d.point_code} ngày {dmy(d.day)}
                </h2>
                <ol>
                  {d.items.map((i) => (
                    <li key={i.code}>
                      {i.code} · {itemLabel(i)}{" "}
                      <span className="mute">({KIND_LABEL[i.kind]})</span>{" "}
                      {steward && i.kind === "local" ? (
                        <button
                          type="button"
                          className="btn btn-o btn-sm"
                          disabled={act.busy}
                          onClick={() => unassign(i.code)}
                        >
                          Bỏ gán
                        </button>
                      ) : null}
                    </li>
                  ))}
                </ol>
              </section>
              {steward ? (
                <section
                  className="card"
                  style={{ padding: 16 }}
                  aria-label="Gán mặt hàng Địa phương"
                >
                  <h2>Gán mặt hàng Địa phương</h2>
                  {locals
                    .filter((i) => !assigned.has(i.code))
                    .map((i) => (
                      <label key={i.code} className="row">
                        <input
                          type="checkbox"
                          checked={chosen.includes(i.code)}
                          onChange={(e) =>
                            setChosen(
                              e.target.checked
                                ? [...chosen, i.code]
                                : chosen.filter((c) => c !== i.code),
                            )
                          }
                        />
                        {i.code} · {itemLabel(i)}
                      </label>
                    ))}
                  <button
                    type="button"
                    className="btn btn-p btn-sm"
                    disabled={act.busy || !chosen.length}
                    onClick={assign}
                  >
                    Gán cho xã
                  </button>
                </section>
              ) : null}
            </div>
          )}
        </LoadState>
      ) : (
        <p className="mute">Nhập mã xã báo giá để xem và gán mặt hàng.</p>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Tab Bộ theo vụ
// ---------------------------------------------------------------------------------------------------------------------

function SetsTab({
  pa,
  at,
  items,
  steward,
  nonce,
  onChanged,
}: {
  pa: PriceItemApi;
  at: string;
  items: PriceItem[];
  steward: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const s = useLoad(() => pa.sets(at), [at, nonce]);
  const act = useAction();
  const [edit, setEdit] = useState<{
    create: boolean;
    code: string;
    name: string;
    season: string;
    item_codes: string[];
    enabled: boolean;
    valid_from: string;
  } | null>(null);
  const body = (
    e: NonNullable<typeof edit>,
    patch: Partial<{ enabled: boolean; retire: boolean }> = {},
  ) => ({
    code: e.code,
    name: e.name.trim(),
    season: e.season.trim(),
    item_codes: e.item_codes,
    enabled: e.enabled,
    ...(e.valid_from ? { valid_from: e.valid_from } : {}),
    ...patch,
  });
  const save = async () => {
    if (!edit) return;
    const r = await act.run(() =>
      edit.create
        ? pa.createSet(body(edit))
        : pa.reviseSet(edit.code, body(edit)),
    );
    if (r) {
      setEdit(null);
      onChanged();
    }
  };
  const fromSet = (x: PriceItemSet) => ({
    create: false,
    code: x.code,
    name: x.name,
    season: x.season,
    item_codes: x.item_codes,
    enabled: x.enabled,
    valid_from: "",
  });
  const toggle = (x: PriceItemSet) =>
    act
      .run(
        () => pa.reviseSet(x.code, body(fromSet(x), { enabled: !x.enabled })),
        (r) => `Đã ${x.enabled ? "tắt" : "bật"} bộ từ ${dmy(r.valid_from)}`,
      )
      .then((r) => r && onChanged());
  const retire = (x: PriceItemSet) =>
    act
      .run(
        () => pa.reviseSet(x.code, body(fromSet(x), { retire: true })),
        "Đã ngưng bộ",
      )
      .then((r) => r && onChanged());
  return (
    <>
      {act.view}
      {steward ? (
        <div className="row" style={{ marginBottom: 12 }}>
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() =>
              setEdit({
                create: true,
                code: "",
                name: "",
                season: "",
                item_codes: [],
                enabled: true,
                valid_from: "",
              })
            }
          >
            Tạo bộ mặt hàng
          </button>
        </div>
      ) : null}
      <LoadState s={s}>
        {(d) => (
          <DataTable
            caption="Bộ mặt hàng theo vụ"
            columns={[
              { key: "code", header: "Mã", cell: (x) => x.code },
              { key: "name", header: "Tên bộ", cell: (x) => x.name },
              { key: "season", header: "Vụ", cell: (x) => x.season },
              {
                key: "n",
                header: "Số mặt hàng",
                cell: (x) => x.item_codes.length,
              },
              {
                key: "from",
                header: "Hiệu lực từ",
                cell: (x) => dmy(x.valid_from),
              },
              {
                key: "on",
                header: "Trạng thái",
                cell: (x) =>
                  x.state === "retired" ? (
                    <StatusPill>Ngưng</StatusPill>
                  ) : x.enabled ? (
                    <StatusPill tone="ok">Bật</StatusPill>
                  ) : (
                    <StatusPill tone="warn">Tắt</StatusPill>
                  ),
              },
              {
                key: "acts",
                header: "Thao tác",
                cell: (x) =>
                  steward && x.state !== "retired" ? (
                    <span className="row">
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        disabled={act.busy}
                        onClick={() => toggle(x)}
                      >
                        {x.enabled ? "Tắt bộ" : "Bật bộ"}
                      </button>
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        onClick={() => setEdit(fromSet(x))}
                      >
                        Sửa
                      </button>
                      <button
                        type="button"
                        className="btn btn-d btn-sm"
                        disabled={act.busy}
                        onClick={() => retire(x)}
                      >
                        Ngưng
                      </button>
                    </span>
                  ) : null,
              },
            ]}
            rows={d.sets}
            rowKey={(x) => x.code}
            page={1}
            pageSize={100}
            total={d.sets.length}
            onPage={() => undefined}
            empty="Chưa có bộ mặt hàng theo vụ."
          />
        )}
      </LoadState>
      {edit ? (
        <Dialog
          title={edit.create ? "Tạo bộ mặt hàng" : `Sửa bộ ${edit.code}`}
          onClose={() => setEdit(null)}
          actions={
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy}
              onClick={save}
            >
              Lưu
            </button>
          }
        >
          {act.view}
          <div className="grid2">
            <div className="field">
              <label htmlFor="ps-code" className="req">
                Mã bộ
              </label>
              <input
                id="ps-code"
                className="input"
                disabled={!edit.create}
                placeholder="he_thu_2026"
                value={edit.code}
                onChange={(e) =>
                  setEdit({ ...edit, code: e.target.value.trim() })
                }
              />
            </div>
            <div className="field">
              <label htmlFor="ps-name" className="req">
                Tên bộ
              </label>
              <input
                id="ps-name"
                className="input"
                maxLength={100}
                value={edit.name}
                onChange={(e) => setEdit({ ...edit, name: e.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor="ps-season" className="req">
                Vụ
              </label>
              <input
                id="ps-season"
                className="input"
                maxLength={50}
                placeholder="Hè Thu 2026"
                value={edit.season}
                onChange={(e) => setEdit({ ...edit, season: e.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor="ps-from">Ngày hiệu lực</label>
              <input
                id="ps-from"
                type="date"
                className="input"
                value={edit.valid_from}
                onChange={(e) =>
                  setEdit({ ...edit, valid_from: e.target.value })
                }
              />
            </div>
          </div>
          <label className="row">
            <input
              type="checkbox"
              checked={edit.enabled}
              onChange={(e) => setEdit({ ...edit, enabled: e.target.checked })}
            />{" "}
            Bật bộ
          </label>
          <fieldset className="field">
            <legend>Mặt hàng trong bộ</legend>
            {items
              .filter((i) => i.state !== "retired")
              .map((i) => (
                <label key={i.code} className="row">
                  <input
                    type="checkbox"
                    checked={edit.item_codes.includes(i.code)}
                    onChange={(e) =>
                      setEdit({
                        ...edit,
                        item_codes: e.target.checked
                          ? [...edit.item_codes, i.code]
                          : edit.item_codes.filter((c) => c !== i.code),
                      })
                    }
                  />
                  {i.code} · {itemLabel(i)}
                </label>
              ))}
          </fieldset>
        </Dialog>
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// Tab Đề xuất từ xã (FN-XA-03.01 luồng phụ)
// ---------------------------------------------------------------------------------------------------------------------

function ProposalsTab({
  pa,
  cat,
  steward,
  propose,
  nonce,
  onChanged,
}: {
  pa: PriceItemApi;
  cat: PriceCatalog;
  steward: boolean;
  propose: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [status, setStatus] = useState<"" | PriceProposal["status"]>(
    steward ? "pending" : "",
  );
  const s = useLoad(() => pa.proposals(status || undefined), [status, nonce]);
  const [dlg, setDlg] = useState<
    | { kind: "approve" | "reject"; p: PriceProposal }
    | { kind: "propose" }
    | null
  >(null);
  const groupName = (c: string) =>
    cat.groups.find((g) => g.code === c)?.name ?? c;
  const unitName = (c: string) =>
    cat.units.find((u) => u.code === c)?.name ?? c;
  return (
    <>
      <FilterBar>
        <FilterChip
          label="Trạng thái"
          value={status}
          onChange={(v) => setStatus(v as typeof status)}
          options={[
            { value: "", label: "Tất cả" },
            ...(["pending", "approved", "rejected"] as const).map((x) => ({
              value: x,
              label: PROPOSAL_LABEL[x],
            })),
          ]}
        />
        {propose ? (
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => setDlg({ kind: "propose" })}
          >
            Đề xuất mặt hàng
          </button>
        ) : null}
      </FilterBar>
      <LoadState s={s}>
        {(d) => (
          <DataTable
            caption="Đề xuất mặt hàng từ xã"
            columns={[
              {
                key: "at",
                header: "Ngày đề xuất",
                cell: (p) => dmy(p.created_at.slice(0, 10)),
              },
              { key: "point", header: "Xã", cell: (p) => p.point_code },
              {
                key: "name",
                header: "Mặt hàng",
                cell: (p) =>
                  `${itemLabel(p)} · ${unitName(p.unit_code)}${p.spec ? ` · ${p.spec}` : ""}`,
              },
              {
                key: "group",
                header: "Nhóm hàng",
                cell: (p) => groupName(p.group_code),
              },
              { key: "reason", header: "Lý do", cell: (p) => p.reason },
              {
                key: "st",
                header: "Trạng thái",
                cell: (p) => (
                  <>
                    <StatusPill
                      tone={
                        p.status === "approved"
                          ? "ok"
                          : p.status === "rejected"
                            ? "bad"
                            : "info"
                      }
                    >
                      {PROPOSAL_LABEL[p.status]}
                    </StatusPill>
                    {p.item_code ? (
                      <div className="mute small">Mặt hàng {p.item_code}</div>
                    ) : null}
                    {p.decision_note ? (
                      <div className="mute small">{p.decision_note}</div>
                    ) : null}
                  </>
                ),
              },
              {
                key: "acts",
                header: "Thao tác",
                cell: (p) =>
                  steward && p.status === "pending" ? (
                    <span className="row">
                      <button
                        type="button"
                        className="btn btn-p btn-sm"
                        onClick={() => setDlg({ kind: "approve", p })}
                      >
                        Duyệt
                      </button>
                      <button
                        type="button"
                        className="btn btn-d btn-sm"
                        onClick={() => setDlg({ kind: "reject", p })}
                      >
                        Từ chối
                      </button>
                    </span>
                  ) : null,
              },
            ]}
            rows={d.proposals}
            rowKey={(p) => p.id}
            page={1}
            pageSize={200}
            total={d.proposals.length}
            onPage={() => undefined}
            empty="Không có đề xuất."
          />
        )}
      </LoadState>
      {dlg?.kind === "approve" ? (
        <ItemDialog
          title={`Duyệt đề xuất của xã ${dlg.p.point_code}`}
          cat={cat}
          base={{
            name: dlg.p.name,
            variety: dlg.p.variety,
            group_code: dlg.p.group_code,
            unit_code: dlg.p.unit_code,
            spec: dlg.p.spec ?? "",
            kind: "local",
            points: [dlg.p.point_code],
          }}
          create
          onClose={() => setDlg(null)}
          onSave={(b) => pa.approve(dlg.p.id, b)}
          onDone={onChanged}
        />
      ) : null}
      {dlg?.kind === "reject" ? (
        <RejectDialog
          pa={pa}
          p={dlg.p}
          onClose={() => setDlg(null)}
          onDone={onChanged}
        />
      ) : null}
      {dlg?.kind === "propose" ? (
        <ProposeItemDialog
          pa={pa}
          cat={cat}
          onClose={() => setDlg(null)}
          onDone={onChanged}
        />
      ) : null}
    </>
  );
}

function RejectDialog({
  pa,
  p,
  onClose,
  onDone,
}: {
  pa: PriceItemApi;
  p: PriceProposal;
  onClose: () => void;
  onDone: () => void;
}) {
  const [note, setNote] = useState("");
  const act = useAction();
  const save = async () => {
    const r = await act.run(() => pa.reject(p.id, note.trim()));
    if (r) {
      onDone();
      onClose();
    }
  };
  return (
    <Dialog
      title={`Từ chối đề xuất "${itemLabel(p)}"`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-d btn-sm"
          disabled={act.busy || !note.trim()}
          onClick={save}
        >
          Từ chối
        </button>
      }
    >
      {act.view}
      <div className="field">
        <label htmlFor="pj-note" className="req">
          Lý do từ chối
        </label>
        <textarea
          id="pj-note"
          className="input"
          maxLength={500}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </div>
    </Dialog>
  );
}

/** "Đề xuất mặt hàng" (FN-XA-03.01): tên, giống/phẩm cấp, nhóm hàng, đơn vị tính, lý do → gửi Cấp Tỉnh. Dùng lại ở màn Nhập báo giá (W11.75). */
export function ProposeItemDialog({
  pa,
  cat,
  point = "",
  onClose,
  onDone,
}: {
  pa: PriceItemApi;
  cat: PriceCatalog;
  point?: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const [f, setF] = useState({
    point_code: point,
    name: "",
    variety: "",
    group_code: cat.groups[0]?.code ?? "",
    unit_code: cat.units[0]?.code ?? "",
    spec: "",
    reason: "",
  });
  const act = useAction();
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const save = async () => {
    const r = await act.run(() =>
      pa.propose({
        ...f,
        point_code: f.point_code.trim(),
        name: f.name.trim(),
        variety: f.variety.trim(),
        spec: f.spec.trim(),
        reason: f.reason.trim(),
      }),
    );
    if (r) {
      onDone();
      onClose();
    }
  };
  return (
    <Dialog
      title="Đề xuất mặt hàng mới"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={save}
        >
          Gửi đề xuất
        </button>
      }
    >
      {act.view}
      <p className="mute small">
        Mặt hàng chỉ xuất hiện ở màn Nhập báo giá khi Cấp Tỉnh duyệt và gán cho
        xã.
      </p>
      <div className="grid2">
        <div className="field">
          <label htmlFor="pp-point" className="req">
            Mã xã báo giá
          </label>
          <input
            id="pp-point"
            className="input"
            value={f.point_code}
            onChange={(e) => set({ point_code: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pp-name" className="req">
            Tên loại
          </label>
          <input
            id="pp-name"
            className="input"
            maxLength={100}
            value={f.name}
            onChange={(e) => set({ name: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pp-variety" className="req">
            Giống / phẩm cấp
          </label>
          <input
            id="pp-variety"
            className="input"
            maxLength={100}
            value={f.variety}
            onChange={(e) => set({ variety: e.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor="pp-group" className="req">
            Nhóm hàng
          </label>
          <select
            id="pp-group"
            className="select"
            value={f.group_code}
            onChange={(e) => set({ group_code: e.target.value })}
          >
            {cat.groups.map((g) => (
              <option key={g.code} value={g.code}>
                {g.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="pp-unit" className="req">
            Đơn vị tính
          </label>
          <select
            id="pp-unit"
            className="select"
            value={f.unit_code}
            onChange={(e) => set({ unit_code: e.target.value })}
          >
            {cat.units.map((u) => (
              <option key={u.code} value={u.code}>
                {u.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="pp-spec">Quy cách</label>
          <input
            id="pp-spec"
            className="input"
            maxLength={100}
            value={f.spec}
            onChange={(e) => set({ spec: e.target.value })}
          />
        </div>
      </div>
      <div className="field">
        <label htmlFor="pp-reason" className="req">
          Lý do
        </label>
        <textarea
          id="pp-reason"
          className="input"
          maxLength={500}
          value={f.reason}
          onChange={(e) => set({ reason: e.target.value })}
        />
      </div>
    </Dialog>
  );
}
