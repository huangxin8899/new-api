#!/usr/bin/env bash
# 本地合并官方上游到二开分支（Git Bash / Linux / macOS）
# 在仓库根目录执行:
#   bash deploy/scripts/sync-upstream.sh
#
# 环境变量:
#   CUSTOM_BRANCH=custom   # 二开分支，默认 custom
#   UPSTREAM_REF=main      # 上游分支，默认 main
#   PUSH=1                 # 合并后 push origin，默认 1
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

CUSTOM_BRANCH="${CUSTOM_BRANCH:-custom}"
UPSTREAM_REF="${UPSTREAM_REF:-main}"
PUSH="${PUSH:-1}"

log() { echo "[sync-upstream] $*"; }
die() { echo "[sync-upstream] ERROR: $*" >&2; exit 1; }

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "请在 git 仓库内执行"

if ! git remote get-url upstream >/dev/null 2>&1; then
  die "未配置 upstream。请执行:
  git remote add upstream https://github.com/QuantumNous/new-api.git"
fi

if [[ -n "$(git status --porcelain)" ]]; then
  die "工作区不干净，请先 commit 或 stash:
  git status"
fi

log "fetch upstream / origin ..."
git fetch upstream
git fetch origin

# 确保本地有二开分支
if git show-ref --verify --quiet "refs/heads/${CUSTOM_BRANCH}"; then
  git checkout "${CUSTOM_BRANCH}"
elif git show-ref --verify --quiet "refs/remotes/origin/${CUSTOM_BRANCH}"; then
  git checkout -b "${CUSTOM_BRANCH}" "origin/${CUSTOM_BRANCH}"
else
  log "本地/远端均无 ${CUSTOM_BRANCH}，从 upstream/${UPSTREAM_REF} 创建"
  git checkout -b "${CUSTOM_BRANCH}" "upstream/${UPSTREAM_REF}"
fi

BEFORE="$(git rev-parse --short HEAD)"
log "当前 ${CUSTOM_BRANCH}: ${BEFORE} $(git log -1 --pretty=%s)"

log "merge upstream/${UPSTREAM_REF} → ${CUSTOM_BRANCH}"
if ! git merge "upstream/${UPSTREAM_REF}" -m "chore: merge upstream/${UPSTREAM_REF} into ${CUSTOM_BRANCH}"; then
  cat <<EOF

合并出现冲突。请手动解决后执行:
  git add .
  git commit
  git push origin ${CUSTOM_BRANCH}

然后在 VPS:
  cd /root/new-api && ./update.sh
EOF
  exit 1
fi

AFTER="$(git rev-parse --short HEAD)"
log "合并完成: ${BEFORE} → ${AFTER}"

if [[ "$PUSH" == "1" ]]; then
  log "push origin ${CUSTOM_BRANCH} ..."
  git push -u origin "${CUSTOM_BRANCH}"
fi

cat <<EOF

下一步（VPS）:
  cd /root/new-api
  ./update.sh

建议合并后先本地或测试环境验证关键功能，再上生产。
EOF
