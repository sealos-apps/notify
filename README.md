# sealos-notify

sealos-notify 是 Sealos 平台的统一通知服务，支持多渠道通知投递，具备可靠重试、幂等保证和高可用能力。

## 功能特性

- **多渠道支持** — 站内信（CRD）、邮件、短信、语音、飞书 Webhook、飞书应用
- **飞书加急通知** — 发送消息后自动触发应用内/短信/电话加急提醒
- **可靠投递** — 数据库任务队列 + 指数退避重试，支持最大重试次数配置
- **幂等 API** — 相同 idempotencyKey 的重复请求安全幂等
- **高可用** — 多副本共享任务队列，通过数据库级 `FOR UPDATE SKIP LOCKED` 实现无冲突任务分配
- **配置热加载** — 修改渠道、Provider 和模板配置无需重启服务
- **优雅退出** — 停机时等待所有进行中的投递任务完成

## 架构概览

```
HTTP API → Engine → delivery_tasks 表 → Dispatcher → Channel Adapters
                                              ↕
                                      delivery_attempts 表
```

1. `POST /api/v1/notifications` 创建通知记录、接收人和投递任务（每个接收人×渠道生成一条任务）。
2. **Dispatcher** 按配置间隔轮询任务队列，通过 `FOR UPDATE SKIP LOCKED` 并发获取待执行和待重试任务（两类任务并行拉取）。
3. 每条任务在独立 goroutine 中调用对应 **Adapter**，结果写入 `delivery_attempts`；失败按退避调度重试，超过 `maxRetry` 后标记为 `dead`。

## 快速开始

### 前置依赖

- Go 1.21+
- PostgreSQL 14+

### 1. 克隆并准备配置

```bash
git clone https://github.com/labring/sealos-notify.git
cd sealos-notify
cp config.example.yaml config.yaml
# 按需修改 config.yaml 中的数据库配置和渠道配置
```

### 2. 启动 PostgreSQL（开发用）

```bash
docker run -d --name postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=sealos_notify \
  -p 5432:5432 postgres:16-alpine
```

### 3. 运行服务

```bash
go run . -c config.yaml
```

或使用 Docker：

```bash
docker build -t sealos-notify .
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml sealos-notify -c /config.yaml
```

### 4. 发送通知

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "test-001",
    "title": "系统告警",
    "content": "CPU 使用率超过 90%",
    "channels": ["feishu_app"],
    "recipients": [{"type": "feishu_user_id", "value": "ou_xxxxxxxx"}]
  }'
```

## API 接口

### `POST /api/v1/notifications` — 发送通知

**请求体：**

```json
{
  "idempotencyKey": "唯一标识，相同 key 的请求只执行一次",
  "title": "通知标题",
  "content": "通知内容",
  "channels": ["feishu_app", "email"],
  "recipients": [
    {"type": "feishu_user_id", "value": "ou_xxxxxxxx"},
    {"type": "email", "value": "user@example.com"}
  ],
  "template": "可选模板名",
  "variables": {"key": "value"}
}
```

接收人 `type` 与渠道的对应关系：

| 渠道            | 接收人 type         |
|-----------------|---------------------|
| `email`         | `email`             |
| `sms`, `voice`  | `phone`             |
| `inapp`         | `user_id`           |
| `feishu_app`, `feishu_webhook` | `feishu_user_id` 或 `email` |

**响应：**

```json
{"notificationId": "uuid", "status": "accepted"}
```

### `GET /api/v1/notifications/:id` — 查询通知状态

返回通知详情及所有投递任务。

### `GET /api/v1/notifications/:id/deliveries` — 查询投递任务

返回该通知的所有投递任务列表。

### `GET /health` — 健康检查

数据库可达时返回 `200 {"status":"ok"}`。

## 配置说明

完整示例见 `config.example.yaml`。

### `server`

| 字段           | 默认值  | 说明             |
|----------------|---------|------------------|
| `address`      | `:8080` | HTTP 监听地址    |
| `readTimeout`  | `30s`   | 读超时           |
| `writeTimeout` | `30s`   | 写超时           |
| `idleTimeout`  | `60s`   | 空闲超时         |

### `database`

| 字段              | 默认值          | 说明                 |
|-------------------|-----------------|----------------------|
| `host`            | `localhost`     | PostgreSQL 主机      |
| `port`            | `5432`          | PostgreSQL 端口      |
| `user`            | `postgres`      | 数据库用户           |
| `password`        |                 | 数据库密码           |
| `dbname`          | `sealos_notify` | 数据库名             |
| `sslMode`         | `disable`       | SSL 模式             |
| `maxOpenConns`    | `25`            | 最大连接数           |
| `maxIdleConns`    | `5`             | 最大空闲连接数       |
| `connMaxLifetime` | `5m`            | 连接最大生命周期     |

### `dispatcher`

| 字段           | 默认值  | 说明                                            |
|----------------|---------|-------------------------------------------------|
| `enabled`      | `true`  | 是否启用调度器                                  |
| `interval`     | `10s`   | 轮询间隔                                        |
| `batchSize`    | `100`   | 每轮次每类任务（pending/retry）的最大拉取数量   |
| `leaseTimeout` | `5m`    | 任务租约超时（超时后任务可被其他副本重新获取）  |

### `defaults`

| 字段                  | 默认值           | 说明                         |
|-----------------------|------------------|------------------------------|
| `maxRetry`            | `3`              | 最大重试次数（超过则标记 dead）|
| `retryBackoffSeconds` | `[30, 120, 300]` | 各次重试的等待秒数           |

## 飞书加急通知

飞书加急（紧急通知）是飞书应用消息的一个特殊功能，可以在普通消息基础上触发额外提醒方式：应用内弹窗加急、短信通知、电话通知。

### 配置步骤

1. 在[飞书开放平台](https://open.feishu.cn/app)创建企业自建应用。
2. 开通权限（"权限管理" 页面）：
   - `im:message:send_as_bot` — 以机器人身份发消息
   - `im:message.group_urgent_app:create` — 应用内加急（`urgentType: app`）
   - `im:message.group_urgent_sms:create` — 短信加急（`urgentType: sms`）
   - `im:message.group_urgent_phone:create` — 电话加急（`urgentType: phone`）
3. 在应用"凭证与基础信息"页获取 App ID 和 App Secret。
4. 将机器人添加到目标群组，或确保有权限给用户发送单聊消息。

### Provider 配置

```yaml
channels:
  feishu_app:
    enabled: true
    provider: feishu-app-urgent   # 引用下方 provider 名称

