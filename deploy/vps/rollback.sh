#!/usr/bin/env bash
# 回滚到指定版本镜像
# 用法:
#   ./rollback.sh                 # 列出可用版本
#   ./rollback.sh 20260712-153045 # 回滚到该 tag
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

COMPOSE="${COMPOSE:-docker compose}"
IMAGE_NAME="${IMAGE_NAME:-my-new-api}"
IMAGE_TAG_CUSTOM="${IMAGE_TAG_CUSTOM:-custom}"

list_tags() {
  echo "可用 ${IMAGE_NAME} 镜像:"
  docker images --format '{{.Tag}}\t{{.ID}}\t{{.CreatedSince}}\t{{.Size}}' "$IMAGE_NAME" \
    | sed 's/^/  /'
}

if [[ $# -lt 1 ]]; then
  list_tags
  echo
  echo "用法: $0 <tag>"
  echo "示例: $0 20260712-153045"
  exit 0
fi

TAG="$1"
if ! docker image inspect "${IMAGE_NAME}:${TAG}" >/dev/null 2>&1; then
  echo "镜像不存在: ${IMAGE_NAME}:${TAG}"
  list_tags
  exit 1
fi

echo "回滚: ${IMAGE_NAME}:${TAG} → ${IMAGE_NAME}:${IMAGE_TAG_CUSTOM}"
docker tag "${IMAGE_NAME}:${TAG}" "${IMAGE_NAME}:${IMAGE_TAG_CUSTOM}"
$COMPOSE up -d new-api
$COMPOSE ps new-api
echo "完成。日志: $COMPOSE logs --tail=100 new-api"
