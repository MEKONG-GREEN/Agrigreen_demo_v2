"use client";

// FN-AD-04.02 — Danh mục xã báo giá (BRD Báo giá nông sản v1.3; W11.72, cap/price) + phân công cán bộ Xã / cán bộ Cấp
// Tỉnh theo ngày hiệu lực (FN-AD-04.01) + nhóm vùng. Admin ghi (can_manage từ /v1/price/status); Cấp Tỉnh, Cấp Quốc gia
// chỉ xem. Mọi thay đổi có ngày hiệu lực — vắng = ngày báo giá kế tiếp; lịch sử giữ nguyên (sổ chỉ ghi thêm).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import {
  DataTable,
  PageHeader,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import {
  createPricePointApi,
  dmy,
  OFFICER_ROLE_LABEL,
  officerLabel,
  POINT_STATE_LABEL,
  pointProblems,
  type PricePoint,
  type PricePointApi,
  type PricePointInput,
  type PriceRegion,
} from "./api";

type Tab = "points" | "regions";

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

export function usePricePointApi(): PricePointApi {
  const { api } = useAuth();
  return useMemo(() => createPricePointApi(api), [api]);
}

const stateTone = (s: PricePoint["state"]) =>
  s === "active" ? "ok" : s === "pending" ? "info" : "neutral";

export function PointsScreen() {
  const pa = usePricePointApi();
  const [tab, setTab] = useState<Tab>("points");
  const [at, setAt] = useState(vnToday());
  const [nonce, setNonce] = useState(0);
  const bump = () => setNonce((n) => n + 1);
  const status = useLoad(() => pa.status(), []);
  const regions = useLoad(() => pa.regions(at), [at, nonce]);
  const canManage = status.data?.can_manage ?? false;
  return (
    <>
      <PageHeader title="Xã báo giá">
        Mỗi xã báo giá liên kết một mã ĐVHC dùng chung và một nhóm vùng; cán bộ
        Xã phụ trách và cán bộ Cấp Tỉnh được phân công theo ngày hiệu lực.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12 }}>
        <label htmlFor="pp-at">Tại ngày</label>
        <input
          id="pp-at"
          type="date"
          className="input"
          style={{ width: 170 }}
          value={at}
          onChange={(e) => e.target.value && setAt(e.target.value)}
        />
        {!canManage && status.data ? (
          <span className="muted">Chỉ xem — Admin quản lý danh mục xã.</span>
        ) : null}
      </div>
      <div className="dtabs" role="tablist" aria-label="Xã báo giá">
        {(
          [
            { id: "points", label: "Xã báo giá" },
            { id: "regions", label: "Nhóm vùng" },
          ] as const
        ).map((t) => (
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
      <LoadState s={regions}>
        {(r) =>
          tab === "points" ? (
            <PointsTab
              pa={pa}
              at={at}
              regions={r.regions}
              canManage={canManage}
              nonce={nonce}
              onChanged={bump}
            />
          ) : (
            <RegionsTab
              pa={pa}
              regions={r.regions}
              canManage={canManage}
              onChanged={bump}
            />
          )
        }
      </LoadState>
    </>
  );
}

type PointDialog =
  | { kind: "create" }
  | { kind: "revise"; point: PricePoint }
  | { kind: "retire"; point: PricePoint }
  | { kind: "officers"; point: PricePoint };

function PointsTab({
  pa,
  at,
  regions,
  canManage,
  nonce,
  onChanged,
}: {
  pa: PricePointApi;
  at: string;
  regions: PriceRegion[];
  canManage: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [region, setRegion] = useState("");
  const s = useLoad(() => pa.points({ at, region: region || undefined }), [
    at,
    region,
    nonce,
  ]);
  const [dlg, setDlg] = useState<PointDialog | null>(null);
  const columns: Column<PricePoint>[] = [
    { key: "code", header: "Mã", cell: (p) => p.code },
    { key: "name", header: "Xã", cell: (p) => p.name },
    {
      key: "au",
      header: "Mã ĐVHC",
      cell: (p) =>
        p.admin_unit_valid ? (
          p.admin_unit_code
        ) : (
          <span title="ĐVHC hết hiệu lực — chuyển sang mã ĐVHC mới">
            {p.admin_unit_code}{" "}
            <StatusPill tone="warn">
              {p.successor_codes.length
                ? `Hết hiệu lực → ${p.successor_codes.join(", ")}`
                : "ĐVHC hết hiệu lực"}
            </StatusPill>
          </span>
        ),
    },
    { key: "region", header: "Nhóm vùng", cell: (p) => p.region_name || p.region_code },
    {
      key: "officer",
      header: "Cán bộ Xã phụ trách",
      cell: (p) =>
        officerLabel(p.officer_ref, p.officer_name, "Chưa có cán bộ Xã"),
    },
    {
      key: "owner",
      header: "Cán bộ Tỉnh",
      cell: (p) => officerLabel(p.owner_ref, p.owner_name, "Chưa phân công"),
    },
    { key: "items", header: "Số mặt hàng", cell: (p) => p.item_count },
    {
      key: "state",
      header: "Trạng thái",
      cell: (p) => (
        <StatusPill tone={stateTone(p.state)}>
          {POINT_STATE_LABEL[p.state]}
          {p.state === "retired" && p.valid_to ? ` từ ${dmy(p.valid_to)}` : ""}
        </StatusPill>
      ),
    },
    {
      key: "acts",
      header: "Thao tác",
      cell: (p) =>
        canManage && p.state !== "retired" ? (
          <span className="row">
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setDlg({ kind: "officers", point: p })}
            >
              Phân công
            </button>
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setDlg({ kind: "revise", point: p })}
            >
              Sửa
            </button>
            <button
              type="button"
              className="btn btn-d btn-sm"
              onClick={() => setDlg({ kind: "retire", point: p })}
            >
              Ngưng
            </button>
          </span>
        ) : null,
    },
  ];
  const close = (changed: boolean) => {
    setDlg(null);
    if (changed) onChanged();
  };
  return (
    <>
      <div className="row" style={{ marginBottom: 12 }}>
        <label htmlFor="pp-region">Nhóm vùng</label>
        <select
          id="pp-region"
          className="input"
          style={{ width: 200 }}
          value={region}
          onChange={(e) => setRegion(e.target.value)}
        >
          <option value="">Toàn tỉnh</option>
          {regions.map((r) => (
            <option key={r.code} value={r.code}>
              {r.name}
            </option>
          ))}
        </select>
        {canManage ? (
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => setDlg({ kind: "create" })}
          >
            Thêm xã báo giá
          </button>
        ) : null}
      </div>
      <LoadState s={s}>
        {(d) => (
          <DataTable
            caption="Danh mục xã báo giá"
            columns={columns}
            rows={d.points}
            rowKey={(p) => p.code}
            page={1}
            pageSize={200}
            total={d.points.length}
            onPage={() => undefined}
          />
        )}
      </LoadState>
      {dlg && dlg.kind !== "officers" ? (
        <PointForm
          pa={pa}
          at={at}
          regions={regions}
          dlg={dlg}
          onClose={close}
        />
      ) : null}
      {dlg?.kind === "officers" ? (
        <OfficerForm pa={pa} at={at} point={dlg.point} onClose={close} />
      ) : null}
    </>
  );
}

