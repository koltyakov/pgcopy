# Internals overview

A high-level look at how pgcopy runs a copy operation.

## Execution layers
- Planning: discover tables, sizes, FKs; build plan/state
- Display: pluggable display modes (plain/progress/interactive/web)
- Execution: parallel workers copy tables with batching or COPY-pipe
- Safety: confirmation, truncation, transactional boundaries
- FK management: replica mode or drop/restore with recovery

## Streaming COPY pipeline
- `--copy-pipe` streams source->dest via PostgreSQL COPY (binary)
- `--compress` optionally gzips the stream to reduce bandwidth
- Minimal memory footprint, great for high-latency links

## Progress & state
- Plain logging, progress bar, interactive TUI, and web dashboard
- State is tracked for table-level progress/statistics

For code, start with `internal/copier/` and `cmd/copy.go`.
