# Sealos Notify

Sealos Notify 是一个统一的通知服务系统，为 Sealos 平台提供多渠道通知能力。

## 功能特性

- **多渠道支持**：支持站内信、邮件、短信、语音、飞书等多种通知渠道
- **统一管理**：统一的通知发送入口和状态管理
- **可靠投递**：支持失败重试、任务抢占、幂等保证
- **灵活配置**：支持配置热加载，无需重启服务
- **高可用**：支持多副本部署，通过数据库任务抢占实现负载均衡
- **可观测性**：完整的投递记录和审计日志

## 架构设计

### 核心组件

```
┌─────────────────────────────────────────────────────────┐
│                      API Layer                          │
│  (接收通知请求、提供查询接口、配置管理)                   │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                 Notification Engine                      │
│  (请求校验、渠道解析、模板处理、任务生成)                 │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                    Database                              │
│  (通知记录、投递任务、审计日志)                          │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                   Dispatcher                             │
│  (任务抢占、调度执行、失败重试)                          │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│               Channel Adapters                           │
│  (inapp, email, sms, voice, feishu)                    │
└─────────────────────────────────────────────────────────┘
```

### 工作流程

1. 调用方通过 API 发送通知请求
2. Engine 校验请求并生成通知记录
3. Engine 为每个接收人和渠道生成投递任务
4. Dispatcher 定期从数据库抢占待执行任务
5. Dispatcher 调用对应的 Channel Adapter 执行发送
6. 更新任务状态，失败时按退避策略重试

## 快速开始

### 前置要求

- Go 1.25+
- PostgreSQL 12+
- Kubernetes 1.20+ (生产部署)

### 本地开发

1. 克隆代码：

```bash
git clone https://github.com/labring/sealos-notify.git
cd sealos-notify
```

2. 安装依赖：

```bash
go mod download
```

3. 准备配置文件：

```bash
cp config.example.yaml config.local.yaml
# 修改 config.local.yaml 中的数据库配置
```

4. 启动 PostgreSQL（使用 Docker）：

```bash
docker run -d \
  --name postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=sealos_notify \
  -p 5432:5432 \
  postgres:16-alpine
```

5. 运行服务：

```bash
make run
# 或者
go run main.go --config config.local.yaml
```

### 构建

```bash
# 构建二进制
make build

# 构建 Docker 镜像
make docker-build
```

## API 使用

### 发送通知

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "order-123-notify-1",
    "title": "订单通知",
    "content": "您的订单已创建成功",
    "channels": ["email", "sms"],
    "recipients": [
      {
        "type": "email",
        "value": "user@example.com"
      },
      {
        "type": "phone",
        "value": "+8613800000000"
      }
    ],
    "variables": {
      "orderId": "123",
      "amount": "99.00"
    }
  }'
```

### 查询通知状态

```bash
curl http://localhost:8080/api/v1/notifications/{notificationId}
```

### 查询投递记录

```bash
curl http://localhost:8080/api/v1/notifications/{notificationId}/deliveries
```

### 健康检查

```bash
curl http://localhost:8080/health
```

## 配置说明

### 服务器配置

```yaml
server:
  address: ":8080"          # 监听地址
  readTimeout: 30s          # 读超时
  writeTimeout: 30s         # 写超时
  idleTimeout: 60s          # 空闲超时
```

### 数据库配置

```yaml
database:
  host: localhost           # 数据库主机
  port: 5432                # 数据库端口
  user: postgres            # 数据库用户
  password: postgres        # 数据库密码
  dbname: sealos_notify     # 数据库名
  sslMode: disable          # SSL 模式
  maxOpenConns: 25          # 最大连接数
  maxIdleConns: 5           # 最大空闲连接数
  connMaxLifetime: 5m       # 连接最大生命周期
```

### 调度器配置

```yaml
dispatcher:
  enabled: true             # 是否启用调度器
  interval: 10s             # 轮询间隔
  batchSize: 100            # 每批次任务数
  leaseTimeout: 5m          # 任务租约超时
```

### 渠道配置

```yaml
channels:
  email:
    enabled: true           # 是否启用
    provider: smtp-default  # 使用的 provider

providers:
  smtp-default:
    type: smtp
    host: smtp.example.com
    port: 465
    username: notify@example.com
    passwordFromSecret:     # 从 Secret 读取密码
      name: notify-smtp-secret
      key: password
```

## 数据库表结构

- **notifications**: 通知主表
- **notification_recipients**: 接收人表
- **delivery_tasks**: 投递任务表
- **delivery_attempts**: 投递尝试记录表
- **config_change_audits**: 配置变更审计表

详细 schema 见 `pkg/database/postgres.go`。

## 部署

### Kubernetes 部署

1. 创建命名空间和 Secret：

```bash
kubectl create namespace sealos
kubectl create secret generic postgres-secret \
  --from-literal=password=your-password \
  -n sealos
```

2. 应用配置：

```bash
kubectl apply -f deploy/kubernetes/
```

### 配置热加载

服务支持配置热加载。修改 ConfigMap 后，服务会自动检测变更并重新加载配置，无需重启。

```bash
kubectl edit configmap sealos-notify-config -n sealos
```

## 开发指南

### 项目结构

```
sealos-notify/
├── main.go                      # 主入口
├── pkg/
│   ├── config/                  # 配置管理
│   ├── logger/                  # 日志管理
│   ├── database/                # 数据库层
│   ├── storage/                 # 存储层
│   ├── engine/                  # 通知引擎
│   ├── dispatcher/              # 任务调度器
│   └── adapter/                 # 渠道适配器
├── server/                      # HTTP 服务器
└── deploy/                      # 部署文件
```

### 添加新渠道

1. 在 `pkg/adapter/` 下创建新渠道目录
2. 实现 `adapter.Adapter` 接口
3. 在配置中添加对应的 provider 和 channel 配置
4. 在 `server/server.go` 的 `initAdapters` 方法中初始化适配器

### 测试

```bash
# 运行测试
make test

# 运行 lint
make lint
```

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License