function PointForm({
  pa,
  at,
  regions,
  dlg,
  onClose,
}: {
  pa: PricePointApi;
  at: string;
  regions: PriceRegion[];
  dlg: Exclude<PointDialog, { kind: "officers" }>;
  onClose: (changed: boolean) => void;
}) {
  const act = useAction();
  const p = dlg.kind === "create" ? null : dlg.point;
  const [b, setB] = useState<PricePointInput>({
    admin_unit_code: dlg.kind === "create" ? "" : undefined,
    region_code: p?.region_code ?? regions.find((r) => r.status === "active")?.code ?? "",
    valid_from: "",
  });
  const day = b.valid_from || at;
  const units = useLoad(
    dlg.kind === "retire" ? null : () => pa.adminUnits(day),
    [day],
  );
  const errs = pointProblems(b, dlg.kind);
  const save = async () => {
    const body: PricePointInput = {
      ...b,
      valid_from: b.valid_from || undefined,
    };
    const r = await act.run(() =>
      dlg.kind === "create"
        ? pa.createPoint(body)
        : dlg.kind === "revise"
          ? pa.revisePoint(dlg.point.code, body)
          : pa.retirePoint(dlg.point.code, body),
    );
    if (r) onClose(true);
  };
  const title =
    dlg.kind === "create"
      ? "Thêm xã báo giá"
      : dlg.kind === "revise"
        ? `Sửa xã ${dlg.point.code} — ${dlg.point.name}`
        : `Ngưng xã ${dlg.point.code} — ${dlg.point.name}`;
  return (
    <Dialog
      title={title}
      onClose={() => onClose(false)}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || errs.length > 0}
          onClick={save}
        >
          Lưu
        </button>
      }
    >
      {act.view}
      {dlg.kind === "create" ? (
        <p className="muted">Mã xã do hệ thống tự sinh (XBG####); tên xã lấy theo ĐVHC.</p>
      ) : null}
      {dlg.kind !== "retire" ? (
        <>
          <div className="field">
            <label
              htmlFor="pp-au"
              className={dlg.kind === "create" ? "req" : undefined}
            >
              {dlg.kind === "create" ? "ĐVHC (xã)" : "Chuyển sang mã ĐVHC mới"}
            </label>
            <LoadState s={units}>
              {(u) => (
                <select
                  id="pp-au"
                  className="input"
                  value={b.admin_unit_code ?? ""}
                  onChange={(e) =>
                    setB({ ...b, admin_unit_code: e.target.value || undefined })
                  }
                >
                  <option value="">
                    {dlg.kind === "create" ? "— Chọn ĐVHC —" : `Giữ ${p?.admin_unit_code}`}
                  </option>
                  {u.admin_units.map((a) => (
                    <option
                      key={a.code}
                      value={a.code}
                      disabled={!!a.taken_by && a.taken_by !== p?.code}
                    >
                      {a.code} — {a.name}
                      {a.taken_by && a.taken_by !== p?.code
                        ? ` (đã gắn ${a.taken_by})`
                        : ""}
                    </option>
                  ))}
                </select>
              )}
            </LoadState>
          </div>
          <div className="field">
            <label htmlFor="pp-rg" className="req">
              Nhóm vùng
            </label>
            <select
              id="pp-rg"
              className="input"
              value={b.region_code ?? ""}
              onChange={(e) => setB({ ...b, region_code: e.target.value })}
            >
              <option value="">— Chọn nhóm vùng —</option>
              {regions
                .filter((r) => r.status === "active")
                .map((r) => (
                  <option key={r.code} value={r.code}>
                    {r.name}
                  </option>
                ))}
            </select>
          </div>
        </>
      ) : null}
      <div className="field">
        <label htmlFor="pp-from">
          {dlg.kind === "create"
            ? "Ngày bắt đầu báo giá"
            : dlg.kind === "retire"
              ? "Ngưng từ ngày"
              : "Áp dụng từ ngày"}
        </label>
        <input
          id="pp-from"
          type="date"
          className="input"
          value={b.valid_from ?? ""}
          onChange={(e) => setB({ ...b, valid_from: e.target.value })}
        />
        <small className="muted">Để trống = ngày báo giá kế tiếp.</small>
      </div>
      {dlg.kind !== "create" ? (
        <div className="field">
          <label
            htmlFor="pp-note"
            className={dlg.kind === "retire" ? "req" : undefined}
          >
            {dlg.kind === "retire" ? "Lý do ngưng" : "Ghi chú (vd sắp xếp ĐVHC)"}
          </label>
          <textarea
            id="pp-note"
            className="input"
            maxLength={500}
            value={(dlg.kind === "retire" ? b.reason : b.note) ?? ""}
            onChange={(e) =>
              setB(
                dlg.kind === "retire"
                  ? { ...b, reason: e.target.value }
                  : { ...b, note: e.target.value },
              )
            }
          />
        </div>
      ) : null}
      {errs.length ? (
        <ul className="muted" aria-label="Thiếu thông tin">
          {errs.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      ) : null}
    </Dialog>
  );
}

