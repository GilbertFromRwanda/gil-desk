# Reproducible C++ build (F-23) — used because CMake/a C++ toolchain isn't
# assumed to be installed on every developer machine; `docker build .` here
# is the canonical way to build and test the codec library.
FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
    cmake g++ make ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY . .
RUN cmake -B build -DCMAKE_BUILD_TYPE=Release && \
    cmake --build build --parallel && \
    ctest --test-dir build --output-on-failure
