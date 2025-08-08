# pgcopy

**pgcopy** is a high-performance CLI tool for efficiently copying data between PostgreSQL databases with identical schemas. It's designed for data-only migrations and provides parallel processing capabilities for optimal performance.

![pgcopy in action](./assets/pgcopy.gif)

> Safety & Overwrite Semantics: pgcopy truncates destination tables and replaces their data. Review the short guide before running: see [Safety & Overwrite Semantics](docs/safety.md).

## When to Use pgcopy

### Use Cases

**pgcopy** is specifically designed for **casual data synchronization** scenarios where you need to copy data between PostgreSQL databases with identical schemas. It's particularly valuable when:

- pg_restore is too slow or doesn't work for your use case
- You don't have superuser permissions but need to handle foreign key constraints
- You're dealing with complex foreign key relationships including circular dependencies
- You need parallel processing for better performance than traditional dump/restore
- You want data-only synchronization without schema changes
- You're working in development/staging environments and need frequent data refreshes

### What pgcopy is NOT

- Incremental sync or CDC (it overwrites destination data)
- Real-time replication (use PostgreSQL replication for production)
- Schema migration tool (schemas must already match)
- Backup tool (have proper backups before using)

### Why Choose pgcopy Over Alternatives

#### vs. pg_dump/pg_restore

- Performance: Parallel processing often outperforms serial dump/restore operations
- Foreign Key Handling: Automatically manages complex FK constraints without manual intervention
- Flexibility: Table-level filtering capabilities
- Progress Tracking: Real-time progress monitoring with detailed logging

#### vs. ETL Tools

- Simplicity: No complex configuration or learning curve
- PostgreSQL-Optimized: Built specifically for PostgreSQL-to-PostgreSQL transfers
- Constraint-Aware: Understands and preserves PostgreSQL foreign key relationships

#### vs. Manual SQL Scripts

- Automated FK Management: No need to manually drop/recreate constraints
- Error Recovery: Built-in error handling and constraint restoration
- Parallel Execution: Concurrent table processing for better performance

## Quick Start

```bash
# 1) Preview
pgcopy copy --source "postgres://user:pass@src:5432/db" --dest "postgres://user:pass@dst:5432/db" --dry-run
# 2) Run with prompt
pgcopy copy --source "postgres://user:pass@src:5432/db" --dest "postgres://user:pass@dst:5432/db"
# 3) High-throughput
pgcopy copy --source "postgres://..." --dest "postgres://..." --parallel 8 --copy-pipe --compress
# 4) Progress UI
# (progress UI example removed)
# 5) Web dashboard
pgcopy copy --source "postgres://..." --dest "postgres://..." --output web
```

## Common Scenarios

| Scenario | Command |
|---------|---------|
| Refresh staging from prod | `pgcopy copy --source "postgres://user:pass@prod:5432/db" --dest "postgres://user:pass@staging:5432/db"` |
| Migrate hosts (data-only) | `pgcopy copy --source "postgres://user:pass@old:5432/db" --dest "postgres://user:pass@new:5432/db"` |
| Copy a subset of tables | `pgcopy copy --source "..." --dest "..." --include "public.users,public.orders,public.products"` |
| Speed up over WAN | `pgcopy copy --source "..." --dest "..." --parallel 8 --copy-pipe --compress` |
| Web dashboard | `pgcopy copy --source "..." --dest "..." --output web` |

## Features

- High Performance: Parallel table copying with configurable workers
- Batch Processing: Configurable batch sizes for optimal memory usage
- Progress Tracking: Real-time progress monitoring with visual progress bar enabled by default (stays fixed at top while logs scroll below)
- Flexible Configuration: Support for connection strings, config files, and command-line options
- Table Filtering: Include/exclude specific tables from the copy operation
- Data Safety: Comprehensive validation and confirmation dialogs
- Dry Run Mode: Preview what will be copied without actually copying data
- Transaction Safety: Uses transactions to ensure data consistency
- Advanced Foreign Key Handling: Automatically detects and manages foreign key constraints, including circular dependencies
- Streaming COPY Pipeline: Direct source→destination streaming via PostgreSQL COPY for maximum throughput (`--copy-pipe`)
- In-flight Compression: Optional gzip compression of the COPY stream to reduce network I/O (`--compress`, requires `--copy-pipe`)

## Installation

