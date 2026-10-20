// ============================================================================
// BACKEND GIẢ LẬP cho bề mặt KN v7.7 (kiểu trên dây = kiểu sinh từ OpenAPI, src/kn/contract.ts):
//   GET  /v1/me/contexts                 (P1) — các lần gán vai trò của người gọi
//   POST /v1/me/context                  (P1) — chọn vai trò + phòng ban → context_id
//   GET  /v1/menu                        (P2) — KNMenu theo ngữ cảnh phiên (X-AG-Context) hoặc ?audience=&department=
//   GET  /v1/departments/{code}/modules  (P2) — cây module của một phòng ban (?audience=; Admin: nhóm của phòng ban)
//   GET  /v1/me/menu-badges              (P4) — số đếm việc cần xử lý
// Bảng menu = bảng Go internal/platform/party/domain/menu.go (BRD v7.7 §6, Hình 3c–3f): Vitest
// src/api/mock/kn-menu.contract.test.ts so từng ca với ca vàng fixtures/kn-menu.json do Go sinh
// (AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture).
// Đăng nhập mật khẩu giả lập ở `mockKnLogin`.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { DepartmentRef, KnMenu, KnMenuGroup, KnMenuItem, KnRole, MenuTree, RoleAssignment, SessionContext } from "@/kn/contract";
import { fromKnMenu } from "@/kn/menuAdapter";
import { json, problem, type Route } from "./http";
import { findKnAccount, findKnAccountByIdentifier, findKnDepartment, KN_DEPARTMENTS, KN_MENU_PROFILE, knPersona, MOCK_KN_PASSWORD, type KnMenuProfile, type MockKnAccount } from "./kn-accounts";
import type { MockPersona } from "./personas";
import { workSchedulePendingBadge } from "./kn-schedules";
import { cycleBadges } from "./kn-reports/store";

/** domain.MenuVersion. */
export const MENU_VERSION = "kn.menu/v6";

/** Màn Lịch công tác (P5): một đường dẫn cho Cấp Xã (đăng ký) và Cấp Tỉnh/Admin (duyệt) — trang tự chọn theo vai trò. */
export const WORK_SCHEDULE_ROUTE = "/kn/nhiem-vu/lich-cong-tac";
/** Nguồn số đếm menu theo hợp đồng P5 (ExtWorkScheduleCounts.pending_open_weeks) và menu P2 (domain.BadgeScheduleApprovals). */
export const WORK_SCHEDULE_BADGE = "extension.work_schedules.pending";

// Default routes below are generated from the application registration.

/** Cờ "Báo cáo đột xuất": chỉ hiện khi phòng ban đã có mẫu loại này (FN-AD-04.06). Mặc định tắt như Hình 3e/3f. */
const ADHOC_FORMS: Record<string, boolean> = { trong_trot: false };

// Khóa số đếm (domain.Badge*).


interface Item {
  key: string;
  label: string;
  route?: string;
  icon: string;
  badge?: string;
  /** Chỉ hiện khi có mẫu "đột xuất" (MenuRequirement). */
  adHoc?: boolean;
  /** Module chưa có: hiện mờ "Sắp có" (domain.MenuItem.ComingSoon, kn.menu/v3). */
  soon?: boolean;
  children?: Item[];
}
interface Group {
  key: string;
  label: string;
  items: Item[];
}

// BEGIN GENERATED WEB MENU
export const DEFAULT_ROUTE: Record<KnRole, string> = {
  "admin": "/kn/tong-quan",
  "kn_tinh": "/kn/dashboard",
  "kn_tw": "/kn/dashboard",
  "kn_xa": "/kn/nhiem-vu/cong-viec"
};

export const CONTEXT_DEFAULT_ROUTES: Partial<Record<KnMenuProfile, Partial<Record<KnRole, string>>>> = {
  "ke_hoach_tai_chinh": {
    "kn_tinh": "/kn/khtc/ky-bao-cao"
  },
  "trong_trot": {
    "kn_tinh": "/kn/bao-cao-khtc"
  },
  "chan_nuoi_thuy_san": {
    "kn_tinh": "/kn/bao-cao-khtc",
    "kn_xa": "/kn/cnts/bao-cao-ky"
  },
  "thong_tin_truyen_thong": {
    "kn_tinh": "/kn/bao-cao-khtc"
  },
  "hanh_chinh_tong_hop": {
    "kn_tinh": "/kn/bao-cao-khtc"
  }
};

const TRONG_TROT: Record<Exclude<KnRole, "admin">, Group[]> = {
  "kn_tinh": [
    {
      "key": "dieu_hanh",
      "label": "Điều hành",
      "items": [
        {
          "key": "tinh.trang_chu",
          "label": "Trang chủ phòng",
          "route": "/kn/trang-chu",
          "icon": "dashboard"
        },
        {
          "key": "tinh.dashboard",
          "label": "Dashboard",
          "route": "/kn/dashboard",
          "icon": "dashboard"
        },
        {
          "key": "tinh.nhiem_vu",
          "label": "Nhiệm vụ",
          "icon": "task",
          "children": [
            {
              "key": "tinh.nhiem_vu.phat_hanh",
              "label": "Phát hành biểu mẫu",
              "route": "/kn/nhiem-vu/phat-hanh",
              "icon": "send"
            },
            {
              "key": "tinh.nhiem_vu.trang_thai_nop",
              "label": "Trạng thái nộp",
              "route": "/kn/nhiem-vu/trang-thai-nop",
              "icon": "bell",
              "badge": "extension.report_status.not_submitted"
            },
            {
              "key": "tinh.nhiem_vu.ra_soat",
              "label": "Rà soát & chốt kỳ",
              "route": "/kn/nhiem-vu/ra-soat",
              "icon": "check",
              "badge": "extension.submissions.pending_review"
            },
            {
              "key": "tinh.nhiem_vu.lich_cong_tac",
              "label": "Lịch công tác",
              "route": "/kn/nhiem-vu/lich-cong-tac",
              "icon": "calendar",
              "badge": "extension.work_schedules.pending"
            }
          ]
        }
      ]
    },
    {
      "key": "bao_cao",
      "label": "Báo cáo",
      "items": [
        {
          "key": "tinh.bao_cao.tong_hop",
          "label": "Báo cáo tổng hợp các xã",
          "icon": "layers",
          "children": [
            {
              "key": "tinh.bao_cao.tong_hop.tuan",
              "label": "Báo cáo tuần",
              "icon": "file",
              "children": [
                {
                  "key": "tinh.bao_cao.tong_hop.tuan.t01",
                  "label": "T01 · Tiến độ XG",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.tuan.t03",
                  "label": "T03 · Phân trà",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.tuan.t04",
                  "label": "T04 · Rau màu",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.tuan.t05",
                  "label": "T05 · Sâu bệnh lúa",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.tuan.t06",
                  "label": "T06 · Sâu bệnh khác",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.tuan.t07",
                  "label": "T07 · Thu hoạch",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                }
              ]
            },
            {
              "key": "tinh.bao_cao.tong_hop.thang",
              "label": "Báo cáo tháng",
              "route": "/kn/bao-cao/tong-hop",
              "icon": "calendar"
            },
            {
              "key": "tinh.bao_cao.tong_hop.hang_vu",
              "label": "Báo cáo hàng vụ",
              "icon": "sprout",
              "children": [
                {
                  "key": "tinh.bao_cao.tong_hop.hang_vu.v01",
                  "label": "V01 · 3G3T",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.hang_vu.v02",
                  "label": "V02 · 1P5G",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.hang_vu.v03",
                  "label": "V03 · CGH",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.hang_vu.v04",
                  "label": "V04 · CCG",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.hang_vu.v05",
                  "label": "V05 · Lượng giống",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                },
                {
                  "key": "tinh.bao_cao.tong_hop.hang_vu.v06",
                  "label": "V06 · Mô hình",
                  "route": "/kn/bao-cao/tong-hop",
                  "icon": "file"
                }
              ]
            },
            {
              "key": "tinh.bao_cao.tong_hop.nam",
              "label": "Báo cáo năm",
              "route": "/kn/bao-cao/tong-hop",
              "icon": "chart"
            }
          ]
        },
        {
          "key": "tinh.bao_cao.chi_cuc",
          "label": "Báo cáo Chi cục",
          "route": "/kn/bao-cao/chi-cuc",
          "icon": "file"
        },
        {
          "key": "tinh.bao_cao.dot_xuat",
          "label": "Báo cáo đột xuất",
          "route": "/kn/bao-cao/dot-xuat",
          "icon": "zap",
          "badge": "extension.report_status.ad_hoc_pending",
          "adHoc": true
        }
      ]
    },
    {
      "key": "du_lieu",
      "label": "Dữ liệu",
      "items": [
        {
          "key": "tinh.thu_thap",
          "label": "Thu thập dữ liệu",
          "icon": "database",
          "children": [
            {
              "key": "tinh.thu_thap.htx",
              "label": "Danh sách HTX",
              "route": "/kn/du-lieu/htx",
              "icon": "coop"
            },
            {
              "key": "tinh.thu_thap.may_moc",
              "label": "Máy móc HTX",
              "route": "/kn/du-lieu/may-moc",
              "icon": "tractor"
            },
            {
              "key": "tinh.thu_thap.thua",
              "label": "Thửa ruộng",
              "route": "/kn/du-lieu/ve-thua",
              "icon": "map"
            },
            {
              "key": "tinh.thu_thap.vat_tu",
              "label": "Vật tư",
              "route": "/kn/du-lieu/vat-tu",
              "icon": "package"
            }
          ]
        },
        {
          "key": "tinh.van_ban",
          "label": "Quy trình, văn bản",
          "route": "/kn/van-ban",
          "icon": "library"
        }
      ]
    }
  ],
  "kn_tw": [
    {
      "key": "dieu_hanh",
      "label": "Điều hành",
      "items": [
        {
          "key": "tw.dashboard",
          "label": "Dashboard",
          "route": "/kn/dashboard",
          "icon": "dashboard"
        }
      ]
    },
    {
      "key": "bao_cao",
      "label": "Báo cáo",
      "items": [
        {
          "key": "tw.bao_cao.tuan",
          "label": "Báo cáo tuần",
          "route": "/kn/bao-cao/tuan",
          "icon": "file"
        },
        {
          "key": "tw.bao_cao.thang",
          "label": "Báo cáo tháng",
          "route": "/kn/bao-cao/thang",
          "icon": "calendar"
        },
        {
          "key": "tw.bao_cao.hang_vu",
          "label": "Báo cáo hàng vụ",
          "route": "/kn/bao-cao/vu",
          "icon": "sprout"
        },
        {
          "key": "tw.bao_cao.nam",
          "label": "Báo cáo năm",
          "route": "/kn/bao-cao/nam",
          "icon": "chart"
        }
      ]
    }
  ],
  "kn_xa": [
    {
      "key": "dieu_hanh",
      "label": "Điều hành",
      "items": [
        {
          "key": "xa.nhiem_vu",
          "label": "Nhiệm vụ",
          "icon": "task",
          "children": [
            {
              "key": "xa.nhiem_vu.cong_viec",
              "label": "Công việc cần làm",
              "route": "/kn/nhiem-vu/cong-viec",
              "icon": "bell",
              "badge": "extension.report_tasks.todo"
            },
            {
              "key": "xa.nhiem_vu.lich_cong_tac",
              "label": "Lịch công tác",
              "route": "/kn/nhiem-vu/lich-cong-tac",
              "icon": "calendar"
            }
          ]
        }
      ]
    },
    {
      "key": "bao_cao",
      "label": "Báo cáo",
      "items": [
        {
          "key": "xa.bao_cao.tuan",
          "label": "Báo cáo tuần",
          "icon": "file",
          "children": [
            {
              "key": "xa.bao_cao.tuan.t01",
              "label": "T01 · Tiến độ XG",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.tuan.t02",
              "label": "T02 · Lúa mùa",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.tuan.t03",
              "label": "T03 · Phân trà",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.tuan.t04",
              "label": "T04 · Rau màu",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.tuan.t05",
              "label": "T05 · Sâu bệnh lúa",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.tuan.t06",
              "label": "T06 · Sâu bệnh khác",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.tuan.t07",
              "label": "T07 · Thu hoạch",
              "route": "/kn/bao-cao/tuan",
              "icon": "file"
            }
          ]
        },
        {
          "key": "xa.bao_cao.thang",
          "label": "Báo cáo tháng",
          "route": "/kn/bao-cao/thang",
          "icon": "calendar"
        },
        {
          "key": "xa.bao_cao.hang_vu",
          "label": "Báo cáo hàng vụ",
          "icon": "sprout",
          "children": [
            {
              "key": "xa.bao_cao.hang_vu.v01",
              "label": "V01 · 3G3T",
              "route": "/kn/bao-cao/vu",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.hang_vu.v02",
              "label": "V02 · 1P5G",
              "route": "/kn/bao-cao/vu",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.hang_vu.v03",
              "label": "V03 · CGH",
              "route": "/kn/bao-cao/vu",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.hang_vu.v04",
              "label": "V04 · CCG",
              "route": "/kn/bao-cao/vu",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.hang_vu.v05",
              "label": "V05 · Lượng giống",
              "route": "/kn/bao-cao/vu",
              "icon": "file"
            },
            {
              "key": "xa.bao_cao.hang_vu.v06",
              "label": "V06 · Mô hình",
              "route": "/kn/bao-cao/vu",
              "icon": "file"
            }
          ]
        },
        {
          "key": "xa.bao_cao.cua_xa",
          "label": "Báo cáo của xã",
          "route": "/kn/bao-cao/cua-xa",
          "icon": "chart"
        },
        {
          "key": "xa.bao_cao.dot_xuat",
          "label": "Báo cáo đột xuất",
          "route": "/kn/bao-cao/dot-xuat",
          "icon": "zap",
          "badge": "extension.report_tasks.ad_hoc_todo",
          "adHoc": true
        }
      ]
    },
    {
      "key": "du_lieu",
      "label": "Dữ liệu",
      "items": [
        {
          "key": "xa.thu_thap",
          "label": "Thu thập dữ liệu",
          "icon": "database",
          "children": [
            {
              "key": "xa.thu_thap.htx",
              "label": "Danh sách HTX",
              "route": "/kn/du-lieu/htx",
              "icon": "coop"
            },
            {
              "key": "xa.thu_thap.may_moc",
              "label": "Máy móc HTX",
              "route": "/kn/du-lieu/may-moc",
              "icon": "tractor"
            },
            {
              "key": "xa.thu_thap.thua",
              "label": "Vẽ thửa ruộng",
              "route": "/kn/du-lieu/ve-thua",
              "icon": "map"
            },
            {
              "key": "xa.thu_thap.vat_tu",
              "label": "Vật tư",
              "route": "/kn/du-lieu/vat-tu",
              "icon": "package"
            }
          ]
        },
        {
          "key": "xa.van_ban",
          "label": "Quy trình, văn bản",
          "route": "/kn/van-ban",
          "icon": "library"
        }
      ]
    }
  ]
};

