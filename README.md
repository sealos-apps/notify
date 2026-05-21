# sealos-notify

sealos-notify is the unified notification service for the Sealos platform. It supports multi-channel delivery with reliable retries, idempotent requests, and horizontally scalable workers.

## Features

- **Multiple channels**: in-app messages through CRDs, email, SMS, voice calls, Feishu webhooks, and Feishu app messages.
- **Feishu urgent notifications**: after sending a message, the Feishu app adapter can trigger in-app, SMS, or phone-call urgent reminders.
- **Template-driven content**: notification content is rendered from database-managed templates, with template CRUD exposed through the API.
- **API authentication and auditability**: all `/api/v1` endpoints use `appId` + `appSecret` authentication, and notifications store the sender `appId`.
- **Reliable delivery**: database-backed delivery queue, exponential backoff retries, and configurable retry limits.
- **Idempotent API**: repeated requests with the same `idempotencyKey` are handled safely.
- **High availability**: multiple replicas share the delivery queue and use database-level `FOR UPDATE SKIP LOCKED` to claim tasks without conflicts.
- **Hot reload**: channel, provider, and authentication credential changes can be reloaded without restarting the service.
- **Graceful shutdown**: the service waits for in-flight delivery tasks before exiting.

## Architecture

```text
HTTP API -> Engine -> delivery_tasks table -> Dispatcher -> Channel Adapters
                                                |
                                                v
                                      delivery_attempts table
```

1. `POST /api/v1/notifications` creates a notification record, recipient records, and delivery tasks. A task is generated for each compatible recipient and channel pair.
2. The **Dispatcher** polls the queue at the configured interval. It concurrently claims pending and retry-ready tasks with `FOR UPDATE SKIP LOCKED`.
3. Each task runs in its own goroutine. The dispatcher loads the template, renders content, calls the configured **Adapter**, records the result in `delivery_attempts`, and schedules retries with backoff. Tasks that exceed `maxRetry` are marked `dead`.

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 14+

### 1. Clone and Prepare Configuration

```bash
git clone https://github.com/labring/sealos-notify.git
cd sealos-notify
cp config.example.yaml config.yaml
# Edit config.yaml for your database and channel settings.
```

### 2. Start PostgreSQL for Development

```bash
docker run -d --name postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=sealos_notify \
  -p 5432:5432 postgres:16-alpine
```

### 3. Run the Service

For local development, create an API credential file first:

```bash
mkdir -p /tmp/sealos-notify-auth
cat >/tmp/sealos-notify-auth/apps.yaml <<'EOF'
apps:
  - appId: "notify-console"
    appSecret: "dev-secret"
    name: "Notify Console"
    enabled: true
EOF
```

Then set `auth.credentialsFilePath` in `config.yaml` to `/tmp/sealos-notify-auth/apps.yaml`.

```bash
go run . -c config.yaml
```

Or run with Docker:

```bash
docker build -t sealos-notify .
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml sealos-notify -c /config.yaml
```

### 4. Create a Template and Send a Notification

```bash
# Create a template.
curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: dev-secret" \
  -d '{
    "name": "feishu-alert",
    "channel": "feishu_app",
    "msgType": "text",
    "body": "[Alert] {{ .incident }} (severity: {{ .severity }})"
  }'

# Send a notification. Template parameters live under channels;
# recipients use the {type, value} structure.
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: dev-secret" \
  -d '{
    "idempotencyKey": "incident-001",
    "channels": {
      "feishu_app": {
        "template": "feishu-alert",
        "params": {"incident": "database primary unavailable", "severity": "P0"}
      }
    },
    "recipients": [
      {"type": "feishu_user_id", "value": "ou_xxxxxxxx"},
      {"type": "feishu_user_id", "value": "ou_yyyyyyyy"}
    ]
  }'
```

## API

All `/api/v1/*` endpoints require authentication except `GET /health`. The recommended authentication method is headers:

```http
X-App-Id: notify-console
X-App-Secret: dev-secret
```

`Authorization: Bearer <appId>:<appSecret>` is also supported.

