"""Mô hình ảnh chụp đầu vào (contracts/optimizer/snapshot.v1.schema.json).

`extra="forbid"` ở MỌI cấp: trường lạ (vd `name`, `lat`, `phone`) bị từ chối —
optimizer chỉ nhận dữ liệu không định danh (RB-7), và hợp đồng là mặc định từ chối.
Số thập phân đi dạng chuỗi để giữ đúng giá trị decimal của nền tảng.
"""

from __future__ import annotations

import re
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator

DECIMAL = r"^-?[0-9]+(\.[0-9]+)?([eE][-+]?[0-9]+)?$"
UUID = r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
NODE = re.compile(r"^(coop|hub):[0-9a-fA-F-]{36}$|^plant$")

Dec = str


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class Plant(_Strict):
    plant_ref: str | None = Field(default=None, pattern=UUID)
    annual_demand_tons: Dec | None = Field(default=None, pattern=DECIMAL)


class Scenario(_Strict):
    scenario_id: str = Field(pattern=UUID)
    code: str = Field(max_length=40)
    lifecycle_years: int = Field(ge=1, le=50)
    discounted: bool
    baseline_mode: Literal["no_hub", "manual", "scenario"]
    baseline_manual_cost_per_ton: Dec | None = Field(default=None, pattern=DECIMAL)
    baseline_scenario_id: str | None = Field(default=None, pattern=UUID)
    baseline_scenario_cost_per_ton: Dec | None = Field(default=None, pattern=DECIMAL)
    plant: Plant


class Hub(_Strict):
    scenario_hub_id: str = Field(pattern=UUID)
    hub_ref: str = Field(pattern=UUID)
    code: str = Field(max_length=40)
    sort_order: int
    service_radius_km: Dec = Field(pattern=DECIMAL)
    design_capacity_tons: Dec = Field(pattern=DECIMAL)
    barge_payload_tons: Dec | None = Field(default=None, pattern=DECIMAL)
    mode_field_hub: Literal["auto", "road", "waterway"]
    mode_hub_plant: Literal["auto", "road", "waterway"]
    land_mode: Literal["mua", "thue"]


class CoopSeason(_Strict):
    season_key: str = Field(min_length=1, max_length=80)
    paddy_tons: Dec = Field(pattern=DECIMAL)
    planted_area_ha: Dec = Field(pattern=DECIMAL)


class Cooperative(_Strict):
    cooperative_ref: str = Field(pattern=UUID)
    member_count: int = Field(ge=0)
    seasons: list[CoopSeason]


class Season(_Strict):
    season_key: str = Field(min_length=1, max_length=80)
    season_type: str = Field(pattern=r"^[A-Z]{1,8}$")
    end_month: int = Field(ge=1, le=12)
    sort_order: int


class Observation(_Strict):
    day_of_year: int = Field(ge=1, le=365)
    tons: Dec = Field(pattern=DECIMAL)


class Radius(_Strict):
    cooperative_ref: str = Field(pattern=UUID)
    hub_ref: str = Field(pattern=UUID)
    radius_km: Dec = Field(pattern=DECIMAL)


class Route(_Strict):
    from_: str = Field(alias="from")
    to: str
    mode: Literal["road", "waterway"]
    distance_km: Dec = Field(pattern=DECIMAL)
    source: Literal["routing_thuc_te", "tuyen_so_hoa_noi_bo", "haversine_hieu_chinh"]
    straight_km: Dec | None = Field(default=None, pattern=DECIMAL)
    waterway_access: bool | None = None

    model_config = ConfigDict(extra="forbid", frozen=True, populate_by_name=True)

    @field_validator("from_", "to")
    @classmethod
    def _node(cls, v: str) -> str:
        if not NODE.match(v):
            raise ValueError("nút định tuyến phải là coop:<uuid>, hub:<uuid> hoặc plant")
        return v


class ParamValue(_Strict):
    code: str = Field(pattern=r"^[a-z][a-z0-9_]*$")
    number: int = Field(ge=1, le=99)
    name: str = Field(max_length=200)
    classification: Literal["thi_truong", "gia_dinh", "khac"]
    value: Dec | None = Field(default=None, pattern=DECIMAL)
    text_value: dict[str, Dec] | None = None
    min: Dec | None = Field(default=None, pattern=DECIMAL)
    max: Dec | None = Field(default=None, pattern=DECIMAL)
    approved: bool
    param_version: int | None = None


class Unapproved(_Strict):
    code: str
    number: int
    name: str


class Params(_Strict):
    set_version: str = Field(min_length=1, max_length=100)
    all_approved: bool
    unapproved: list[Unapproved]
    values: list[ParamValue]


class Options(_Strict):
    mode: Literal["evaluate", "select_hubs"] = "evaluate"
    sensitivity_codes: list[str] = Field(default_factory=list)
    max_hubs: int | None = Field(default=None, ge=1)


class Snapshot(_Strict):
    schema_version: Literal[1]
    snapshot_id: str = Field(pattern=UUID)
    simulation_id: str = Field(pattern=UUID)
    tenant_ref: str = Field(pattern=UUID)
    algorithm_version: str = Field(min_length=1, max_length=100)
    network_version: str = Field(min_length=1, max_length=100)
    created_at: str
    scenario: Scenario
    hubs: list[Hub]
    cooperatives: list[Cooperative]
    seasons: list[Season]
    harvest_observations: dict[str, list[Observation]]
    radius: list[Radius]
    routes: list[Route]
    params: Params
    options: Options


def validate(data: dict) -> Snapshot:
    return Snapshot.model_validate(data)
