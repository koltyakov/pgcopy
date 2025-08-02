# pgcopy

**pgcopy** is a high-performance CLI tool for efficiently copying data between PostgreSQL databases with identical schemas. It's designed for data-only migrations and provides parallel processing capabilities for optimal performance.

## Features

- **High Performance**: Parallel table copying with configurable workers
- **Batch Processing**: Configurable batch sizes for optimal memory usage
- **Progress Tracking**: Real-time progress monitoring with visual progress bar enabled by default (stays fixed at top while logs scroll below)
- **Flexible Configuration**: Support for connection strings, config files, and command-line options
- **Table Filtering**: Include/exclude specific tables from the copy operation
- **Resume Capability**: Resume interrupted copy operations
- **Dry Run Mode**: Preview what will be copied without actually copying data
- **Transaction Safety**: Uses transactions to ensure data consistency
- **Advanced Foreign Key Handling**: Automatically detects and manages foreign key constraints, including circular dependencies

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

### Basic Usage

Copy data from one database to another:

```bash
pgcopy copy \
  --source "postgres://user:password@source-host:5432/source_db" \
  --dest "postgres://user:password@dest-host:5432/dest_db"
```

### Using Configuration Files

Create connection configuration files:

```bash
echo "postgres://user:password@source-host:5432/source_db" > source.conf
echo "postgres://user:password@dest-host:5432/dest_db" > dest.conf

pgcopy copy --source-file source.conf --dest-file dest.conf
```

### Advanced Options

```bash
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --parallel 8 \
  --batch-size 5000 \
  --exclude-tables "logs,temp_data" \
  --include-tables "users,orders,products"
```

### Progress Bar Mode

A visual progress bar is shown by default during copy operations with log messages scrolling below. 
To disable the progress bar (useful for CI/headless environments):

```bash
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --dest "postgres://user:pass@localhost:5433/destdb" \
  --no-progress
```

The progress bar stays fixed at the top of the terminal while operational log messages scroll underneath, providing both progress visualization and detailed operation feedback.

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

## Commands

### `copy`

Copy data from source to destination database.

**Flags:**

- `--source, -s`: Source database connection string
- `--dest, -d`: Destination database connection string
- `--source-file`: Source database connection config file
- `--dest-file`: Destination database connection config file
- `--parallel, -p`: Number of parallel workers (default: 4)
- `--batch-size`: Batch size for data copying (default: 1000)
- `--exclude-tables`: Tables to exclude from copying (comma-separated)
- `--include-tables`: Tables to include in copying (comma-separated)
- `--resume`: Resume from previous incomplete copy
- `--dry-run`: Show what would be copied without actually copying
- `--no-progress`: Disable progress bar (useful for CI/headless environments)

### `list`

List tables in a database with row counts and sizes.

**Flags:**

- `--source, -s`: Source database connection string
- `--source-file`: Source database connection config file
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

### Automatic Detection and Handling

`pgcopy` automatically detects and handles foreign key constraints in your database, including complex scenarios with circular dependencies.

### How It Works

1. **Detection**: Scans all tables for foreign key constraints
2. **Strategy Selection**: 
   - **Preferred**: Tries to use `session_replication_role = replica` (requires superuser)
   - **Fallback**: Drops foreign keys temporarily if replica mode unavailable
3. **Safe Restoration**: Ensures all foreign keys are restored after copy completion

### Supported Scenarios

- **Simple Foreign Keys**: Standard table-to-table references
- **Circular Dependencies**: Tables that reference each other in cycles
- **Self-Referencing Tables**: Tables with foreign keys to themselves
- **Complex Cascades**: Handles ON DELETE/UPDATE CASCADE, RESTRICT, SET NULL, etc.

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

### Example Output

```
Detecting foreign key constraints...
Found 15 foreign key constraints
Cannot use replica mode (requires superuser), will drop/recreate FKs
Copying table public.users (10000 rows)
Dropping FK constraint fk_orders_user_id on public.orders
Completed copying table public.users (10000 rows) in 2.1s
...
Restoring 15 foreign key constraints...
Successfully restored 15/15 foreign key constraints
```

## Performance Considerations

### Optimal Settings

- **Parallel Workers**: Start with 4-8 workers, adjust based on your system resources
- **Batch Size**: 1000-5000 rows per batch usually provides good performance
- **Connection Pooling**: The tool automatically configures connection pools

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
  --batch-size 10000
```

### Selective Copy

```bash
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --include-tables "users,orders,products,inventory"
```

### Exclude System Tables

```bash
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest "postgres://user:pass@dest:5432/db" \
  --exclude-tables "logs,sessions,temp_cache"
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
