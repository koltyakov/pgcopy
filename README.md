# pgcopy

**pgcopy** is a CLI tool for copying data between PostgreSQL databases with identical schemas. It's built for data-only migrations and supports parallel processing.

![pgcopy in action](./assets/pgcopy.gif)

> Safety & Overwrite Semantics: pgcopy truncates destination tables and replaces their data. Review the short guide before running: see [Safety & Overwrite Semantics](docs/safety.md).

## When to Use pgcopy

Designed for fast data-only syncs between PostgreSQL databases with matching schemas. Ideal when pg_restore is slow, you lack superuser privileges, or you need parallelism and robust foreign key handling. Not CDC/replication or a schema migration tool.


## Quick Start

See the concise guide at docs/quick-start.md for install, safety, and the 4 commands you’ll use most.

## Common Scenarios

- Refresh staging from prod: `pgcopy copy --source "postgres://user:pass@prod:5432/db" --dest "postgres://user:pass@staging:5432/db"`
- Migrate hosts (data-only): `pgcopy copy --source "postgres://user:pass@old:5432/db" --dest "postgres://user:pass@new:5432/db"`
- Copy subset: `--include "public.users,public.orders,public.products"`
- Speed up over WAN: `--parallel 8 --copy-pipe --compress`
- Web dashboard: `--output web`

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

Start with [docs/quick-start.md](docs/quick-start.md). For patterns and flags:
- Safety & overwrite semantics: docs/safety.md
- Wildcards and filtering: docs/wildcards.md
- Output modes (plain, interactive, web): below
- Streaming COPY and compression: see quick-start and performance docs

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

See [Quick start](docs/quick-start.md) for compact, copy-pasteable examples covering dry-run, web UI, performance flags, and filtering.

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
