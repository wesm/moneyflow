"""
Command-line interface for moneyflow.

Provides Click-based CLI for launching moneyflow with different backends
(Monarch Money, Amazon, Demo) and managing data imports.
"""

from pathlib import Path

import click

from .tui.formatters import ViewPresenter


def _get_amazon_backend_with_profile_support(db_path=None, config_dir=None):
    """
    Helper to create an AmazonBackend with profile-aware database path resolution.

    Priority:
    1. Explicit --db-path (if provided)
    2. Migrated profile path (if amazon account exists in profiles)
    3. Legacy location (~/.moneyflow/amazon.db as fallback)

    Args:
        db_path: Optional explicit database path
        config_dir: Optional config directory

    Returns:
        tuple: (backend, config_dir, profile_dir)
    """
    from pathlib import Path

    from moneyflow.backends.amazon import AmazonBackend
    from moneyflow.data.account_manager import AccountManager

    # Ensure config_dir has a value
    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    # Determine the correct db_path
    # Priority: 1) explicit --db-path, 2) migrated profile, 3) legacy location
    amazon_profile_dir = None
    if db_path is None:
        # Check if Amazon account exists in profiles
        config_path = Path(config_dir)
        account_manager = AccountManager(config_dir=config_path)
        accounts = account_manager.list_accounts()

        # Look for an amazon account
        amazon_account = None
        for account in accounts:
            if account.backend_type == "amazon":
                amazon_account = account
                break

        if amazon_account:
            # Use migrated profile path
            amazon_profile_dir = account_manager.get_profile_dir(amazon_account.id)
            db_path = str(amazon_profile_dir / "amazon.db")
        # else: db_path stays None, AmazonBackend will use default

    backend = AmazonBackend(db_path=db_path, config_dir=config_dir, profile_dir=amazon_profile_dir)

    return backend, config_dir, amazon_profile_dir


@click.group(invoke_without_command=True)
@click.option(
    "--year",
    type=int,
    metavar="YYYY",
    help="Only load transactions from this year onwards (e.g., --year 2025)",
)
@click.option(
    "--since",
    type=str,
    metavar="YYYY-MM-DD",
    help="Only load transactions from this date onwards (overrides --year)",
)
@click.option(
    "--mtd", is_flag=True, help="Load month-to-date transactions (from 1st of current month)"
)
@click.option(
    "--no-cache",
    is_flag=True,
    help="Disable encrypted caching (caching is enabled by default)",
)
@click.option("--refresh", is_flag=True, help="Force refresh from API, skip cache even if valid")
@click.option(
    "--demo", is_flag=True, help="Run in demo mode with sample data (no authentication required)"
)
@click.option(
    "--config-dir",
    type=click.Path(),
    default=None,
    help="Config directory (default: ~/.moneyflow). Useful for testing with isolated configs.",
)
@click.option(
    "--theme",
    type=click.Choice(
        ["default", "berg", "nord", "gruvbox", "dracula", "solarized-dark", "monokai"]
    ),
    default=None,
    help="Override theme for this session",
)
@click.pass_context
def cli(ctx, year, since, mtd, no_cache, refresh, demo, config_dir, theme):
    """moneyflow - Terminal UI for personal finance management.

    Run with no arguments to launch the default backend (Monarch Money).
    Use subcommands for other backends (e.g., 'moneyflow amazon').

    Caching is now ENABLED BY DEFAULT with encrypted cache files.
    Use --no-cache to disable caching.
    """
    # If a subcommand is provided, don't launch default backend
    if ctx.invoked_subcommand is not None:
        return

    # Launch default backend (Monarch Money)
    from moneyflow.tui.app import launch_monarch_mode

    # Convert no-cache flag to cache path
    # Caching is enabled by default (unless --no-cache is passed)
    if no_cache:
        cache_path = None
    else:
        # Enable caching with default location
        # Use empty string to trigger profile-specific cache directory logic in app.py
        # If config_dir is specified, use that; otherwise empty string for default behavior
        cache_path = f"{config_dir}/cache" if config_dir else ""

    launch_monarch_mode(
        year=year,
        since=since,
        mtd=mtd,
        cache=cache_path,
        refresh=refresh,
        demo=demo,
        config_dir=config_dir,
        theme=theme,
    )


