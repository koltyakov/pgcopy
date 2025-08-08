# Quick Start

This page gets you productive with pgcopy in minutes. For safety notes and deeper docs, see links at the end.

## Install

- Download a prebuilt binary: https://github.com/koltyakov/pgcopy/releases
- Or build from source:
  - `git clone https://github.com/koltyakov/pgcopy.git`
  - `cd pgcopy && make build`

## Safety first

pgcopy truncates destination tables before loading data. Read the short guide: docs/safety.md. In short: verify targets, dry-run first, and have backups.

## The essentials

- Dry run (preview only)
  - `pgcopy copy --source "postgres://user:pass@src:5432/db" --dest "postgres://user:pass@dst:5432/db" --dry-run`
- Run with confirmation
  - `pgcopy copy --source "postgres://user:pass@src:5432/db" --dest "postgres://user:pass@dst:5432/db"`

## Go faster (network-bound)

- Add streaming COPY with gzip compression:
  - `pgcopy copy --source "postgres://..." --dest "postgres://..." --parallel 8 --copy-pipe --compress`
- Notes:
  - `--compress` requires `--copy-pipe`
  - Progress is reported per-table at completion when streaming

## Copy only what you need

- Include a subset
  - `pgcopy copy --source "..." --dest "..." --include "public.users,public.orders,public.products"`
- Exclude by wildcards
  - `pgcopy copy --source "..." --dest "..." --exclude "temp_*,*_logs,*_cache"`

See docs/wildcards.md for patterns.

## List tables

- `pgcopy list --source "postgres://user:pass@host:5432/db"`

## Troubleshooting tips

- Connection strings: standard postgres:// URIs; env vars like PGPASSWORD are respected
- Permissions: you don’t need superuser; pgcopy handles foreign keys automatically
- Cancel: Ctrl-C cancels gracefully; web UI waits for completion acknowledgment

## Learn more

- [Safety & Overwrite Semantics](docs/safety.md)
- [Performance Tuning](docs/performance.md)
- [Foreign Keys](docs/foreign-keys.md)
- [Advanced Internals & Architecture](docs/internals.md)
- [Architecture](docs/architecture.md)
