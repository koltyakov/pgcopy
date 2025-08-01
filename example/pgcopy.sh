#!/bin/bash

# Configuration "postgres://user:password@localhost:5432/database"
SOURCE_DB="postgres://postgres:postgres@localhost:5442/postgres"
DEST_DB="postgres://postgres:postgres@localhost:5452/postgres"

pgcopy copy \
  --source-file "$(dirname "$0")/db1.conn" \
  # --source "$SOURCE_DB" \
  --dest-file "$(dirname "$0")/db2.conn" \
  # --dest "$DEST_DB" \
  --parallel 8 \
  --batch-size 5000 \
  --exclude-tables "logs,sessions,cache_entries,temp_files" \
  --progress
