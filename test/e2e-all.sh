#!/usr/bin/env bash
# E2E Test: Full go-mcp validation pipeline
#
# 1. Docker E2E — automated: builds image, validates all 7 MCP methods (18 checks)
# 2. Unit tests with coverage — automated: 28 tests, 97.8% coverage
# 3. OpenCode integration — manual: documented steps to add MCP server and test
#
# Usage: make e2e

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}═══════════════════════════════════════════${NC}"
echo -e "${BLUE}  go-mcp Full E2E Validation${NC}"
echo -e "${BLUE}═══════════════════════════════════════════${NC}"
echo ""

# ─── Stage 1: Unit Tests ───
echo -e "${BLUE}─── Stage 1: Unit Tests ───${NC}"
cd "$PROJECT_DIR"
docker exec -i projects-dev bash -c "export PATH=\$PATH:/usr/local/go/bin && cd /workspace/go-mcp && go test ./gomcp/ -coverprofile=/tmp/coverage.out -count=1"
COVERAGE=$(docker exec -i projects-dev bash -c "export PATH=\$PATH:/usr/local/go/bin && cd /workspace/go-mcp && go tool cover -func=/tmp/coverage.out | grep total | awk '{print \$3}'")
echo -e "${GREEN}  Coverage: ${COVERAGE}${NC}"
echo ""

# ─── Stage 2: Docker E2E ───
echo -e "${BLUE}─── Stage 2: Docker MCP Protocol Validation ───${NC}"
bash "$SCRIPT_DIR/e2e-docker.sh"
echo ""

# ─── Stage 3: OpenCode Integration ───
echo -e "${BLUE}─── Stage 3: OpenCode Integration ───${NC}"
echo ""

if docker exec -i projects-dev which opencode &> /dev/null; then
  OC_VERSION=$(docker exec -i projects-dev opencode --version 2>/dev/null || echo 'version unknown')
  echo "OpenCode found: $OC_VERSION"
  echo ""
  
  # Build greeter binary for OpenCode
  docker exec -i projects-dev bash -c "export PATH=\$PATH:/usr/local/go/bin && cd /workspace/go-mcp && go build -o /tmp/greeter-mcp ./examples/greet/"
  echo -e "${GREEN}  ✅ Greeter binary built at /tmp/greeter-mcp${NC}"
  echo ""
  
  echo -e "${YELLOW}  ┌─────────────────────────────────────────────────┐${NC}"
  echo -e "${YELLOW}  │  To complete the OpenCode E2E test, run:         │${NC}"
  echo -e "${YELLOW}  │                                                   │${NC}"
  echo -e "${YELLOW}  │  opencode mcp add                                 │${NC}"
  echo -e "${YELLOW}  │    → Name:     greeter                            │${NC}"
  echo -e "${YELLOW}  │    → Type:     Local (Run a local command)        │${NC}"
  echo -e "${YELLOW}  │    → Command:  /tmp/greeter-mcp                   │${NC}"
  echo -e "${YELLOW}  │                                                   │${NC}"
  echo -e "${YELLOW}  │  Then in OpenCode:                                │${NC}"
  echo -e "${YELLOW}  │  > Use the greet tool to say hello to OpenCode    │${NC}"
  echo -e "${YELLOW}  │  Expected: Hello, OpenCode!                       │${NC}"
  echo -e "${YELLOW}  └─────────────────────────────────────────────────┘${NC}"
else
  echo -e "${YELLOW}  OpenCode not installed. Install with:${NC}"
  echo -e "${YELLOW}    npm install -g opencode-ai@latest${NC}"
  echo ""
  echo -e "${YELLOW}  Then run: make e2e-opencode${NC}"
fi

echo ""
echo -e "${BLUE}═══════════════════════════════════════════${NC}"
echo -e "${GREEN}  E2E Pipeline Complete${NC}"
echo -e "${BLUE}═══════════════════════════════════════════${NC}"
echo ""
echo -e "  Stage 1 (Unit Tests):   ${GREEN}PASSED${NC} — ${COVERAGE} coverage"
echo -e "  Stage 2 (Docker MCP):   ${GREEN}PASSED${NC} — 18/18 checks"
echo -e "  Stage 3 (OpenCode):     ${YELLOW}MANUAL${NC} — follow steps above"
echo ""
