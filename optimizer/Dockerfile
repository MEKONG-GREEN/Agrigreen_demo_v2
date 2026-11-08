# optimizer (Python 3.12) — ảnh slim, chạy không phải root. Chỉ nhận dữ liệu không định danh (RB-7).
# Ngữ cảnh chính là optimizer/; lõi dùng chung lib/optcore (ADR-0036, W8.1) đến qua ngữ cảnh
# đặt tên `optcore` (BuildKit). Từ gốc repo:
#   docker build --build-context optcore=lib/optcore -t agrigreen/optimizer optimizer
# Compose: additional_contexts trong deploy/compose/docker-compose.yml.
FROM python:3.12-slim-trixie AS base
# CVE-2026-109518 và DSA-6613-1: build/runtime dùng chung APT và zlib đã vá.
# Tắt retry khi bootstrap APT cũ; dừng build nếu kho chưa cấp đủ phiên bản tối thiểu.
RUN set -eux; \
    apt-get -o Acquire::Retries=0 update; \
    apt-get -o Acquire::Retries=0 install -y --no-install-recommends --only-upgrade apt libapt-pkg7.0 zlib1g; \
    for apt_package in apt libapt-pkg7.0; do \
        apt_version="$(dpkg-query -W -f='${Version}' "$apt_package")"; \
        dpkg --compare-versions "$apt_version" ge 3.0.4; \
        echo "$apt_package=$apt_version"; \
    done; \
    zlib_version="$(dpkg-query -W -f='${Version}' zlib1g)"; \
    dpkg --compare-versions "$zlib_version" ge '1:1.3.dfsg+really1.3.1-1+deb13u1'; \
    echo "zlib1g=$zlib_version"; \
    rm -rf /var/lib/apt/lists/*

FROM base AS build
WORKDIR /src
COPY --from=optcore pyproject.toml README.md /optcore/
COPY --from=optcore src /optcore/src
COPY pyproject.toml README.md ./
COPY src ./src
# Wheel lõi trước, rồi wheel optimizer phân giải agrigreen-optcore==<ghim> từ chính thư mục đó.
RUN pip install --no-cache-dir --upgrade pip \
 && pip wheel --no-cache-dir --no-deps --wheel-dir /wheels /optcore \
 && pip wheel --no-cache-dir --find-links /wheels --wheel-dir /wheels ".[milp]"

FROM base
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN groupadd --system --gid 10001 optimizer \
 && useradd --system --uid 10001 --gid optimizer --no-create-home --shell /usr/sbin/nologin optimizer
COPY --from=build /wheels /wheels
RUN pip install --no-cache-dir --no-index --find-links /wheels agrigreen-optimizer[milp] && rm -rf /wheels
USER 10001:10001
ENTRYPOINT ["agrigreen-optimizer"]
# Mặc định: vòng nhận RabbitMQ (pf.simulation.job.requested) — cấu hình qua biến môi trường,
# xem src/agrigreen_optimizer/consumer.py. `evaluate`/`handle-event` vẫn gọi được bằng cách đổi CMD.
CMD ["consume"]
