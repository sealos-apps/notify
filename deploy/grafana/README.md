# Grafana 通过 sealos-notify 发送飞书加急通知

本文档说明如何使用 Grafana Operator 的 `GrafanaContactPoint` 资源，把 Grafana 告警发送到 `sealos-notify`，再由 `sealos-notify` 发送飞书应用消息并触发加急通知。

对应配置文件：

```text
deploy/grafana/contactpoint.yaml
```

## 工作链路

```text
Grafana Alert Rule
  -> GrafanaContactPoint webhook
  -> POST /api/v1/notifications
  -> sealos-notify feishu_app channel
  -> 飞书应用消息
  -> 飞书应用内加急
```

当前 contact point 请求地址是：

```text
http://sealos-notify.ns-admin.svc.cluster.local:8080/api/v1/notifications
```

如果你的 `sealos-notify` 服务不在 `ns-admin` namespace，或者 Service 名不是 `sealos-notify`，需要先修改 `contactpoint.yaml` 里的 `spec.settings.url`。

## 前置条件

1. Kubernetes 集群里已经安装 Grafana Operator。
2. 目标 Grafana 实例带有如下 label：

```yaml
dashboards: grafana
```

如果 label 不同，修改 `contactpoint.yaml`：

```yaml
spec:
  instanceSelector:
    matchLabels:
      dashboards: grafana
```

3. `sealos-notify` 已经部署并且可从 Grafana Pod 访问。
4. `sealos-notify` 的 `feishu_app` provider 已配置好飞书应用，并且 `urgentType` 已启用，例如 `app`。
5. 已准备好一个飞书接收人的 Open ID。

当前 notify 配置使用的是：

```yaml
receiveIdType: open_id
urgentUserIdType: open_id
```

所以 `feishu-open-id` 要填写飞书 Open ID，例如 `ou_xxx`。

## 1. 修改 Secret

打开：

```text
deploy/grafana/contactpoint.yaml
```

修改最上面的 Secret：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: sealos-notify-contactpoint
  namespace: grafana
type: Opaque
stringData:
  bearer-token: notify-console:CHANGE_ME_TO_A_LONG_RANDOM_SECRET
  feishu-open-id: ou_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `bearer-token` | notify API 鉴权信息，格式是 `appId:appSecret` |
| `feishu-open-id` | 飞书接收人的 Open ID |

`sealos-notify` 支持两种鉴权方式：

```http
X-App-Id: notify-console
X-App-Secret: xxx
```

以及：

```http
Authorization: Bearer notify-console:xxx
```

Grafana webhook 这里使用的是第二种，所以 Secret 里要写成 `appId:appSecret`。

## 2. 创建 notify 模板

Grafana contact point 发送给 notify 的 payload 里使用了模板名：

```text
feishu-grafana-alert
```

因此需要先在 `sealos-notify` 中创建这个模板。

在能访问 `sealos-notify` Service 的环境里执行：

```bash
export NOTIFY_BASE_URL="http://sealos-notify.ns-admin.svc.cluster.local:8080"
export NOTIFY_APP_ID="notify-console"
export NOTIFY_APP_SECRET="CHANGE_ME_TO_A_LONG_RANDOM_SECRET"

curl -X POST "$NOTIFY_BASE_URL/api/v1/templates" \
  -H "Content-Type: application/json" \
  -H "X-App-Id: $NOTIFY_APP_ID" \
  -H "X-App-Secret: $NOTIFY_APP_SECRET" \
  -d '{
    "name": "feishu-grafana-alert",
    "channel": "feishu_app",
    "msgType": "text",
    "body": "[Grafana {{ .status }}] {{ .alertname }} severity={{ .severity }}\n{{ .summary }}\n{{ .description }}\n{{ .externalURL }}"
  }'
```

如果模板已经存在，使用更新接口：

```bash
curl -X PUT "$NOTIFY_BASE_URL/api/v1/templates/feishu-grafana-alert" \
  -H "Content-Type: application/json" \
  -H "X-App-Id: $NOTIFY_APP_ID" \
  -H "X-App-Secret: $NOTIFY_APP_SECRET" \
  -d '{
    "body": "[Grafana {{ .status }}] {{ .alertname }} severity={{ .severity }}\n{{ .summary }}\n{{ .description }}\n{{ .externalURL }}",
    "msgType": "text"
  }'
```

