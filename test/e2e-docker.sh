#!/usr/bin/env bash
# E2E Test: Docker → MCP Protocol → Full Flow Validation
#
# Builds the greeter MCP server as a Docker image, runs it,
# and validates the full MCP protocol: initialize, tools/list,
# tools/call, resources/list, resources/read, prompts/list, prompts/get.
#
# Usage: ./test/e2e-docker.sh

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE="go-mcp-greeter:e2e"

echo "═══ Building greeter Docker image ═══"
docker build -t "$IMAGE" -f "$PROJECT_DIR/Dockerfile" "$PROJECT_DIR"
echo ""

PASS=0
FAIL=0

send_jsonrpc() {
  local method="$1" id="$2" params="$3"
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"method\":\"$method\",\"params\":$params}"
}

assert_contains() {
  local desc="$1" response="$2" expected="$3"
  if echo "$response" | grep -q "$expected"; then
    echo "  ✅ PASS: $desc"
    PASS=$((PASS + 1))
  else
    echo "  ❌ FAIL: $desc"
    echo "     Expected: $expected"
    echo "     Got:      $response"
    FAIL=$((FAIL + 1))
  fi
}

echo "═══ Running greeter MCP server ═══"

# Send all MCP requests via stdin, capture all responses from stdout
RESPONSE=$(
  (
    # 1. Initialize
    send_jsonrpc "initialize" 1 '{"protocolVersion":"2024-11-05","capabilities":{}}'
    # 2. Initialized notification (should be silently consumed)
    echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
    # 3. List tools
    send_jsonrpc "tools/list" 2 '{}'
    # 4. Call the greet tool
    send_jsonrpc "tools/call" 3 '{"name":"greet","arguments":{"name":"OpenCode"}}'
    # 5. List resources
    send_jsonrpc "resources/list" 4 '{}'
    # 6. Read the greeting resource
    send_jsonrpc "resources/read" 5 '{"uri":"greeting://world"}'
    # 7. List prompts
    send_jsonrpc "prompts/list" 6 '{}'
    # 8. Get the polite-greeting prompt
    send_jsonrpc "prompts/get" 7 '{"name":"polite-greeting","arguments":{"name":"OpenCode"}}'
  ) | docker run -i --rm "$IMAGE" 2>/dev/null
)

echo ""
echo "═══ Validating MCP responses ═══"
echo ""

# Extract individual responses by JSON-RPC id
RESP_1=$(echo "$RESPONSE" | grep '"id":1' || echo "")
RESP_2=$(echo "$RESPONSE" | grep '"id":2' || echo "")
RESP_3=$(echo "$RESPONSE" | grep '"id":3' || echo "")
RESP_4=$(echo "$RESPONSE" | grep '"id":4' || echo "")
RESP_5=$(echo "$RESPONSE" | grep '"id":5' || echo "")
RESP_6=$(echo "$RESPONSE" | grep '"id":6' || echo "")
RESP_7=$(echo "$RESPONSE" | grep '"id":7' || echo "")

echo "─── initialize ───"
assert_contains "Protocol version" "$RESP_1" "2024-11-05"
assert_contains "Server name" "$RESP_1" "greeter"
assert_contains "Capabilities include tools" "$RESP_1" "tools"
assert_contains "Capabilities include resources" "$RESP_1" "resources"
assert_contains "Capabilities include prompts" "$RESP_1" "prompts"
echo ""

echo "─── tools/list ───"
assert_contains "Returns greet tool" "$RESP_2" "greet"
assert_contains "Has description" "$RESP_2" "Greet a person by name"
assert_contains "Has input schema" "$RESP_2" "inputSchema"
echo ""

echo "─── tools/call ───"
assert_contains "Returns greeting text" "$RESP_3" "Hello, OpenCode!"
assert_contains "Content type is text" "$RESP_3" "text"
assert_contains "No error field" "$RESP_3" "result"
echo ""

echo "─── resources/list ───"
assert_contains "Returns greeting resource" "$RESP_4" "greeting://world"
assert_contains "Has resource name" "$RESP_4" "World Greeting"
echo ""

echo "─── resources/read ───"
assert_contains "Returns resource content" "$RESP_5" "Hello, World!"
assert_contains "Has uri field" "$RESP_5" "greeting://world"
echo ""

echo "─── prompts/list ───"
assert_contains "Returns polite-greeting prompt" "$RESP_6" "polite-greeting"
echo ""

echo "─── prompts/get ───"
assert_contains "Returns prompt messages" "$RESP_7" "messages"
assert_contains "Message references OpenCode" "$RESP_7" "OpenCode"
echo ""

echo "═══════════════════════════════════════"
echo "  Results: $PASS passed, $FAIL failed"
echo "═══════════════════════════════════════"

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "Full raw response for debugging:"
  echo "$RESPONSE"
  exit 1
fi

echo ""
echo "✅ All MCP protocol validations passed in Docker!"