const GENERIC: Record<Exclude<KnRole, "admin">, Group[]> = {
  "kn_tinh": [
    {
      "key": "dieu_hanh",
      "label": "Điều hành",
      "items": [
        {
          "key": "tinh.trang_chu",
          "label": "Trang chủ phòng",
          "route": "/kn/trang-chu",
          "icon": "dashboard"
        },
        {
          "key": "tinh.dashboard",
          "label": "Dashboard",
          "route": "/kn/dashboard",
          "icon": "dashboard"
        },
        {
          "key": "tinh.nhiem_vu",
          "label": "Nhiệm vụ",
          "icon": "task",
          "children": [
            {
              "key": "tinh.nhiem_vu.phat_hanh",
              "label": "Phát hành biểu mẫu",
              "route": "/kn/nhiem-vu/phat-hanh",
              "icon": "send"
            },
            {
              "key": "tinh.nhiem_vu.trang_thai_nop",
              "label": "Trạng thái nộp",
              "route": "/kn/nhiem-vu/trang-thai-nop",
              "icon": "bell",
              "badge": "extension.report_status.not_submitted"
            },
            {
              "key": "tinh.nhiem_vu.ra_soat",
              "label": "Rà soát & chốt kỳ",
              "route": "/kn/nhiem-vu/ra-soat",
              "icon": "check",
              "badge": "extension.submissions.pending_review"
            }
          ]
        }
      ]
    },
    {
      "key": "bao_cao",
      "label": "Báo cáo",
      "items": [
        {
          "key": "tinh.bao_cao.tong_hop",
          "label": "Báo cáo tổng hợp các xã",
          "route": "/kn/bao-cao/tong-hop",
          "icon": "layers"
        },
        {
          "key": "tinh.bao_cao.dot_xuat",
          "label": "Báo cáo đột xuất",
          "route": "/kn/bao-cao/dot-xuat",
          "icon": "zap",
          "badge": "extension.report_status.ad_hoc_pending",
          "adHoc": true
        }
      ]
    }
  ],
  "kn_tw": [
    {
      "key": "dieu_hanh",
      "label": "Điều hành",
      "items": [
        {
          "key": "tw.dashboard",
          "label": "Dashboard",
          "route": "/kn/dashboard",
          "icon": "dashboard"
        }
      ]
    },
    {
      "key": "bao_cao",
      "label": "Báo cáo",
      "items": [
        {
          "key": "tw.bao_cao.tuan",
          "label": "Báo cáo tuần",
          "route": "/kn/bao-cao/tuan",
          "icon": "file"
        },
        {
          "key": "tw.bao_cao.thang",
          "label": "Báo cáo tháng",
          "route": "/kn/bao-cao/thang",
          "icon": "calendar"
        },
        {
          "key": "tw.bao_cao.hang_vu",
          "label": "Báo cáo hàng vụ",
          "route": "/kn/bao-cao/vu",
          "icon": "sprout"
        },
        {
          "key": "tw.bao_cao.nam",
          "label": "Báo cáo năm",
          "route": "/kn/bao-cao/nam",
          "icon": "chart"
        }
      ]
    }
  ],
  "kn_xa": [
    {
      "key": "dieu_hanh",
      "label": "Điều hành",
      "items": [
        {
          "key": "xa.nhiem_vu",
          "label": "Nhiệm vụ",
          "icon": "task",
          "children": [
            {
              "key": "xa.nhiem_vu.cong_viec",
              "label": "Công việc cần làm",
              "route": "/kn/nhiem-vu/cong-viec",
              "icon": "bell",
              "badge": "extension.report_tasks.todo"
            }
          ]
        }
      ]
    },
    {
      "key": "bao_cao",
      "label": "Báo cáo",
      "items": [
        {
          "key": "xa.bao_cao.tuan",
          "label": "Báo cáo tuần",
          "route": "/kn/bao-cao/tuan",
          "icon": "file"
        },
        {
          "key": "xa.bao_cao.thang",
          "label": "Báo cáo tháng",
          "route": "/kn/bao-cao/thang",
          "icon": "calendar"
        },
        {
          "key": "xa.bao_cao.hang_vu",
          "label": "Báo cáo hàng vụ",
          "route": "/kn/bao-cao/vu",
          "icon": "sprout"
        },
        {
          "key": "xa.bao_cao.cua_xa",
          "label": "Báo cáo của xã",
          "route": "/kn/bao-cao/cua-xa",
          "icon": "chart"
        },
        {
          "key": "xa.bao_cao.dot_xuat",
          "label": "Báo cáo đột xuất",
          "route": "/kn/bao-cao/dot-xuat",
          "icon": "zap",
          "badge": "extension.report_tasks.ad_hoc_todo",
          "adHoc": true
        }
      ]
    }
  ]
};