providers:
  feishu-app-urgent:
    type: feishu_app
    appId: "cli_xxxxxxxxxxxxxxxx"   # 飞书 App ID
    appSecret: "xxxxxxxxxxxxxxxx"   # 飞书 App Secret
    receiveIdType: "open_id"        # open_id | user_id | union_id | email
    msgType: "text"                 # text | post | interactive
    urgentType: "app"               # app（应用内加急）| sms（短信加急）| phone（电话加急）| ""（不加急）
```

### 发送飞书加急通知示例

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "incident-2024-001",
    "title": "P0 故障告警",
    "content": "数据库主节点不可用，请立即处理！",
    "channels": ["feishu_app"],
    "recipients": [
      {"type": "feishu_user_id", "value": "ou_xxxxxxxx"}
    ]
  }'
```

Adapter 执行逻辑：
1. 调用飞书 `im.v1.message.create` API 发送消息。
2. 获取返回的 `message_id`，调用对应的加急 API（`urgent_app` / `urgent_sms` / `urgent_phone`）。
3. 加急调用失败不会影响主消息的投递结果（非致命错误）。

### receiveIdType 与接收人 type 对应关系

| `receiveIdType` | 请求中接收人 `type` |
|-----------------|---------------------|
| `open_id`       | `feishu_user_id`    |
| `user_id`       | `feishu_user_id`    |
| `union_id`      | `feishu_user_id`    |
| `email`         | `email`             |

## 项目结构

```
sealos-notify/
├── main.go                         # 程序入口
├── config.example.yaml             # 配置示例
├── pkg/
│   ├── config/                     # 配置加载与热重载
│   ├── logger/                     # 日志初始化
│   ├── database/                   # 数据库连接（GORM）与 Schema 初始化
│   ├── storage/                    # 数据访问层（GORM ORM）
│   │   ├── notification.go         # 通知与接收人存储
│   │   └── delivery.go             # 投递任务与投递记录存储
│   ├── engine/                     # 通知引擎（请求校验、任务生成）
│   ├── dispatcher/                 # 任务调度器（轮询、并发分发、重试）
│   └── adapter/
│       ├── adapter.go              # Adapter 接口定义
│       └── feishu_app/             # 飞书应用加急通知 Adapter
├── server/                         # HTTP 服务器与路由
└── deploy/kubernetes/              # K8s 部署 manifests
```

## 添加新渠道

1. 在 `pkg/adapter/<channel_name>/` 下创建目录，实现 `adapter.Adapter` 接口：
   ```go
   type Adapter interface {
       Send(ctx context.Context, request *SendRequest) (*SendResponse, error)
       Name() string
       ChannelType() ChannelType
       Validate() error
   }
   ```
2. 在 `server/server.go` 的 `initAdapters()` 中注册该类型：
   ```go
   case "my_channel":
       a, err := mychannel.New(providerConfig.Data)
       s.adapters[providerName] = a
   ```
3. 在 `config.example.yaml` 中添加对应的 channel 和 provider 示例配置。

## Kubernetes 部署

```bash
# 创建命名空间和数据库 Secret
kubectl create namespace sealos
kubectl create secret generic postgres-secret \
  --from-literal=password=your-db-password -n sealos

# 部署
kubectl apply -f deploy/kubernetes/
```

多副本场景下直接调整 Deployment 的 `replicas`，无需额外配置，各副本通过数据库任务队列自动分工。

## 环境变量覆盖

所有配置字段均可通过环境变量覆盖，前缀对应配置节：

| 环境变量前缀    | 对应配置节     |
|----------------|---------------|
| `SERVER_`      | `server`      |
| `DATABASE_`    | `database`    |
| `LOGGING_`     | `logging`     |
| `DISPATCHER_`  | `dispatcher`  |

示例：`DATABASE_HOST=db.prod DATABASE_PASSWORD=secret ./sealos-notify -c config.yaml`

## 构建

```bash
make build         # 构建二进制
make docker-build  # 构建 Docker 镜像
make run           # 本地运行（需要 PostgreSQL）
```

## 许可证

Apache 2.0
