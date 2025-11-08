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
@click.option(
    "--config-dir",
    type=click.Path(),
    default=None,
    help="Config directory (default: ~/.moneyflow). Useful for testing with isolated configs.",
)
@click.pass_context
def cli(ctx, year, since, mtd, cache, refresh, demo, config_dir):
    """moneyflow - Terminal UI for personal finance management.

    Run with no arguments to launch the default backend (Monarch Money).
    Use subcommands for other backends (e.g., 'moneyflow amazon').
    """
    # If a subcommand is provided, don't launch default backend
    if ctx.invoked_subcommand is not None:
        return

    # Launch default backend (Monarch Money)
    from moneyflow.app import launch_monarch_mode

    # Convert cache flag to path (None if not enabled, respect config_dir if enabled)
    if cache:
        cache_path = f"{config_dir}/cache" if config_dir else "~/.moneyflow/cache"
    else:
        cache_path = None

    launch_monarch_mode(
        year=year,
        since=since,
        mtd=mtd,
        cache=cache_path,
        refresh=refresh,
        demo=demo,
        config_dir=config_dir,
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
    # Store db_path and config_dir in context for subcommands
    ctx.ensure_object(dict)
    ctx.obj["db_path"] = db_path
    ctx.obj["config_dir"] = config_dir

    # If no subcommand, launch the UI
    if ctx.invoked_subcommand is None:
        from pathlib import Path

        from moneyflow.account_manager import AccountManager
        from moneyflow.app import launch_amazon_mode
        from moneyflow.backends.amazon import AmazonBackend

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

        backend = AmazonBackend(
            db_path=db_path, config_dir=config_dir, profile_dir=amazon_profile_dir
        )

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
        launch_amazon_mode(db_path=db_path, config_dir=config_dir, profile_dir=amazon_profile_dir)


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
    """Display current category hierarchy.

    Shows categories from config.yaml if available (fetched from backend),
    otherwise shows built-in defaults. This is NOT a merge - it's one or
    the other (priority: config.yaml > defaults).

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
    from pathlib import Path

    import polars as pl

    from .cache_manager import CacheManager
    from .categories import get_effective_category_groups

    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    if cache_dir is None:
        cache_dir = str(Path.home() / ".moneyflow" / "cache")

    # Load category structure from config
    category_groups = get_effective_category_groups(config_dir)

    # Build set of all known categories
    known_categories = set()
    for group_name, categories in category_groups.items():
        known_categories.update(categories)

    click.echo(f"Loaded {len(known_categories)} categories from config")
    click.echo("Checking cached transaction data...\n")

    # Try to load cached data
    cache_manager = CacheManager(cache_dir=cache_dir)
    cached_data = cache_manager.load_cache()

    if not cached_data:
        click.echo("❌ No cached data found.")
        click.echo("\nRun moneyflow with --cache flag first to create cache:")
        click.echo("  $ moneyflow --cache")
        return

    df, _, _, _ = cached_data

    # Find unique categories in transactions
    transaction_categories = set(df["category"].unique().to_list())

    # Find categories in transactions but not in config
    unknown_categories = transaction_categories - known_categories

    # Find categories in config but not in transactions
    unused_categories = known_categories - transaction_categories

    # Results
    click.echo("📊 Audit Results\n")
    click.echo(f"Total transactions: {len(df):,}")
    click.echo(f"Unique categories in data: {len(transaction_categories)}")
    click.echo(f"Known categories in config: {len(known_categories)}\n")

    if unknown_categories:
        click.echo(
            f"⚠️  Found {len(unknown_categories)} categories in transactions NOT in config.yaml:\n"
        )
        for cat in sorted(unknown_categories):
            # Count how many transactions have this category
            count = df.filter(pl.col("category") == cat).shape[0]
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


if __name__ == "__main__":
    cli()