@cli.group(invoke_without_command=True)
@click.option(
    "--db-path",
    type=click.Path(),
    default=None,
    help="Path to Amazon SQLite database (default: ~/.moneyflow/amazon.db)",
)
@click.option(
    "--config-dir",
    type=click.Path(),
    default=None,
    help="Config directory (default: ~/.moneyflow). Used for loading categories from config.yaml.",
)
@click.pass_context
def amazon(ctx, db_path, config_dir):
    """Amazon purchase analysis mode.

    Run 'moneyflow amazon' to launch the UI.
    Use subcommands for import/status operations.
    """
    # Store backend and profile config in context for subcommands
    ctx.ensure_object(dict)

    try:
        backend, cfg_dir, prof_dir = _get_amazon_backend_with_profile_support(
            db_path=db_path, config_dir=config_dir
        )
    except (OSError, ValueError, ImportError) as e:
        click.echo(f"Initialization failed: {e}", err=True)
        raise click.Abort()

    ctx.obj["backend"] = backend
    ctx.obj["config_dir"] = cfg_dir
    ctx.obj["profile_dir"] = prof_dir

    # If no subcommand, launch the UI
    if ctx.invoked_subcommand is None:
        from moneyflow.tui.app import launch_amazon_mode

        # Check if database exists
        if not backend.db_path.exists():
            click.echo("No Amazon data found.")
            click.echo("\nPlease import your Amazon purchase data first:")
            click.echo('  $ moneyflow amazon import ~/Downloads/"Your Orders"')
            click.echo("\nFor help:")
            click.echo("  $ moneyflow amazon --help")
            raise click.Abort()

        # Check if database has data
        stats = backend.get_database_stats()
        if stats["total_transactions"] == 0:
            click.echo("Amazon database is empty.")
            click.echo("\nPlease import your Amazon purchase data:")
            click.echo('  $ moneyflow amazon import ~/Downloads/"Your Orders"')
            raise click.Abort()

        # Launch the UI
        launch_amazon_mode(db_path=str(backend.db_path), config_dir=cfg_dir, profile_dir=prof_dir)


@amazon.command(name="import")
@click.pass_context
@click.argument("orders_dir", type=click.Path(exists=True))
@click.option("--force", is_flag=True, help="Force reimport of duplicates (overwrites existing)")
def amazon_import(ctx, orders_dir, force):
    """Import Amazon orders from 'Your Orders' data dump directory.

    Scans directory for Retail.OrderHistory.*.csv files and imports all orders.

    Expected directory: Unzipped 'Your Orders' folder from Amazon data export.
    Contains files like: Retail.OrderHistory.1/Retail.OrderHistory.1.csv

    Example:
        moneyflow amazon import ~/Downloads/"Your Orders"
    """
    from moneyflow.importers.amazon_orders_csv import import_amazon_orders

    click.echo(f"Importing Amazon orders from {orders_dir}...")

    try:
        backend = ctx.obj["backend"]
        stats = import_amazon_orders(orders_dir, backend=backend, force=force)

        click.echo("\n✓ Import complete!")
        click.echo(f"  Imported: {stats['imported']:,} new transactions")

        if stats["duplicates"] > 0:
            click.echo(f"  Duplicates: {stats['duplicates']:,} (already in database)")

        if stats["skipped"] > 0:
            click.echo(f"  Skipped: {stats['skipped']:,} (cancelled/invalid orders)")

        # Show database stats
        db_stats = backend.get_database_stats()
        click.echo("\nDatabase summary:")
        click.echo(f"  Total transactions: {db_stats['total_transactions']:,}")
        click.echo(f"  Date range: {db_stats['earliest_date']} → {db_stats['latest_date']}")
        click.echo(f"  Total amount: {ViewPresenter.format_amount(db_stats['total_amount'])}")
        click.echo(f"  Unique items: {db_stats['item_count']:,}")

        click.echo("\n✓ Ready! Launch moneyflow:")
        click.echo("  $ moneyflow amazon")

    except FileNotFoundError as e:
        click.echo(f"Error: {e}", err=True)
        click.echo("\nMake sure you've unzipped the Amazon data dump first.", err=True)
        raise click.Abort()
    except ValueError as e:
        click.echo(f"Error: {e}", err=True)
        click.echo("\nExpected directory structure:", err=True)
        click.echo("  Your Orders/", err=True)
        click.echo("    Retail.OrderHistory.1/Retail.OrderHistory.1.csv", err=True)
        click.echo("    Retail.OrderHistory.2/Retail.OrderHistory.2.csv", err=True)
        raise click.Abort()
    except Exception as e:
        click.echo(f"Import failed: {e}", err=True)
        raise click.Abort()


@amazon.command(name="status")
@click.pass_context
def amazon_status(ctx):
    """Show Amazon database status and import history."""
    backend = ctx.obj["backend"]

    # Check if database exists
    if not backend.db_path.exists():
        click.echo("No Amazon data found.")
        click.echo("\nTo import data:")
        click.echo("  $ moneyflow amazon import ~/Downloads/amazon-purchases.csv")
        return

    # Show database stats
    db_stats = backend.get_database_stats()

    click.echo("Amazon Purchase Database")
    click.echo(f"\nLocation: {backend.db_path}")
    click.echo("\nStatistics:")
    click.echo(f"  Total transactions: {db_stats['total_transactions']}")
    click.echo(f"  Date range: {db_stats['earliest_date']} to {db_stats['latest_date']}")
    click.echo(f"  Total amount: {ViewPresenter.format_amount(db_stats['total_amount'])}")
    click.echo(f"  Unique items: {db_stats['item_count']}")
    click.echo(f"  Categories: {db_stats['category_count']}")

    # Show import history
    history = backend.get_import_history()

    if history:
        click.echo("\nImport History:")
        for record in history[:5]:  # Show last 5 imports
            click.echo(
                f"  {record['import_date']}: {record['filename']} "
                f"({record['record_count']} imported, "
                f"{record['duplicate_count']} duplicates)"
            )

        if len(history) > 5:
            click.echo(f"  ... and {len(history) - 5} more")


