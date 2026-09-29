import type { NavIcon } from "@/nav/nav";

export type IconName =
  | NavIcon
  | "logout"
  | "menu"
  | "leaf"
  | "alert"
  | "info"
  | "search"
  | "clock"
  | "area"
  | "plot"
  | "download"
  | "bookmark"
  | "check"
  | "lock"
  | "unlock"
  | "mail";

const PATHS: Record<IconName, string> = {
  dashboard: "M3 3h7v9H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 16h7v5H3z",
  map: "M9 4 3 6v14l6-2 6 2 6-2V4l-6 2-6-2zM9 4v14M15 6v14",
  coop: "M3 21V9l9-6 9 6v12M9 21v-6h6v6",
  task: "M9 11l3 3 8-8M20 12v7a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h9",
  library: "M4 19.5A2.5 2.5 0 0 1 6.5 17H20V3H6.5A2.5 2.5 0 0 0 4 5.5v14zM20 17v4H6.5",
  catalog: "M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01",
  param: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  org: "M12 3v5M5 13v-2h14v2M5 13v3M19 13v3M12 11V8M9 3h6v5H9zM2 16h6v5H2zM16 16h6v5h-6z",
  tractor: "M3 17a3 3 0 1 0 6 0 3 3 0 0 0-6 0zM16 18a2 2 0 1 0 4 0 2 2 0 0 0-4 0zM9 17h7M4 14V8h6l2 6M12 14h8v-3l-3-3h-5",
  logout: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9",
  menu: "M3 6h18M3 12h18M3 18h18",
  leaf: "M11 20A7 7 0 0 1 4 13c0-6 7-10 16-10 0 9-4 16-10 16zM4 21c3-4 6-7 11-10",
  alert: "M12 9v4M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z",
  info: "M12 16v-4M12 8h.01M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z",
  search: "M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.3-4.3",
  clock: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2",
  area: "M3 3h18v18H3zM3 9h18M9 3v18",
  plot: "M4 4l7 3 9-3v16l-9 3-7-3zM11 7v16",
  megaphone: "M3 11v2a1 1 0 0 0 1 1h2l5 4V6L6 10H4a1 1 0 0 0-1 1zM15.5 8.5a5 5 0 0 1 0 7M18.5 5.5a9 9 0 0 1 0 13",
  report: "M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9zM14 3v6h6M8 17v-3M12 17v-6M16 17v-4",
  bell: "M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0",
  sync: "M21 12a9 9 0 0 1-15.5 6.2L3 16M3 12a9 9 0 0 1 15.5-6.2L21 8M21 3v5h-5M3 21v-5h5",
  user: "M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z",
  conflict: "M7 4v16M17 4v16M7 8h4l2 3-2 3H7M17 10h-4",
  upload: "M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12",
  download: "M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3",
  bookmark: "M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z",
  check: "M20 6L9 17l-5-5",
  lock: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4",
  unlock: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 7.5-2",
  mail: "M4 4h16v16H4zM4 6l8 7 8-7",
  layers: "M12 2 2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5",
  water: "M2 12c2-2 4-2 6 0s4 2 6 0 4-2 6 0M2 17c2-2 4-2 6 0s4 2 6 0 4-2 6 0M2 7c2-2 4-2 6 0s4 2 6 0 4-2 6 0",
  warehouse: "M3 21V8l9-5 9 5v13M7 21v-8h10v8M7 17h10",
  sim: "M3 3v18h18M7 15l4-4 3 3 6-7",
  season: "M12 21V11M12 11C12 7 9 4 5 4c0 4 3 7 7 7zM12 14c0-3 3-6 7-6 0 4-3 6-7 6z",
  journal: "M5 3h11a3 3 0 0 1 3 3v15H8a3 3 0 0 1-3-3zM5 18a3 3 0 0 1 3-3h11M9 7h6M9 11h4",
  approve: "M9 11l3 3 8-8M4 4h9M4 9h4M4 14h4M4 19h14",
  harvest: "M4 20h16M6 20V10l6-6 6 6v10M10 20v-5h4v5",
  chart: "M3 3v18h18M7 15l4-4 3 3 6-7",
  qr: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h3v3h-3zM18 18h3v3h-3zM18 14h3M14 18v3",
  support: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3M12 17h.01",
  sop: "M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2M9 3h6v4H9zM9 12h6M9 16h4",
  phone:
    "M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1 1 .4 1.9.7 2.8a2 2 0 0 1-.5 2.1L8 9.9a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.5c.9.3 1.8.6 2.8.7a2 2 0 0 1 1.7 2z",
};

export function Icon({ name, size = 18, label }: { name: IconName; size?: number; label?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      focusable="false"
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