function OfficerForm({
  pa,
  at,
  point,
  onClose,
}: {
  pa: PricePointApi;
  at: string;
  point: PricePoint;
  onClose: (changed: boolean) => void;
}) {
  const act = useAction();
  const [role, setRole] = useState<"xa" | "tinh">("xa");
  const [ref, setRef] = useState("");
  const [from, setFrom] = useState("");
  const detail = useLoad(() => pa.point(point.code, at), [point.code, at]);
  const dir = useLoad(() => pa.officers(role === "xa" ? "XA" : "TINH"), [role]);
  const current = role === "xa" ? point.officer_ref : point.owner_ref;
  const save = async (clear: boolean) => {
    const r = await act.run(
      () =>
        pa.assignOfficer(point.code, {
          role,
          officer_ref: clear ? null : ref,
          valid_from: from || undefined,
        }),
      clear ? "Đã bỏ phân công" : "Đã phân công",
    );
    if (r) onClose(true);
  };
  return (
    <Dialog
      title={`Phân công — ${point.code} ${point.name}`}
      onClose={() => onClose(false)}
      actions={
        <span className="row">
          {current ? (
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={act.busy}
              onClick={() => save(true)}
            >
              Bỏ phân công
            </button>
          ) : null}
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy || !ref}
            onClick={() => save(false)}
          >
            Lưu
          </button>
        </span>
      }
    >
      {act.view}
      <div className="field">
        <label htmlFor="po-role">Vai phụ trách</label>
        <select
          id="po-role"
          className="input"
          value={role}
          onChange={(e) => {
            setRole(e.target.value as "xa" | "tinh");
            setRef("");
          }}
        >
          <option value="xa">{OFFICER_ROLE_LABEL.xa}</option>
          <option value="tinh">{OFFICER_ROLE_LABEL.tinh}</option>
        </select>
      </div>
      {role === "xa" && point.officer_ref ? (
        <div className="alert alert-warn" role="note">
          Xã đã có cán bộ Xã phụ trách chính (
          {officerLabel(point.officer_ref, point.officer_name, "")}). Người mới
          thay thế từ ngày hiệu lực; lịch sử phân công được giữ.
        </div>
      ) : null}
      <div className="field">
        <label htmlFor="po-ref" className="req">
          Cán bộ
        </label>
        <LoadState s={dir}>
          {(d) =>
            d.directory ? (
              <select
                id="po-ref"
                className="input"
                value={ref}
                onChange={(e) => setRef(e.target.value)}
              >
                <option value="">— Chọn cán bộ —</option>
                {d.officers.map((o) => (
                  <option key={o.ref} value={o.ref}>
                    {o.name}
                    {o.communes.length ? ` (${o.communes.join(", ")})` : ""}
                  </option>
                ))}
              </select>
            ) : (
              <input
                id="po-ref"
                className="input"
                placeholder="Mã tham chiếu tài khoản (UUID)"
                value={ref}
                onChange={(e) => setRef(e.target.value.trim())}
              />
            )
          }
        </LoadState>
      </div>
      <div className="field">
        <label htmlFor="po-from">Từ ngày</label>
        <input
          id="po-from"
          type="date"
          className="input"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
        />
        <small className="muted">Để trống = ngày báo giá kế tiếp.</small>
      </div>
      <LoadState s={detail}>
        {(d) =>
          d.officers.length ? (
            <DataTable
              caption="Lịch sử phân công"
              columns={[
                { key: "role", header: "Vai", cell: (o) => OFFICER_ROLE_LABEL[o.role] },
                {
                  key: "who",
                  header: "Cán bộ",
                  cell: (o) => officerLabel(o.officer_ref, o.name, "(bỏ phân công)"),
                },
                { key: "from", header: "Từ ngày", cell: (o) => dmy(o.valid_from) },
              ]}
              rows={d.officers}
              rowKey={(o) => `${o.role}:${o.version}`}
              page={1}
              pageSize={100}
              total={d.officers.length}
              onPage={() => undefined}
            />
          ) : (
            <p className="muted">Chưa có phân công.</p>
          )
        }
      </LoadState>
    </Dialog>
  );
}

