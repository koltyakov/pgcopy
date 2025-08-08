# pgcopy

**pgcopy** is a high-performance CLI tool for efficiently copying data between PostgreSQL databases with identical schemas. It's designed for data-only migrations and provides parallel processing capabilities for optimal performance.

![pgcopy in action](./assets/pgcopy.gif)

## ⚠️ Important Warning: Data Overwrite

**pgcopy will TRUNCATE and OVERWRITE all data in destination database tables.** 

- All existing data in target tables will be **permanently deleted**
- Source database data will completely replace destination data
- **This action cannot be undone**

By default, pgcopy shows a confirmation dialog before proceeding. You can skip this confirmation using the `--skip-backup` flag for automated scenarios.

```bash
# Default behavior - shows confirmation dialog
pgcopy copy --source "..." --dest "..."

# Skip confirmation for automated scripts/CI
pgcopy copy --source "..." --dest "..." --skip-backup
```

**Always ensure you have proper backups before running pgcopy on production data.**

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

- **Not for incremental sync**: pgcopy completely overwrites destination data, it doesn't merge or update existing records
- **Not for online replication**: This is not a real-time replication solution
- **Not Change Data Capture (CDC)**: It doesn't track or sync incremental changes
- **Not for production replication**: Use PostgreSQL's built-in logical/physical replication for production
- **Not a schema migration tool**: Both source and destination databases must have identical schemas
- **Not a backup tool**: Always ensure you have proper backups before using pgcopy

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

### Ideal Scenarios

1. **Development Environment Data Refresh**
   ```bash
   # Refresh staging with production data
   pgcopy copy --source "postgres://user:pass@prod:5432/db" --dest "postgres://user:pass@staging:5432/db"
   ```

2. **Database Migration Between Hosts**
   ```bash
   # Move database to new server (data-only)
   pgcopy copy --source "postgres://user:pass@old-server:5432/db" --dest "postgres://user:pass@new-server:5432/db"
   ```

3. **Subset Data Synchronization**
   ```bash
   # Sync only specific tables
   pgcopy copy --source "postgres://user:pass@prod:5432/db" --dest "postgres://user:pass@dev:5432/db" \
     --include "public.users,public.orders,public.products"
   ```

4. **Cross-Cloud Data Transfer**
   ```bash
   # Transfer between cloud providers or regions
   pgcopy copy --source "postgres://user:pass@aws-rds:5432/prod_db" \
     --dest "postgres://user:pass@gcp-sql:5432/prod_db" --parallel 8
   ```

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

### Table Filtering with Wildcards

Both `--exclude` and `--include` support wildcard patterns for flexible table selection:

```bash
# Exclude all temporary tables and logs
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --exclude "temp_*,*_logs,*_cache,session_*"

# Include only specific patterns
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --include "user_*,order_*,product_*"

# Mix exact names and patterns
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --include "users,profiles,*_settings,config_*"
```

**Supported wildcard patterns:**

- `*` - Matches any sequence of characters
- `temp_*` - Matches tables starting with "temp_"
- `*_logs` - Matches tables ending with "_logs"
- `*cache*` - Matches tables containing "cache"
- `test_*_data` - Matches tables like "test_user_data", "test_order_data"

### Progress Bar Mode

A visual progress bar can be enabled during copy operations. By default, plain output mode is used (suitable for CI/headless environments):

```bash
# Plain output mode (default)
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb"

# Progress bar mode
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --output progress

# Interactive mode with live table progress
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --output interactive
```

The progress bar stays fixed at the top of the terminal while operational log messages scroll underneath, providing both progress visualization and detailed operation feedback.

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

### Data Overwrite Confirmation

By default, pgcopy shows a safety confirmation dialog before overwriting data:

```bash
# Will prompt for confirmation
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb"

# Example confirmation dialog:
# ⚠️  WARNING: Data Overwrite Operation
# ═══════════════════════════════════════════════════════════════
# This operation will OVERWRITE data in the destination database:
# 
# 🎯 Destination: postgres://myserver.com:5432/production
# 📊 Tables to overwrite: 5 (with data)
# 📈 Total rows to copy: 1,234,567
# 
# ⚠️  ALL EXISTING DATA in these tables will be DELETED:
#    • public.orders (500,000 rows)
#    • public.products (300,000 rows)
#    • public.users (50,000 rows)
#    • public.categories (1,200 rows)
#    • public.settings (45 rows)
# 
# ═══════════════════════════════════════════════════════════════
# This action CANNOT be undone. Are you sure you want to proceed?
# Type 'yes' to confirm, or anything else to cancel:
```

### Skip Confirmation for Automation

For scripts and CI environments, use `--skip-backup` to bypass the confirmation:

```bash
# No confirmation dialog - proceed directly
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --skip-backup

# Useful for automated environments
pgcopy copy \
  --source "postgres://user:pass@prod:5432/db" \
  --dest "postgres://user:pass@staging:5432/db" \
  --skip-backup \
  --parallel 8
```

**⚠️ Use `--skip-backup` only when you're certain about the destination and have proper backups.**

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
- `--output, -o`: Output mode: 'plain' (minimal output, default), 'progress' (progress bar), 'interactive' (live table progress)
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

### How It Works

1. Detection: Scans all tables for foreign key constraints
2. Strategy Selection: 
   - Preferred: Tries to use `session_replication_role = replica` (requires superuser)
   - Fallback: Drops foreign keys temporarily if replica mode unavailable
3. Safe Restoration: Ensures all foreign keys are restored after copy completion

### Supported Scenarios

- Simple Foreign Keys: Standard table-to-table references
- Circular Dependencies: Tables that reference each other in cycles
- Self-Referencing Tables: Tables with foreign keys to themselves
- Complex Cascades: Handles ON DELETE/UPDATE CASCADE, RESTRICT, SET NULL, etc.

### Non-Superuser Operation

When running as a non-superuser (most common scenario):
- Foreign keys are temporarily dropped before copying each table
- Original constraint definitions are preserved
- All constraints are recreated after successful copy
- Failed operations still attempt to restore dropped constraints

### Error Recovery

If the copy operation fails:
- A cleanup process attempts to restore all dropped foreign keys
- Warning messages indicate any constraints that couldn't be restored
- Manual intervention may be required if restoration fails

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

### Real-World Performance Examples

#### Example 1: E-commerce Database

```
Database: 50 tables, 10M total rows, complex FK relationships
pg_dump/pg_restore: 45 minutes (single-threaded)
pgcopy --parallel 8: 12 minutes (with FK handling)
Result: 3.75x improvement + automatic FK management
```

#### Example 2: SaaS Application Data Refresh 

```
Scenario: Daily staging refresh from production
Tables: 200+ tables, circular dependencies
pg_dump: Failed (FK constraint errors)
pgcopy: 8 minutes, fully automated
Result: Actually works where pg_dump fails
```

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
