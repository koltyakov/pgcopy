#!/bin/bash

# Example script for copying a production database to staging

# Configuration
SOURCE_DB="postgres://prod_user:prod_pass@prod-server:5432/myapp_prod"
DEST_DB="postgres://staging_user:staging_pass@staging-server:5432/myapp_staging"

pgcopy copy \
  --source "$SOURCE_DB" \
  --dest "$DEST_DB" \
  --parallel 8 \
  --batch-size 5000 \
  --exclude-tables "logs,sessions,cache_entries,temp_files"
