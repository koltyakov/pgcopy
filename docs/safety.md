# Safety & Overwrite Semantics

pgcopy is a destructive data synchronization tool for PostgreSQL databases with identical schemas. It will truncate destination tables and replace their contents with data from the source database.

## What happens during a copy

- Destination tables are truncated before data is written
- Data from source replaces all destination data
- The operation cannot be undone without a backup

## Confirmation & automation

By default, pgcopy shows a confirmation prompt with a summary of affected tables and row counts. Counts are based on fast estimates unless you enable `--exact-rows` to compute precise values. For automation/CI, you can skip the prompt:

```bash
pgcopy copy \
  --source "postgres://user:pass@source:5432/db" \
  --dest   "postgres://user:pass@dest:5432/db" \
  --skip-backup
```

Use `--skip-backup` only when you’ve validated the destination and have good backups.

## Recommended safe workflow

1) Start with `--dry-run` to preview tables and row counts (use `--exact-rows` if estimates may be misleading after TRUNCATE)
2) Run without `--skip-backup` and review the confirmation dialog
3) Use named includes/excludes to avoid accidental table selection
4) For automation, pin exact connection strings and table lists

## Failure & recovery

- If the copy fails mid-run, pgcopy attempts to restore foreign keys it disabled/dropped during the run
- You may need to intervene manually if the environment changed while copying
- Always keep recent backups for critical environments

---

Stay cautious with production data. Validate targets before you hit enter.
