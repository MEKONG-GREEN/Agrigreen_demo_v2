"use client";

// FN-14 — Kho tài liệu nhân sự (BRD HCTH v3.0; W11.24, dom/hrm). Màn kho: cây thư mục bên trái, danh sách tài liệu bên phải
// (B1), tạo/đổi tên thư mục con (B2), tải lên với phân loại theo nhân viên, phòng ban, loại quyết định, năm (B3), tra cứu,
// xem trước, tải xuống (B4); chi tiết tệp (Hình 28) với lịch sử, nhật ký xem/tải, "xóa = ẩn". Màn Admin "Thư mục tài liệu":
// cấu trúc thư mục gốc + quyền theo thư mục. Quyền lấy từ máy chủ (can_* của từng thư mục/tài liệu). Nội dung tệp chỉ giữ
// trong bộ nhớ của màn — không lưu trình duyệt.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { BackLink, LabelValueBlock } from "@/components/patterns/detail";
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
import { saveBlob } from "@/lib/files";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import {
  createDocApi,
  DOC_CHANGE_LABEL,
  DOC_FIELD_LABEL,
  DOC_TYPE_LABEL,
  fileSize,
  type DocApi,
  type DocFilter,
  type HrmDoc,
  type HrmDocInput,
  type HrmDocType,
  type HrmFolder,
  type HrmFolderACL,
} from "./doc-api";
import { GROUP_LABEL, SCOPE_LABEL, vnDate, type HrmScope } from "./staff-api";
import { useStaffApi } from "./StaffScreens";

const PAGE = 20;
const DETAIL = "/kn/nhan-su/kho-tai-lieu";

export function useDocApi(): DocApi {
  const { api } = useAuth();
  return useMemo(() => createDocApi(api), [api]);
}

/** Bảng không phân trang (lịch sử, nhật ký). */
const onePage = (n: number) => ({
  page: 1,
  pageSize: Math.max(n, 1),
  total: n,
  onPage: () => {},
});

const vnDateTime = (s: string) => {
  const d = new Date(new Date(s).getTime() + 7 * 3_600_000).toISOString();
  return `${vnDate(d.slice(0, 10))} ${d.slice(11, 16)}`;
};

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}

