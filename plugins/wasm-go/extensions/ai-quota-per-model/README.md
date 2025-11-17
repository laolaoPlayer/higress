---
title: AI 按模型配额管理
keywords: [ AI网关, AI配额, 模型配额 ]
description: AI 按模型配额管理插件配置参考
---

## 功能说明

`ai-quota-per-model` 插件实现给特定 consumer 按 AI 模型分配独立的 quota，支持同一用户使用不同模型时分别计算和扣减配额。同时支持 quota 管理能力，包括查询 quota、刷新 quota、增减 quota。

**核心特性：**
- ✅ **按模型独立配额**：同一个 consumer 可以为不同模型（如 gpt-4、gpt-3.5-turbo）分配不同的 token 配额
- ✅ **自动扣减**：根据实际使用的 token 数量（输入 + 输出 token）自动扣减对应模型的配额
- ✅ **配额管理接口**：支持查询、刷新、增减任意 consumer 和模型的配额
- ✅ **灵活管理**：通过 REST API 动态管理配额，无需重启服务

**插件依赖：**
- 需要配合认证插件（如 `key-auth`、`jwt-auth`）获取认证身份的 consumer 名称
- 需要配合 `ai-statistics` 插件获取 AI Token 统计信息
- 需要 Redis 服务存储配额数据

## 运行属性

插件执行阶段：`默认阶段`
插件执行优先级：`280`

## 配置说明

| 名称                 | 数据类型   | 填写要求 | 默认值            | 描述                                |
|--------------------|--------|------|----------------|-----------------------------------|
| `redis_key_prefix` | string | 选填   | ai_model_quota: | quota redis key 前缀               |
| `admin_consumer`   | string | 必填   | -              | 管理 quota 的 consumer 名称           |
| `admin_path`       | string | 选填   | /quota         | 管理 quota 请求 path 前缀              |
| `redis`            | object | 必填   | -              | redis 相关配置                        |

### `redis` 配置字段说明

| 配置项          | 类型     | 必填 | 默认值                                       | 说明                                                                             |
|--------------|--------|----|--------------------------------------------|--------------------------------------------------------------------------------|
| service_name | string | 必填 | -                                          | redis 服务名称，带服务类型的完整 FQDN 名称，例如 my-redis.dns、redis.my-ns.svc.cluster.local |
| service_port | int    | 否  | 服务类型为固定地址（static service）默认值为 80，其他为 6379 | 输入 redis 服务的服务端口                                                               |
| username     | string | 否  | -                                          | redis 用户名                                                                      |
| password     | string | 否  | -                                          | redis 密码                                                                       |
| timeout      | int    | 否  | 1000                                       | redis 连接超时时间，单位毫秒                                                              |
| database     | int    | 否  | 0                                          | 使用的数据库 id，例如配置为 1，对应 `SELECT 1`                                              |

## 配置示例

### 基础配置

```yaml
redis_key_prefix: "ai_model_quota:"
admin_consumer: admin
admin_path: /quota
redis:
  service_name: redis-service.default.svc.cluster.local
  service_port: 6379
  timeout: 2000
```

### 完整 WasmPlugin 配置示例

```yaml
apiVersion: extensions.higress.io/v1alpha1
kind: WasmPlugin
metadata:
  name: ai-quota-per-model
  namespace: higress-system
spec:
  defaultConfig:
    redis_key_prefix: "ai_model_quota:"
    admin_consumer: admin
    admin_path: /quota
    redis:
      service_name: redis-service.default.svc.cluster.local
      service_port: 6379
      timeout: 2000
  priority: 280
```

## 使用说明

### 前置条件

1. **部署 Redis 服务**
2. **配置认证插件**（例如 key-auth）：

```yaml
apiVersion: extensions.higress.io/v1alpha1
kind: WasmPlugin
metadata:
  name: key-auth
  namespace: higress-system
spec:
  defaultConfig:
    consumers:
    - name: user1
      credential: "apikey-user1-12345"
    - name: user2
      credential: "apikey-user2-67890"
    - name: admin
      credential: "apikey-admin-secret"
    keys:
    - "x-api-key"
```

3. **配置 ai-statistics 插件**用于 token 统计

### 管理配额

插件假设在 `/v1/chat/completions` 路由上生效。

#### 1. 刷新配额（设置指定值）

为 user1 设置 gpt-4 模型的配额为 10000 tokens：

```bash
curl https://your-gateway.com/v1/chat/completions/quota/refresh \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-4&quota=10000"
```

**响应示例：**
```json
{
  "consumer": "user1",
  "model": "gpt-4",
  "quota": 10000,
  "status": "refreshed"
}
```

为同一个 user1 设置 gpt-3.5-turbo 模型的配额为 50000 tokens：

```bash
curl https://your-gateway.com/v1/chat/completions/quota/refresh \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-3.5-turbo&quota=50000"
```

**Redis 存储结构：**
```
ai_model_quota:user1:gpt-4          -> 10000
ai_model_quota:user1:gpt-3.5-turbo  -> 50000
```

#### 2. 查询配额

查询 user1 的 gpt-4 剩余配额：

```bash
curl "https://your-gateway.com/v1/chat/completions/quota?consumer=user1&model=gpt-4" \
  -H "x-api-key: apikey-admin-secret"
```

**响应示例：**
```json
{
  "consumer": "user1",
  "model": "gpt-4",
  "quota": 9500
}
```

查询 user1 的 gpt-3.5-turbo 剩余配额：

```bash
curl "https://your-gateway.com/v1/chat/completions/quota?consumer=user1&model=gpt-3.5-turbo" \
  -H "x-api-key: apikey-admin-secret"
```

#### 3. 增减配额

为 user1 的 gpt-4 增加 1000 tokens：

