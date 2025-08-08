# Architecture

A high-level view of how pgcopy is organized and how a copy run executes.

## Project structure (high level)
- `cmd/` — CLI entry points (copy, list, version)
- `internal/copier/` — core copy engine (planning, progress, execution, FK handling)
- `internal/server/` — lightweight web dashboard (web mode)
- `internal/state/` — state tracking and hooks
- `internal/utils/` — formatting, colors, logging helpers
- `examples/` — usage examples
- `scripts/` — release and utility scripts

## Key components

- CLI (Cobra)
  - Flags parsing, subcommands, help
  - Output mode selection (plain/progress/interactive/web)
- DB connections
  - PostgreSQL drivers and connection options
  - Pool sizing and lifecycle management
- Planner
  - Discovers tables, sizes, and foreign keys
  - Builds execution plan and initializes state
- Execution engine
  - Parallel workers copying tables concurrently
  - Batching or streaming (COPY pipeline) per table
  - Progress updates and statistics
- Foreign key handling
  - Prefer replica mode when available
  - Fallback: drop/restore with idempotent, retried restoration
- Displays
  - Plain logs, progress bar, interactive TUI, and web dashboard

## Execution flow (simplified)
1) Parse config and output mode
2) Connect to source/destination; scan schema and FKs
3) Confirm overwrite (unless skipped); prepare plan/state
4) Apply FK strategy (replica or drop when needed)
5) Copy tables in parallel (batch insert or COPY stream)
6) Restore constraints and finalize statistics
7) Present results in chosen display mode
