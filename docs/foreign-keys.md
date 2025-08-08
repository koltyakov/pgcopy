# Foreign key handling

pgcopy manages FK constraints automatically. It prefers replica mode (requires superuser) and falls back to temporarily dropping and restoring constraints. Circular/self-referencing relationships are supported.

## Modes

- Replica mode: `SET session_replication_role = replica` during inserts
- Drop/restore: Detect, drop, copy, and recreate FKs (idempotent restoration with retries)

## Notes

- Works without superuser privileges (via drop/restore)
- Resilient: retries transient errors during restore
- Idempotent: skips re-adding constraints if they already match

See also: implementation details in `internal/copier/foreignkey.go`.
