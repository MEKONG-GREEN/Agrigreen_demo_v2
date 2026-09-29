"use client";

import qrcode from "qrcode-generator";
import { useMemo } from "react";

type Ecc = "L" | "M" | "Q" | "H";

/** Ma trận QR (true = ô tối) — dựng hoàn toàn phía trình duyệt, không gửi dữ liệu đi đâu. */
export function qrMatrix(data: string, ecc: Ecc = "M"): boolean[][] {
  const qr = qrcode(0, ecc);
  qr.addData(data, "Byte");
  qr.make();
  const n = qr.getModuleCount();
  return Array.from({ length: n }, (_, r) => Array.from({ length: n }, (_, c) => qr.isDark(r, c)));
}

/**
 * Mã QR SVG cho liên kết kích hoạt tài khoản nông dân (HTX US-ACC-01). Thư viện qrcode-generator (MIT).
 * `minModulePx` theo gợi ý backend (FarmerActivation.qr.min_module_px) để camera điện thoại đọc được.
 */
export function QrCode({ data, label, ecc = "M", minModulePx = 4 }: { data: string; label: string; ecc?: string; minModulePx?: number }) {
  const level: Ecc = ecc === "L" || ecc === "Q" || ecc === "H" ? ecc : "M";
  const m = useMemo(() => qrMatrix(data, level), [data, level]);
  const quiet = 4;
  const size = m.length + quiet * 2;
  const px = Math.max(minModulePx, 4) * size;
  let d = "";
  m.forEach((row, r) =>
    row.forEach((dark, c) => {
      if (dark) d += `M${c + quiet} ${r + quiet}h1v1h-1z`;
    }),
  );
  return (
    <svg viewBox={`0 0 ${size} ${size}`} width={px} height={px} role="img" aria-label={label} shapeRendering="crispEdges" data-modules={m.length}>
      <rect width={size} height={size} fill="#fff" />
      <path d={d} fill="#000" />
    </svg>
  );
}