function RegionsTab({
  pa,
  regions,
  canManage,
  onChanged,
}: {
  pa: PricePointApi;
  regions: PriceRegion[];
  canManage: boolean;
  onChanged: () => void;
}) {
  const act = useAction();
  const [edit, setEdit] = useState<{ code?: string; name: string } | null>(
    null,
  );
  const save = async () => {
    if (!edit) return;
    const r = await act.run(() =>
      edit.code
        ? pa.reviseRegion(edit.code, { name: edit.name.trim() })
        : pa.createRegion({ name: edit.name.trim() }),
    );
    if (r) {
      setEdit(null);
      onChanged();
    }
  };
  const retire = (code: string) =>
    act
      .run(() => pa.retireRegion(code), "Đã ngưng nhóm vùng")
      .then((r) => r && onChanged());
  return (
    <>
      {act.view}
      {canManage ? (
        <div className="row" style={{ marginBottom: 12 }}>
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => setEdit({ name: "" })}
          >
            Thêm nhóm vùng
          </button>
        </div>
      ) : null}
      <DataTable
        caption="Nhóm vùng"
        columns={[
          { key: "code", header: "Mã", cell: (r) => r.code },
          { key: "name", header: "Tên nhóm vùng", cell: (r) => r.name },
          { key: "points", header: "Số xã đang hoạt động", cell: (r) => r.points },
          {
            key: "status",
            header: "Trạng thái",
            cell: (r) => (
              <StatusPill tone={r.status === "active" ? "ok" : "neutral"}>
                {r.status === "active" ? "Hoạt động" : "Ngưng"}
              </StatusPill>
            ),
          },
          {
            key: "acts",
            header: "Thao tác",
            cell: (r) =>
              canManage && r.status === "active" ? (
                <span className="row">
                  <button
                    type="button"
                    className="btn btn-o btn-sm"
                    onClick={() => setEdit({ code: r.code, name: r.name })}
                  >
                    Sửa tên
                  </button>
                  <button
                    type="button"
                    className="btn btn-d btn-sm"
                    disabled={act.busy}
                    onClick={() => retire(r.code)}
                  >
                    Ngưng
                  </button>
                </span>
              ) : null,
          },
        ]}
        rows={regions}
        rowKey={(r) => r.code}
        page={1}
        pageSize={100}
        total={regions.length}
        onPage={() => undefined}
      />
      {edit ? (
        <Dialog
          title={edit.code ? `Sửa nhóm vùng ${edit.code}` : "Thêm nhóm vùng"}
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
            <label htmlFor="pr-name" className="req">
              Tên nhóm vùng
            </label>
            <input
              id="pr-name"
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
