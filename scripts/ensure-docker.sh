#!/usr/bin/env bash
# ensure-docker.sh — guarantee a working Docker daemon for B34/B345 gates.
# macOS: auto-installs/start Colima via brew if Docker is not running.
# Linux: expects Docker already installed; exits 1 with instructions if missing.
#
# Exit codes:
#   0 — Docker is running and reachable
#   1 — Cannot install or start Docker on this host
#
# Never uses curl|bash. Uses brew (already on the host) or colima directly.
set -euo pipefail

TIMEOUT=120
START=$(date +%s)

# ── 1. Already running? ──────────────────────────────────────────────────────
if docker info >/dev/null 2>&1; then
    echo "[ensure-docker] Docker daemon already running"
    exit 0
fi

echo "[ensure-docker] Docker is not running — attempting to start..."

# ── 2. Try colima (already installed) ────────────────────────────────────────
if command -v colima >/dev/null 2>&1; then
    echo "[ensure-docker] colima found — starting..."
    colima start --cpu 4 --memory 8 2>&1 || {
        echo "[ensure-docker] colima start failed — retrying with defaults"
        colima start 2>&1 || {
            echo "[ensure-docker] FATAL: colima start failed"
            exit 1
        }
    }
fi

# ── 3. No colima? Try brew install ───────────────────────────────────────────
if ! command -v colima >/dev/null 2>&1; then
    if command -v brew >/dev/null 2>&1; then
        echo "[ensure-docker] Installing colima, docker, lima via brew..."
        brew install colima docker lima 2>&1
        echo "[ensure-docker] Starting colima..."
        colima start --cpu 4 --memory 8 2>&1 || {
            echo "[ensure-docker] colima start failed — retrying with defaults"
            colima start 2>&1 || {
                echo "[ensure-docker] FATAL: colima start failed after brew install"
                exit 1
            }
        }
    else
        echo "[ensure-docker] FATAL: No Docker daemon, no colima, no brew."
        echo "[ensure-docker] Cannot install Docker on this host automatically."
        echo "[ensure-docker] Please install Docker manually: https://docs.docker.com/engine/install/"
        exit 1
    fi
fi

# ── 4. Export DOCKER_HOST for colima ─────────────────────────────────────────
COLIMA_SOCK="$HOME/.colima/default/docker.sock"
if [ -S "$COLIMA_SOCK" ]; then
    export DOCKER_HOST="unix://$COLIMA_SOCK"
    echo "[ensure-docker] DOCKER_HOST=unix://$COLIMA_SOCK"
fi

# ── 5. Wait for Docker to become ready (up to TIMEOUT) ───────────────────────
echo "[ensure-docker] Waiting for Docker daemon (up to ${TIMEOUT}s)..."
while true; do
    if docker info >/dev/null 2>&1; then
        ELAPSED=$(($(date +%s) - START))
        echo "[ensure-docker] Docker daemon ready after ${ELAPSED}s"
        exit 0
    fi
    ELAPSED=$(($(date +%s) - START))
    if [ "$ELAPSED" -ge "$TIMEOUT" ]; then
        echo "[ensure-docker] FATAL: Docker daemon not ready after ${TIMEOUT}s"
        echo "[ensure-docker] Check: colima status, docker info"
        exit 1
    fi
    sleep 2
done
