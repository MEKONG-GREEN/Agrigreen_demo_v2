"use client";

// Quy chuẩn hiển thị bản đồ đang hiệu lực (GIS US-CFG-01/02): GET /v1/masterdata/display-config (mọi người dùng đã
// xác thực). Không tải được → bảng màu mặc định nhúng (chú giải vẫn hiện, không chặn bản đồ).
import { useMemo } from "react";
import { unwrap } from "@/api/client";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { useAsync } from "@/lib/useAsync";
import { DEFAULT_PALETTE, paletteFromConfig, type DisplayConfig, type DisplayPalette } from "./legend";

export interface DisplayPaletteState {
  palette: DisplayPalette;
  config: DisplayConfig | undefined;
  /** Lỗi tải cấu hình (đang dùng mặc định). */
  error: ApiProblem | null;
  loading: boolean;
  reload: () => void;
}

export function useDisplayPalette(): DisplayPaletteState {
  const { api } = useAuth();
  const cfg = useAsync(() => unwrap(api.GET("/v1/masterdata/display-config")), [api]);
  const palette = useMemo(() => (cfg.data ? paletteFromConfig(cfg.data) : DEFAULT_PALETTE), [cfg.data]);
  return { palette, config: cfg.data, error: cfg.error, loading: cfg.loading, reload: cfg.reload };
}
