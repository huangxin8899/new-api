#!/usr/bin/env bash
# 在 VPS /root/new-api 下执行：./update.sh
# 流程：拉二开代码 → 可选备份 DB → build → 打版本 tag → 重启 new-api → 健康检查
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

COMPOSE="${COMPOSE:-docker compose}"
FORK_DIR="${FORK_DIR:-$ROOT_DIR/new-api-fork}"
IMAGE_NAME="${IMAGE_NAME:-my-new-api}"
IMAGE_TAG_CUSTOM="${IMAGE_TAG_CUSTOM:-custom}"

if [[ -f "$ROOT_DIR/.env" ]]; then
  # shellcheck disable=SC1091
  set -a
  source "$ROOT_DIR/.env"
  set +a
fi

BRANCH="${DEPLOY_BRANCH:-custom}"
DO_BACKUP="${DO_BACKUP:-1}"
SKIP_PULL="${SKIP_PULL:-0}"

log() { echo "[$(date '+%F %T')] $*"; }
die() { echo "[$(date '+%F %T')] ERROR: $*" >&2; exit 1; }

command -v docker >/dev/null || die "未找到 docker"
$COMPOSE version >/dev/null 2>&1 || die "未找到 docker compose"

[[ -d "$FORK_DIR/.git" ]] || die "源码目录不存在或不是 git 仓库: $FORK_DIR
请先执行:
  git clone -b $BRANCH https://github.com/huangxin8899/new-api.git new-api-fork"

if [[ "$SKIP_PULL" != "1" ]]; then
  log "拉取 origin/$BRANCH ..."
  git -C "$FORK_DIR" fetch origin
  git -C "$FORK_DIR" checkout "$BRANCH"
  git -C "$FORK_DIR" pull --ff-only origin "$BRANCH"
  log "当前提交: $(git -C "$FORK_DIR" rev-parse --short HEAD) $(git -C "$FORK_DIR" log -1 --pretty=%s)"
else
  log "跳过 git pull (SKIP_PULL=1)"
fi

if [[ "$DO_BACKUP" == "1" ]]; then
  mkdir -p "$ROOT_DIR/backups"
  BACKUP_FILE="$ROOT_DIR/backups/new-api-$(date +%Y%m%d-%H%M%S).sql"
  if $COMPOSE ps --status running --services 2>/dev/null | grep -qx postgres; then
    log "备份 PostgreSQL → $BACKUP_FILE"
    # 兼容 .env 中的账号库名
    PGUSER="${POSTGRES_USER:-root}"
    PGDB="${POSTGRES_DB:-new-api}"
    $COMPOSE exec -T postgres pg_dump -U "$PGUSER" "$PGDB" >"$BACKUP_FILE" \
      || log "警告: 数据库备份失败，继续构建（请确认 postgres 已启动且账号正确）"
    # 只保留最近 10 份
    ls -1t "$ROOT_DIR/backups"/new-api-*.sql 2>/dev/null | tail -n +11 | xargs -r rm -f
  else
    log "postgres 未运行，跳过备份"
  fi
fi

VERSION_TAG="$(date +%Y%m%d-%H%M%S)"
log "构建镜像 ${IMAGE_NAME}:${IMAGE_TAG_CUSTOM} ..."
$COMPOSE build new-api

log "打版本标签 ${IMAGE_NAME}:${VERSION_TAG}"
docker tag "${IMAGE_NAME}:${IMAGE_TAG_CUSTOM}" "${IMAGE_NAME}:${VERSION_TAG}"

log "重启 new-api ..."
$COMPOSE up -d new-api

log "等待健康检查 ..."
ok=0
for i in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:13000/api/status" 2>/dev/null | grep -q '"success"[[:space:]]*:[[:space:]]*true'; then
    ok=1
    break
  fi
  sleep 2
done

$COMPOSE ps new-api
if [[ "$ok" -eq 1 ]]; then
  log "部署成功: ${IMAGE_NAME}:${IMAGE_TAG_CUSTOM} (= ${VERSION_TAG})"
  log "回滚示例: docker tag ${IMAGE_NAME}:${VERSION_TAG} ${IMAGE_NAME}:${IMAGE_TAG_CUSTOM} && $COMPOSE up -d new-api"
else
  log "警告: 健康检查未通过，请查看日志: $COMPOSE logs --tail=100 new-api"
  exit 1
fi
