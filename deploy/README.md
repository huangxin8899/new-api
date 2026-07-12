# 二开部署（方案 A：VPS 本地 build）

面向路径：`/root/new-api/`

## 目录结构

```text
/root/new-api/
├── docker-compose.yml
├── .env
├── update.sh
├── rollback.sh
├── backups/              # update.sh 自动备份
├── data/
├── logs/
└── new-api-fork/         # 你的 fork 源码
```

## 一、本地（Windows）准备二开分支

```bash
cd /d/work/code/new-api
git fetch upstream
git checkout main
git merge upstream/main

# 创建并推送二开分支（只需一次）
git checkout -b custom
git push -u origin custom
```

日常改代码后：

```bash
git checkout custom
git add .
git commit -m "feat(custom): ..."
git push origin custom
```

合并官方更新：

```bash
bash deploy/scripts/sync-upstream.sh
```

## 二、VPS 首次部署

```bash
# 1. 进入现有 compose 目录
cd /root/new-api

# 2. 备份当前 compose（可选）
cp docker-compose.yml docker-compose.yml.bak.$(date +%Y%m%d)

# 3. 从本仓库拷贝部署文件（任选一种）
# 方式 A：已 clone fork 时
cp deploy/vps/docker-compose.yml /root/new-api/docker-compose.yml
cp deploy/vps/update.sh /root/new-api/update.sh
cp deploy/vps/rollback.sh /root/new-api/rollback.sh
cp deploy/vps/.env.example /root/new-api/.env.example
chmod +x /root/new-api/update.sh /root/new-api/rollback.sh

# 4. 克隆二开源码（与 compose 同级）
git clone -b custom https://github.com/huangxin8899/new-api.git /root/new-api/new-api-fork
# 若还没有 custom：-b main

# 5. 环境变量
# 若已有 .env，核对 POSTGRES_* / SESSION_SECRET 与现网一致，不要改密码导致连不上库
cp -n .env.example .env
# 编辑 .env，至少设置 POSTGRES_PASSWORD、SESSION_SECRET、DEPLOY_BRANCH=custom

# 6. 备份数据库后切换镜像
docker compose exec postgres pg_dump -U root new-api > backup-before-custom.sql
docker compose build new-api
docker compose up -d
docker compose ps
curl -s http://127.0.0.1:13000/api/status
```

> 从官方镜像切到自建镜像时：**不要删** `data/` 和 `pg_data` volume。

## 三、日常更新二开

本地 push 后，在 VPS：

```bash
cd /root/new-api
./update.sh
```

跳过备份 / 跳过 pull：

```bash
DO_BACKUP=0 ./update.sh
SKIP_PULL=1 ./update.sh   # 仅用当前本地源码重新 build
```

## 四、合并上游后再发版

```bash
# 本地
bash deploy/scripts/sync-upstream.sh
# 解决冲突（如有）后已自动 push

# VPS
cd /root/new-api && ./update.sh
```

## 五、回滚

```bash
cd /root/new-api
./rollback.sh                 # 列出 tag
./rollback.sh 20260712-153045 # 回滚到某次 update.sh 打的 tag
```

## 六、资源说明

- 构建包含前端（bun）+ Go，建议 VPS **内存 ≥ 4GB**
- 构建失败若是 OOM，可加大 swap 或改用 CI 推镜像方案
