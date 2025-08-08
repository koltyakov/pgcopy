# pgcopy

**pgcopy** is a CLI tool for copying data between PostgreSQL databases with identical schemas. It's built for data-only migrations and supports parallel processing.

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
- Progress Tracking: Live progress monitoring with a visual progress bar enabled by default (stays fixed at top while logs scroll below)
- Flexible Configuration: Support for connection strings, config files, and command-line options
- Table Filtering: Include/exclude specific tables from the copy operation
- Data Safety: Comprehensive validation and confirmation dialogs
- Dry Run Mode: Preview what will be copied without actually copying data
- Transaction Safety: Uses transactions to ensure data consistency
- Advanced Foreign Key Handling: Automatically detects and manages foreign key constraints, including circular dependencies
- Streaming COPY Pipeline: Direct source→destination streaming via PostgreSQL COPY for higher throughput (`--copy-pipe`)
- In-flight Compression: Optional gzip compression of the COPY stream to reduce network I/O (`--compress`, requires `--copy-pipe`)

## Installation

### Download prebuilt binaries

Grab the latest release for your OS from GitHub Releases:

- https://github.com/koltyakov/pgcopy/releases

After download:
- macOS/Linux: `chmod +x pgcopy` && move to a directory on your PATH (e.g., `/usr/local/bin`)
- Windows: use `pgcopy.exe` or add its folder to PATH

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

See [Safety & Overwrite Semantics](docs/safety.md) for pre-flight checks and automation guidance.

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

For network-bound transfers, combine `--copy-pipe` with `--compress` to improve throughput on high-latency links.

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
<sub>Web mode launches a local dashboard for live monitoring.</sub>

The progress bar stays fixed at the top of the terminal while operational log messages scroll underneath. In web mode, a local dashboard is launched showing live status.

### Streaming COPY pipeline and compression

`--copy-pipe` streams table data directly from the source to the destination using PostgreSQL COPY in binary mode. Adding `--compress` gzip-compresses the stream on the fly to cut network bandwidth. This combo can yield speedups in network-bound scenarios, with a minimal memory footprint.

Key points:

- Use `--copy-pipe` for higher throughput without intermediate row buffering
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

See the automation notes in [Safety & Overwrite Semantics](docs/safety.md) for `--skip-backup` usage.

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

pgcopy automatically handles complex foreign key constraints without superuser privileges. See [Foreign key handling](docs/foreign-keys.md) for strategies (replica vs drop/restore), idempotent restore, and recovery.

## Performance Considerations

See [Performance](docs/performance.md) for comparisons and tuning guidance.

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