const SHARED: Group = {
  "key": "dung_chung",
  "label": "Dùng chung",
  "items": [
    {
      "key": "chung.contact",
      "label": "Contact",
      "route": "/kn/dung-chung/contact",
      "icon": "phone"
    }
  ]
};

const SHARED_BY_AUDIENCE: Partial<Record<KnRole, Item[]>> = {
  "kn_tinh": [
    {
      "key": "tinh.chung.nhan_su_phong",
      "label": "Nhân sự của phòng",
      "route": "/kn/nhan-su/cua-phong",
      "icon": "users"
    },
    {
      "key": "tinh.chung.ke_hoach_phong",
      "label": "Kế hoạch của phòng",
      "route": "/kn/nhan-su/ke-hoach-phong",
      "icon": "calendar"
    },
    {
      "key": "tinh.chung.dashboard_nhan_su",
      "label": "Dashboard nhân sự",
      "route": "/kn/nhan-su/dashboard",
      "icon": "chart"
    },
    {
      "key": "tinh.chung.tai_lieu_phong",
      "label": "Tài liệu của phòng",
      "route": "/kn/nhan-su/tai-lieu-phong",
      "icon": "library"
    }
  ]
};

const BUSINESS: Partial<Record<KnMenuProfile, Partial<Record<Exclude<KnRole, "admin">, Item[]>>>> = {};