### Download prebuilt binaries

Grab the latest release for your OS from GitHub Releases:

- https://github.com/koltyakov/pgcopy/releases

After download:
- macOS/Linux: chmod +x pgcopy && move to a directory on your PATH (e.g., /usr/local/bin)
- Windows: use pgcopy.exe or add its folder to PATH

### From Source

```bash
git clone https://github.com/koltyakov/pgcopy.git
cd pgcopy
make build
```

The binary will be available in the `bin/` directory.

### Install to System

```bash
make install
```

This will install `pgcopy` to `/usr/local/bin/`.

## Usage

### ⚠️ Data Safety First

Before using pgcopy, understand that it will **completely overwrite** data in the destination database:

1. **Backup your destination database** before running pgcopy
2. **Verify your connection strings** - double-check source and destination
3. **Use --dry-run first** to preview what will be copied
4. **Test with non-production data** before running on production systems

### Basic Usage

Copy data from one database to another:

```bash
pgcopy copy \
  --source "postgres://user:password@source-host:5432/source_db" \
  --dest "postgres://user:password@dest-host:5432/dest_db"
```

### Using Configuration Files

For convenience, you can save connection strings to files and reference them:

```bash
echo "postgres://user:password@source-host:5432/source_db" > source.conn
echo "postgres://user:password@dest-host:5432/dest_db" > dest.conn

# Use the connection strings directly
pgcopy copy --source "$(cat source.conn)" --dest "$(cat dest.conn)"
```

### Advanced Options

```bash
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --parallel 8 \
  --batch-size 5000 \
  --copy-pipe \
  --compress \
  --exclude "logs,temp_*,*_cache" \
  --include "users,orders,products"
```

For network-bound transfers, combine `--copy-pipe` with `--compress` to significantly boost throughput (commonly multiple times faster on high-latency links).

### Table Filtering

See [Table filtering with wildcards](docs/wildcards.md) for patterns and examples.

### Output Modes

A visual progress bar can be enabled during copy operations. By default, plain output mode is used (suitable for CI/headless environments):

```bash
# Plain output mode (default)
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb"

# Interactive mode with live table progress
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --output interactive

# Web dashboard
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --output web
```

![Web UI dashboard](./assets/webui.jpg)
<sub>Web mode launches a local dashboard for real-time monitoring.</sub>

The progress bar stays fixed at the top of the terminal while operational log messages scroll underneath. In web mode, a local dashboard is launched showing real-time status.

### Streaming COPY pipeline and compression

`--copy-pipe` streams table data directly from the source to the destination using PostgreSQL COPY in binary mode. Adding `--compress` gzip-compresses the stream on the fly to cut network bandwidth. This combo can deliver dramatic speedups in network-bound scenarios (often several times faster, up to ~10x on slow links), with minimal memory footprint.

Key points:

- Use `--copy-pipe` for maximum throughput without intermediate row buffering
- Add `--compress` to reduce bandwidth usage (requires `--copy-pipe`)
- Works with parallel workers across multiple tables
- Per-row progress granularity is limited during streaming; progress is reported at table completion
- Foreign key handling and safety checks still apply as usual

Examples:

```bash
# Stream with compression (recommended over WAN)
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --parallel 8 \
  --copy-pipe \
  --compress

# Stream without compression (good on fast local networks)
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --parallel 8 \
  --copy-pipe
```

Note: `--compress` can only be used together with `--copy-pipe`.

### List Tables

Before copying, you can list all tables in a database:

```bash
pgcopy list --source "postgres://user:password@host:5432/database"
```

### Dry Run

Preview what will be copied without actually copying:

```bash
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --dry-run
```

### Safety & Overwrite Semantics

For the confirmation dialog, skip flags, recovery notes, and best practices, see [Safety & Overwrite Semantics](docs/safety.md).

See the automation notes in [Safety & Overwrite Semantics](docs/safety.md) for --skip-backup usage.

## Commands

### `copy`

Copy data from source to destination database.

**Flags:**

