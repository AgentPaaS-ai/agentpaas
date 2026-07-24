#!/usr/bin/env python3
"""Mock MCP server for Docker cross-container e2e tests.

Listens on 0.0.0.0:8080, accepts POST JSON-RPC tools/call requests.
Requires X-AgentPaaS-MCP-Capability header with a non-empty 64-hex value.
"""

from http.server import HTTPServer, BaseHTTPRequestHandler
import json
import os
import sys


CAP_HEADER = os.environ.get("MCP_CAP_HEADER", "X-AgentPaaS-MCP-Capability")


def is_64hex(s: str) -> bool:
    """Check if string is exactly 64 hex characters."""
    if len(s) != 64:
        return False
    for c in s:
        if c not in "0123456789abcdefABCDEF":
            return False
    return True


class MockMCPHandler(BaseHTTPRequestHandler):
    """Handles JSON-RPC tools/call POST requests with capability check."""

    def do_POST(self):
        # Read body
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length) if content_length > 0 else b""

        # Capability header check
        cap_value = self.headers.get(CAP_HEADER, "")
        if not cap_value or not is_64hex(cap_value):
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(
                json.dumps({
                    "jsonrpc": "2.0",
                    "error": {"code": -32001, "message": "invalid capability header"},
                    "id": None
                }).encode()
            )
            return

        # Parse JSON-RPC
        try:
            request = json.loads(body)
        except json.JSONDecodeError:
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(
                json.dumps({
                    "jsonrpc": "2.0",
                    "error": {"code": -32700, "message": "Parse error"},
                    "id": None
                }).encode()
            )
            return

        method = request.get("method", "")
        params = request.get("params", {})
        request_id = request.get("id")

        if method != "tools/call":
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(
                json.dumps({
                    "jsonrpc": "2.0",
                    "error": {"code": -32601, "message": f"Method not found: {method}"},
                    "id": request_id
                }).encode()
            )
            return

        tool_name = params.get("name", "")
        arguments = params.get("arguments", {})

        if tool_name == "lookup_feedback":
            result = {
                "jsonrpc": "2.0",
                "result": {
                    "content": [
                        {
                            "type": "text",
                            "text": json.dumps({
                                "marker": "b33-t08-docker-e2e",
                                "items": [
                                    {"id": 1, "feedback": "Great mock service!"},
                                    {"id": 2, "feedback": "Cross-container works"},
                                ],
                                "capability_provided": bool(cap_value),
                            }),
                        }
                    ]
                },
                "id": request_id,
            }
        else:
            result = {
                "jsonrpc": "2.0",
                "error": {"code": -32601, "message": f"Tool not found: {tool_name}"},
                "id": request_id,
            }

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(result).encode())

    def log_message(self, format, *args):
        """Log to stderr for Docker log visibility."""
        print(f"[mock-mcp] {format % args}", file=sys.stderr)


def main():
    port = int(os.environ.get("MCP_PORT", "8080"))
    server = HTTPServer(("0.0.0.0", port), MockMCPHandler)
    print(f"[mock-mcp] listening on 0.0.0.0:{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