const DEPT_MENUS: Partial<Record<KnMenuProfile, Partial<Record<Exclude<KnRole, "admin">, Group[]>>>> = {
  "chan_nuoi_thuy_san": {
    "kn_tinh": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tinh.trang_chu",
            "label": "Trang chủ phòng",
            "route": "/kn/trang-chu",
            "icon": "dashboard"
          },
          {
            "key": "tinh.cnts.trang_chu",
            "label": "Trang chủ",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          },
          {
            "key": "tinh.cnts.canh_bao",
            "label": "Cảnh báo",
            "route": "/kn/cnts/canh-bao",
            "icon": "zap"
          },
          {
            "key": "tinh.cnts.bieu_mau",
            "label": "Biểu mẫu",
            "icon": "table",
            "children": [
              {
                "key": "tinh.cnts.bieu_mau.thiet_ke",
                "label": "Thiết kế biểu mẫu",
                "route": "/kn/cnts/thiet-ke-bieu-mau",
                "icon": "table",
                "soon": true
              },
              {
                "key": "tinh.cnts.bieu_mau.phat_hanh",
                "label": "Phát hành, theo dõi nộp",
                "route": "/kn/cnts/bieu-mau",
                "icon": "send"
              },
              {
                "key": "tinh.cnts.bieu_mau.nhac_nho",
                "label": "Cấu hình nhắc nhở",
                "route": "/kn/cnts/canh-bao",
                "icon": "bell"
              },
              {
                "key": "tinh.cnts.bieu_mau.ra_soat_nhat_ky",
                "label": "Rà soát nhật ký",
                "route": "/kn/cnts/ra-soat-nhat-ky",
                "icon": "journal"
              },
              {
                "key": "tinh.cnts.bieu_mau.cap_quyen",
                "label": "Cấp quyền",
                "route": "/kn/cnts/cap-quyen",
                "icon": "key"
              }
            ]
          },
          {
            "key": "tinh.cnts.lich",
            "label": "Lịch",
            "icon": "calendar",
            "children": [
              {
                "key": "tinh.cnts.lich.truc",
                "label": "Lịch trực KSGM",
                "route": "/kn/cnts/lich-truc",
                "icon": "calendar"
              },
              {
                "key": "tinh.cnts.lich.tiem",
                "label": "Lịch tiêm phòng",
                "route": "/kn/cnts/lich-tiem",
                "icon": "check"
              },
              {
                "key": "tinh.cnts.lich.cong_tac_xa",
                "label": "Lịch công tác cán bộ xã",
                "route": "/kn/nhiem-vu/lich-cong-tac",
                "icon": "calendar",
                "badge": "extension.work_schedules.pending"
              },
              {
                "key": "tinh.cnts.lich.cong_tac_phong",
                "label": "Lịch công tác phòng",
                "route": "/kn/nhiem-vu/lich-cong-tac",
                "icon": "calendar"
              }
            ]
          },
          {
            "key": "tinh.cnts.chien_dich",
            "label": "Chiến dịch điều tra",
            "icon": "megaphone",
            "children": [
              {
                "key": "tinh.cnts.chien_dich.quan_ly",
                "label": "Quản lý chiến dịch",
                "route": "/kn/cnts/chien-dich",
                "icon": "megaphone"
              },
              {
                "key": "tinh.cnts.chien_dich.tao",
                "label": "Tạo chiến dịch",
                "route": "/kn/cnts/chien-dich/tao",
                "icon": "megaphone",
                "soon": true
              }
            ]
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tinh.cnts.bao_cao_dinh_ky",
            "label": "Báo cáo định kỳ",
            "route": "/kn/cnts/bao-cao-dinh-ky",
            "icon": "report",
            "soon": true
          },
          {
            "key": "tinh.cnts.ksgm",
            "label": "KSGM tuần, tháng",
            "route": "/kn/cnts/ksgm",
            "icon": "chart"
          },
          {
            "key": "tinh.cnts.phu_luc_2",
            "label": "Thủy sản (Phụ lục 2)",
            "route": "/kn/cnts/phu-luc-2",
            "icon": "report"
          },
          {
            "key": "tinh.cnts.dich_benh",
            "label": "Báo cáo dịch bệnh",
            "route": "/kn/cnts/dich-benh",
            "icon": "shield"
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "tinh.cnts.so_lieu_xa",
            "label": "Số liệu cấp xã",
            "icon": "database",
            "children": [
              {
                "key": "tinh.cnts.so_lieu_xa.nhap_lieu_tuan",
                "label": "Nhập liệu tuần",
                "route": "/kn/cnts/so-lieu-xa/nhap-lieu-tuan",
                "icon": "table",
                "soon": true
              },
              {
                "key": "tinh.cnts.so_lieu_xa.khai_thac_thuy_san",
                "label": "Khai thác thủy sản",
                "route": "/kn/cnts/so-lieu-xa/khai-thac-thuy-san",
                "icon": "water",
                "soon": true
              },
              {
                "key": "tinh.cnts.so_lieu_xa.nhat_ky",
                "label": "Nhật ký sản xuất",
                "route": "/kn/cnts/so-lieu-xa/nhat-ky",
                "icon": "journal",
                "soon": true
              },
              {
                "key": "tinh.cnts.so_lieu_xa.lich_tiem",
                "label": "Lịch tiêm (thực tế)",
                "route": "/kn/cnts/lich-tiem",
                "icon": "check"
              }
            ]
          },
          {
            "key": "tinh.cnts.ban_do_dich",
            "label": "Bản đồ vùng dịch",
            "route": "/kn/cnts/ban-do-dich",
            "icon": "map"
          },
          {
            "key": "tinh.cnts.du_an_knqg",
            "label": "Dự án KNQG",
            "icon": "report",
            "children": [
              {
                "key": "tinh.cnts.du_an_knqg.du_an",
                "label": "Dự án, điểm trình diễn",
                "route": "/kn/cnts/du-an",
                "icon": "report"
              },
              {
                "key": "tinh.cnts.du_an_knqg.mau_bieu",
                "label": "Mẫu biểu dự án",
                "route": "/kn/cnts/du-an-mau-bieu",
                "icon": "table",
                "soon": true
              }
            ]
          },
          {
            "key": "tinh.cnts.quy_trinh",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "sop"
          }
        ]
      }
    ],
    "kn_xa": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "xa.cnts.viec_can_lam",
            "label": "Việc cần làm",
            "route": "/kn/cnts/bao-cao-ky",
            "icon": "bell"
          },
          {
            "key": "xa.cnts.lich",
            "label": "Lịch trực KSGM của tôi",
            "route": "/kn/cnts/lich-truc",
            "icon": "calendar"
          },
          {
            "key": "xa.cnts.lich_cong_tac",
            "label": "Lịch công tác",
            "route": "/kn/nhiem-vu/lich-cong-tac",
            "icon": "calendar"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "xa.cnts.ksgm_ngay",
            "label": "KSGM hằng ngày",
            "route": "/kn/cnts/bao-cao-ky",
            "icon": "chart"
          },
          {
            "key": "xa.cnts.nhap_lieu_tuan",
            "label": "Nhập liệu tuần chăn nuôi",
            "route": "/kn/cnts/bao-cao-ky",
            "icon": "table"
          },
          {
            "key": "xa.cnts.khai_thac_thuy_san",
            "label": "Khai thác thủy sản tuần",
            "route": "/kn/cnts/bao-cao-ky",
            "icon": "water"
          },
          {
            "key": "xa.cnts.cong_viec_tuan",
            "label": "Báo cáo công việc tuần",
            "route": "/kn/cnts/bao-cao-ky",
            "icon": "check"
          },
          {
            "key": "xa.cnts.dich_benh",
            "label": "Báo cáo dịch bệnh",
            "route": "/kn/cnts/dich-benh",
            "icon": "shield"
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "xa.cnts.ho_mo_hinh",
            "label": "Hộ mô hình",
            "route": "/kn/cnts/ho-mo-hinh",
            "icon": "users"
          },
          {
            "key": "xa.cnts.nhat_ky",
            "label": "Nhật ký sản xuất",
            "icon": "journal",
            "children": [
              {
                "key": "xa.cnts.nhat_ky.thuy_san",
                "label": "Thủy sản",
                "route": "/kn/cnts/nhat-ky",
                "icon": "water"
              },
              {
                "key": "xa.cnts.nhat_ky.chan_nuoi",
                "label": "Chăn nuôi",
                "route": "/kn/cnts/nhat-ky",
                "icon": "sprout"
              }
            ]
          },
          {
            "key": "xa.cnts.lich_tiem",
            "label": "Lịch tiêm phòng",
            "route": "/kn/cnts/lich-tiem",
            "icon": "check"
          },
          {
            "key": "xa.cnts.ban_do_dich",
            "label": "Bản đồ vùng dịch",
            "route": "/kn/cnts/ban-do-dich",
            "icon": "map"
          },
          {
            "key": "xa.cnts.diem_trinh_dien",
            "label": "Điểm trình diễn",
            "route": "/kn/cnts/ho-mo-hinh",
            "icon": "report"
          },
          {
            "key": "xa.cnts.quy_trinh",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "sop"
          }
        ]
      }
    ],
    "kn_tw": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tw.dashboard",
            "label": "Dashboard",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tw.bao_cao.tuan",
            "label": "Báo cáo tuần",
            "route": "/kn/bao-cao/tuan",
            "icon": "file"
          },
          {
            "key": "tw.bao_cao.thang",
            "label": "Báo cáo tháng",
            "route": "/kn/bao-cao/thang",
            "icon": "calendar"
          },
          {
            "key": "tw.bao_cao.hang_vu",
            "label": "Báo cáo hàng vụ",
            "route": "/kn/bao-cao/vu",
            "icon": "sprout"
          },
          {
            "key": "tw.bao_cao.nam",
            "label": "Báo cáo năm",
            "route": "/kn/bao-cao/nam",
            "icon": "chart"
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "tw.cnts.du_an_knqg",
            "label": "Dự án KNQG",
            "route": "/kn/cnts/du-an",
            "icon": "report"
          }
        ]
      }
    ]
  },
  "ke_hoach_tai_chinh": {
    "kn_tinh": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tinh.trang_chu",
            "label": "Trang chủ phòng",
            "route": "/kn/trang-chu",
            "icon": "dashboard"
          },
          {
            "key": "tinh.dashboard",
            "label": "Dashboard",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          },
          {
            "key": "tinh.nhiem_vu",
            "label": "Nhiệm vụ",
            "icon": "task",
            "children": [
              {
                "key": "tinh.nhiem_vu.phat_hanh",
                "label": "Phát hành biểu mẫu",
                "route": "/kn/nhiem-vu/phat-hanh",
                "icon": "send"
              },
              {
                "key": "tinh.nhiem_vu.trang_thai_nop",
                "label": "Trạng thái nộp",
                "route": "/kn/nhiem-vu/trang-thai-nop",
                "icon": "bell",
                "badge": "extension.report_status.not_submitted"
              },
              {
                "key": "tinh.nhiem_vu.ra_soat",
                "label": "Rà soát & chốt kỳ",
                "route": "/kn/nhiem-vu/ra-soat",
                "icon": "check",
                "badge": "extension.submissions.pending_review"
              }
            ]
          },
          {
            "key": "tinh.khtc.ky_bao_cao",
            "label": "Kỳ báo cáo & phiếu các phòng",
            "route": "/kn/khtc/ky-bao-cao",
            "icon": "task"
          },
          {
            "key": "tinh.khtc.ke_hoach_nam",
            "label": "Kế hoạch năm",
            "route": "/kn/khtc/ke-hoach-nam",
            "icon": "calendar"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tinh.bao_cao.tong_hop",
            "label": "Báo cáo tổng hợp các xã",
            "route": "/kn/bao-cao/tong-hop",
            "icon": "layers"
          },
          {
            "key": "tinh.bao_cao.dot_xuat",
            "label": "Báo cáo đột xuất",
            "route": "/kn/bao-cao/dot-xuat",
            "icon": "zap",
            "badge": "extension.report_status.ad_hoc_pending",
            "adHoc": true
          },
          {
            "key": "tinh.khtc.bao_cao",
            "label": "Soạn báo cáo (Mẫu 1–5)",
            "route": "/kn/khtc/bao-cao",
            "icon": "report"
          },
          {
            "key": "tinh.khtc.tien_do",
            "label": "Tiến độ các chương trình",
            "route": "/kn/khtc/tien-do",
            "icon": "chart"
          },
          {
            "key": "tinh.khtc.da_xuat",
            "label": "Báo cáo đã xuất",
            "route": "/kn/khtc/bao-cao-da-xuat",
            "icon": "report"
          },
          {
            "key": "tinh.khtc.mau_bao_cao",
            "label": "Mẫu báo cáo KHTC",
            "route": "/kn/khtc/mau-bao-cao",
            "icon": "table",
            "soon": true
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "tinh.khtc.so_lieu_ngoai",
            "label": "Số liệu nguồn ngoài",
            "route": "/kn/khtc/so-lieu-ngoai",
            "icon": "database",
            "soon": true
          },
          {
            "key": "tinh.van_ban",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "library"
          }
        ]
      }
    ],
    "kn_tw": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tw.dashboard",
            "label": "Dashboard",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tw.bao_cao.tuan",
            "label": "Báo cáo tuần",
            "route": "/kn/bao-cao/tuan",
            "icon": "file"
          },
          {
            "key": "tw.bao_cao.thang",
            "label": "Báo cáo tháng",
            "route": "/kn/bao-cao/thang",
            "icon": "calendar"
          },
          {
            "key": "tw.bao_cao.hang_vu",
            "label": "Báo cáo hàng vụ",
            "route": "/kn/bao-cao/vu",
            "icon": "sprout"
          },
          {
            "key": "tw.bao_cao.nam",
            "label": "Báo cáo năm",
            "route": "/kn/bao-cao/nam",
            "icon": "chart"
          },
          {
            "key": "tw.khtc.bao_cao_tinh",
            "label": "Báo cáo Tỉnh (KHTC)",
            "route": "/kn/khtc/bao-cao-da-xuat",
            "icon": "report",
            "soon": true
          }
        ]
      }
    ]
  },
  "thong_tin_truyen_thong": {
    "kn_tinh": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tinh.trang_chu",
            "label": "Trang chủ phòng",
            "route": "/kn/trang-chu",
            "icon": "dashboard"
          },
          {
            "key": "tinh.dashboard",
            "label": "Dashboard",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          },
          {
            "key": "tinh.nhiem_vu",
            "label": "Nhiệm vụ",
            "icon": "task",
            "children": [
              {
                "key": "tinh.nhiem_vu.phat_hanh",
                "label": "Phát hành biểu mẫu",
                "route": "/kn/nhiem-vu/phat-hanh",
                "icon": "send"
              },
              {
                "key": "tinh.nhiem_vu.trang_thai_nop",
                "label": "Trạng thái nộp",
                "route": "/kn/nhiem-vu/trang-thai-nop",
                "icon": "bell",
                "badge": "extension.report_status.not_submitted"
              },
              {
                "key": "tinh.nhiem_vu.ra_soat",
                "label": "Rà soát & chốt kỳ",
                "route": "/kn/nhiem-vu/ra-soat",
                "icon": "check",
                "badge": "extension.submissions.pending_review"
              }
            ]
          },
          {
            "key": "tinh.bao_gia.tien_do",
            "label": "Tiến độ nhập giá",
            "route": "/kn/bao-gia/tien-do",
            "icon": "bell"
          },
          {
            "key": "tinh.bao_gia.kiem_duyet",
            "label": "Kiểm duyệt & công bố",
            "route": "/kn/bao-gia/kiem-duyet",
            "icon": "approve"
          },
          {
            "key": "tinh.bao_gia.cau_hinh",
            "label": "Cấu hình báo giá",
            "route": "/kn/bao-gia/cau-hinh",
            "icon": "param"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tinh.bao_cao.tong_hop",
            "label": "Báo cáo tổng hợp các xã",
            "route": "/kn/bao-cao/tong-hop",
            "icon": "layers"
          },
          {
            "key": "tinh.bao_cao.dot_xuat",
            "label": "Báo cáo đột xuất",
            "route": "/kn/bao-cao/dot-xuat",
            "icon": "zap",
            "badge": "extension.report_status.ad_hoc_pending",
            "adHoc": true
          },
          {
            "key": "tinh.bao_gia.bang_gia",
            "label": "Bảng giá tổng hợp",
            "route": "/kn/bao-gia/bang-gia",
            "icon": "chart"
          },
          {
            "key": "tinh.bao_gia.bien_dong",
            "label": "Báo cáo biến động giá",
            "route": "/kn/bao-gia/bien-dong",
            "icon": "layers"
          },
          {
            "key": "tinh.bao_gia.gia_thanh",
            "label": "Giá thành sản xuất",
            "route": "/kn/bao-gia/gia-thanh",
            "icon": "report"
          },
          {
            "key": "tinh.bao_gia.dashboard",
            "label": "Dashboard theo vùng",
            "route": "/kn/bao-gia/dashboard-vung",
            "icon": "map"
          },
          {
            "key": "tinh.bao_gia.thong_ke",
            "label": "Thống kê phiếu báo giá",
            "route": "/kn/bao-gia/thong-ke",
            "icon": "table"
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "tinh.bao_gia.dieu_tra",
            "label": "Điều tra giá thành",
            "route": "/kn/bao-gia/dieu-tra",
            "icon": "season"
          },
          {
            "key": "tinh.bao_gia.mat_hang",
            "label": "Mặt hàng",
            "route": "/kn/bao-gia/mat-hang",
            "icon": "package",
            "soon": true
          },
          {
            "key": "tinh.van_ban",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "library"
          }
        ]
      }
    ],
    "kn_tw": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tw.dashboard",
            "label": "Dashboard",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tw.bao_cao.tuan",
            "label": "Báo cáo tuần",
            "route": "/kn/bao-cao/tuan",
            "icon": "file"
          },
          {
            "key": "tw.bao_cao.thang",
            "label": "Báo cáo tháng",
            "route": "/kn/bao-cao/thang",
            "icon": "calendar"
          },
          {
            "key": "tw.bao_cao.hang_vu",
            "label": "Báo cáo hàng vụ",
            "route": "/kn/bao-cao/vu",
            "icon": "sprout"
          },
          {
            "key": "tw.bao_cao.nam",
            "label": "Báo cáo năm",
            "route": "/kn/bao-cao/nam",
            "icon": "chart"
          },
          {
            "key": "tw.bao_gia.bang_gia",
            "label": "Bảng giá tổng hợp",
            "route": "/kn/bao-gia/bang-gia",
            "icon": "chart"
          },
          {
            "key": "tw.bao_gia.dashboard",
            "label": "Dashboard theo vùng",
            "route": "/kn/bao-gia/dashboard-vung",
            "icon": "map"
          },
          {
            "key": "tw.bao_gia.bien_dong",
            "label": "Báo cáo biến động giá",
            "route": "/kn/bao-gia/bien-dong",
            "icon": "layers"
          },
          {
            "key": "tw.bao_gia.gia_thanh",
            "label": "Giá thành sản xuất",
            "route": "/kn/bao-gia/gia-thanh",
            "icon": "report"
          }
        ]
      }
    ],
    "kn_xa": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "xa.nhiem_vu",
            "label": "Nhiệm vụ",
            "icon": "task",
            "children": [
              {
                "key": "xa.nhiem_vu.cong_viec",
                "label": "Công việc cần làm",
                "route": "/kn/nhiem-vu/cong-viec",
                "icon": "bell",
                "badge": "extension.report_tasks.todo"
              }
            ]
          },
          {
            "key": "xa.bao_gia.nhap",
            "label": "Nhập báo giá",
            "route": "/kn/bao-gia/nhap",
            "icon": "upload"
          },
          {
            "key": "xa.bao_gia.xac_minh",
            "label": "Yêu cầu xác minh",
            "route": "/kn/bao-gia/xac-minh",
            "icon": "check"
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "xa.bao_cao.tuan",
            "label": "Báo cáo tuần",
            "route": "/kn/bao-cao/tuan",
            "icon": "file"
          },
          {
            "key": "xa.bao_cao.thang",
            "label": "Báo cáo tháng",
            "route": "/kn/bao-cao/thang",
            "icon": "calendar"
          },
          {
            "key": "xa.bao_cao.hang_vu",
            "label": "Báo cáo hàng vụ",
            "route": "/kn/bao-cao/vu",
            "icon": "sprout"
          },
          {
            "key": "xa.bao_cao.cua_xa",
            "label": "Báo cáo của xã",
            "route": "/kn/bao-cao/cua-xa",
            "icon": "chart"
          },
          {
            "key": "xa.bao_cao.dot_xuat",
            "label": "Báo cáo đột xuất",
            "route": "/kn/bao-cao/dot-xuat",
            "icon": "zap",
            "badge": "extension.report_tasks.ad_hoc_todo",
            "adHoc": true
          },
          {
            "key": "xa.bao_gia.bang_gia",
            "label": "Bảng giá tổng hợp",
            "route": "/kn/bao-gia/bang-gia",
            "icon": "chart"
          },
          {
            "key": "xa.bao_gia.bien_dong",
            "label": "Báo cáo biến động giá",
            "route": "/kn/bao-gia/bien-dong",
            "icon": "layers"
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "xa.bao_gia.lich_su",
            "label": "Lịch sử báo giá",
            "route": "/kn/bao-gia/lich-su",
            "icon": "journal"
          },
          {
            "key": "xa.van_ban",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "library"
          }
        ]
      }
    ]
  },
  "hanh_chinh_tong_hop": {
    "kn_tinh": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "tinh.trang_chu",
            "label": "Trang chủ phòng",
            "route": "/kn/trang-chu",
            "icon": "dashboard"
          },
          {
            "key": "tinh.dashboard",
            "label": "Dashboard",
            "route": "/kn/dashboard",
            "icon": "dashboard"
          },
          {
            "key": "tinh.nhiem_vu",
            "label": "Nhiệm vụ",
            "icon": "task",
            "children": [
              {
                "key": "tinh.nhiem_vu.phat_hanh",
                "label": "Phát hành biểu mẫu",
                "route": "/kn/nhiem-vu/phat-hanh",
                "icon": "send"
              },
              {
                "key": "tinh.nhiem_vu.trang_thai_nop",
                "label": "Trạng thái nộp",
                "route": "/kn/nhiem-vu/trang-thai-nop",
                "icon": "bell",
                "badge": "extension.report_status.not_submitted"
              },
              {
                "key": "tinh.nhiem_vu.ra_soat",
                "label": "Rà soát & chốt kỳ",
                "route": "/kn/nhiem-vu/ra-soat",
                "icon": "check",
                "badge": "extension.submissions.pending_review"
              }
            ]
          },
          {
            "key": "tinh.hcth.dashboard",
            "label": "Dashboard nhân sự",
            "route": "/kn/nhan-su/dashboard",
            "icon": "chart"
          },
          {
            "key": "tinh.hcth.nang_bac",
            "label": "Danh sách nâng bậc",
            "route": "/kn/nhan-su/nang-bac",
            "icon": "chart"
          },
          {
            "key": "tinh.hcth.quyet_dinh_nang_bac",
            "label": "Quyết định nâng bậc",
            "route": "/kn/nhan-su/quyet-dinh-nang-bac",
            "icon": "journal"
          },
          {
            "key": "tinh.hcth.ke_hoach",
            "label": "Kế hoạch nhân lực năm",
            "route": "/kn/nhan-su/ke-hoach",
            "icon": "calendar"
          },
          {
            "key": "tinh.hcth.danh_gia",
            "label": "Đánh giá",
            "route": "/kn/nhan-su/danh-gia",
            "icon": "approve",
            "soon": true
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "tinh.bao_cao.tong_hop",
            "label": "Báo cáo tổng hợp các xã",
            "route": "/kn/bao-cao/tong-hop",
            "icon": "layers"
          },
          {
            "key": "tinh.bao_cao.dot_xuat",
            "label": "Báo cáo đột xuất",
            "route": "/kn/bao-cao/dot-xuat",
            "icon": "zap",
            "badge": "extension.report_status.ad_hoc_pending",
            "adHoc": true
          },
          {
            "key": "tinh.hcth.bao_cao_so",
            "label": "Biểu mẫu báo cáo Sở",
            "route": "/kn/nhan-su/bao-cao-so",
            "icon": "report"
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "tinh.hcth.ho_so",
            "label": "Hồ sơ nhân sự",
            "route": "/kn/nhan-su/ho-so",
            "icon": "users"
          },
          {
            "key": "tinh.hcth.kho_tai_lieu",
            "label": "Hồ sơ dữ liệu",
            "route": "/kn/nhan-su/kho-tai-lieu",
            "icon": "library"
          },
          {
            "key": "tinh.hcth.danh_muc",
            "label": "Danh mục nhân sự",
            "route": "/kn/nhan-su/danh-muc",
            "icon": "catalog"
          },
          {
            "key": "tinh.van_ban",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "library"
          }
        ]
      }
    ],
    "kn_xa": [
      {
        "key": "dieu_hanh",
        "label": "Điều hành",
        "items": [
          {
            "key": "xa.nhiem_vu",
            "label": "Nhiệm vụ",
            "icon": "task",
            "children": [
              {
                "key": "xa.nhiem_vu.cong_viec",
                "label": "Công việc cần làm",
                "route": "/kn/nhiem-vu/cong-viec",
                "icon": "bell",
                "badge": "extension.report_tasks.todo"
              }
            ]
          },
          {
            "key": "xa.hcth.danh_gia",
            "label": "Đánh giá của tôi",
            "route": "/kn/nhan-su/danh-gia-cua-toi",
            "icon": "approve",
            "soon": true
          }
        ]
      },
      {
        "key": "bao_cao",
        "label": "Báo cáo",
        "items": [
          {
            "key": "xa.bao_cao.tuan",
            "label": "Báo cáo tuần",
            "route": "/kn/bao-cao/tuan",
            "icon": "file"
          },
          {
            "key": "xa.bao_cao.thang",
            "label": "Báo cáo tháng",
            "route": "/kn/bao-cao/thang",
            "icon": "calendar"
          },
          {
            "key": "xa.bao_cao.hang_vu",
            "label": "Báo cáo hàng vụ",
            "route": "/kn/bao-cao/vu",
            "icon": "sprout"
          },
          {
            "key": "xa.bao_cao.cua_xa",
            "label": "Báo cáo của xã",
            "route": "/kn/bao-cao/cua-xa",
            "icon": "chart"
          },
          {
            "key": "xa.bao_cao.dot_xuat",
            "label": "Báo cáo đột xuất",
            "route": "/kn/bao-cao/dot-xuat",
            "icon": "zap",
            "badge": "extension.report_tasks.ad_hoc_todo",
            "adHoc": true
          }
        ]
      },
      {
        "key": "du_lieu",
        "label": "Dữ liệu",
        "items": [
          {
            "key": "xa.van_ban",
            "label": "Quy trình, văn bản",
            "route": "/kn/van-ban",
            "icon": "library"
          }
        ]
      }
    ]
  }
};