- `--source, -s`: Source database connection string
- `--dest, -d`: Destination database connection string
- `--parallel, -p`: Number of parallel workers (default: 4)
- `--batch-size`: Batch size for data copying (default: 1000)
- `--exclude`: Tables to exclude from copying (comma-separated, supports wildcards: `temp_*,*_logs`)
- `--include`: Tables to include in copying (comma-separated, supports wildcards: `user_*,*_data`)
- `--dry-run`: Show what would be copied without actually copying
- `--skip-backup`: Skip confirmation dialog for data overwrite (use with caution)
- `--output, -o`: Output mode: 'plain' (minimal output, default), 'progress' (progress bar), 'interactive' (live table progress), 'web' (web dashboard)
- `--copy-pipe`: Use streaming COPY pipeline (source→dest) instead of row fetch + insert
- `--compress`: Gzip-compress streaming COPY pipeline (requires `--copy-pipe`)

### `list`

List tables in a database with row counts and sizes.

**Flags:**

- `--source, -s`: Source database connection string
- `--schema`: Specific schema to list (optional)

### `version`

Display version information.

## Configuration

### Connection Strings

PostgreSQL connection strings follow the standard format:

```
postgres://username:password@hostname:port/database_name?sslmode=disable
```

### Environment Variables

You can use environment variables in your connection strings:

```bash
export PGPASSWORD=mypassword
pgcopy copy \
  --source "postgres://user@localhost:5432/sourcedb" \
  --dest "postgres://user@localhost:5433/destdb"
```

## Foreign Key Management

### pgcopy's Superpower: Non-Superuser FK Handling

One of **pgcopy's** key advantages is its ability to handle complex foreign key constraints **without requiring superuser privileges**. This addresses a common pain point where:

- Cloud databases often don't provide superuser access (AWS RDS, Google Cloud SQL, Azure Database)
- Managed environments restrict administrative privileges
- pg_dump/pg_restore fails with permission errors on constraint operations
- Manual FK management becomes error-prone with circular dependencies

### The Foreign Key Challenge

When copying data between PostgreSQL databases with foreign key constraints, you typically face these issues:

1. Constraint Violations: Inserting data in wrong order causes FK violations
2. Circular Dependencies: Tables that reference each other create chicken-and-egg problems
3. Permission Requirements: Standard solutions often require superuser access
4. Manual Complexity: Hand-managing constraints is tedious and error-prone

### pgcopy's Solution

**pgcopy** solves these problems with intelligent constraint management:

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Scan all tables for foreign key constraints              │
│ 2. Try replica mode (if superuser available)                │
│ 3. Fall back to smart FK dropping (non-superuser)           │
│ 4. Copy data in parallel (constraints disabled)             │
│ 5. Restore all constraints with original definitions        │
└─────────────────────────────────────────────────────────────┘
```

### Automatic Detection and Handling

`pgcopy` automatically detects and handles foreign key constraints in your database, including complex scenarios with circular dependencies.

See [Foreign key handling](docs/foreign-keys.md) for modes, caveats, and recovery.

More details in [Foreign key handling](docs/foreign-keys.md).

### Real-World Example

```bash
# This works even without superuser privileges!
pgcopy copy \
  --source "postgres://user:pass@prod-rds.amazonaws.com:5432/myapp" \
  --dest "postgres://user:pass@staging-rds.amazonaws.com:5432/myapp"
```

**Output:**

```
Starting data copy operation...
20:53:26 INFO Detecting foreign key constraints...
20:53:26 DONE Found 5 foreign key constraints
20:53:26 DONE Using replica mode for foreign key handling
20:53:26 INFO Copying table public.ContainerHistory (84.8K rows)
...
20:53:46 DONE Completed copying public.ContainerHistory (84.8K rows) in 20s
20:53:48 DONE Completed copying public.ContainerBarcodes (112.9K rows) in 22s