/** Cây thư mục: thư mục sắp theo đường dẫn (máy chủ), thụt lề theo cấp. */
function FolderTree({
  items,
  value,
  onChange,
  showAll = true,
}: {
  items: HrmFolder[];
  value: string | null;
  onChange: (id: string | null) => void;
  showAll?: boolean;
}) {
  return (
    <nav aria-label="Cây thư mục" className="doc-tree">
      <ul style={{ listStyle: "none", paddingLeft: 0, margin: 0 }}>
        {showAll ? (
          <li>
            <button
              type="button"
              className={`btn btn-sm ${value === null ? "btn-p" : "btn-g"}`}
              aria-pressed={value === null}
              onClick={() => onChange(null)}
            >
              Tất cả tài liệu
            </button>
          </li>
        ) : null}
        {items.map((f) => (
          <li
            key={f.folder_id}
            style={{ paddingLeft: `${(f.path.length - 1) * 16}px` }}
          >
            <button
              type="button"
              className={`btn btn-sm ${value === f.folder_id ? "btn-p" : "btn-g"}`}
              aria-pressed={value === f.folder_id}
              onClick={() => onChange(f.folder_id)}
              title={f.path.join(" / ")}
            >
              {f.name}
              {f.docs ? <span className="mute small"> ({f.docs})</span> : null}
              {f.hidden ? <span className="mute small"> · đã ẩn</span> : null}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  );
}

function ReasonDialog({
  title,
  label,
  onClose,
  onSubmit,
}: {
  title: string;
  label: string;
  onClose: () => void;
  onSubmit: (reason: string) => Promise<unknown>;
}) {
  const act = useAction();
  const [reason, setReason] = useState("");
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn btn-g btn-sm" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy || reason.trim().length < 10}
            onClick={() =>
              void act
                .run(() => onSubmit(reason.trim()))
                .then((r) => r !== null && onClose())
            }
          >
            Xác nhận
          </button>
        </>
      }
    >
      {act.view}
      <Field label={label}>
        <textarea
          aria-label={label}
          value={reason}
          rows={3}
          onChange={(e) => setReason(e.target.value)}
        />
      </Field>
      <p className="mute small">Tối thiểu 10 ký tự. Thao tác được lưu vết.</p>
    </Dialog>
  );
}

function NameDialog({
  title,
  initial = "",
  onClose,
  onSubmit,
}: {
  title: string;
  initial?: string;
  onClose: () => void;
  onSubmit: (name: string) => Promise<unknown>;
}) {
  const act = useAction();
  const [name, setName] = useState(initial);
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn btn-g btn-sm" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy || !name.trim()}
            onClick={() =>
              void act
                .run(() => onSubmit(name.trim()))
                .then((r) => r !== null && onClose())
            }
          >
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      <Field label="Tên thư mục">
        <input
          aria-label="Tên thư mục"
          value={name}
          maxLength={120}
          onChange={(e) => setName(e.target.value)}
        />
      </Field>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Phân loại (B3): tải lên + sửa phân loại.
// --------------------------------------------------------------------------------------------------------------------

function ClassifyDialog({
  da,
  folder,
  doc,
  recordsEnabled,
  extensions,
  maxBytes,
  onClose,
  onDone,
}: {
  da: DocApi;
  folder: HrmFolder | null;
  doc?: HrmDoc;
  recordsEnabled: boolean;
  extensions: string[];
  maxBytes: number;
  onClose: () => void;
  onDone: (note: string) => void;
}) {
  const sa = useStaffApi();
  const act = useAction();
  const [file, setFile] = useState<File | null>(null);
  const [v, setV] = useState({
    title: doc?.title ?? "",
    doc_type: (doc?.doc_type ?? folder?.doc_type ?? "khac") as HrmDocType,
    year: doc?.year ? String(doc.year) : "",
    unit_code: doc?.unit_code ?? "",
    doc_no: doc?.doc_no ?? "",
    issued_on: doc?.issued_on ?? "",
    note: doc?.note ?? "",
  });
  const [staff, setStaff] = useState<string[]>(doc?.staff_ids ?? []);
  const staffList = useLoad(
    recordsEnabled ? () => sa.list({ limit: 100 }) : null,
    [recordsEnabled],
  );
  const units = useLoad(recordsEnabled ? () => sa.units() : null, [
    recordsEnabled,
  ]);
  const up = (k: keyof typeof v) => (e: { target: { value: string } }) =>
    setV((x) => ({ ...x, [k]: e.target.value }));
  const tooBig = !!file && file.size > maxBytes;
  const submit = () => {
    const meta: HrmDocInput = {
      folder_id: folder?.folder_id ?? doc?.folder_id,
      title: v.title.trim(),
      doc_type: v.doc_type,
      year: v.year ? Number(v.year) : null,
      unit_code: v.unit_code || undefined,
      staff_ids: staff,
      doc_no: v.doc_no || undefined,
      issued_on: v.issued_on || undefined,
      note: v.note || undefined,
    };
    void act
      .run(() => (doc ? da.classify(doc.doc_id, meta) : da.upload(meta, file!)))
      .then(
        (r) =>
          r !== null &&
          onDone(doc ? "Đã lưu phân loại." : "Đã tải lên tài liệu."),
      );
  };
  return (
    <Dialog
      title={
        doc ? "Sửa phân loại" : `Tải lên — ${folder?.path.join(" / ") ?? ""}`
      }
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn btn-g btn-sm" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={
              act.busy || !v.title.trim() || (!doc && (!file || tooBig))
            }
            onClick={submit}
          >
            {doc ? "Lưu" : "Tải lên"}
          </button>
        </>
      }
    >
      {act.view}
      {!doc ? (
        <Field label="Tệp">
          <input
            type="file"
            aria-label="Tệp"
            accept={extensions.join(",")}
            onChange={(e) => {
              const f = e.target.files?.[0] ?? null;
              setFile(f);
              if (f && !v.title)
                setV((x) => ({ ...x, title: f.name.replace(/\.[^.]+$/, "") }));
            }}
          />
          <span className="mute small">
            {extensions.join(", ")} · tối đa {fileSize(maxBytes)}
            {tooBig ? " — tệp vượt dung lượng cho phép" : ""}
          </span>
        </Field>
      ) : null}
      <Field label="Tên tài liệu">
        <input
          aria-label="Tên tài liệu"
          value={v.title}
          maxLength={300}
          onChange={up("title")}
        />
      </Field>
      <Field label="Loại quyết định">
        <select
          aria-label="Loại quyết định"
          value={v.doc_type}
          onChange={up("doc_type")}
        >
          {Object.entries(DOC_TYPE_LABEL).map(([k, l]) => (
            <option key={k} value={k}>
              {l}
            </option>
          ))}
        </select>
      </Field>
      <Field label="Số quyết định">
        <input
          aria-label="Số quyết định"
          value={v.doc_no}
          maxLength={60}
          onChange={up("doc_no")}
        />
      </Field>
      <Field label="Ngày ban hành">
        <input
          type="date"
          aria-label="Ngày ban hành"
          value={v.issued_on}
          onChange={up("issued_on")}
        />
      </Field>
      <Field label="Năm">
        <input
          type="number"
          aria-label="Năm"
          value={v.year}
          min={1975}
          max={2100}
          onChange={up("year")}
          placeholder="theo ngày ban hành"
        />
      </Field>
      <Field label="Phòng ban">
        {units.data ? (
          <select
            aria-label="Phòng ban"
            value={v.unit_code}
            onChange={up("unit_code")}
          >
            <option value="">— theo nhân viên —</option>
            {units.data.items
              .filter((u) => u.active || u.code === v.unit_code)
              .map((u) => (
                <option key={u.code} value={u.code}>
                  {u.name}
                </option>
              ))}
          </select>
        ) : (
          <input
            aria-label="Phòng ban"
            value={v.unit_code}
            onChange={up("unit_code")}
            placeholder="mã đơn vị nội bộ"
          />
        )}
      </Field>
      {recordsEnabled ? (
        <Field label="Nhân viên">
          <select
            multiple
            aria-label="Nhân viên"
            value={staff}
            size={5}
            onChange={(e) =>
              setStaff(Array.from(e.target.selectedOptions, (o) => o.value))
            }
          >
            {(staffList.data?.items ?? []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.code} · {s.label}
              </option>
            ))}
          </select>
          {staffList.problem ? (
            <span className="mute small">
              Không tải được danh sách hồ sơ (cần quyền xem hồ sơ).
            </span>
          ) : null}
        </Field>
      ) : (
        <p className="mute small">
          Hồ sơ nhân sự chưa bật (Q-005) — chưa gắn tài liệu với nhân viên được.
        </p>
      )}
      <Field label="Ghi chú">
        <textarea
          aria-label="Ghi chú"
          value={v.note}
          rows={2}
          maxLength={1000}
          onChange={up("note")}
        />
      </Field>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Màn kho (B1–B4). mode = "unit": "Tài liệu của phòng" (lãnh đạo — máy chủ giới hạn phạm vi đơn vị mình).