const ADMIN_BUSINESS: Partial<Record<KnMenuProfile, Item[]>> = {
  "chan_nuoi_thuy_san": [
    {
      "key": "admin.pb.cnts_danh_muc",
      "label": "Danh mục CNTS",
      "route": "/kn/phong-ban/{department}/danh-muc-cnts",
      "icon": "catalog"
    },
    {
      "key": "admin.pb.cnts_bieu_ky",
      "label": "Biểu kỳ Cấp Xã",
      "route": "/kn/phong-ban/{department}/bieu-ky-cnts",
      "icon": "table"
    },
    {
      "key": "admin.pb.cnts_lich_truc",
      "label": "Lịch trực KSGM",
      "route": "/kn/phong-ban/{department}/lich-truc",
      "icon": "calendar"
    },
    {
      "key": "admin.pb.cnts_lich_tiem",
      "label": "Lịch tiêm phòng",
      "route": "/kn/phong-ban/{department}/lich-tiem",
      "icon": "check"
    }
  ],
  "ke_hoach_tai_chinh": [
    {
      "key": "admin.pb.khtc_ke_hoach",
      "label": "Kế hoạch năm",
      "route": "/kn/phong-ban/{department}/ke-hoach-nam",
      "icon": "calendar"
    },
    {
      "key": "admin.pb.khtc_danh_muc",
      "label": "Danh mục KHTC",
      "route": "/kn/phong-ban/{department}/danh-muc-khtc",
      "icon": "catalog"
    },
    {
      "key": "admin.pb.khtc_bao_cao",
      "label": "Báo cáo KHTC",
      "route": "/kn/phong-ban/{department}/bao-cao-khtc",
      "icon": "report"
    }
  ],
  "thong_tin_truyen_thong": [
    {
      "key": "admin.pb.bao_gia_xa",
      "label": "Xã báo giá",
      "route": "/kn/bao-gia/xa-bao-gia",
      "icon": "map"
    },
    {
      "key": "admin.pb.bao_gia_lich",
      "label": "Kỳ báo giá & giờ khóa",
      "route": "/kn/phong-ban/{department}/bao-gia-lich",
      "icon": "calendar"
    },
    {
      "key": "admin.pb.bao_gia_mat_hang",
      "label": "Mặt hàng",
      "route": "/kn/bao-gia/mat-hang",
      "icon": "package",
      "soon": true
    }
  ],
  "hanh_chinh_tong_hop": [
    {
      "key": "admin.pb.hcth_danh_muc",
      "label": "Danh mục nhân sự",
      "route": "/kn/phong-ban/{department}/danh-muc-nhan-su",
      "icon": "catalog"
    },
    {
      "key": "admin.pb.hcth_phan_quyen",
      "label": "Phân quyền hồ sơ",
      "route": "/kn/phong-ban/{department}/phan-quyen-ho-so",
      "icon": "shield"
    },
    {
      "key": "admin.pb.hcth_thu_muc",
      "label": "Thư mục tài liệu",
      "route": "/kn/phong-ban/{department}/thu-muc-tai-lieu",
      "icon": "library"
    }
  ]
};

