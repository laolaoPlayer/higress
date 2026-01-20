#!/bin/bash
#
# 快速添加 consumer 到 AI 路由的授权列表
#
# 使用方法：
#   ./add-consumer-to-ai-route.sh <consumer-name> [ingress-namespace/ingress-name]
#
# 示例：
#   ./add-consumer-to-ai-route.sh consumer3 default/ai-route
#

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查参数
if [ $# -lt 1 ]; then
    echo -e "${RED}错误：缺少参数${NC}"
    echo ""
    echo "使用方法："
    echo "  $0 <consumer-name> [ingress-namespace/ingress-name]"
    echo ""
    echo "示例："
    echo "  $0 consumer3"
    echo "  $0 consumer3 default/ai-route"
    exit 1
fi

CONSUMER_NAME="$1"
INGRESS_REF="${2:-}"

echo -e "${GREEN}=== Higress AI 路由消费者添加工具 ===${NC}"
echo ""
echo "要添加的 Consumer: ${YELLOW}${CONSUMER_NAME}${NC}"

# 检查 kubectl 是否可用
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}错误：kubectl 命令不可用${NC}"
    echo ""
    echo "请选择以下方法之一手动添加 consumer："
    echo ""
    echo "1. 通过 Higress Console 界面："
    echo "   - 登录 Higress Console"
    echo "   - 进入 AI/Route 页面"
    echo "   - 编辑对应的 AI 路由"
    echo "   - 在'允许的消费者'中添加: ${CONSUMER_NAME}"
    echo ""
    echo "2. 参考详细文档："
    echo "   cat AI路由消费者列表同步问题修复指南.md"
    exit 1
fi

# 备份配置
BACKUP_FILE="key-auth-backup-$(date +%Y%m%d-%H%M%S).yaml"
echo -e "${YELLOW}正在导出当前配置...${NC}"

if kubectl get wasmplugin key-auth -n higress-system -o yaml > "$BACKUP_FILE" 2>/dev/null; then
    echo -e "${GREEN}✓ 配置已备份到: ${BACKUP_FILE}${NC}"
else
    echo -e "${RED}错误：无法导出 key-auth WasmPlugin 配置${NC}"
    echo "请检查："
    echo "  1. Kubernetes 集群连接是否正常"
    echo "  2. key-auth WasmPlugin 是否存在于 higress-system namespace"
    exit 1
fi

# 检查 consumer 是否已在 defaultConfig 中
echo ""
echo -e "${YELLOW}检查 consumer 是否已在认证配置中...${NC}"

if grep -q "name: ${CONSUMER_NAME}" "$BACKUP_FILE"; then
    echo -e "${GREEN}✓ Consumer '${CONSUMER_NAME}' 已存在于认证配置中${NC}"
else
    echo -e "${RED}✗ Consumer '${CONSUMER_NAME}' 不在认证配置中！${NC}"
    echo ""
    echo "请先将 consumer 添加到 key-auth.internal.yaml 或 WasmPlugin 的 defaultConfig.consumers"
    echo ""
    echo "示例配置："
    cat << 'EOF'
spec:
  defaultConfig:
    consumers:
      - credential: your-token-here
        name: your-consumer-name
EOF
    exit 1
fi

# 如果指定了 Ingress，添加 matchRule
if [ -n "$INGRESS_REF" ]; then
    echo ""
    echo -e "${YELLOW}正在添加 consumer 到 AI 路由授权列表...${NC}"
    echo "目标 Ingress: ${YELLOW}${INGRESS_REF}${NC}"

    # 创建新配置文件
    NEW_CONFIG="key-auth-updated-$(date +%Y%m%d-%H%M%S).yaml"
    cp "$BACKUP_FILE" "$NEW_CONFIG"

    # 使用 yq 或手动提示编辑
    if command -v yq &> /dev/null; then
        echo "正在使用 yq 更新配置..."
        # 这里需要根据实际配置结构调整 yq 命令
        echo -e "${YELLOW}注意：请手动检查并编辑 ${NEW_CONFIG}${NC}"
    else
        echo -e "${YELLOW}yq 工具未安装，请手动编辑配置文件${NC}"
    fi

    echo ""
    echo "请在 ${NEW_CONFIG} 中找到以下部分并添加 consumer："
    cat << EOF

spec:
  matchRules:
    - config:
        allow:
          - consumer1
          - consumer2
          - ${CONSUMER_NAME}    # <-- 添加这一行
      ingress:
        - ${INGRESS_REF}
EOF

    echo ""
    echo -e "${YELLOW}编辑完成后，执行以下命令应用配置：${NC}"
    echo "  kubectl apply -f ${NEW_CONFIG}"
else
    echo ""
    echo -e "${YELLOW}未指定 Ingress，显示当前的 matchRules：${NC}"
    echo ""

    kubectl get wasmplugin key-auth -n higress-system -o yaml | \
        sed -n '/matchRules:/,/^[^ ]/p' | head -n -1

    echo ""
    echo -e "${GREEN}请选择要更新的 AI 路由对应的 Ingress，然后重新运行：${NC}"
    echo "  $0 ${CONSUMER_NAME} <namespace/ingress-name>"
fi

echo ""
echo -e "${GREEN}=== 提示 ===${NC}"
echo "1. 配置备份位于: ${BACKUP_FILE}"
echo "2. 应用配置后，等待几秒钟让配置生效"
echo "3. 刷新 Higress Console 页面查看更新"
echo "4. 详细文档：cat AI路由消费者列表同步问题修复指南.md"
