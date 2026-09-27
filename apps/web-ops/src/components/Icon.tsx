import type { NavIcon } from "@/nav/nav";

export type IconName = NavIcon | "logout" | "menu" | "leaf" | "alert" | "info" | "search" | "clock" | "area" | "plot";

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