const ADMIN_DEPT_BASE: Record<string, Item[]> = {
  "generic": [
    {
      "key": "admin.pb.theo_doi_nop",
      "label": "Theo dõi nộp báo cáo",
      "route": "/kn/phong-ban/{department}/theo-doi-nop",
      "icon": "bell"
    },
    {
      "key": "admin.pb.tong_hop",
      "label": "Báo cáo tổng hợp các xã",
      "route": "/kn/phong-ban/{department}/tong-hop",
      "icon": "layers"
    },
    {
      "key": "admin.pb.du_lieu_bao_cao",
      "label": "Dữ liệu báo cáo các xã",
      "route": "/kn/phong-ban/{department}/du-lieu-bao-cao",
      "icon": "file"
    },
    {
      "key": "admin.pb.danh_muc",
      "label": "Danh mục nông nghiệp",
      "route": "/kn/phong-ban/{department}/danh-muc",
      "icon": "package"
    },
    {
      "key": "admin.pb.mau_bao_cao",
      "label": "Mẫu báo cáo",
      "route": "/kn/phong-ban/{department}/mau-bao-cao",
      "icon": "table"
    },
    {
      "key": "admin.pb.trang_chu",
      "label": "Trang chủ phòng",
      "route": "/kn/phong-ban/{department}/trang-chu",
      "icon": "dashboard"
    },
    {
      "key": "admin.pb.van_ban",
      "label": "Quy trình, văn bản",
      "route": "/kn/phong-ban/{department}/van-ban",
      "icon": "library"
    }
  ],
  "trong_trot": [
    {
      "key": "admin.pb.theo_doi_nop",
      "label": "Theo dõi nộp báo cáo",
      "route": "/kn/phong-ban/{department}/theo-doi-nop",
      "icon": "bell"
    },
    {
      "key": "admin.pb.tong_hop",
      "label": "Báo cáo tổng hợp các xã",
      "route": "/kn/phong-ban/{department}/tong-hop",
      "icon": "layers"
    },
    {
      "key": "admin.pb.du_lieu_bao_cao",
      "label": "Dữ liệu báo cáo các xã",
      "route": "/kn/phong-ban/{department}/du-lieu-bao-cao",
      "icon": "file"
    },
    {
      "key": "admin.pb.thu_thap",
      "label": "Thu thập dữ liệu",
      "route": "/kn/phong-ban/{department}/thu-thap",
      "icon": "database"
    },
    {
      "key": "admin.pb.lich_cong_tac",
      "label": "Lịch công tác",
      "route": "/kn/nhiem-vu/lich-cong-tac",
      "icon": "calendar"
    },
    {
      "key": "admin.pb.danh_muc",
      "label": "Danh mục nông nghiệp",
      "route": "/kn/phong-ban/{department}/danh-muc",
      "icon": "package"
    },
    {
      "key": "admin.pb.mau_bao_cao",
      "label": "Mẫu báo cáo",
      "route": "/kn/phong-ban/{department}/mau-bao-cao",
      "icon": "table"
    },
    {
      "key": "admin.pb.trang_chu",
      "label": "Trang chủ phòng",
      "route": "/kn/phong-ban/{department}/trang-chu",
      "icon": "dashboard"
    },
    {
      "key": "admin.pb.van_ban",
      "label": "Quy trình, văn bản",
      "route": "/kn/phong-ban/{department}/van-ban",
      "icon": "library"
    }
  ]
};

const KHTC_REPORT: Item = {
  "key": "tinh.bao_cao_khtc",
  "label": "Báo cáo cho KHTC",
  "icon": "send",
  "children": [
    {
      "key": "tinh.bao_cao_khtc.phieu_ky_nay",
      "label": "Phiếu kỳ này",
      "route": "/kn/bao-cao-khtc",
      "icon": "task"
    },
    {
      "key": "tinh.bao_cao_khtc.da_gui",
      "label": "Phiếu đã gửi",
      "route": "/kn/bao-cao-khtc/da-gui",
      "icon": "check"
    },
    {
      "key": "tinh.bao_cao_khtc.tien_do",
      "label": "Tiến độ các CT của phòng",
      "route": "/kn/bao-cao-khtc/tien-do",
      "icon": "chart",
      "soon": true
    },
    {
      "key": "tinh.bao_cao_khtc.da_xuat",
      "label": "Báo cáo KHTC đã xuất",
      "route": "/kn/bao-cao-khtc/da-xuat",
      "icon": "report",
      "soon": true
    }
  ]
};

const KHTC_REPORT_ADMIN: Item = {
  "key": "admin.pb.bao_cao_khtc",
  "label": "Báo cáo cho KHTC",
  "route": "/kn/phong-ban/{department}/bao-cao-khtc",
  "icon": "send"
};

const ADMIN_OVERVIEW: Group = {
  "key": "dieu_hanh",
  "label": "Điều hành",
  "items": [
    {
      "key": "admin.tong_quan",
      "label": "Tổng quan hệ thống",
      "route": "/kn/tong-quan",
      "icon": "dashboard"
    }
  ]
};

const ADMIN_SYSTEM: Group = {
  "key": "quan_tri",
  "label": "Quản trị hệ thống",
  "items": [
    {
      "key": "admin.qt.dvhc",
      "label": "Đơn vị hành chính",
      "route": "/kn/quan-tri/don-vi-hanh-chinh",
      "icon": "map"
    },
    {
      "key": "admin.qt.co_cau",
      "label": "Cơ cấu tổ chức 3 cấp",
      "route": "/kn/quan-tri/co-cau-to-chuc",
      "icon": "org"
    },
    {
      "key": "admin.qt.phong_ban",
      "label": "Phòng ban",
      "route": "/kn/quan-tri/phong-ban",
      "icon": "building"
    },
    {
      "key": "admin.qt.nguoi_dung",
      "label": "Người dùng",
      "route": "/kn/quan-tri/nguoi-dung",
      "icon": "users"
    },
    {
      "key": "admin.qt.menu_cac_cap",
      "label": "Menu các cấp",
      "route": "/kn/quan-tri/menu-cac-cap",
      "icon": "key"
    },
    {
      "key": "admin.qt.cong_cu",
      "label": "Công cụ nền tảng (tạm)",
      "icon": "param",
      "children": [
        {
          "key": "admin.qt.cong_cu.tham_so",
          "label": "Tham số & phê duyệt",
          "route": "/quan-tri/tham-so",
          "icon": "param"
        },
        {
          "key": "admin.qt.cong_cu.tai_khoan",
          "label": "Tài khoản cán bộ (G1)",
          "route": "/quan-tri/tai-khoan",
          "icon": "user"
        },
        {
          "key": "admin.qt.cong_cu.to_chuc",
          "label": "Cây tổ chức (G1)",
          "route": "/quan-tri/to-chuc",
          "icon": "org"
        },
        {
          "key": "admin.qt.cong_cu.mau_xuat",
          "label": "Mẫu xuất & lịch gửi",
          "route": "/quan-tri/mau-bao-cao",
          "icon": "report"
        },
        {
          "key": "admin.qt.cong_cu.xung_dot",
          "label": "Xung đột đồng bộ",
          "route": "/quan-tri/xung-dot",
          "icon": "conflict"
        },
        {
          "key": "admin.qt.cong_cu.giam_sat",
          "label": "Giám sát đồng bộ",
          "route": "/quan-tri/giam-sat-dong-bo",
          "icon": "sync"
        },
        {
          "key": "admin.qt.cong_cu.thiet_bi",
          "label": "Đồng bộ thiết bị",
          "route": "/dong-bo",
          "icon": "sync"
        },
        {
          "key": "admin.qt.cong_cu.khach_hang_api",
          "label": "Khách hàng Public API",
          "route": "/quan-tri/khach-hang-api",
          "icon": "key"
        },
        {
          "key": "admin.qt.cong_cu.su_dung_api",
          "label": "Sử dụng Public API",
          "route": "/quan-tri/su-dung-api",
          "icon": "chart"
        },
        {
          "key": "admin.qt.cong_cu.bang_ke",
          "label": "Bảng kê sử dụng",
          "route": "/quan-tri/bang-ke",
          "icon": "report"
        },
        {
          "key": "admin.qt.cong_cu.nhat_ky_xuat",
          "label": "Nhật ký xuất dữ liệu",
          "route": "/quan-tri/nhat-ky-xuat",
          "icon": "shield"
        }
      ]
    },
    {
      "key": "admin.qt.mau_van_ban",
      "label": "Mẫu văn bản & lịch sử xuất",
      "route": "/kn/quan-tri/mau-van-ban",
      "icon": "report"
    }
  ]
};

