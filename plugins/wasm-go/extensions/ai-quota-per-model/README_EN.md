---
title: AI Quota Management Per Model
keywords: [ AI Gateway, AI Quota, Model Quota ]
description: AI Quota Management Per Model Plugin Configuration Reference
---

## Overview

The `ai-quota-per-model` plugin enables quota management for specific consumers based on AI models, allowing the same user to have different token quotas for different models. It also supports quota management capabilities including querying, refreshing, and adjusting quotas.

**Key Features:**
- ✅ **Independent Quota Per Model**: Each consumer can have different token quotas for different models (e.g., gpt-4, gpt-3.5-turbo)
- ✅ **Automatic Deduction**: Automatically deducts quota based on actual token usage (input + output tokens)
- ✅ **Quota Management API**: Query, refresh, and adjust quotas for any consumer and model
- ✅ **Flexible Management**: Dynamically manage quotas via REST API without service restart

**Plugin Dependencies:**
- Requires authentication plugin (e.g., `key-auth`, `jwt-auth`) to obtain consumer identity
- Requires `ai-statistics` plugin to collect AI token usage statistics
- Requires Redis service to store quota data

## Execution Attributes

Plugin execution phase: `Default Phase`
Plugin execution priority: `280`

## Configuration

| Name               | Type   | Required | Default         | Description                        |
|--------------------|--------|----------|-----------------|-----------------------------------|
| `redis_key_prefix` | string | No       | ai_model_quota: | Redis key prefix for quota storage |
| `admin_consumer`   | string | Yes      | -               | Consumer name for quota management |
| `admin_path`       | string | No       | /quota          | Path prefix for quota management   |
| `redis`            | object | Yes      | -               | Redis configuration               |

### `redis` Configuration Fields

| Field        | Type   | Required | Default                                         | Description                                                                          |
|--------------|--------|----------|-------------------------------------------------|--------------------------------------------------------------------------------------|
| service_name | string | Yes      | -                                               | Redis service name (FQDN), e.g., my-redis.dns, redis.my-ns.svc.cluster.local        |
| service_port | int    | No       | 80 for static service, 6379 for others          | Redis service port                                                                    |
| username     | string | No       | -                                               | Redis username                                                                        |
| password     | string | No       | -                                               | Redis password                                                                        |
| timeout      | int    | No       | 1000                                            | Redis connection timeout in milliseconds                                              |
| database     | int    | No       | 0                                               | Database ID, e.g., 1 corresponds to `SELECT 1`                                        |

## Configuration Examples

### Basic Configuration

```yaml
redis_key_prefix: "ai_model_quota:"
admin_consumer: admin
admin_path: /quota
redis:
  service_name: redis-service.default.svc.cluster.local
  service_port: 6379
  timeout: 2000
```

### Complete WasmPlugin Configuration

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

## Usage Guide

### Prerequisites

1. **Deploy Redis Service**
2. **Configure Authentication Plugin** (e.g., key-auth):

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

3. **Configure ai-statistics Plugin** for token usage tracking

### Quota Management

The plugin assumes it is applied to the `/v1/chat/completions` route.

#### 1. Refresh Quota (Set to Specific Value)

Set gpt-4 quota to 10000 tokens for user1:

```bash
curl https://your-gateway.com/v1/chat/completions/quota/refresh \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-4&quota=10000"
```

**Response:**
```json
{
  "consumer": "user1",
  "model": "gpt-4",
  "quota": 10000,
  "status": "refreshed"
}
```

Set gpt-3.5-turbo quota to 50000 tokens for the same user1:

```bash
curl https://your-gateway.com/v1/chat/completions/quota/refresh \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-3.5-turbo&quota=50000"
```

**Redis Storage Structure:**
```
ai_model_quota:user1:gpt-4          -> 10000
ai_model_quota:user1:gpt-3.5-turbo  -> 50000
```

#### 2. Query Quota

Query remaining gpt-4 quota for user1:

```bash
curl "https://your-gateway.com/v1/chat/completions/quota?consumer=user1&model=gpt-4" \
  -H "x-api-key: apikey-admin-secret"
```

**Response:**
```json
{
  "consumer": "user1",
  "model": "gpt-4",
  "quota": 9500
}
```

Query remaining gpt-3.5-turbo quota for user1:

```bash
curl "https://your-gateway.com/v1/chat/completions/quota?consumer=user1&model=gpt-3.5-turbo" \
  -H "x-api-key: apikey-admin-secret"
```

#### 3. Adjust Quota (Delta)

Increase gpt-4 quota by 1000 tokens for user1:

```bash
curl https://your-gateway.com/v1/chat/completions/quota/delta \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-4&value=1000"
```

**Response:**
```json
{
  "consumer": "user1",
  "model": "gpt-4",
  "delta": 1000,
  "new_quota": 10500
}
```