Credential files support YAML or JSON:

```yaml
apps:
  - appId: notify-console
    appSecret: CHANGE_ME_TO_A_LONG_RANDOM_SECRET
    name: Notify Console
    enabled: true
```

Disabled credentials are ignored. Credential file changes are hot reloaded when the auth watcher is running.

### Notifications

#### `POST /api/v1/notifications` - Send a Notification

Request body:

```json
{
  "idempotencyKey": "unique key; repeated requests with the same key run once",
  "channels": {
    "feishu_app": {
      "template": "feishu-alert",
      "params": {"incident": "database primary unavailable", "severity": "P0"}
    },
    "email": {
      "template": "email-alert",
      "params": {"incident": "database primary unavailable", "severity": "P0"}
    }
  },
  "recipients": [
    {"type": "feishu_user_id", "value": "ou_xxxxxxxx"},
    {"type": "email", "value": "alice@example.com"},
    {"type": "phone", "value": "+8613800000000"}
  ]
}
```

- `channels`: a map from channel name to `{template, params}`.
- `template`: the template name stored in the database. This field is required for each channel.
- `params`: values injected into the template. All recipients for the same channel share the same rendered content.
- `recipients`: a list of delivery addresses.
- `type`: the address type used to match recipients to channels.
- `value`: the concrete delivery address, such as an Open ID, email address, or phone number.

Recipient `type` to channel mapping:

| `type` value | Matching channels |
| --- | --- |
| `email` | `email`, `feishu_app`, `feishu_webhook` |
| `phone` | `sms`, `voice` |
| `user_id` | `inapp` |
| `feishu_user_id` | `feishu_app`, `feishu_webhook` |

Response:

```json
{"notificationId": "uuid", "status": "accepted"}
```

#### `GET /api/v1/notifications/:id` - Get Notification Status

Returns the notification details and all delivery tasks, including `senderAppId`.

#### `GET /api/v1/notifications/:id/deliveries` - List Delivery Tasks

Returns all delivery tasks for the notification.

### Template Management

Templates are stored in the database and managed through the API. Each template belongs to one channel and contains a Go `text/template` body plus an optional subject for email.

#### `POST /api/v1/templates` - Create a Template

Request body:

```json
{
  "name": "feishu-incident",
  "channel": "feishu_app",
  "description": "Feishu incident alert template",
  "subject": "",
  "body": "[{{ .severity }}] {{ .incident }}\nAffected user: {{ .name }}",
  "msgType": "text",
  "templateCode": ""
}
```

| Field | Description |
| --- | --- |
| `name` | Unique template name used by notification requests. Required. |
| `channel` | Channel name, such as `feishu_app`, `email`, or `sms`. Required. |
| `body` | Message body written with Go `text/template` syntax. |
| `subject` | Email subject, also rendered with Go `text/template`. |
| `msgType` | Message format used by Feishu channels: `text`, `post`, or `interactive`. |
| `templateCode` | Provider-side template code used by SMS or voice channels. |

Response: the created template object with HTTP `201`.

#### `GET /api/v1/templates` - List Templates

Use `?channel=feishu_app` to filter by channel.

#### `GET /api/v1/templates/:name` - Get a Template

Returns the template identified by `name`.

#### `PUT /api/v1/templates/:name` - Update a Template

The request body uses the same shape as create. `name` and `channel` are not changed by this endpoint; the target template is selected by the URL parameter.

#### `DELETE /api/v1/templates/:name` - Delete a Template

Deletes the template identified by `name`.

### Health Check

#### `GET /health`

Returns `200 {"status":"healthy"}` when the database is reachable.

## Configuration

See `config.example.yaml` for a complete example.

### `server`

| Field | Default | Description |
| --- | --- | --- |
| `address` | `:8080` | HTTP listen address. |
| `readTimeout` | `30s` | HTTP read timeout. |
| `writeTimeout` | `30s` | HTTP write timeout. |
| `idleTimeout` | `60s` | HTTP idle timeout. |

### `database`

