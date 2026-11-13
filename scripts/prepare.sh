#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
docker build --platform "${DOCKER_PLATFORM:-linux/amd64}" -t "${APP_IMAGE:-final-prices:local}" "$project_root"
