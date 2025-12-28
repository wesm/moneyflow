"""
MCP (Model Context Protocol) server for moneyflow.

Exposes moneyflow functionality to LLM applications like Claude Desktop.
"""

from .server import create_mcp_server, run_mcp_server

__all__ = ["create_mcp_server", "run_mcp_server"]
