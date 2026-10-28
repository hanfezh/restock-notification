# restock-notification

Interview-sized Go implementation of a restock notification system.

## Run

```bash
go run ./cmd/restock-notification
```

Optional environment variables:

- `RESTOCK_ADDR`: HTTP listen address, default `:8080`
- `RESTOCK_DB_PATH`: SQLite path, default `restock.db`

## Demo

Create an out-of-stock product:

```bash
curl -sS -X POST localhost:8080/products \
  -H 'Content-Type: application/json' \
  -d '{"sku":"sku-1","name":"Sneaker","stock":0}'
```

Register a customer:

```bash
curl -sS -X POST localhost:8080/products/1/registrations \
  -H 'Content-Type: application/json' \
  -d '{"email":"customer@example.com"}'
```

Replenish the product:

```bash
curl -sS -X POST localhost:8080/products/1/replenish \
  -H 'Content-Type: application/json' \
  -d '{"quantity":5}'
```

Inspect notifications:

```bash
curl -sS localhost:8080/notifications
```
