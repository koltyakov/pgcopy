#!/bin/bash

# Configuration "postgres://user:password@localhost:5432/database"
SOURCE_DB="postgres://postgres:postgres@localhost:5442/postgres"
DEST_DB="postgres://postgres:postgres@localhost:5452/postgres"

pgcopy copy \
  --source "$SOURCE_DB" \
  --dest "$DEST_DB" \
  --parallel 8 \
  --batch-size 5000 \
  --exclude "logs,sessions,cache_entries,temp_files" \
  --include "public.Container*"