╔══════════════════════════════════════════════════════════════╗
║                        📊 COPY STATISTICS                    ║
╠══════════════════════════════════════════════════════════════╣
║  📋 Tables Processed:                                     9  ║
║  📊 Rows Copied:                                     439025  ║
║  ⏱️  Duration:                                          22s  ║
║  🚀 Average Speed:                             19322 rows/s  ║
╚══════════════════════════════════════════════════════════════╝
```

## Performance Considerations

### When pgcopy Outperforms Traditional Methods

**pgcopy** is specifically optimized for scenarios where traditional PostgreSQL tools fall short:

#### vs. pg_dump/pg_restore Performance

| Scenario | pg_dump/pg_restore | pgcopy | Advantage |
|----------|-------------------|--------|-----------|
| Large tables (>1M rows) | Serial processing | Parallel batching | 3-5x faster |
| Many small tables | Overhead per table | Parallel workers | 2-4x faster |
| Complex FK constraints | Manual intervention | Automatic handling | Much easier |
| Cloud/managed databases | Permission issues | Works without superuser | Actually works |

#### Performance Characteristics

- Parallel Processing: Multiple tables copied simultaneously
- Batch Optimization: Configurable batch sizes for memory efficiency  
- Connection Pooling: Optimized database connections
- Progress Tracking: Real-time feedback without performance impact
- Streaming Pipeline: COPY-based streaming eliminates per-row overhead and minimizes round-trips
- In-flight Compression: Gzip can greatly reduce network transfer time on bandwidth-limited links

### Output feature matrix

| Feature | raw (plain) | interactive | web |
|---------|:-----------:|:-----------:|:---:|
| Minimal logs | ✓ | ✓ | ✓ |
| Overall progress bar |  | ✓ | ✓ |
| Per-table live status |  | ✓ | ✓ |
| Real-time dashboard |  |  | ✓ |
| Auto-open UI |  |  | ✓ |
| Good for CI | ✓ |  |  |

## Advanced docs

- [Safety & Overwrite Semantics](docs/safety.md)
- [Table filtering with wildcards](docs/wildcards.md)
- [Foreign key handling](docs/foreign-keys.md)
- [Internals overview](docs/internals.md)
- [Architecture](docs/architecture.md)
- [Performance](docs/performance.md)
- [Build & Release](docs/build-release.md)
- [Quality & Security](docs/quality.md)
- [Future Enhancements](docs/future.md)

### Optimal Settings

- Parallel Workers: Start with 4-8 workers, adjust based on your system resources
- Batch Size: 1000-5000 rows per batch usually provides good performance
- Connection Pooling: The tool automatically configures connection pools

### Large Databases

For very large databases:

1. Use higher parallel worker counts (8-16)
2. Increase batch size (5000-10000)
3. Ensure both source and destination databases have adequate resources
4. Consider network bandwidth between source and destination

### Memory Usage

Memory usage scales with:
- Number of parallel workers
- Batch size
- Number of columns in tables

Monitor memory usage and adjust settings accordingly.

## Examples

### Safe Development Workflow

```bash
# 1. Always start with dry-run to verify what will be copied
pgcopy copy \
  --source "postgres://user:pass@prod-db:5432/myapp" \
  --dest "postgres://user:pass@staging-db:5432/myapp" \
  --dry-run

# 2. Run with confirmation dialog (default)
pgcopy copy \
  --source "postgres://user:pass@prod-db:5432/myapp" \
  --dest "postgres://user:pass@staging-db:5432/myapp"

# 3. For automation/CI, skip confirmation
pgcopy copy \
  --source "postgres://user:pass@prod-db:5432/myapp" \
  --dest "postgres://user:pass@staging-db:5432/myapp" \
  --skip-backup
```

### Basic Copy

```bash
pgcopy copy \
  --source "postgres://user:pass@prod-db:5432/myapp" \
  --dest "postgres://user:pass@staging-db:5432/myapp"
```

### High-Performance Copy

```bash
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --parallel 16 \
  --batch-size 10000 \
  --copy-pipe \
  --compress
```

### Selective Copy

```bash
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --include "users,orders,products,inventory"
```

### Exclude System Tables

```bash
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --exclude "logs,sessions,temp_cache"
```

### Wildcard-based Filtering

```bash
# Exclude all temporary and log tables
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --exclude "temp_*,*_logs,*_cache"

# Include only user-related tables
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --include "user_*,*_users,customer_*"
```

## Building from Source

### Prerequisites

- Go 1.21 or later
- PostgreSQL client libraries

### Build

```bash
# Clone the repository
git clone https://github.com/koltyakov/pgcopy.git
cd pgcopy

# Install dependencies
make deps

# Build
make build

# Run tests
make test

# Build for all platforms
make build-all
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Run `make test` and `make lint`
6. Submit a pull request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- Create an issue on GitHub for bug reports or feature requests
- Check existing issues before creating new ones

## Acknowledgments

- Built with [Cobra](https://github.com/spf13/cobra) for CLI functionality
- Uses [lib/pq](https://github.com/lib/pq) PostgreSQL driver
- Inspired by the need for efficient PostgreSQL data migration tools
