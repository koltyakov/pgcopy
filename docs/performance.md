# Performance

Guidance and notes for getting the best throughput.

## When pgcopy shines
- Large tables: concurrent table copying beats serial dump/restore
- Many small tables: parallel workers reduce per-table overhead
- Complex FKs: automatic handling avoids manual orchestration
- Managed DBs: avoids superuser requirements that block pg_dump

## Characteristics
- Parallel workers: 4–8 as a baseline; raise for larger DBs
- Batch sizing: 1000–5000 typical for row-based mode
- Streaming COPY: `--copy-pipe` avoids per-row overhead
- In-flight compression: `--compress` reduces WAN time

## Tuning tips
- Scale workers gradually; watch CPU, IO, and locks
- Prefer COPY + compress on high latency or low bandwidth links
- Ensure adequate resources on both source and destination
- Monitor memory usage relative to workers and batch sizes

## Real-world examples
See the README performance section for scenario comparisons and example runs.