const KHTC_REPORT_PROFILES = new Set<KnMenuProfile>(["chan_nuoi_thuy_san","thong_tin_truyen_thong","trong_trot","hanh_chinh_tong_hop"]);

// END GENERATED WEB MENU

function profileGroups(profile: KnMenuProfile, audience: Exclude<KnRole, "admin">): Group[] {
  const biz = BUSINESS[profile]?.[audience] ?? [];
  const groups =
    profile === "trong_trot"
      ? TRONG_TROT[audience]
      : (DEPT_MENUS[profile]?.[audience] ??
        GENERIC[audience].flatMap((g) => (g.key === "dieu_hanh" && biz.length ? [g, { key: "nghiep_vu", label: "Nghiệp vụ", items: biz }] : [g])));
  if (audience !== "kn_tinh" || !KHTC_REPORT_PROFILES.has(profile)) return groups;
  return groups.map((g) => (g.key === "bao_cao" ? { ...g, items: [...g.items, KHTC_REPORT] } : g));
}

function bindMenuItems(items: Item[], base: string): Item[] {
  return items.map((item) => ({ ...item,
    ...(item.route ? { route: item.route.replace("/kn/phong-ban/{department}", base) } : {}),
    ...(item.children ? { children: bindMenuItems(item.children, base) } : {}),
  }));
}

function adminDeptItems(code: string, profile: KnMenuProfile): Item[] {
  const base = `/kn/phong-ban/${code}`;
  return [
    ...bindMenuItems(ADMIN_DEPT_BASE[profile] ?? ADMIN_DEPT_BASE.generic!, base),
    ...(KHTC_REPORT_PROFILES.has(profile) ? bindMenuItems([KHTC_REPORT_ADMIN], base) : []),
    ...bindMenuItems(ADMIN_BUSINESS[profile] ?? [], base),
  ];
}

const STATUS_LABEL: Record<DepartmentRef["status"], string> = { active: "Đang áp dụng", not_open: "Chưa mở", suspended: "Ngưng hoạt động" };

function wire(items: readonly Item[], enabled: boolean, adHoc: boolean): KnMenuItem[] {
  const out: KnMenuItem[] = [];
  for (const it of items) {
    if (it.adHoc && !adHoc) continue;
    const children = wire(it.children ?? [], enabled, adHoc);
    if (it.children?.length && !children.length) continue;
    const base: KnMenuItem = { key: it.key, label: it.label, route: it.route ?? null, icon: it.icon, badge_source: it.badge ?? null, enabled, children };
    out.push(it.soon ? { ...base, enabled: false, status: "coming_soon", status_label: "Sắp có" } : base);
  }
  return out;
}

const profileOf = (code: string): KnMenuProfile => KN_MENU_PROFILE[code] ?? "generic";
const deptWire = (d: DepartmentRef): NonNullable<KnMenu["department"]> => ({ code: d.code, name: d.name, status: d.status, menu_profile: profileOf(d.code) });

function adminDeptItem(d: DepartmentRef): KnMenuItem {
  const enabled = d.status === "active";
  return {
    key: `phong_ban.${d.code}`,
    label: d.name,
    route: null,
    icon: profileOf(d.code) === "trong_trot" ? "sprout" : "building",
    badge_source: null,
    enabled,
    status: d.status,
    status_label: STATUS_LABEL[d.status],
    children: wire(adminDeptItems(d.code, profileOf(d.code)), enabled, false),
  };
}

/** Select only enabled leaf routes, using generated owner preferences. */
export function navigationRoute(audience: KnRole, profile: KnMenuProfile | null, groups: readonly KnMenuGroup[]): string {
  const routes: string[] = [];
  const visit = (items: readonly KnMenuItem[]) => {
    for (const item of items) {
      if (!item.enabled) continue;
      if (item.children.length) { visit(item.children); continue; }
      if (item.route?.startsWith("/") && !item.route.startsWith("//")) routes.push(item.route);
    }
  };
  groups.forEach((group) => visit(group.items));
  const preferred = profile ? CONTEXT_DEFAULT_ROUTES[profile]?.[audience] : undefined;
  for (const route of [preferred, DEFAULT_ROUTE[audience]]) if (route && routes.includes(route)) return route;
  if (!routes[0]) throw new Error("navigation: no enabled menu page");
  return routes[0];
}

/**
 * KNMenu trên dây (= Go app.DepartmentMenu / app.AdminMenu). Admin: Điều hành / Phòng ban (mọi phòng ban, chưa mở
 * enabled = false) / Quản trị hệ thống / Dùng chung (kn.menu/v3). Phòng ban chưa mở → 409 ở handler.
 */
export function knMenu(audience: KnRole, departmentCode: string | null, opts: { adHoc?: boolean; orgRef?: string | null } = {}): KnMenu {
  if (audience === "admin") {
    const groups: KnMenuGroup[] = [
      { key: ADMIN_OVERVIEW.key, label: ADMIN_OVERVIEW.label, items: wire(ADMIN_OVERVIEW.items, true, false) },
      { key: "phong_ban", label: "Phòng ban", items: KN_DEPARTMENTS.map(adminDeptItem) },
      { key: ADMIN_SYSTEM.key, label: ADMIN_SYSTEM.label, items: wire(ADMIN_SYSTEM.items, true, false) },
      { key: SHARED.key, label: SHARED.label, items: wire(SHARED.items, true, false) },
    ];
    return { menu_version: MENU_VERSION, audience, org_ref: opts.orgRef ?? null, department: null, default_route: navigationRoute(audience, null, groups), groups };
  }
  const d = KN_DEPARTMENTS.find((x) => x.code === (departmentCode ?? "trong_trot")) ?? KN_DEPARTMENTS[0]!;
  const adHoc = opts.adHoc ?? ADHOC_FORMS[d.code] ?? false;
  const shared: Group = { ...SHARED, items: [...SHARED.items, ...(SHARED_BY_AUDIENCE[audience] ?? [])] };
  const groups = [...profileGroups(profileOf(d.code), audience), shared]
    .map((g) => ({ key: g.key, label: g.label, items: wire(g.items, true, adHoc) }))
    .filter((g) => g.items.length > 0);
  return { menu_version: MENU_VERSION, audience, org_ref: opts.orgRef ?? null, department: deptWire(d), default_route: navigationRoute(audience, profileOf(d.code), groups), groups };
}

/** Cây menu của khung (cho kiểm thử khung/Sidebar). */
export function knMenuFor(role: KnRole, departmentCode: string | null): MenuTree {
  return fromKnMenu(knMenu(role, departmentCode));
}

/** Số đếm giả lập theo vai trò (Hình 3e: 15 / 3 / 2; Hình 3f: 6). 0 → menu ẩn số đếm. */
const BADGES: Record<KnRole, Record<string, number>> = {
  kn_tw: {},
  // Lịch chờ duyệt (B.schedules) tính từ sổ lịch giả lập P5 ở handler menu-badges.
  // Số đếm vòng đời báo cáo tính từ sổ giả lập P4 (kn-reports/store.ts cycleBadges).
  kn_tinh: {},
  kn_xa: {},
  admin: {},
};

// --------------------------------------------------------------------------
// Ngữ cảnh phiên (giữ trong bộ nhớ tab, như mọi trạng thái giả lập khác)
// --------------------------------------------------------------------------
/** Phiên hỗ trợ của Admin (FN-AD-04.07): thay cho ai, vai trò, lý do mở phiên. */
export interface KnSupportInfo {
  sessionId: string;
  /** Tài khoản được hỗ trợ (id tài khoản giả lập) — null: hỗ trợ theo vai trò/xã, không gắn tài khoản. */
  onBehalfOf: string | null;
  onBehalfOfName: string | null;
  reason: string;
  startedAt: string;
  endedAt: string | null;
}
export interface StoredContext {
  accountId: string;
  assignment: RoleAssignment;
  ctx: SessionContext;
  support?: KnSupportInfo;
}
const CONTEXTS = new Map<string, StoredContext>();

/** Ngữ cảnh đã lưu (backend giả lập P4/FN-AD-04.07 đọc vai trò, phòng ban, xã, phiên hỗ trợ). */
export function knStoredContext(contextId: string | null | undefined): StoredContext | undefined {
  return contextId ? CONTEXTS.get(contextId) : undefined;
}

/** Lưu ngữ cảnh hỗ trợ do POST /v1/accounts/support-sessions tạo (kn-admin.ts). */
export function registerKnContext(stored: StoredContext): void {
  CONTEXTS.set(stored.ctx.context_id, stored);
}

/** Thoát hỗ trợ: ngữ cảnh của phiên hết hiệu lực ngay. */
export function dropSupportContexts(sessionId: string): void {
  for (const [id, st] of CONTEXTS) if (st.support?.sessionId === sessionId) CONTEXTS.delete(id);
}

function accountOf(p: MockPersona): MockKnAccount | undefined {
  return findKnAccount(p.id);
}