| Field | Default | Description |
| --- | --- | --- |
| `host` | `localhost` | PostgreSQL host. |
| `port` | `5432` | PostgreSQL port. |
| `user` | `postgres` | Database user. |
| `password` | | Database password. |
| `dbname` | `sealos_notify` | Database name. |
| `sslMode` | `disable` | PostgreSQL SSL mode. |
| `maxOpenConns` | `25` | Maximum open connections. |
| `maxIdleConns` | `5` | Maximum idle connections. |
| `connMaxLifetime` | `5m` | Maximum connection lifetime. |

### `dispatcher`

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `true` | Enables the dispatcher. |
| `interval` | `10s` | Queue polling interval. |
| `batchSize` | `100` | Maximum number of pending and retry tasks claimed per cycle. |
| `leaseTimeout` | `5m` | Processing lease timeout. Expired tasks can be reclaimed by another replica. |

### `auth`

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `true` | Enables authentication for `/api/v1` endpoints. |
| `credentialsFilePath` | | Path to the app credential file, usually mounted from a Kubernetes Secret. |

### `defaults`

| Field | Default | Description |
| --- | --- | --- |
| `maxRetry` | `3` | Maximum retry count before a task is marked `dead`. |
| `retryBackoffSeconds` | `[30, 120, 300]` | Retry delay for each retry attempt. |

### `channels`

Each channel entry has this shape:

```yaml
channels:
  feishu_app:
    enabled: true
    provider: feishu-app-urgent   # References a provider name under providers.
```

### `providers`

Each provider uses `type` to select an adapter. The remaining fields are passed to the adapter constructor as provider data.

## Feishu Urgent Notifications

Feishu urgent notification is a Feishu app message feature. After the normal app message is created, the adapter can trigger an additional in-app urgent alert, SMS alert, or phone-call alert.

### Setup

