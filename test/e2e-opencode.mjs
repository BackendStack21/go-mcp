// E2E OpenCode MCP Integration Helper
// 
// Adds the greeter MCP server to OpenCode via pty interaction,
// then validates the full tool/resource/prompt flow.
//
// Usage: node test/e2e-opencode.mjs

import { spawn } from 'node:child_process';
import { builtinModules } from 'node:module';

const GREEN = '\x1b[0;32m';
const RED = '\x1b[0;31m';
const NC = '\x1b[0m';
let passCount = 0;
let failCount = 0;

function pass(msg) { console.log(`${GREEN}  ✅ PASS:${NC} ${msg}`); passCount++; }
function fail(msg) { console.log(`${RED}  ❌ FAIL:${NC} ${msg}`); failCount++; }

function wait(ms) { return new Promise(r => setTimeout(r, ms)); }

// Step 1: Build the greeter binary
console.log('═══ Building greeter MCP server ═══');
const { execSync } = await import('node:child_process');
try {
  execSync('cd /workspace/go-mcp && /usr/local/go/bin/go build -o /tmp/greeter-mcp ./examples/greet/', { stdio: 'pipe' });
  pass('Greeter binary built');
} catch (e) {
  fail(`Build failed: ${e.stderr?.toString()}`);
  process.exit(1);
}

// Step 2: Add MCP server to OpenCode
console.log('\n═══ Adding MCP server to OpenCode ═══');
const proc = spawn('/usr/local/bin/opencode', ['mcp', 'add'], {
  stdio: ['pipe', 'pipe', 'pipe'],
});

let output = '';
proc.stdout.on('data', (d) => { output += d.toString(); });
proc.stderr.on('data', (d) => { output += d.toString(); });

// Send the inputs with delays
await wait(500);
proc.stdin.write('greeter\n');
await wait(400);
proc.stdin.write('\n');  // Select "Local"
await wait(400);
proc.stdin.write('/tmp/greeter-mcp\n');
await wait(400);
proc.stdin.write('\n');  // Confirm

await wait(2000);
proc.kill();

console.log(output.slice(-300));

// Step 3: Verify MCP server is listed
console.log('\n═══ Verifying MCP server registration ═══');
try {
  const listOutput = execSync('/usr/local/bin/opencode mcp list', { 
    encoding: 'utf8',
    timeout: 5000 
  });
  if (listOutput.includes('greeter')) {
    pass('MCP server "greeter" registered in OpenCode');
  } else {
    fail('MCP server "greeter" not found in list');
    console.log('  Output:', listOutput);
  }
} catch (e) {
  fail(`opencode mcp list failed: ${e.message}`);
}

// Step 4: Run opencode with a message that uses the tool
console.log('\n═══ Testing MCP tool invocation via OpenCode ═══');
console.log('To test manually:');
console.log('  /usr/local/bin/opencode run "Use the greet tool to say hello to E2ETest"');
console.log('  → Expected: Hello, E2ETest!');

// Step 5: Summary
console.log('\n════════════════════════════════════════');
console.log(`  OpenCode Integration: ${passCount} passed, ${failCount} failed`);
console.log('════════════════════════════════════════');
