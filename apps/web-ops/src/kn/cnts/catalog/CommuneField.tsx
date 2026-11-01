"use client";

// Ô chọn xã/phường/đặc khu từ danh mục ĐVHC (BF-CN-SLAUGHTER #169, FN-06 BR-02, FN-01 AC-02): chọn tỉnh rồi chọn xã có
// hiệu lực tại ngày áp dụng, hiện "Tên (mã)". Máy chủ vẫn kiểm lại (masterdata.cnts_commune_invalid).
import { useState } from "react";
import { unitResolver } from "@/api/adapters/units";
import { unwrap, type ApiClient } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { AdminUnit } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField } from "@/components/form";
import { useLoad } from "@/kn/cycle/common";

/** Tên ĐVHC theo mã (bộ nhớ đệm theo client của unitResolver). Mã không có trong danh mục → không có tên. */
export function useUnitNames(codes: readonly string[]): Record<string, string> {
  const { api } = useAuth();
  const key = [...new Set(codes.filter(Boolean))].sort().join(",");
  const s = useLoad(
    key
      ? () =>
          unitResolver(api)
            .many(key.split(","), false)
            .then((m) => Object.fromEntries([...m].map(([c, u]) => [c, u.name])) as Record<string, string>)
      : null,
    [key], // `api` không vào deps: client có thể dựng lại mỗi lần render (như useRefNames)
  );
  return s.data ?? {};
}

function listUnits(api: ApiClient, at: string, parent?: string): Promise<AdminUnit[]> {
  return collectPages((cursor) => unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: { parent, at, cursor, limit: 200 } } })), 5000);
}

export function CommuneField({
  label,
  required,
  value,
  at,
  error,
  onChange,
}: {
  label: string;
  required?: boolean;
  value: string;
  /** Ngày áp dụng: chỉ liệt kê đơn vị có hiệu lực tại ngày này (như máy chủ kiểm). */
  at: string;
  error?: string;
  onChange: (v: string) => void;
}) {
  const { api } = useAuth();
  const [picked, setPicked] = useState("");
  const provinces = useLoad(() => listUnits(api, at).then((us) => us.filter((u) => u.level === "province")), [at]);
  const current = useLoad(value ? () => unitResolver(api).get(value) : null, [value]);
  const plist = provinces.data ?? [];
  const parent = current.data?.parent_code ?? "";
  const province = picked || (plist.some((p) => p.code === parent) ? parent : plist.length === 1 ? plist[0]!.code : "");
  const communes = useLoad(
    province ? () => listUnits(api, at, province).then((us) => us.filter((u) => u.level === "commune").sort((a, b) => a.name.localeCompare(b.name, "vi"))) : null,
    [at, province],
  );
  const options = (communes.data ?? []).map((u) => ({ value: u.code, label: `${u.name} (${u.code})` }));
  if (value && !options.some((o) => o.value === value)) {
    // Giá trị đang lưu không thuộc danh sách đang xem (tỉnh khác, hoặc hết hiệu lực tại ngày áp dụng): vẫn hiện để không mất.
    options.unshift({ value, label: current.data ? `${current.data.name} (${value})` : `${value} — không có trong danh mục ĐVHC tại ngày áp dụng` });
  }
  return (
    <>
      <SelectField
        label="Tỉnh/thành phố của xã"
        value={province}
        placeholder={provinces.loading ? "Đang tải…" : "Chọn tỉnh"}
        onChange={(v) => {
          setPicked(v);
          if (v !== province) onChange("");
        }}
        options={plist.map((p) => ({ value: p.code, label: p.name }))}
      />
      <SelectField
        label={label}
        required={required}
        value={value}
        onChange={onChange}
        error={error}
        placeholder={!province ? "Chọn tỉnh trước" : communes.loading ? "Đang tải…" : "Chọn xã/phường/đặc khu"}
        hint={province && communes.data && communes.data.length === 0 ? "Không có xã có hiệu lực tại ngày áp dụng." : undefined}
        options={options}
      />
    </>
  );
}
