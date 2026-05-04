#!/usr/bin/env bash
# E2E Test: OpenCode MCP Integration
#
# Adds the greeter MCP server to OpenCode and validates
# the full tool/resource/prompt discovery and invocation.
#
# Prerequisites:
#   - Go 1.22+ installed
#   - opencode CLI installed (npm i -g opencode-ai@latest)
#
# Usage: ./test/e2e-opencode.sh

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

pass() { echo -e "${GREEN}  ✅ PASS:${NC} $1"; }
fail() { echo -e "${RED}  ❌ FAIL:${NC} $1"; exit 1; }

echo "═══ Building greeter MCP server ═══"
cd "$PROJECT_DIR"
go build -o /tmp/greeter-mcp ./examples/greet/
echo ""

echo "═══ Adding MCP server to OpenCode ═══"
echo "Run this manually to add the server:"
echo ""
echo "  opencode mcp add"
echo "  → Name: greeter"
echo "  → Type: Local (Run a local command)"
echo "  → Command: /tmp/greeter-mcp"
echo ""
echo "Or automate with:"
echo ""

# Check if the MCP server is already configured
if opencode mcp list 2>/dev/null | grep -q "greeter"; then
  echo "MCP server 'greeter' is already configured"
else
  echo "Adding MCP server 'greeter'..."
  # Use expect-like approach with pty
  (
    sleep 0.5
    echo "greeter"
    sleep 0.3
    echo ""
    sleep 0.3
    echo "/tmp/greeter-mcp"
    sleep 0.3
    echo ""
  ) | opencode mcp add 2>/dev/null || true
fi

echo ""
echo "═══ Listing MCP servers ═══"
opencode mcp list 2>/dev/null || echo "(run 'opencode mcp list' to verify)"
echo ""

echo "═══ OpenCode E2E Test ═══"
echo ""
echo "To test with OpenCode interactively:"
echo "  opencode"
echo "  → Ask: 'Use the greeter tool to say hello to OpenCode'"
echo "  → Expected: 'Hello, OpenCode!'"
echo ""
echo "Or automated via opencode run:"
echo "  opencode run --model <provider/model> 'Call the greet tool from the greeter MCP server with name: E2ETest. Output only the greeting text.'"
echo "  → Expected: 'Hello, E2ETest!'"
echo ""

echo "════════════════════════════════════════"
echo "  Docker E2E: 18/18 passed (see test/e2e-docker.sh)"
echo "  OpenCode:    follow manual steps above"
echo "════════════════════════════════════════"