1. Create an internal app in the [Feishu Open Platform](https://open.feishu.cn/app).
2. Enable these permissions in the permission management page:
   - `im:message:send_as_bot`: send messages as the bot.
   - `im:message.group_urgent_app:create`: in-app urgent alert (`urgentType: app`).
   - `im:message.group_urgent_sms:create`: SMS urgent alert (`urgentType: sms`).
   - `im:message.group_urgent_phone:create`: phone-call urgent alert (`urgentType: phone`).
3. Copy the App ID and App Secret from the app credentials page.
4. Add the bot to target groups, or make sure it can send direct messages to the target users.

### Provider Configuration

```yaml
channels:
  feishu_app:
    enabled: true
    provider: feishu-app-urgent

providers:
  feishu-app-urgent:
    type: feishu_app
    appId: "cli_xxxxxxxxxxxxxxxx"
    appSecret: "xxxxxxxxxxxxxxxx"
    receiveIdType: "open_id"    # open_id | user_id | union_id | email
    urgentUserIdType: "open_id" # open_id | user_id | union_id; defaults to receiveIdType except email/chat_id
    msgType: "text"             # text | post | interactive
    urgentType: "app"           # app | sms | phone | empty string disables urgent alerts
```

### Adapter Flow

1. Calls Feishu `im.v1.message.create` to send the message.
2. Extracts the returned `message_id` and calls the selected urgent API: `urgent_app`, `urgent_sms`, or `urgent_phone`.
3. Urgent API failures do not fail the main delivery because the message has already been created. The error is stored in `details.urgent_error`.

### `receiveIdType` to Recipient Key Mapping

| `receiveIdType` | Recipient key |
| --- | --- |
| `open_id` | `feishu_user_id` |
| `user_id` | `feishu_user_id` |
| `union_id` | `feishu_user_id` |
| `email` | `email` |

## Project Layout

```text
sealos-notify/
├── main.go                         # Program entrypoint
├── config.example.yaml             # Example configuration
├── pkg/
│   ├── config/                     # Configuration loading and hot reload
│   ├── logger/                     # Logger setup
│   ├── database/                   # GORM database connection and schema initialization
│   ├── storage/                    # Data access layer
│   │   ├── notification.go         # Notification and recipient storage
│   │   ├── delivery.go             # Delivery task and attempt storage
│   │   └── template.go             # Template CRUD storage
│   ├── render/                     # Template rendering with text/template
│   ├── engine/                     # Request validation and task generation
│   ├── dispatcher/                 # Queue polling, dispatch, and retry logic
│   └── adapter/
│       ├── adapter.go              # Adapter interface definitions
│       └── feishu_app/             # Feishu app urgent notification adapter
├── server/                         # HTTP server, routes, and handlers
└── deploy/kubernetes/              # Kubernetes manifests
```

## Adding a New Channel

1. Create `pkg/adapter/<channel_name>/` and implement the `adapter.Adapter` interface:

   ```go
   type Adapter interface {
       Send(ctx context.Context, request *SendRequest) (*SendResponse, error)
       Name() string
       ChannelType() ChannelType
       Validate() error
   }
   ```

2. Add the recipient identifier mapping in `RecipientIdentifierKeys()` in `pkg/adapter/adapter.go`.
3. Register the provider type in `server/server.go`:

   ```go
   case "my_channel":
       a, err := mychannel.New(providerConfig.Data)
       s.adapters[providerName] = a
   ```

4. Add example channel and provider configuration to `config.example.yaml`.

## Kubernetes Deployment

```bash
# Build and push the image to Docker Hub.
make docker-build IMAGE=docker.io/<dockerhub-user>/sealos-notify VERSION=test
make docker-push IMAGE=docker.io/<dockerhub-user>/sealos-notify VERSION=test

# Update deploy/kubernetes/deployment.yaml with the image name, then create
# the Feishu credential Secret and API authentication Secret.
kubectl create namespace ns-admin --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic sealos-notify-feishu \
  --from-literal=app-id=cli_xxxxxxxxxxxxxxxx \
  --from-literal=app-secret=xxxxxxxxxxxxxxxx \
  -n ns-admin
kubectl create secret generic sealos-notify-api-auth \
  --from-file=apps.yaml=/path/to/apps.yaml \
  -n ns-admin

# Deploy.
kubectl apply -f deploy/kubernetes/
```

The default test manifests use:

| Item | Value |
| --- | --- |
| namespace | `ns-admin` |
| PostgreSQL host | `sealos-notify-pg-postgresql-0.sealos-notify-pg-postgresql-hl.ns-admin.svc.cluster.local` |
| PostgreSQL Secret | `sealos-notify-pg-postgresql` / `postgres-password` |
| Service URL | `http://sealos-notify.ns-admin.svc.cluster.local:8080` |

Smoke-test the send path:

```bash
kubectl -n ns-admin port-forward svc/sealos-notify 8080:8080

curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: CHANGE_ME_TO_A_LONG_RANDOM_SECRET" \
  -d '{"name":"feishu-urgent-test","channel":"feishu_app","body":"[Urgent test] {{ .message }}","msgType":"text"}'

curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -H "X-App-Id: notify-console" \
  -H "X-App-Secret: CHANGE_ME_TO_A_LONG_RANDOM_SECRET" \
  -d '{"idempotencyKey":"feishu-urgent-test-001","channels":{"feishu_app":{"template":"feishu-urgent-test","params":{"message":"sealos-notify staging integration test"}}},"recipients":[{"type":"feishu_user_id","value":"ou_xxxxxxxxxxxxxxxx"}]}'
```

For multiple replicas, update the Deployment `replicas` field. Replicas automatically share work through the database delivery queue.

## Environment Variable Overrides

Configuration fields can be overridden with environment variables:

| Environment variable prefix | Configuration section |
| --- | --- |
| `SERVER_` | `server` |
| `DATABASE_` | `database` |
| `LOGGING_` | `logging` |
| `DISPATCHER_` | `dispatcher` |
| `AUTH_` | `auth` |

Example: `DATABASE_HOST=db.prod DATABASE_PASSWORD=secret ./sealos-notify -c config.yaml`

## Build

```bash
make build         # Build the binary.
make docker-build  # Build the Docker image.
make run           # Run locally; requires PostgreSQL.
make test          # Run unit tests with race detection and coverage.
```

## License

Apache 2.0
