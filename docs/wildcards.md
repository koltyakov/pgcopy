# Table filtering with wildcards

Both `--include` and `--exclude` accept comma-separated lists with wildcard patterns.

## Patterns

- `*` – any sequence of characters
- `temp_*` – tables starting with `temp_`
- `*_logs` – tables ending with `_logs`
- `*cache*` – tables containing `cache`
- `test_*_data` – tables like `test_user_data`, `test_order_data`

## Examples

```bash
# Exclude temp and logs
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --target   "postgres://user:pass@localhost:5433/destdb" \
  --exclude "temp_*,*_logs,*_cache,session_*"

# Include only specific families
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --target   "postgres://user:pass@localhost:5433/destdb" \
  --include "user_*,order_*,product_*"

# Mix exact + wildcard
pgcopy copy \
  --source "postgres://user:pass@localhost:5432/sourcedb" \
  --target   "postgres://user:pass@localhost:5433/destdb" \
  --include "users,profiles,*_settings,config_*"
```
