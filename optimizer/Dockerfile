# optimizer (Python 3.12) — ảnh slim, chạy không phải root. Chỉ nhận dữ liệu không định danh (RB-7).
# Build từ gốc repo: docker build -f optimizer/Dockerfile -t agrigreen/optimizer optimizer
FROM python:3.12-slim AS build
WORKDIR /src
COPY pyproject.toml README.md ./
COPY src ./src
RUN pip install --no-cache-dir --upgrade pip \
 && pip wheel --no-cache-dir --wheel-dir /wheels ".[milp]"

FROM python:3.12-slim
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