/**
 * Persona theo header X-AG-Context (mô phỏng middleware P1): chỉ nhận context của chính tài khoản
 * trong token. Context lạ/của người khác → giữ persona gốc; handler trả 403 trước đó (contextProblem).
 */
export function personaForContext(base: MockPersona, contextId: string | null): MockPersona {
  if (!contextId) return base;
  const stored = CONTEXTS.get(contextId);
  const account = accountOf(base);
  if (!stored || !account || stored.accountId !== account.id) return base;
  return knPersona(account, stored.assignment);
}

/**
 * Như middleware P1 thật: header X-AG-Context lạ, không phải UUID hoặc của người khác → 403
 * accounts.context_invalid (không bao giờ âm thầm bỏ qua header). null = hợp lệ hoặc không có header.
 */
export function contextProblem(base: MockPersona, contextId: string | null): Response | null {
  if (!contextId) return null;
  const stored = CONTEXTS.get(contextId);
  if (!stored || stored.accountId !== accountOf(base)?.id) {
    return problem(403, "accounts.context_invalid", "Ngữ cảnh phiên không hợp lệ hoặc không thuộc người gọi");
  }
  return null;
}

/** Context đã lưu (cho kiểm thử và khôi phục sau tải lại trong cùng tab). */
export function knContext(contextId: string): SessionContext | undefined {
  const stored = CONTEXTS.get(contextId);
  return stored ? { ...stored.ctx, default_route: knMenu(stored.ctx.role, stored.ctx.department_code, { orgRef: stored.ctx.org_ref }).default_route } : undefined;
}

const NO_STORE = { "Cache-Control": "no-store" };

/**
 * Đối tượng + phòng ban của lời gọi menu như app.resolveAudience (P2): ngữ cảnh phiên (X-AG-Context) thắng;
 * ?audience= (tên cũ ?role=) khác ngữ cảnh → 403 party.menu_audience_forbidden (trừ Admin hỗ trợ mọi cấp).
 * Không có ngữ cảnh: bắt buộc ?audience=.
 */
function menuQuery(
  request: Request,
  url: URL,
  pathDepartment?: string,
): Response | { audience: KnRole; department: string | null; orgRef: string | null } {
  const asked = (url.searchParams.get("audience") ?? url.searchParams.get("role")) as KnRole | null;
  if (asked && !(asked in DEFAULT_ROUTE)) return problem(422, "validation_failed", "audience: kn_tw | kn_tinh | kn_xa | admin");
  let department = pathDepartment ?? url.searchParams.get("department");
  const stored = CONTEXTS.get(request.headers.get("X-AG-Context") ?? "");
  if (stored) {
    const role = stored.ctx.role;
    if (role !== "admin" && ((asked && asked !== role) || (department && department !== stored.ctx.department_code))) {
      return problem(403, "party.menu_audience_forbidden", "chỉ trả menu của ngữ cảnh phiên đang dùng");
    }
    const audience = asked ?? role;
    if (!department && audience === role) department = stored.ctx.department_code;
    return { audience, department, orgRef: stored.ctx.org_ref };
  }
  if (!asked) return problem(422, "validation_failed", "audience: bắt buộc khi không có ngữ cảnh phiên");
  return { audience: asked, department, orgRef: null };
}

export const knAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/me\/contexts$/,
    // Vai thử cũ (không phải tài khoản KN) → danh sách rỗng: giữ bề mặt menu hiện có.
    handler: ({ persona }) => json({ assignments: accountOf(persona)?.assignments ?? [] }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/me\/context$/,
    handler: ({ request }) => {
      // The mock middleware has already rejected foreign or invalid contexts.
      const id = request.headers.get("X-AG-Context");
      if (!id) return problem(428, "accounts.context_required", "Cần chọn vai trò và phòng ban (header X-AG-Context)");
      const current = knContext(id);
      return current ? json(current, 200, NO_STORE) : problem(403, "accounts.context_invalid", "Ngữ cảnh phiên không hợp lệ");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/me\/context$/,
    handler: ({ persona, body }) => {
      const account = accountOf(persona);
      const req = (body ?? {}) as { assignment_id?: unknown; department_code?: unknown };
      const asg = account?.assignments.find((x) => x.assignment_id === req.assignment_id);
      if (!account || !asg) return problem(404, "accounts.assignment_not_found", "Assignment not found");
      let dept: DepartmentRef | null = null;
      if (asg.role !== "admin") {
        dept = asg.departments.find((d) => d.code === req.department_code) ?? null;
        if (!dept) return problem(422, "accounts.department_required", "Department required");
        if (dept.status !== "active") return problem(409, "accounts.department_not_open", "Department not open");
      }
      const ctx: SessionContext = {
        context_id: uuidv7(),
        assignment_id: asg.assignment_id,
        role: asg.role,
        department_code: dept?.code ?? null,
        department_name: dept?.name ?? null,
        org_ref: asg.org_ref,
        org_name: asg.org_name,
        communes: asg.communes,
        default_route: knMenu(asg.role, dept?.code ?? null, { orgRef: asg.org_ref }).default_route,
        // Server: 24 giờ hoặc tới khi lần gán hết hiệu lực (P1 domain.ContextTTL).
        expires_at: new Date(Date.now() + 24 * 3_600_000).toISOString(),
        support_session_id: null,
        on_behalf_of: null,
      };
      CONTEXTS.set(ctx.context_id, { accountId: account.id, assignment: asg, ctx });
      return json(ctx, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/menu$/,
    handler: ({ request, url }) => {
      const q = menuQuery(request, url);
      if (q instanceof Response) return q;
      if (q.audience === "admin") return json(knMenu("admin", null, { orgRef: q.orgRef }), 200, NO_STORE);
      const d = q.department ? findKnDepartment(q.department) : KN_DEPARTMENTS.find((x) => x.status === "active");
      if (!d) return problem(404, "party.department_not_found", "Department not found");
      if (d.status !== "active") return problem(409, "party.department_not_open", "Department not open");
      return json(knMenu(q.audience, d.code, { orgRef: q.orgRef }), 200, NO_STORE);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/departments\/([\w-]+)\/modules$/,
    handler: ({ params, request, url }) => {
      const q = menuQuery(request, url, params[0] ?? "");
      if (q instanceof Response) return q;
      const d = findKnDepartment(params[0] ?? "");
      if (!d) return problem(404, "party.department_not_found", "Department not found");
      const code = d.code;
      if (q.audience === "admin") {
        // Admin: nhóm của MỘT phòng ban (kể cả chưa mở — mục con enabled = false), như app.DepartmentModules.
        const item = knMenu("admin", null).groups.find((g) => g.key === "phong_ban")!.items.find((i) => i.key === `phong_ban.${code}`)!;
        return json(
          { menu_version: MENU_VERSION, audience: "admin", org_ref: q.orgRef, department: { code: d.code, name: d.name, status: d.status, menu_profile: profileOf(d.code) },
            default_route: DEFAULT_ROUTE.admin, groups: [{ key: item.key, label: d.name, items: item.children }] } satisfies KnMenu,
          200,
          NO_STORE,
        );
      }
      if (d.status !== "active") return problem(409, "party.department_not_open", "Department not open");
      return json(knMenu(q.audience, d.code, { orgRef: q.orgRef }), 200, NO_STORE);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/me\/menu-badges$/,
    handler: ({ request, persona }) => {
      const contextId = request.headers.get("X-AG-Context") ?? "";
      const stored = CONTEXTS.get(contextId);
      if (!stored) return json({ counts: {} });
      const counts: Record<string, number> = { ...BADGES[stored.assignment.role], ...cycleBadges(contextId) };
      // Lịch chờ duyệt: tính từ sổ lịch giả lập (P5), không cố định.
      if (stored.assignment.role === "kn_tinh") counts[WORK_SCHEDULE_BADGE] = workSchedulePendingBadge(persona, contextId);
      return json({ counts });
    },
  },
];

// --------------------------------------------------------------------------
// Đăng nhập mật khẩu giả lập (FN-CH-00.01): sai → còn n lần; sai 5 lần → khóa tạm 15 phút.
// Bản thật: Keycloak brute-force detection (bản vá realm P1: waitIncrementSeconds 900).
// --------------------------------------------------------------------------
export const MAX_ATTEMPTS = 5;
export const LOCK_MINUTES = 15;
const ATTEMPTS = new Map<string, { failed: number; lockedUntil: number | null }>();

export type MockLoginResult =
  | { ok: true; personaId: string }
  | { ok: false; error: "invalid_credentials"; remaining: number }
  | { ok: false; error: "locked"; lockedUntil: string };

export function mockKnLogin(identifier: string, password: string, now = Date.now()): MockLoginResult {
  const key = identifier.trim().toLowerCase();
  const st = ATTEMPTS.get(key) ?? { failed: 0, lockedUntil: null };
  if (st.lockedUntil && st.lockedUntil > now) return { ok: false, error: "locked", lockedUntil: new Date(st.lockedUntil).toISOString() };
  if (st.lockedUntil) {
    st.failed = 0;
    st.lockedUntil = null;
  }
  const account = findKnAccountByIdentifier(identifier);
  // Cùng một thông báo cho tài khoản không tồn tại và sai mật khẩu (không lộ tài khoản nào có thật).
  if (!account || password !== MOCK_KN_PASSWORD) {
    st.failed += 1;
    if (st.failed >= MAX_ATTEMPTS) {
      st.lockedUntil = now + LOCK_MINUTES * 60_000;
      ATTEMPTS.set(key, st);
      return { ok: false, error: "locked", lockedUntil: new Date(st.lockedUntil).toISOString() };
    }
    ATTEMPTS.set(key, st);
    return { ok: false, error: "invalid_credentials", remaining: MAX_ATTEMPTS - st.failed };
  }
  ATTEMPTS.delete(key);
  return { ok: true, personaId: account.id };
}

/** Cho kiểm thử: xóa bộ đếm sai mật khẩu. */
export function resetMockLoginAttempts(): void {
  ATTEMPTS.clear();
}
