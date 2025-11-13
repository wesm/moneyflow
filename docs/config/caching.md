# Caching

Speed up startup by caching transaction data locally.

## Quick Start

Enable caching to load transactions from disk instead of fetching from API:

```bash
moneyflow --cache
```

First run fetches and saves data. Subsequent runs load from cache in milliseconds.

## How It Works

Cache files are stored per-account:

```
~/.moneyflow/profiles/{account-id}/cache/
├── transactions.parquet  # Transaction data
├── categories.json       # Categories
└── metadata.json         # Cache metadata
```

The cache automatically invalidates when you use different filters (`--year`, `--since`).

## Refresh Cache

Force a fresh fetch and rebuild:

```bash
moneyflow --cache --refresh-cache
```

## Encrypted Cache

### Overview

When you provide an encryption password, the cache is automatically encrypted:

```bash
moneyflow --cache
# Enter encryption password: ********
```

**Without encryption:**
- Cache files are plaintext Parquet/JSON
- Anyone with filesystem access can read your transaction data

**With encryption:**
- All cache files encrypted with AES-256-GCM
- Only accessible with your password

### How Encryption Works

moneyflow uses PyArrow's native Parquet encryption:

1. **Key Derivation:** Your password → PBKDF2-HMAC-SHA256 (100k iterations) → 256-bit master key
2. **Parquet Encryption:** PyArrow generates random Data Encryption Keys (DEKs) and encrypts data with AES-256-GCM
3. **Key Wrapping:** Master key wraps the DEKs, which are stored in Parquet metadata
4. **JSON Encryption:** Metadata and categories files encrypted separately with AES-256-GCM

A separate salt file (`cache_salt`) is created for key derivation.

### Why SimpleKmsClient?

PyArrow's `InMemoryKmsClient` has a hardcoded 16-byte master key limit. Our PBKDF2-derived keys are 32 bytes (256 bits), so we implement a custom `SimpleKmsClient` that:

- Uses the same wrapping algorithm (concatenate + base64)
- Handles arbitrary-length master keys
- Works with our 32-byte PBKDF2 keys

The master key only protects the DEKs in metadata. The actual data is encrypted by PyArrow using proper AES-256-GCM with the DEKs.

### Migration

If you have an unencrypted cache and start using a password, moneyflow automatically migrates:

```bash
moneyflow --cache
# Enter encryption password: ********
# Decryption failed, attempting migration from unencrypted cache...
# Migration: Re-saving cache with encryption...
# Migration complete: Cache is now encrypted
```

This is a one-time automatic upgrade.

### Wrong Password

Entering the wrong password fails gracefully - moneyflow fetches fresh data and re-caches with the correct password.

## Cache with Filters

Combine caching with filters:

```bash
# Cache only 2025 transactions
moneyflow --cache --year 2025

# Cache since specific date
moneyflow --cache --since 2024-06-01
```

Changing filters invalidates the cache and triggers a fresh fetch.
