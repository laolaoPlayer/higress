# AI 路由消费者列表同步问题修复指南

## 问题描述

在添加新 consumer 到 `key-auth.internal.yaml` 后：
- ✅ 可以正常使用 API key 调用模型
- ❌ Higress Console 的 AI/Route 界面中，消费者列表没有更新

## 根本原因

Higress 的认证和授权是**分两层配置**的：

### 1. 认证层（Authentication）
配置文件：`key-auth.internal.yaml` 或 WasmPlugin 的 `defaultConfig`

作用：定义哪些 consumer 和 credential 是有效的

```yaml
apiVersion: extensions.higress.io/v1alpha1
kind: WasmPlugin
metadata:
  name: key-auth
  namespace: higress-system
spec:
  defaultConfig:
    consumers:
      - credential: token11111111111111111111
        name: consumer1
      - credential: token22222222222222222222
        name: consumer2
      - credential: your-new-token-here       # 你添加的新 consumer
        name: consumer3                        # 新 consumer 名称
    keys:
      - x-api-key
      - apikey
```

### 2. 授权层（Authorization）
配置位置：WasmPlugin 的 `matchRules` 或 AI 路由配置中的 `authConfig.allowedConsumers`

作用：定义哪些 consumer 被允许访问特定的路由

```yaml
spec:
  matchRules:
    - config:
        allow:
          - consumer1
          - consumer2
          # - consumer3  # ❌ 新 consumer 没有添加到这里！
      ingress:
        - namespace/ai-route-ingress-name
```

**Higress Console 界面显示的是第二层（授权层）的配置，而不是第一层（认证层）！**

## 解决方案

### 方案 1：通过 Higress Console 界面（推荐）

1. 登录 Higress Console
2. 导航到 **AI / Route** 页面
3. 找到并点击编辑对应的 AI 路由
4. 找到 **"认证配置"** 或 **"允许的消费者"** 部分
5. 在消费者列表中添加新 consumer 的名称（例如：`consumer3`）
6. 点击 **保存** 按钮

### 方案 2：通过 kubectl 命令行

#### 步骤 1：导出当前配置

```bash
# 导出 key-auth WasmPlugin 配置
kubectl get wasmplugin key-auth -n higress-system -o yaml > key-auth-config.yaml
```

#### 步骤 2：编辑配置文件

在 `key-auth-config.yaml` 中，找到对应 AI 路由的 `matchRules`，添加新 consumer：

```yaml
spec:
  defaultConfig:
    consumers:
      - credential: token11111111111111111111
        name: consumer1
      - credential: token22222222222222222222
        name: consumer2
      - credential: your-new-token-here
        name: consumer3                        # ✓ 已在第一层配置

  matchRules:
    - config:
        allow:
          - consumer1
          - consumer2
          - consumer3                          # ✓ 添加到第二层配置
      ingress:
        - namespace/ai-route-ingress-name      # 替换为实际的 namespace/ingress 名称
```

#### 步骤 3：应用配置

```bash
kubectl apply -f key-auth-config.yaml
```

#### 步骤 4：验证配置

```bash
# 查看配置是否更新成功
kubectl get wasmplugin key-auth -n higress-system -o yaml | grep -A 10 "matchRules"

# 刷新 Higress Console 页面，检查消费者列表是否更新
```

### 方案 3：通过 Higress API（如果你使用 Higress Console 的 REST API）

#### 步骤 1：获取当前 AI 路由配置

```bash
# 假设 AI 路由名称为 "my-ai-route"
curl -X GET "http://your-console-url/v1/ai/routes/my-ai-route" \
  -H "Authorization: Bearer your-token"
```

#### 步骤 2：更新 AI 路由配置

```bash
curl -X PUT "http://your-console-url/v1/ai/routes/my-ai-route" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{
    "name": "my-ai-route",
    "authConfig": {
      "enabled": true,
      "allowedConsumers": [
        "consumer1",
        "consumer2",
        "consumer3"     // 添加新 consumer
      ]
    },
    // ... 其他配置保持不变
  }'
```

## 配置检查清单

在修改配置前，请确认：

- [ ] 新 consumer 已添加到 `key-auth.internal.yaml` 或 WasmPlugin 的 `defaultConfig.consumers`
- [ ] 新 consumer 的名称和 credential 正确无误
- [ ] 找到了对应 AI 路由的 Ingress 名称和 namespace
- [ ] 在 `matchRules` 或 `authConfig.allowedConsumers` 中添加了新 consumer 名称
- [ ] 应用配置后，等待几秒钟让配置生效
- [ ] 刷新 Higress Console 页面查看更新

## 验证方法

### 1. 通过 Console 界面验证
- 刷新 Higress Console 的 AI/Route 页面
- 检查消费者列表是否包含新 consumer

### 2. 通过 API 测试验证

```bash
# 使用新 consumer 的 credential 测试
curl -X POST "http://your-ai-gateway/v1/chat/completions" \
  -H "x-api-key: your-new-token-here" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

预期结果：
- ✅ 返回正常的模型响应（说明认证和授权都通过了）
- ❌ 返回 403 错误（说明认证通过但授权失败，需要检查 allowedConsumers 配置）
- ❌ 返回 401 错误（说明认证失败，需要检查 key-auth.internal.yaml 配置）

## 常见问题

### Q1: 我修改了配置，但 Console 界面还是没有更新？
A: 请检查：
1. 配置是否真的应用成功（kubectl get wasmplugin key-auth -n higress-system -o yaml）
2. 浏览器是否有缓存（强制刷新：Ctrl+F5 或 Cmd+Shift+R）
3. 是否修改了正确的 AI 路由（可能有多个 AI 路由）

### Q2: 如何找到 AI 路由对应的 Ingress 名称？
A: 执行以下命令：
```bash
# 列出所有 Ingress
kubectl get ingress -A

# 查看 Ingress 详情
kubectl describe ingress <ingress-name> -n <namespace>
```

### Q3: 是否可以让 allowedConsumers 自动从 key-auth.internal.yaml 同步？
A: 当前版本不支持自动同步。这是设计如此，因为：
- 认证层（key-auth）定义**谁可以登录**
- 授权层（allowedConsumers）定义**谁可以访问特定资源**
- 这种分离提供了更细粒度的访问控制

## 参考文档

- [Key-Auth 插件文档](../plugins/wasm-go/extensions/key-auth/README_EN.md)
- [Higress Console PR #584](https://github.com/higress-group/higress-console/pull/584) - 修复 allowedConsumers 相关 bug
- [Higress Console PR #573](https://github.com/higress-group/higress-console/pull/573) - 认证模块重构

## 总结

记住这个关键点：

> **在 Higress 中添加新 consumer 需要两步：**
> 1. 在 key-auth 配置中添加 consumer（认证层）
> 2. 在 AI 路由配置中添加 consumer 到 allowedConsumers（授权层）

只完成第一步会导致：
- ✅ API 调用成功（因为认证通过）
- ❌ Console 界面不显示（因为未添加到授权层）
