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

## Internal Optimizations

pgcopy employs several performance optimizations to minimize overhead:

### Memory Management

- **Row Buffer Pool**: Reuses `[]any` scan buffers via `sync.Pool` to reduce GC pressure during high-throughput scanning
- **String Builder Pool**: Pools `strings.Builder` instances for INSERT statement construction, reducing allocations by ~97%
- **Pre-allocated Integer Strings**: Caches string representations of integers 0-65535 to eliminate `strconv.Itoa` allocations in placeholder generation

### Statement Construction

- **Efficient VALUES Clause Building**: Uses pre-computed integer strings and pooled builders for **99% fewer allocations** and **26% faster** INSERT statement generation
- **Batched Multi-row INSERTs**: Chunks large batches to stay within PostgreSQL's 65535 parameter limit while maximizing rows per statement
- **Pre-computed Headers**: INSERT statement headers are computed once per batch and reused across chunks

### Database Optimizations

- **Connection Pool Tuning**: Automatically configures `MaxOpenConns = Parallel * 2` and `MaxIdleConns = Parallel` for optimal connection reuse
- **Synchronous Commit OFF**: Disables `synchronous_commit` per transaction for bulk inserts (safe for initial loads)
- **Keyset Pagination**: Uses cursor-based pagination with primary keys to avoid OFFSET performance degradation on large tables
- **Snapshot Isolation**: Optional REPEATABLE READ transactions for consistent reads during pagination

### Benchmark Results

| Operation | Original | Optimized | Improvement |
|-----------|----------|-----------|-------------|
| Placeholder strings (1000) | 9614 ns, 901 allocs | 241 ns, 0 allocs | 40x faster |
| VALUES clause (10×100) | 16106 ns, 915 allocs | 12267 ns, 14 allocs | 24% faster, 98% fewer allocs |
| Full INSERT (10×100) | 21624 ns, 914 allocs | 15933 ns, 11 allocs | 26% faster, 99% fewer allocs |

## Tuning tips

- Scale workers gradually; watch CPU, IO, and locks
- Prefer COPY + compress on high latency or low bandwidth links
- Ensure adequate resources on both source and destination
- Monitor memory usage relative to workers and batch sizes
- Use `--exact-rows` only when you need precise counts (e.g., recently truncated tables); it adds COUNT(*) per table during discovery and can slow startups on very large tables

## Real-world examples

See the README performance section for scenario comparisons and example runs.
