"""
Command-line interface for moneyflow.

Provides Click-based CLI for launching moneyflow with different backends
(Monarch Money, Amazon, Demo) and managing data imports.
"""

from pathlib import Path

import click

from .formatters import ViewPresenter


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
    "--cache",
    is_flag=True,
    help="Enable caching (uses ~/.moneyflow/cache by default)",
)
@click.option("--refresh", is_flag=True, help="Force refresh from API, skip cache even if valid")
@click.option(
    "--demo", is_flag=True, help="Run in demo mode with sample data (no authentication required)"
)
@click.pass_context
def cli(ctx, year, since, mtd, cache, refresh, demo):
    """moneyflow - Terminal UI for personal finance management.

    Run with no arguments to launch the default backend (Monarch Money).
    Use subcommands for other backends (e.g., 'moneyflow amazon').
    """
    # If a subcommand is provided, don't launch default backend
    if ctx.invoked_subcommand is not None:
        return

    # Launch default backend (Monarch Money)
    from moneyflow.app import launch_monarch_mode

    # Convert cache flag to path (None if not enabled, default path if enabled)
    cache_path = "~/.moneyflow/cache" if cache else None

    launch_monarch_mode(
        year=year,
        since=since,
        mtd=mtd,
        cache=cache_path,
        refresh=refresh,
        demo=demo,
    )


@cli.group(invoke_without_command=True)
@click.option(
    "--db-path",
    type=click.Path(),
    default=None,
    help="Path to Amazon SQLite database (default: ~/.moneyflow/amazon.db)",
)
@click.pass_context
def amazon(ctx, db_path):
    """Amazon purchase analysis mode.

    Run 'moneyflow amazon' to launch the UI.
    Use subcommands for import/status operations.
    """
    # Store db_path in context for subcommands
    ctx.ensure_object(dict)
    ctx.obj["db_path"] = db_path

    # If no subcommand, launch the UI
    if ctx.invoked_subcommand is None:
        from moneyflow.app import launch_amazon_mode
        from moneyflow.backends.amazon import AmazonBackend

        backend = AmazonBackend(db_path=db_path)

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
        launch_amazon_mode(db_path=db_path)


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
    from moneyflow.backends.amazon import AmazonBackend
    from moneyflow.importers.amazon_orders_csv import import_amazon_orders

    click.echo(f"Importing Amazon orders from {orders_dir}...")

    try:
        db_path = ctx.obj.get("db_path")
        backend = AmazonBackend(db_path=db_path)
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
    from moneyflow.backends.amazon import AmazonBackend

    db_path = ctx.obj.get("db_path")
    backend = AmazonBackend(db_path=db_path)

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
    """Display current category hierarchy (defaults + custom from config.yaml).

    Shows the effective category structure including:
    - Built-in defaults
    - Custom categories from ~/.moneyflow/config.yaml
    - Category renames and moves

    Default output is YAML format (copy-pastable into config.yaml under 'categories:').
    Use --format=readable for human-readable format with counts.
    """
    from moneyflow.categories import get_effective_category_groups

    try:
        category_groups = get_effective_category_groups(config_dir)

        if format == "yaml":
            # Output as valid YAML (copy-pastable)
            click.echo("# Current category hierarchy")
            click.echo("# Copy sections below into your config.yaml under 'categories:'\n")

            # Output in YAML format
            for group_name in sorted(category_groups.keys()):
                categories_list = category_groups[group_name]
                # Use quotes if group name has special chars
                if " " in group_name or "&" in group_name:
                    click.echo(f'  "{group_name}":')
                else:
                    click.echo(f"  {group_name}:")
                for cat in sorted(categories_list):
                    # Use quotes if category has special chars
                    if " " in cat or "&" in cat:
                        click.echo(f'    - "{cat}"')
                    else:
                        click.echo(f"    - {cat}")
                click.echo()  # Blank line between groups

        else:
            # Readable format with counts
            click.echo("Current Category Hierarchy")
            click.echo("=" * 60)

            # Count total categories
            total_cats = sum(len(cats) for cats in category_groups.values())
            click.echo(f"Total: {len(category_groups)} groups, {total_cats} categories\n")

            # Display each group
            for group_name in sorted(category_groups.keys()):
                categories_list = category_groups[group_name]
                click.echo(f"\n{group_name} ({len(categories_list)} categories):")
                for cat in sorted(categories_list):
                    click.echo(f"  - {cat}")

        # Show config file location
        if config_dir:
            config_path = Path(config_dir) / "config.yaml"
            legacy_path = Path(config_dir) / "categories.yaml"
        else:
            config_path = Path.home() / ".moneyflow" / "config.yaml"
            legacy_path = Path.home() / ".moneyflow" / "categories.yaml"

        click.echo(f"\n# {'=' * 58}")
        if config_path.exists():
            click.echo(f"# Custom config: {config_path}")
        elif legacy_path.exists():
            click.echo(f"# Custom config: {legacy_path} (legacy format)")
        else:
            click.echo(f"# Using built-in defaults (no custom config at {config_path})")

    except Exception as e:
        click.echo(f"Error: {e}", err=True)
        raise click.Abort()


if __name__ == "__main__":
    cli()
