# Utilities container

Start source Postgres instance with:

```bash
docker run --rm -d \
  --name postgres_source \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5452:5432 \
  -v pg_copy_source_data:/var/lib/postgresql/data \
  postgres:alpine
```

Start target Postgres instance with:

```bash
docker run --rm -d \
  --name postgres_target \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5462:5432 \
  postgres:alpine
```