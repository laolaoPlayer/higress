#!/bin/bash

echo "=== 查找 key-auth WasmPlugin 配置 ==="
kubectl get wasmplugin key-auth -n higress-system -o yaml 2>/dev/null || \
kubectl get wasmplugin -A -o yaml 2>/dev/null | grep -A 50 "name: key-auth" || \
echo "无法访问 Kubernetes 集群"

echo ""
echo "=== 查找包含 AI 路由配置的 Ingress ==="
kubectl get ingress -A -o yaml 2>/dev/null | grep -B 5 -A 20 "ai.*route\|authConfig" || \
echo "未找到 AI 路由相关的 Ingress 配置"

echo ""
echo "=== 解决方案 ==="
echo "1. 如果你使用 Higress Console 管理配置："
echo "   - 登录 Higress Console"
echo "   - 进入 AI/Route 页面"
echo "   - 编辑对应的 AI 路由"
echo "   - 在'允许的消费者'部分添加新 consumer 的名称"
echo "   - 保存配置"
echo ""
echo "2. 如果你使用 kubectl 管理配置："
echo "   - 找到 key-auth WasmPlugin: kubectl get wasmplugin key-auth -n higress-system -o yaml"
echo "   - 在 matchRules 中为对应的 AI 路由 ingress 添加新 consumer 到 allow 列表"
echo "   - 示例："
cat << 'EOF'
  matchRules:
    - config:
        allow:
          - consumer1
          - consumer2
          - 你的新consumer名称    # <-- 添加这一行
      ingress:
        - namespace/ai-route-ingress-name
EOF
echo ""
echo "   - 应用配置: kubectl apply -f updated-wasmplugin.yaml"