```bash
curl https://your-gateway.com/v1/chat/completions/quota/delta \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-4&value=1000"
```

**响应示例：**
```json
{
  "consumer": "user1",
  "model": "gpt-4",
  "delta": 1000,
  "new_quota": 10500
}
```

为 user1 的 gpt-4 减少 500 tokens（使用负数）：

```bash
curl https://your-gateway.com/v1/chat/completions/quota/delta \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-4&value=-500"
```

### 用户使用 API

用户使用 API 时，插件会自动：
1. 从请求体中提取 `model` 字段
2. 检查该 consumer 对应模型的配额是否充足
3. 请求成功后根据实际 token 使用量扣减配额

#### 示例：使用 gpt-4

```bash
curl https://your-gateway.com/v1/chat/completions \
  -H "x-api-key: apikey-user1-12345" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ]
  }'
```

**处理流程：**
1. 认证插件识别出 consumer 为 `user1`
2. ai-quota-per-model 插件提取 model 为 `gpt-4`
3. 检查 Redis key `ai_model_quota:user1:gpt-4` 的值是否 > 0
4. 如果配额充足，允许请求继续
5. AI 响应返回后，统计使用的 token 数（如 500 tokens）
6. 从 Redis key `ai_model_quota:user1:gpt-4` 扣减 500

#### 示例：使用 gpt-3.5-turbo

```bash
curl https://your-gateway.com/v1/chat/completions \
  -H "x-api-key: apikey-user1-12345" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ]
  }'
```

**处理流程与上面类似，但使用的是不同的 Redis key：**
- 检查和扣减的是 `ai_model_quota:user1:gpt-3.5-turbo`

#### 配额不足时的响应

如果配额不足，会返回 403 错误：

```json
HTTP/1.1 403 Forbidden
Content-Type: text/plain

Request denied by ai quota check. No quota left for model 'gpt-4'
```

## Redis 数据结构

插件在 Redis 中的 key 格式为：

```
{redis_key_prefix}{consumer}:{model}
```

**示例数据：**

```
ai_model_quota:user1:gpt-4              -> 10000
ai_model_quota:user1:gpt-3.5-turbo      -> 50000
ai_model_quota:user1:claude-3-opus      -> 5000
ai_model_quota:user2:gpt-4              -> 20000
ai_model_quota:user2:gpt-3.5-turbo      -> 100000
```

**特点：**
- 同一个 consumer 可以有多个模型的配额
- 不同模型的配额互不影响，独立计算和扣减
- 如果某个 consumer+model 组合的 key 不存在或值为 0，则拒绝请求

## 错误码说明

| HTTP 状态码 | 错误码详情                   | 说明                      |
|----------|-------------------------|-------------------------|
| 401      | ai-quota.no_key         | 缺少认证信息（没有找到 consumer） |
| 403      | ai-quota.unauthorized   | 未授权的 consumer           |
| 403      | ai-quota.noquota        | 配额不足                    |
| 400      | ai-quota.invalid_request | 请求体缺少 model 字段          |
| 400      | ai-quota.invalid_params | 管理接口参数错误                |
| 503      | ai-quota.error          | Redis 错误                 |

## 常见问题

### Q1: 如何为同一个用户设置不同模型的配额？

A: 分别调用 refresh 接口，指定不同的 model 参数即可：

```bash
# 设置 gpt-4 配额
curl .../quota/refresh -d "consumer=user1&model=gpt-4&quota=10000" -H "x-api-key: admin-key"

# 设置 gpt-3.5-turbo 配额
curl .../quota/refresh -d "consumer=user1&model=gpt-3.5-turbo&quota=50000" -H "x-api-key: admin-key"
```

### Q2: 配额扣减的时机是什么时候？

A: 配额扣减发生在 AI 响应返回后，基于实际使用的 token 数量（输入 token + 输出 token）进行扣减。

### Q3: 如果请求失败，配额会被扣减吗？

A: 不会。只有在 AI 成功返回响应并且能够统计到 token 使用量时，才会扣减配额。

### Q4: 支持哪些 AI 模型？

A: 插件不限制模型类型，只要请求体中包含 `model` 字段即可。常见的包括：
- OpenAI: gpt-4, gpt-4-turbo, gpt-3.5-turbo
- Anthropic: claude-3-opus, claude-3-sonnet
- 其他兼容 OpenAI API 格式的模型

### Q5: 如何查看所有用户的配额？

A: 需要直接查询 Redis，使用命令：

```bash
redis-cli KEYS "ai_model_quota:*"
redis-cli GET "ai_model_quota:user1:gpt-4"
```

### Q6: 配额可以设置为负数吗？

A: 可以设置为负数（虽然不推荐），但请求时会检查配额是否 > 0，如果 <= 0 则拒绝请求。

## 与 ai-quota 插件的区别

| 特性       | ai-quota               | ai-quota-per-model                         |
|----------|------------------------|-------------------------------------------|
| 配额维度   | consumer               | consumer + model                          |
| Redis Key | `prefix:consumer`      | `prefix:consumer:model`                   |
| 适用场景   | 统一配额，不区分模型           | 需要为不同模型分配不同配额                           |
| 管理接口参数 | consumer, quota/value  | consumer, **model**, quota/value          |

## 开发和编译

### 编译插件

```bash
cd /home/user/higress/plugins/wasm-go
PLUGIN_NAME=ai-quota-per-model make build
```

编译后会在 `extensions/ai-quota-per-model/` 目录下生成 `plugin.wasm` 文件。

### 运行测试

```bash
cd extensions/ai-quota-per-model
go test -v
```

### 本地开发

```bash
# 安装依赖
go mod tidy

# 本地编译
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ./plugin.wasm .
```

## 许可证

Apache License 2.0
