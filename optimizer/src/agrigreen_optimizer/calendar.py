"""Lịch thu hoạch theo ngày — chuyển từ `buildHarvestCalendar` (flow.ts).

Ưu tiên 1: đường cong thực tế theo loại vụ (tiền tố mã DX/HT/TD) do nguồn cung
cấp sẵn trong ảnh chụp (`harvest_observations`); mỗi quan sát được trải đều ra
khoảng thời gian nó đại diện. Ưu tiên 2: suy diễn hình thang trên
`harvest_window_days` ngày cuối tháng kết thúc vụ.
"""

from __future__ import annotations

from dataclasses import dataclass

DAYS_PER_YEAR = 365
MONTH_START = [0, 1, 32, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335]
MONTH_LENGTH = [0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]


@dataclass(frozen=True)
class SeasonCalendar:
    season_key: str
    season_type: str
    source: str  # thuc_te | suy_dien
    source_label: str
    days: tuple[tuple[int, float], ...]  # (day_of_year, weight) đã chuẩn hóa, tổng = 1

    @property
    def first_day(self) -> int:
        return self.days[0][0] if self.days else 1

    @property
    def last_day(self) -> int:
        return self.days[-1][0] if self.days else 1


def day_label(doy: int) -> str:
    month = month_of_day(doy)
    day = doy - MONTH_START[month] + 1
    return f"{day:02d}/{month:02d}"


def month_of_day(doy: int) -> int:
    for m in range(1, 13):
        if doy < MONTH_START[m] + MONTH_LENGTH[m]:
            return m
    return 12


def merge_weights(items: list[tuple[int, float]]) -> list[tuple[int, float]]:
    acc: dict[int, float] = {}
    for doy, w in items:
        acc[doy] = acc.get(doy, 0.0) + w
    return sorted(acc.items(), key=lambda kv: kv[0])


def spread_observations(points: list[tuple[int, float]]) -> list[tuple[int, float]]:
    if len(points) <= 1:
        return points
    gaps = [points[i][0] - points[i - 1][0] for i in range(1, len(points))]
    s = sorted(gaps)
    median = s[len(s) // 2] if s else 7
    median_gap = max(1, min(21, median))
    spread: list[tuple[int, float]] = []
    for i, (doy_i, w_i) in enumerate(points):
        previous = points[i - 1][0] if i > 0 else doy_i - median_gap
        span = max(1, min(21, doy_i - previous))
        share = w_i / span
        for offset in range(span):
            doy = doy_i - offset
            while doy < 1:
                doy += DAYS_PER_YEAR
            spread.append((doy, share))
    return merge_weights(spread)


def derived_window(end_month: int, harvest_window_days: int) -> list[tuple[int, float]]:
    end_month = min(12, max(1, end_month))
    end_day = MONTH_START[end_month] + MONTH_LENGTH[end_month] - 1
    width = max(1, harvest_window_days)
    days: list[tuple[int, float]] = []
    for offset in range(width - 1, -1, -1):
        doy = end_day - offset
        while doy < 1:
            doy += DAYS_PER_YEAR
        position = (width - 1 - offset) / max(width - 1, 1)
        if position < 0.25:
            weight = 0.4 + position * 2.4
        elif position > 0.75:
            weight = 0.4 + (1 - position) * 2.4
        else:
            weight = 1.0
        days.append((doy, weight))
    return merge_weights(days)


def normalize(days: list[tuple[int, float]]) -> list[tuple[int, float]]:
    total = 0.0
    for _, w in days:
        total += w
    if total <= 0:
        return [(d, 0.0) for d, _ in days]
    return [(d, w / total) for d, w in days]


def build(seasons: list[dict], observations: dict[str, list[dict]], harvest_window_days: int) -> dict[str, SeasonCalendar]:
    by_type: dict[str, list[tuple[int, float]]] = {}
    for season_type, rows in observations.items():
        pts: list[tuple[int, float]] = []
        for row in rows:
            doy = int(row["day_of_year"])
            tons = float(row["tons"])
            if not (tons > 0) or doy < 1 or doy > DAYS_PER_YEAR:
                continue
            pts.append((doy, tons))
        if pts:
            by_type[season_type.upper()] = spread_observations(merge_weights(pts))

    calendar: dict[str, SeasonCalendar] = {}
    for season in seasons:
        stype = str(season["season_type"]).upper()
        observed = by_type.get(stype)
        if observed:
            days = merge_weights(observed)
            source, label = "thuc_te", "Ngày thu hoạch thực tế từ file điều tra vụ mùa"
        else:
            days = derived_window(int(season["end_month"]), harvest_window_days)
            source, label = "suy_dien", f"Suy diễn: {harvest_window_days} ngày cuối tháng {season['end_month']}"
        calendar[season["season_key"]] = SeasonCalendar(
            season_key=season["season_key"],
            season_type=stype,
            source=source,
            source_label=label,
            days=tuple(normalize(days)),
        )
    return calendar
