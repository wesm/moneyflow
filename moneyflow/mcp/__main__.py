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

    # Run in read-only mode (no modifications allowed)
    python -m moneyflow.mcp --read-only

    # Run with HTTP transport (for remote access via Tailscale)
    python -m moneyflow.mcp --transport streamable-http

Security Note:
    This server exposes your financial data to LLM applications.
    Only run on trusted networks (localhost or Tailscale).

    For HTTP transport, ensure you're using a secure network like Tailscale.
    There is NO built-in authentication - anyone who can reach the server
    can access your financial data.

Environment Variables:
    MONEYFLOW_PASSWORD  Password for encrypted credentials (if account uses
                        password protection). Required for encrypted accounts
                        since MCP server cannot prompt interactively.
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
        "--read-only",
        "-r",
        action="store_true",
        help="Run in read-only mode (disable all write operations)",
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

    # Security warning for HTTP transport
    if args.transport == "streamable-http":
        print(
            "=" * 70,
            "SECURITY WARNING: HTTP Transport",
            "=" * 70,
            "",
            "You are running the MCP server with HTTP transport.",
            "This server has NO built-in authentication.",
            "",
            "Anyone who can reach this server can:",
            "  - Read all your financial transactions",
            "  - View spending summaries and account details",
            "  - Modify transaction categories (unless --read-only is set)",
            "",
            "Only use HTTP transport on secure networks like Tailscale.",
            "For local Claude Desktop use, prefer stdio transport (default).",
            "",
            "=" * 70,
            sep="\n",
            file=sys.stderr,
        )

    from .server import run_mcp_server

    try:
        run_mcp_server(
            account_id=args.account,
            config_dir=args.config_dir,
            transport=args.transport,
            read_only=args.read_only,
        )
    except KeyboardInterrupt:
        sys.exit(0)
    except Exception as e:
        logging.error(f"Server error: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
