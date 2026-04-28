# LinuxDo 邀请码分发系统

基于 Go 的 LinuxDo 社区邀请码自动分发服务。用户通过 QQ 邮箱申请，邮件验证后领取邀请码。

## 功能

- 邮箱申请 → 邮件验证 → 领取邀请码
- 令牌桶限流 + 分布式锁防并发
- Altcha POW 人机验证（防脚本抢码，不依赖外部服务）
- 管理员上传邀请码、分页查看所有邀请码状态
- 过期 pending 邀请码自动释放

## 技术栈

Go + Gin + PostgreSQL + Redis + [Resend](https://resend.com)（邮件发送）+ [Altcha](https://altcha.org)（POW 验证）

## 快速开始

### 1. 环境要求

- Go 1.23+
- PostgreSQL
- Redis

### 2. 配置

```bash
cp example.env .env
# 编辑 .env 填入实际配置
```

### 3. 建表

```sql
CREATE TABLE IF NOT EXISTS linuxdo_invite_code (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(255) NOT NULL UNIQUE,
    email      VARCHAR(255),
    claimed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);
```

### 4. 运行

```bash
go run .
```

访问 `http://localhost:7386`

### 5. Docker

```bash
docker build -t linuxdo-invitecode .
docker run -d --env-file .env -p 7386:7386 linuxdo-invitecode
```

## 管理员操作

页面底部点击「管理员入口」→ 输入密钥 → 进入独立管理后台。

管理后台功能：
- 上传邀请码（每行一个）
- 分页查看所有邀请码状态（未领取 / 待验证 / 已领取）

也可通过 API 上传：

```bash
curl -X POST http://localhost:7386/api/invite/upload \
  -H "Content-Type: application/json" \
  -H "X-Admin-Key: your_admin_key" \
  -d '{"codes":["invite-code-001","invite-code-002"]}'
```

## 配置说明

| 变量 | 说明 | 必填 |
|------|------|------|
| `DB_PASSWORD` | PostgreSQL 密码 | ✅ |
| `ADMIN_UPLOAD_KEY` | 管理员密钥 | ✅ |
| `RESEND_API_KEY` | Resend 邮件 API Key | ✅ |
| `ALTCHA_HMAC_KEY` | POW 验证签名密钥（不配则跳过验证） | 否 |
| `RESEND_FROM_EMAIL` | 发件人邮箱 | 否，默认 `noreply@mynnmy.top` |
| `FRONT_URL` | 前端地址（邮件中的验证链接） | 否，默认 `http://localhost:7386` |
| `PORT` | 服务端口 | 否，默认 `7386` |
| `REDIS_ADDR` | Redis 地址 | 否，默认 `localhost:6379` |

其余配置见 `example.env`。

## License

MIT
