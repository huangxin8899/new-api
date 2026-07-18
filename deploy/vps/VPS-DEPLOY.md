# VPS 部署与更新操作文档

目标：在 VPS 用 Docker 自构建并运行你 fork 的 new-api（含邮箱模板二开）。

- VPS 现有 docker-compose 位于 `/root/new-api/`，当前用官方镜像 `calciumion/new-api:latest`
- 你的 fork：`https://github.com/huangxin8899/new-api.git`，二开分支 `custom`
- VPS 仅有 Docker，无 Go / bun 本地工具链

---

## 0. 前置确认

```bash
# 确认 docker + compose 可用
docker version && docker compose version

# 确认目录现状
ls -la /root/new-api/
cat /root/new-api/docker-compose.yml | head -30

# 查看现有 env 来源（确定现网真实密码，升级时不能改）
cat /root/new-api/.env 2>/dev/null || echo "无 .env，密码可能在 compose 里"
```

**重点记下现网的真实值（升级全程不能改）：**

- `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB`
- `SESSION_SECRET`
- Redis 是否有密码（原配置 `redis://redis` 无密码）

---

## 1. 备份（必做）

```bash
cd /root/new-api

# 优先用 .env 里的账号库名，回退到 root/new-api
PGUSER="$(grep -E '^POSTGRES_USER=' .env 2>/dev/null | cut -d= -f2 || echo root)"
PGDB="$(grep -E '^POSTGRES_DB=' .env 2>/dev/null | cut -d= -f2 || echo new-api)"
docker compose exec -T postgres pg_dump -U "$PGUSER" "$PGDB" \
  > "backup-$(date +%Y%m%d-%H%M%S).sql"

ls -lh backup-*.sql
```

---

## 2. 拉取 fork 源码

```bash
cd /root/new-api
git clone -b custom https://github.com/huangxin8899/new-api.git new-api-fork

# 若提示 custom 不存在：
# git clone -b main https://github.com/huangxin8899/new-api.git new-api-fork

# 确认拿到邮箱模板二开（应输出 1）
grep -c EmailVerificationSubject new-api-fork/common/constants.go
```

> 部署脚本和 compose 模板以 fork 仓库里的 `deploy/vps/` 为权威，下面拷出来用。

---

## 3. 安装部署文件

```bash
cd /root/new-api

# 备份旧 compose
cp docker-compose.yml "docker-compose.yml.bak.$(date +%Y%m%d)"

# 用 fork 里的部署脚本覆盖
cp new-api-fork/deploy/vps/docker-compose.yml ./docker-compose.yml
cp new-api-fork/deploy/vps/update.sh ./update.sh
cp new-api-fork/deploy/vps/rollback.sh ./rollback.sh
cp new-api-fork/deploy/vps/.env.example ./.env.example
chmod +x update.sh rollback.sh
```

---

## 4. 核对 .env（关键，别把现网密码改飞）

原 compose 用 `${POSTGRES_PASSWORD}` 等变量，现网一定有 `.env`。先查现网 env 来源：

```bash
cat .env 2>/dev/null || echo "现网无 .env，从旧 compose 备份里提取密码"
grep -E 'POSTGRES_PASSWORD|SESSION_SECRET|POSTGRES_USER|POSTGRES_DB' \
  docker-compose.yml.bak.* 2>/dev/null
```

再编辑 `.env`，**保证与现网数据库完全一致**：

```bash
nano .env
```

`.env` 内容（`POSTGRES_*` 和 `SESSION_SECRET` 必须等于现网原值）：

```env
POSTGRES_USER=<与现网一致>
POSTGRES_PASSWORD=<与现网一致>
POSTGRES_DB=<与现网一致，通常 new-api>
SESSION_SECRET=<与现网一致，不要换>
NODE_NAME=new-api-node-1
DEPLOY_BRANCH=custom
```

> Redis：原配置无密码（`redis://redis`），新 compose 也是无密码 redis，一致。若现网 redis 实际有密码，需手动在 `docker-compose.yml` 的 `REDIS_CONN_STRING` 补上。

---

## 5. 构建并切换到自建镜像

```bash
cd /root/new-api
docker compose build new-api
```

- 首次构建会编译前端 + Go，约 5~15 分钟
- 若 OOM（`Killed`），临时加 4G swap 后重 build：

```bash
sudo fallocate -l 4G /swapfile && sudo chmod 600 /swapfile
sudo mkswap /swapfile && sudo swapon /swapfile
free -h
docker compose build new-api
```

构建成功后启动：

```bash
docker compose up -d
docker compose ps
curl -s http://127.0.0.1:13000/api/status    # 应含 "success": true
docker compose logs --tail=100 new-api
```

---

## 6. 验证二开功能

进入后台（管理员）→ **系统设置 → 运维设置 → SMTP Email** 卡片，拉到底部应看到新分组：

- **Email Templates (optional) / 邮件模板（可选）**
- Verification Email Subject（Input）
- Verification Email Body（Textarea）
- Password Reset Email Body（Textarea）

全部留空 = 用回原默认中文模板；填内容即用自定义。可用占位符：`{{system_name}}` `{{code}}` `{{minutes}}` `{{link}}`。

直接验证后端配置读写（需 root token）：

```bash
TOKEN=你的root_token
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:13000/api/option/ \
  | grep -o 'EmailVerificationSubject\|EmailVerificationBody\|EmailResetEmailBody' | sort -u
```

应能看到三个新 key。

---

## 7. 以后日常更新（二开或跟上游）

```bash
cd /root/new-api
./update.sh
```

`update.sh` 流程：git pull `custom` → 备份 DB → build → 打 `my-new-api:YYYYMMDD-HHMMSS` 版本 tag → 重启 new-api → 健康检查。

回滚：

```bash
./rollback.sh                 # 列出历史 tag
./rollback.sh 20260712-153045 # 回到某版本
```

---

## 8. 故障排查

```bash
# 健康检查没过
docker compose logs --tail=200 new-api

# 连不上数据库：核对 .env 的 POSTGRES_* 与现网一致
docker compose exec new-api sh -c 'env | grep SQL_DSN'
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c '\l'

# 应急退回官方镜像
cd /root/new-api
cp docker-compose.yml.bak.<日期> docker-compose.yml   # 改回 image: calciumion/new-api:latest
docker compose up -d new-api
```

---

## 注意事项

1. **绝不删除** `/root/new-api/data/` 和 docker volume `pg_data`，那是所有业务数据
2. `.env` 的 `POSTGRES_*` 和 `SESSION_SECRET` 必须保持现网原值不变，否则连不上库 / 会话失效
3. 构建失败优先贴 `docker compose build new-api` 的**完整日志**反馈，不要盲目重试
4. Redis 在旧配置里无密码；若现网实际为有密码 redis，需同步改 `REDIS_CONN_STRING`，否则 new-api 启动后缓存报错
5. 内存不足先加 swap 再重 build
6. 不要执行 `git config` 任何**全局**身份、不要 `docker system prune -af`（会删镜像导致回滚脚本找不到旧 tag）