// --------------------------------------------------------------------------------------------------------------------

export function HrmDocsScreen({ mode = "all" }: { mode?: "all" | "unit" }) {
  const da = useDocApi();
  const [nonce, setNonce] = useState(0);
  const tree = useLoad(() => da.tree(), [nonce]);
  const [folder, setFolder] = useState<string | null>(null);
  const [f, setF] = useState<DocFilter>({});
  const [page, setPage] = useState(1);
  const [dialog, setDialog] = useState<null | "upload" | "sub" | "rename">(
    null,
  );
  const act = useAction();
  const visible = (tree.data?.items ?? []).filter(
    (x) => x.can_view || x.can_manage,
  );
  const cur = tree.data?.items.find((x) => x.folder_id === folder) ?? null;
  const list = useLoad(
    tree.data
      ? () =>
          da.list({
            ...f,
            folder: folder ?? undefined,
            recursive: folder ? "1" : undefined,
            offset: (page - 1) * PAGE,
            limit: PAGE,
          })
      : null,
    [!!tree.data, folder, JSON.stringify(f), page, nonce],
  );
  const set = (patch: DocFilter) => {
    setF((x) => ({ ...x, ...patch }));
    setPage(1);
  };
  const open = (d: HrmDoc, download: boolean) =>
    void act.run(
      () => da.content(d.doc_id, download),
      (r) => {
        if (download) {
          saveBlob(r.blob, r.name);
          return "Đã tải xuống (thao tác được ghi nhật ký).";
        }
        window.open(URL.createObjectURL(r.blob), "_blank", "noopener");
        return "Đã mở xem trước (thao tác được ghi nhật ký).";
      },
    );
  const cols: Column<HrmDoc>[] = [
    {
      key: "title",
      header: "Tên tài liệu",
      cell: (r) => (
        <>
          <Link href={`${DETAIL}/${r.doc_id}`}>{r.title}</Link>
          {r.status === "hidden" ? (
            <>
              {" "}
              <StatusPill tone="warn">Đã ẩn</StatusPill>
            </>
          ) : null}
          <div className="mute small">{r.folder_path.join(" / ")}</div>
        </>
      ),
    },
    { key: "type", header: "Loại", cell: (r) => DOC_TYPE_LABEL[r.doc_type] },
    { key: "no", header: "Số QĐ", cell: (r) => r.doc_no ?? "—" },
    { key: "year", header: "Năm", cell: (r) => r.year ?? "—" },
    { key: "unit", header: "Phòng ban", cell: (r) => r.unit_code ?? "—" },
    {
      key: "staff",
      header: "Nhân viên",
      cell: (r) => (r.staff_codes.length ? r.staff_codes.join(", ") : "—"),
    },
    {
      key: "at",
      header: "Ngày tải lên",
      cell: (r) => vnDate(r.created_at.slice(0, 10)),
    },
    {
      key: "file",
      header: "Tệp",
      cell: (r) => (
        <span className="row-actions">
          <button
            type="button"
            className="btn btn-g btn-sm"
            onClick={() => open(r, false)}
            aria-label={`Xem trước ${r.title}`}
          >
            Xem
          </button>
          <button
            type="button"
            className="btn btn-g btn-sm"
            onClick={() => open(r, true)}
            aria-label={`Tải xuống ${r.title}`}
          >
            Tải xuống
          </button>
          <span className="mute small">{fileSize(r.size_bytes)}</span>
        </span>
      ),
    },
  ];
  return (
    <>
      <PageHeader
        title={mode === "unit" ? "Tài liệu của phòng" : "Hồ sơ dữ liệu"}
        actions={
          cur && !cur.hidden ? (
            <>
              {cur.can_admin ? (
                <>
                  <button
                    type="button"
                    className="btn btn-o btn-sm"
                    onClick={() => setDialog("sub")}
                  >
                    Thư mục con
                  </button>
                  {cur.parent_id ? (
                    <button
                      type="button"
                      className="btn btn-o btn-sm"
                      onClick={() => setDialog("rename")}
                    >
                      Đổi tên
                    </button>
                  ) : null}
                </>
              ) : null}
              {cur.can_manage && tree.data?.ready ? (
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  onClick={() => setDialog("upload")}
                >
                  Tải lên
                </button>
              ) : null}
            </>
          ) : null
        }
      >
        {mode === "unit"
          ? "Quyết định và hồ sơ nhân sự thuộc phòng của bạn (theo quyền thư mục)."
          : "Kho quyết định nâng bậc, điều động, bổ nhiệm, khen thưởng, kỷ luật, hồ sơ đánh giá và tài liệu nhân sự khác."}
      </PageHeader>
      {act.view}
      <LoadState s={tree}>
        {(t) =>
          visible.length === 0 ? (
            <div className="alert alert-info" role="note">
              <span>
                {t.items.length === 0 && t.can_create_root
                  ? "Chưa có thư mục — tạo cấu trúc thư mục gốc ở Quản trị › Thư mục tài liệu."
                  : "Bạn chưa được cấp quyền xem thư mục tài liệu nào."}
              </span>
            </div>
          ) : (
            <div
              className="split"
              style={{
                display: "grid",
                gridTemplateColumns: "minmax(200px, 1fr) 3fr",
                gap: 16,
              }}
            >
              <FolderTree
                items={t.items}
                value={folder}
                onChange={(id) => {
                  setFolder(id);
                  setPage(1);
                }}
              />
              <div>
                {!t.records_enabled ? (
                  <p className="mute small">
                    Hồ sơ nhân sự chưa bật (Q-005): tài liệu gắn nhân viên không
                    hiển thị.
                  </p>
                ) : null}
                <FilterBar
                  onReset={() =>
                    set({ q: undefined, doc_type: undefined, year: undefined })
                  }
                >
                  <SearchBox
                    value={f.q ?? ""}
                    onChange={(q) => set({ q })}
                    placeholder="Tên tài liệu, số quyết định, tên tệp"
                  />
                  <FilterChip
                    label="Loại"
                    value={f.doc_type ?? ""}
                    options={[
                      { value: "", label: "Tất cả" },
                      ...Object.entries(DOC_TYPE_LABEL).map(
                        ([value, label]) => ({ value, label }),
                      ),
                    ]}
                    onChange={(v) =>
                      set({
                        doc_type: (v || undefined) as HrmDocType | undefined,
                      })
                    }
                  />
                  <label className="small">
                    Năm{" "}
                    <input
                      type="number"
                      aria-label="Năm"
                      style={{ width: 90 }}
                      value={f.year ?? ""}
                      onChange={(e) =>
                        set({
                          year: e.target.value
                            ? Number(e.target.value)
                            : undefined,
                        })
                      }
                    />
                  </label>
                </FilterBar>
                <LoadState s={list}>
                  {(l) => (
                    <DataTable
                      caption="Tài liệu"
                      columns={cols}
                      rows={l.items}
                      rowKey={(r) => r.doc_id}
                      page={page}
                      pageSize={PAGE}
                      total={l.total}
                      onPage={setPage}
                      empty="Không có tài liệu phù hợp."
                    />
                  )}
                </LoadState>
              </div>
            </div>
          )
        }
      </LoadState>
      {dialog === "upload" && cur && tree.data ? (
        <ClassifyDialog
          da={da}
          folder={cur}
          recordsEnabled={tree.data.records_enabled}
          extensions={tree.data.extensions}
          maxBytes={tree.data.max_bytes}
          onClose={() => setDialog(null)}
          onDone={(n) => {
            setDialog(null);
            act.setNote(n);
            setNonce((x) => x + 1);
          }}
        />
      ) : null}
      {dialog === "sub" && cur ? (
        <NameDialog
          title={`Thư mục con của ${cur.name}`}
          onClose={() => setDialog(null)}
          onSubmit={(name) =>
            da
              .createFolder({ parent_id: cur.folder_id, name })
              .then(() => setNonce((x) => x + 1))
          }
        />
      ) : null}
      {dialog === "rename" && cur ? (
        <NameDialog
          title="Đổi tên thư mục"
          initial={cur.name}
          onClose={() => setDialog(null)}
          onSubmit={(name) =>
            da
              .renameFolder(cur.folder_id, name)
              .then(() => setNonce((x) => x + 1))
          }
        />
      ) : null}
    </>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Chi tiết tệp (Hình 28): phân loại, xem trước, tải xuống, sửa, ẩn/hiện, lịch sử, nhật ký xem/tải.
// --------------------------------------------------------------------------------------------------------------------

export function HrmDocDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const da = useDocApi();
  const [nonce, setNonce] = useState(0);
  const s = useLoad(() => da.get(id), [id, nonce]);
  const tree = useLoad(() => da.tree(), []);
  const act = useAction();
  const [dialog, setDialog] = useState<null | "edit" | "hide">(null);
  const [preview, setPreview] = useState<{ url: string; type: string } | null>(
    null,
  );
  useBreadcrumbTail(s.data ? [{ label: s.data.doc.title }] : []);
  useEffect(
    () => () => (preview ? URL.revokeObjectURL(preview.url) : undefined),
    [preview],
  );
  const d = s.data?.doc;
  const canPreview =
    !!d &&
    (d.content_type === "application/pdf" ||
      d.content_type.startsWith("image/"));
  return (
    <>
      <BackLink href={DETAIL}>Hồ sơ dữ liệu</BackLink>
      <PageHeader
        title={d?.title ?? "Tài liệu"}
        actions={
          d ? (
            <>
              {canPreview ? (
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  disabled={act.busy}
                  onClick={() =>
                    void act.run(
                      () => da.content(d.doc_id, false),
                      (r) => (
                        setPreview({
                          url: URL.createObjectURL(r.blob),
                          type: d.content_type,
                        }),
                        "Đã mở xem trước (ghi nhật ký)."
                      ),
                    )
                  }
                >
                  Xem trước
                </button>
              ) : null}
              <button
                type="button"
                className="btn btn-o btn-sm"
                disabled={act.busy}
                onClick={() =>
                  void act.run(
                    () => da.content(d.doc_id, true),
                    (r) => (
                      saveBlob(r.blob, r.name),
                      "Đã tải xuống (ghi nhật ký)."
                    ),
                  )
                }
              >
                Tải xuống
              </button>
              {d.can_manage && d.status === "active" ? (
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  onClick={() => setDialog("edit")}
                >
                  Sửa phân loại
                </button>
              ) : null}
              {d.can_manage ? (
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  onClick={() => setDialog("hide")}
                >
                  {d.status === "active" ? "Xóa (ẩn)" : "Hiện lại"}
                </button>
              ) : null}
            </>
          ) : null
        }
      >
        {d ? d.folder_path.join(" / ") : null}
      </PageHeader>
      {act.view}
      <LoadState s={s}>
        {(x) => (
          <>
            {x.doc.status === "hidden" ? (
              <div className="alert alert-warn" role="note">
                <span>
                  Tài liệu đã ẩn (xóa) — chỉ người quản lý thấy; tệp và lịch sử
                  được giữ.
                </span>
              </div>
            ) : null}
            <LabelValueBlock
              title="Thông tin tệp"
              items={[
                {
                  label: "Loại quyết định",
                  value: DOC_TYPE_LABEL[x.doc.doc_type],
                },
                { label: "Số quyết định", value: x.doc.doc_no },
                { label: "Ngày ban hành", value: vnDate(x.doc.issued_on) },
                { label: "Năm", value: x.doc.year },
                { label: "Phòng ban", value: x.doc.unit_code },
                { label: "Nhân viên", value: x.doc.staff_codes.join(", ") },
                {
                  label: "Tên tệp",
                  value: `${x.doc.file_name} · ${fileSize(x.doc.size_bytes)}`,
                },
                { label: "Tải lên", value: vnDateTime(x.doc.created_at) },
                { label: "Ghi chú", value: x.doc.note },
              ]}
            />
            {preview ? (
              <section aria-label="Xem trước">
                {preview.type === "application/pdf" ? (
                  <iframe
                    title="Xem trước tài liệu"
                    src={preview.url}
                    style={{ width: "100%", height: 560, border: 0 }}
                  />
                ) : (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img
                    alt="Xem trước tài liệu"
                    src={preview.url}
                    style={{ maxWidth: "100%" }}
                  />
                )}
              </section>
            ) : null}
            <h2>Lịch sử</h2>
            <DataTable
              caption="Lịch sử tài liệu"
              columns={[
                { key: "v", header: "Phiên bản", cell: (r) => r.version },
                {
                  key: "c",
                  header: "Thao tác",
                  cell: (r) =>
                    `${DOC_CHANGE_LABEL[r.change] ?? r.change}${r.changed_fields.length ? `: ${r.changed_fields.map((k) => DOC_FIELD_LABEL[k] ?? k).join(", ")}` : ""}`,
                },
                { key: "r", header: "Lý do", cell: (r) => r.reason ?? "—" },
                {
                  key: "t",
                  header: "Lúc",
                  cell: (r) => vnDateTime(r.recorded_at),
                },
              ]}
              rows={x.history}
              rowKey={(r) => String(r.version)}
              {...onePage(x.history.length)}
            />
            {x.access ? (
              <>
                <h2>Nhật ký xem, tải xuống</h2>
                <DataTable
                  caption="Nhật ký xem, tải xuống"
                  columns={[
                    {
                      key: "a",
                      header: "Thao tác",
                      cell: (r) =>
                        r.action === "download" ? "Tải xuống" : "Xem trước",
                    },
                    {
                      key: "s",
                      header: "Phạm vi",
                      cell: (r) => SCOPE_LABEL[r.scope],
                    },
                    {
                      key: "u",
                      header: "Người",
                      cell: (r) => (
                        <code className="small">{r.actor_ref.slice(0, 8)}</code>
                      ),
                    },
                    {
                      key: "t",
                      header: "Lúc",
                      cell: (r) => vnDateTime(r.recorded_at),
                    },
                  ]}
                  rows={x.access}
                  rowKey={(r) => `${r.recorded_at}-${r.actor_ref}`}
                  {...onePage(x.access.length)}
                  empty="Chưa có lượt xem hoặc tải xuống."
                />
              </>
            ) : null}
          </>
        )}
      </LoadState>
      {dialog === "edit" && d && tree.data ? (
        <ClassifyDialog
          da={da}
          folder={null}
          doc={d}
          recordsEnabled={tree.data.records_enabled}
          extensions={tree.data.extensions}
          maxBytes={tree.data.max_bytes}
          onClose={() => setDialog(null)}
          onDone={(n) => {
            setDialog(null);
            act.setNote(n);
            setNonce((v) => v + 1);
          }}
        />
      ) : null}
      {dialog === "hide" && d ? (
        <ReasonDialog
          title={
            d.status === "active" ? "Xóa tài liệu (ẩn)" : "Hiện lại tài liệu"
          }
          label="Lý do"
          onClose={() => setDialog(null)}
          onSubmit={(reason) =>
            da
              .hide(d.doc_id, d.status === "active", reason)
              .then(() => setNonce((v) => v + 1))
          }
        />
      ) : null}
    </>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Admin — Quản trị › Thư mục tài liệu (UR-ADM-10): cấu trúc thư mục gốc, quyền theo thư mục, nhật ký.
// --------------------------------------------------------------------------------------------------------------------

const DOC_PERMS = ["docs.view", "docs.manage"] as const;
const DOC_PERM_LABEL: Record<(typeof DOC_PERMS)[number], string> = {
  "docs.view": "Xem, tải xuống",
  "docs.manage": "Quản lý tài liệu",
};
const ACL_GROUPS = ["hcth", "manager", "tinh", "xa", "tw"];

function ACLEditor({
  da,
  folder,
  onDone,
}: {
  da: DocApi;
  folder: HrmFolder;
  onDone: () => void;
}) {
  const act = useAction();
  const [rows, setRows] = useState<
    {
      key: string;
      perms: Partial<Record<(typeof DOC_PERMS)[number], HrmScope>>;
    }[]
  >(
    Object.entries(folder.acl ?? {}).map(([key, perms]) => ({
      key,
      perms: { ...perms },
    })),
  );
  const [note, setNote] = useState("");
  const setPerm = (i: number, p: (typeof DOC_PERMS)[number], s: string) =>
    setRows((rs) =>
      rs.map((r, j) => {
        if (j !== i) return r;
        const perms = { ...r.perms };
        if (s) perms[p] = s as HrmScope;
        else delete perms[p];
        return { ...r, perms };
      }),
    );
  const save = () => {
    const acl: HrmFolderACL = Object.fromEntries(
      rows.filter((r) => r.key.includes(":")).map((r) => [r.key, r.perms]),
    );
    void act
      .run(
        () => da.setACL(folder.folder_id, acl, note || undefined),
        "Đã lưu quyền thư mục.",
      )
      .then((r) => r !== null && onDone());
  };
  return (
    <section aria-label="Quyền thư mục">
      <h2>Quyền thư mục — {folder.path.join(" / ")}</h2>
      <p className="mute small">
        Ghi đè ma trận Phân quyền hồ sơ (FN-20) cho thư mục này và thư mục con.
        Bỏ trống = theo ma trận/thư mục cha. Quyền theo người dùng ưu tiên hơn
        nhóm; Admin luôn toàn quyền.
      </p>
      {act.view}
      <table className="tbl">
        <thead>
          <tr>
            <th>Nhóm vai / người dùng</th>
            {DOC_PERMS.map((p) => (
              <th key={p}>{DOC_PERM_LABEL[p]}</th>
            ))}
            <th />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              <td>
                <select
                  aria-label={`Đối tượng ${i + 1}`}
                  value={r.key.startsWith("user:") ? "user" : r.key}
                  onChange={(e) =>
                    setRows((rs) =>
                      rs.map((x, j) =>
                        j === i
                          ? {
                              ...x,
                              key:
                                e.target.value === "user"
                                  ? "user:"
                                  : e.target.value,
                            }
                          : x,
                      ),
                    )
                  }
                >
                  {ACL_GROUPS.map((g) => (
                    <option key={g} value={`role:${g}`}>
                      {GROUP_LABEL[g] ?? g}
                    </option>
                  ))}
                  <option value="user">Người dùng (mã)</option>
                </select>
                {r.key.startsWith("user:") ? (
                  <input
                    aria-label={`Mã người dùng ${i + 1}`}
                    value={r.key.slice(5)}
                    placeholder="UUID"
                    onChange={(e) =>
                      setRows((rs) =>
                        rs.map((x, j) =>
                          j === i
                            ? { ...x, key: `user:${e.target.value.trim()}` }
                            : x,
                        ),
                      )
                    }
                  />
                ) : null}
              </td>
              {DOC_PERMS.map((p) => (
                <td key={p}>
                  <select
                    aria-label={`${DOC_PERM_LABEL[p]} ${i + 1}`}
                    value={r.perms[p] ?? ""}
                    onChange={(e) => setPerm(i, p, e.target.value)}
                  >
                    <option value="">(theo ma trận)</option>
                    {(["none", "self", "unit", "all"] as HrmScope[]).map(
                      (sc) => (
                        <option key={sc} value={sc}>
                          {SCOPE_LABEL[sc]}
                        </option>
                      ),
                    )}
                  </select>
                </td>
              ))}
              <td>
                <button
                  type="button"
                  className="btn btn-g btn-sm"
                  onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}
                >
                  Bỏ
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="row-actions">
        <button
          type="button"
          className="btn btn-o btn-sm"
          onClick={() =>
            setRows((rs) => [...rs, { key: "role:manager", perms: {} }])
          }
        >
          Thêm dòng
        </button>
        <input
          aria-label="Ghi chú đổi quyền"
          value={note}
          placeholder="Ghi chú (tùy chọn)"
          maxLength={500}
          onChange={(e) => setNote(e.target.value)}
        />
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={save}
        >
          Lưu quyền
        </button>
      </div>
    </section>
  );
}

export function HrmDocFoldersScreen() {
  const da = useDocApi();
  const [nonce, setNonce] = useState(0);
  const tree = useLoad(() => da.tree(true), [nonce]);
  const [sel, setSel] = useState<string | null>(null);
  const [dialog, setDialog] = useState<
    null | "root" | "sub" | "rename" | "hide"
  >(null);
  const act = useAction();
  const cur = tree.data?.items.find((x) => x.folder_id === sel) ?? null;
  const hist = useLoad(
    cur?.can_admin ? () => da.folderHistory(cur.folder_id) : null,
    [cur?.folder_id, nonce],
  );
  const reload = () => setNonce((x) => x + 1);
  return (
    <>
      <PageHeader
        title="Thư mục tài liệu"
        actions={
          tree.data?.can_create_root ? (
            <>
              <button
                type="button"
                className="btn btn-o btn-sm"
                disabled={act.busy}
                onClick={() =>
                  void act.run(
                    () => da.installDefaults(),
                    (r) => (reload(), `Đã tạo ${r.created} thư mục mặc định.`),
                  )
                }
              >
                Cài thư mục mặc định
              </button>
              <button
                type="button"
                className="btn btn-p btn-sm"
                onClick={() => setDialog("root")}
              >
                Thư mục gốc
              </button>
            </>
          ) : null
        }
      >
        Cấu trúc thư mục kho tài liệu nhân sự dùng chung và quyền theo thư mục
        (FN-14, UR-ADM-10).
      </PageHeader>
      {act.view}
      <LoadState s={tree}>
        {(t) => (
          <div
            className="split"
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(200px, 1fr) 3fr",
              gap: 16,
            }}
          >
            <FolderTree
              items={t.items}
              value={sel}
              onChange={setSel}
              showAll={false}
            />
            <div>
              {!cur ? (
                <p className="mute">
                  {t.items.length
                    ? "Chọn một thư mục để sửa."
                    : "Chưa có thư mục — bấm “Cài thư mục mặc định”."}
                </p>
              ) : (
                <>
                  <LabelValueBlock
                    title={cur.path.join(" / ")}
                    items={[
                      {
                        label: "Trạng thái",
                        value: cur.hidden ? "Đã ẩn" : "Đang dùng",
                      },
                      { label: "Thư mục hệ thống", value: cur.system_key },
                      {
                        label: "Loại tài liệu gợi ý",
                        value: cur.doc_type
                          ? DOC_TYPE_LABEL[cur.doc_type as HrmDocType]
                          : null,
                      },
                      { label: "Tài liệu đang hiện", value: cur.docs },
                    ]}
                  />
                  {cur.can_admin ? (
                    <div className="row-actions">
                      {cur.status === "active" ? (
                        <>
                          <button
                            type="button"
                            className="btn btn-o btn-sm"
                            onClick={() => setDialog("sub")}
                          >
                            Thư mục con
                          </button>
                          <button
                            type="button"
                            className="btn btn-o btn-sm"
                            onClick={() => setDialog("rename")}
                          >
                            Đổi tên
                          </button>
                        </>
                      ) : null}
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        onClick={() => setDialog("hide")}
                      >
                        {cur.status === "active" ? "Ẩn thư mục" : "Hiện lại"}
                      </button>
                    </div>
                  ) : null}
                  {cur.can_admin && cur.status === "active" ? (
                    <ACLEditor
                      key={`${cur.folder_id}-${cur.version}`}
                      da={da}
                      folder={cur}
                      onDone={reload}
                    />
                  ) : null}
                  {hist.data ? (
                    <>
                      <h2>Nhật ký thư mục</h2>
                      <DataTable
                        caption="Nhật ký thư mục"
                        columns={[
                          {
                            key: "v",
                            header: "Phiên bản",
                            cell: (r) => r.version,
                          },
                          {
                            key: "c",
                            header: "Thao tác",
                            cell: (r) => DOC_CHANGE_LABEL[r.change] ?? r.change,
                          },
                          { key: "n", header: "Tên", cell: (r) => r.name },
                          {
                            key: "g",
                            header: "Ghi chú / lý do",
                            cell: (r) => r.note ?? "—",
                          },
                          {
                            key: "t",
                            header: "Lúc",
                            cell: (r) => vnDateTime(r.recorded_at),
                          },
                        ]}
                        rows={hist.data.items}
                        rowKey={(r) => String(r.version)}
                        {...onePage(hist.data.items.length)}
                      />
                    </>
                  ) : null}
                </>
              )}
            </div>
          </div>
        )}
      </LoadState>
      {dialog === "root" ? (
        <NameDialog
          title="Thư mục gốc"
          onClose={() => setDialog(null)}
          onSubmit={(name) => da.createFolder({ name }).then(reload)}
        />
      ) : null}
      {dialog === "sub" && cur ? (
        <NameDialog
          title={`Thư mục con của ${cur.name}`}
          onClose={() => setDialog(null)}
          onSubmit={(name) =>
            da.createFolder({ parent_id: cur.folder_id, name }).then(reload)
          }
        />
      ) : null}
      {dialog === "rename" && cur ? (
        <NameDialog
          title="Đổi tên thư mục"
          initial={cur.name}
          onClose={() => setDialog(null)}
          onSubmit={(name) => da.renameFolder(cur.folder_id, name).then(reload)}
        />
      ) : null}
      {dialog === "hide" && cur ? (
        <ReasonDialog
          title={cur.status === "active" ? "Ẩn thư mục" : "Hiện lại thư mục"}
          label="Lý do"
          onClose={() => setDialog(null)}
          onSubmit={(reason) =>
            da
              .hideFolder(cur.folder_id, cur.status === "active", reason)
              .then(reload)
          }
        />
      ) : null}
    </>
  );
}
