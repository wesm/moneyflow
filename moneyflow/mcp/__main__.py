"""
CLI entry point for the moneyflow MCP server.

Run the server with:
    python -m moneyflow.mcp [--account ACCOUNT_ID] [--transport stdio|streamable-http]

Or use the CLI command:
    moneyflow-mcp [--account ACCOUNT_ID] [--transport stdio|streamable-http]
"""

import argparse
import logging
import sys


def main():
    parser = argparse.ArgumentParser(
        description="Run the moneyflow MCP server for Claude Desktop integration",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    # Run with stdio transport (default, for Claude Desktop)
    python -m moneyflow.mcp

    # Run with specific account
    python -m moneyflow.mcp --account my-monarch-account

    # Run with HTTP transport (for remote access via Tailscale)
    python -m moneyflow.mcp --transport streamable-http

Security Note:
    This server exposes your financial data to LLM applications.
    Only run on trusted networks (localhost or Tailscale).
""",
    )

    parser.add_argument(
        "--account",
        "-a",
        help="Account ID to use (defaults to last active account)",
    )
    parser.add_argument(
        "--config-dir",
        help="Custom config directory (defaults to ~/.moneyflow)",
    )
    parser.add_argument(
        "--transport",
        "-t",
        choices=["stdio", "streamable-http"],
        default="stdio",
        help="Transport type (default: stdio)",
    )
    parser.add_argument(
        "--verbose",
        "-v",
        action="store_true",
        help="Enable verbose logging",
    )

    args = parser.parse_args()

    # Configure logging
    log_level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(
        level=log_level,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        stream=sys.stderr,  # Log to stderr so it doesn't interfere with stdio transport
    )

    from .server import run_mcp_server

    try:
        run_mcp_server(
            account_id=args.account,
            config_dir=args.config_dir,
            transport=args.transport,
        )
    except KeyboardInterrupt:
        sys.exit(0)
    except Exception as e:
        logging.error(f"Server error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