@cli.group()
def categories():
    """Manage category configuration and view category hierarchy."""
    pass


@categories.command(name="dump")
@click.option(
    "--config-dir",
    type=click.Path(),
    default=None,
    help="Config directory (default: ~/.moneyflow)",
)
@click.option(
    "--format",
    type=click.Choice(["yaml", "readable"]),
    default="yaml",
    help="Output format: yaml (copy-pastable) or readable (with counts)",
)
def categories_dump(config_dir, format):
    """Display current category hierarchy.

    Shows categories from config.yaml if available (fetched from backend),
    otherwise shows built-in defaults. This is NOT a merge - it's one or
    the other (priority: config.yaml > defaults).

    Default output is YAML format (copy-pastable into config.yaml under 'categories:').
    Use --format=readable for human-readable format with counts.
    """
    from moneyflow.data.categories import (
        format_categories_readable,
        format_categories_yaml,
        get_effective_category_groups,
    )

    try:
        category_groups = get_effective_category_groups(config_dir)

        if format == "yaml":
            output = format_categories_yaml(category_groups)
        else:
            output = format_categories_readable(category_groups)

        click.echo(output)

        # Show config file location
        if config_dir:
            config_path = Path(config_dir) / "config.yaml"
        else:
            config_path = Path.home() / ".moneyflow" / "config.yaml"

        click.echo(f"\n# {'=' * 58}")
        if config_path.exists():
            click.echo(f"# Custom config: {config_path}")
        else:
            click.echo(f"# Using built-in defaults (no custom config at {config_path})")

    except Exception as e:
        click.echo(f"Error: {e}", err=True)
        raise click.Abort()


@categories.command(name="audit")
@click.option(
    "--config-dir",
    type=click.Path(),
    default=None,
    help="Config directory (default: ~/.moneyflow)",
)
@click.option(
    "--cache-dir",
    type=click.Path(),
    default=None,
    help="Cache directory (default: ~/.moneyflow/cache)",
)
def categories_audit(config_dir, cache_dir):
    """Audit transactions for categories not in config.yaml.

    Compares transaction categories in cached data against the
    category structure in config.yaml to find:
    - Categories that exist in transactions but not in config
    - Potential data quality issues
    - Unmapped or orphaned categories

    Useful for identifying category mismatches after backend changes
    or for validating Amazon mode category mappings.
    """
    from moneyflow.auditor import AuditError, run_category_audit

    try:
        unknown_categories, unused_categories, stats = run_category_audit(config_dir, cache_dir)

        click.echo(f"Loaded {stats['known_categories_count']} categories from config")
        click.echo("Checking cached transaction data...\n")

        # Results
        click.echo("📊 Audit Results\n")
        click.echo(f"Total transactions: {stats['total_transactions']:,}")
        click.echo(f"Unique categories in data: {stats['unique_categories_in_data']}")
        click.echo(f"Known categories in config: {stats['known_categories_in_config']}\n")

        if unknown_categories:
            click.echo(
                f"⚠️  Found {len(unknown_categories)} categories in transactions NOT in config.yaml:\n"
            )
            for cat in sorted(unknown_categories.keys()):
                count = unknown_categories[cat]
                click.echo(f"  • {cat} ({count:,} transactions)")
            click.echo()
        else:
            click.echo("✅ All transaction categories are defined in config.yaml\n")

        if unused_categories:
            click.echo(
                f"ℹ️  Found {len(unused_categories)} categories in config NOT used in transactions:\n"
            )
            for cat in sorted(list(unused_categories)[:10]):  # Show first 10
                click.echo(f"  • {cat}")
            if len(unused_categories) > 10:
                click.echo(f"  ... and {len(unused_categories) - 10} more")
            click.echo()

        # Summary
        if unknown_categories:
            click.echo("💡 Action: Unknown categories may indicate:")
            click.echo("   - New categories added to your backend that haven't synced")
            click.echo("   - Data quality issues")
            click.echo("   - Categories that need to be added to config.yaml")
            click.echo("\n   Restart moneyflow to refresh categories from backend")
        else:
            click.echo("✅ Category audit passed - all categories accounted for!")

    except AuditError as e:
        click.echo(f"❌ {e}")
    except Exception as e:
        click.echo(f"❌ {e}")


if __name__ == "__main__":
    cli()
