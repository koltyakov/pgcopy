# Utilities container

Start Postgres instance with:

```bash
docker run --rm -d \
  --name postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  -v postgres_data:/var/lib/postgresql/data \
  postgres:alpine
```

Target database:

```bash
docker run --rm -d \
  --name postgres_target \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5442:5432 \
  postgres:alpine
```