## 3. 应用 GrafanaContactPoint

执行：

```bash
kubectl apply -f deploy/grafana/contactpoint.yaml
```

这个文件会创建两个资源：

```text
Secret/sealos-notify-contactpoint
GrafanaContactPoint/sealos-notify-feishu-urgent
```

查看资源：

```bash
kubectl get secret sealos-notify-contactpoint -n grafana
kubectl get grafanacontactpoint sealos-notify-feishu-urgent -n grafana
```

查看同步状态：

```bash
kubectl describe grafanacontactpoint sealos-notify-feishu-urgent -n grafana
```

如果 Grafana Operator 正常同步，Grafana UI 的 Contact points 里会出现：

```text
sealos-notify-feishu-urgent
```

## 4. 在 Grafana 告警里使用

在 Grafana 中进入：

```text
Alerting -> Alert rules
```

创建或编辑告警规则，然后在通知策略中把告警路由到：

```text
sealos-notify-feishu-urgent
```

也可以进入：

```text
Alerting -> Contact points
```

找到 `sealos-notify-feishu-urgent`，使用 Grafana 的测试按钮发送一条测试通知。

## 5. 验证 notify 是否收到

查看 `sealos-notify` 日志：

```bash
kubectl logs -n ns-admin deploy/sealos-notify --tail=100
```

正常情况下可以看到类似日志：

```text
Notification created
Task completed successfully
```

也可以通过数据库或 notify API 查看 delivery 状态。成功时 delivery task 状态应为：

```text
success
```

## 6. contactpoint.yaml 关键字段说明

`spec.settings.url`：

```yaml
url: http://sealos-notify.ns-admin.svc.cluster.local:8080/api/v1/notifications
```

这是 Grafana webhook 调用 notify 的接口地址。

`spec.settings.authorization_scheme`：

```yaml
authorization_scheme: Bearer
```

配合 `valuesFrom` 注入的 `authorization_credentials`，最终请求头会变成：

```http
Authorization: Bearer notify-console:xxx
```

`spec.valuesFrom`：

```yaml
valuesFrom:
  - targetPath: authorization_credentials
    valueFrom:
      secretKeyRef:
        name: sealos-notify-contactpoint
        key: bearer-token
  - targetPath: payload.vars.feishu_open_id
    valueFrom:
      secretKeyRef:
        name: sealos-notify-contactpoint
        key: feishu-open-id
```

这里把 Secret 中的敏感信息注入到 Grafana contact point 设置里，避免把真实 token 和飞书 Open ID 写死在 webhook 配置主体中。

`spec.settings.payload.template`：

这个模板会把 Grafana 告警上下文转换成 notify API 需要的 JSON：

```json
{
  "idempotencyKey": "grafana-firing-xxx",
  "channels": {
    "feishu_app": {
      "template": "feishu-grafana-alert",
      "params": {
        "status": "firing",
        "alertname": "...",
        "severity": "...",
        "summary": "...",
        "description": "...",
        "groupKey": "...",
        "externalURL": "..."
      }
    }
  },
  "recipients": [
    {
      "type": "feishu_user_id",
      "value": "ou_xxx"
    }
  ]
}
```

## 常见问题

### Grafana UI 里看不到 contact point

检查 GrafanaContactPoint 的 namespace 和 Grafana 实例 label：

```bash
kubectl get grafana -A --show-labels
kubectl get grafanacontactpoint -n grafana
kubectl describe grafanacontactpoint sealos-notify-feishu-urgent -n grafana
```

重点看：

```yaml
spec:
  instanceSelector:
    matchLabels:
      dashboards: grafana
```

### notify 返回 401

说明 `bearer-token` 不对。检查 Secret 里的值是否是：

```text
appId:appSecret
```

而不是只有 appSecret。

### notify 返回模板不存在

说明还没有创建 `feishu-grafana-alert` 模板。按本文第 2 步创建或更新模板。

### 飞书消息成功但没有加急

检查 `sealos-notify` 的 provider 配置：

```yaml
urgentType: "app"
urgentUserIdType: "open_id"
```

同时确认飞书应用具备发送消息和应用内加急的权限。