Decrease gpt-4 quota by 500 tokens (use negative value):

```bash
curl https://your-gateway.com/v1/chat/completions/quota/delta \
  -H "x-api-key: apikey-admin-secret" \
  -d "consumer=user1&model=gpt-4&value=-500"
```

### Using the API

When users make API calls, the plugin automatically:
1. Extracts the `model` field from the request body
2. Checks if the consumer has sufficient quota for that model
3. Deducts quota based on actual token usage after successful response

#### Example: Using gpt-4

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

**Processing Flow:**
1. Auth plugin identifies consumer as `user1`
2. ai-quota-per-model plugin extracts model as `gpt-4`
3. Checks if Redis key `ai_model_quota:user1:gpt-4` value > 0
4. If quota is sufficient, allows request to proceed
5. After AI response, counts token usage (e.g., 500 tokens)
6. Deducts 500 from Redis key `ai_model_quota:user1:gpt-4`

#### Example: Using gpt-3.5-turbo

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

**Processing flow is similar but uses different Redis key:**
- Checks and deducts from `ai_model_quota:user1:gpt-3.5-turbo`

#### Insufficient Quota Response

When quota is insufficient, returns 403 error:

```
HTTP/1.1 403 Forbidden
Content-Type: text/plain

Request denied by ai quota check. No quota left for model 'gpt-4'
```

## Redis Data Structure

The plugin uses Redis keys in this format:

```
{redis_key_prefix}{consumer}:{model}
```

**Example Data:**

```
ai_model_quota:user1:gpt-4              -> 10000
ai_model_quota:user1:gpt-3.5-turbo      -> 50000
ai_model_quota:user1:claude-3-opus      -> 5000
ai_model_quota:user2:gpt-4              -> 20000
ai_model_quota:user2:gpt-3.5-turbo      -> 100000
```

**Characteristics:**
- Each consumer can have quotas for multiple models
- Quotas for different models are independent
- Requests are rejected if key doesn't exist or value is 0

## Error Codes

| HTTP Status | Error Code              | Description                           |
|-------------|------------------------|---------------------------------------|
| 401         | ai-quota.no_key        | Missing authentication (no consumer)   |
| 403         | ai-quota.unauthorized  | Unauthorized consumer                  |
| 403         | ai-quota.noquota       | Insufficient quota                     |
| 400         | ai-quota.invalid_request | Missing 'model' field in request body |
| 400         | ai-quota.invalid_params | Invalid management API parameters     |
| 503         | ai-quota.error         | Redis error                            |

## FAQ

### Q1: How to set different quotas for different models for the same user?

A: Call the refresh API separately with different model parameters:

```bash
# Set gpt-4 quota
curl .../quota/refresh -d "consumer=user1&model=gpt-4&quota=10000" -H "x-api-key: admin-key"

# Set gpt-3.5-turbo quota
curl .../quota/refresh -d "consumer=user1&model=gpt-3.5-turbo&quota=50000" -H "x-api-key: admin-key"
```

### Q2: When is quota deducted?

A: Quota is deducted after the AI response is returned, based on actual token usage (input tokens + output tokens).

### Q3: Is quota deducted if the request fails?

A: No. Quota is only deducted when the AI successfully returns a response and token usage can be measured.

### Q4: Which AI models are supported?

A: The plugin doesn't limit model types; any request with a `model` field works. Common models include:
- OpenAI: gpt-4, gpt-4-turbo, gpt-3.5-turbo
- Anthropic: claude-3-opus, claude-3-sonnet
- Other OpenAI API-compatible models

### Q5: How to view all user quotas?

A: Query Redis directly:

```bash
redis-cli KEYS "ai_model_quota:*"
redis-cli GET "ai_model_quota:user1:gpt-4"
```

### Q6: Can quota be negative?

A: You can set it to negative (not recommended), but requests check if quota > 0; requests are rejected if <= 0.

## Differences from ai-quota Plugin

| Feature        | ai-quota              | ai-quota-per-model                |
|----------------|-----------------------|-----------------------------------|
| Quota Dimension | consumer              | consumer + model                  |
| Redis Key      | `prefix:consumer`     | `prefix:consumer:model`           |
| Use Case       | Unified quota         | Different quotas per model        |
| API Parameters | consumer, quota/value | consumer, **model**, quota/value  |

## Development and Compilation

### Build Plugin

```bash
cd /home/user/higress/plugins/wasm-go
PLUGIN_NAME=ai-quota-per-model make build
```

This generates `plugin.wasm` in the `extensions/ai-quota-per-model/` directory.

### Run Tests

```bash
cd extensions/ai-quota-per-model
go test -v
```

### Local Development

```bash
# Install dependencies
go mod tidy

# Local build
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ./plugin.wasm .
```

## License

Apache License 2.0
