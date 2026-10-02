# T-Bank SRE Marketplace

REST API маркетплейса: управление товарами, заказами и промокодами.

---

## Быстрый старт

```bash
# 1. Сгенерировать код из OpenAPI-спецификации и SQL-запросов
make generate

# 2. Запустить сервис + PostgreSQL через Docker Compose
make run
```

Сервис будет доступен на `http://localhost:8080`.
Миграции применяются автоматически при старте.

---

## Сборка и запуск без Docker

```bash
# Сгенерировать код
make generate

# Собрать бинарь
make build   # bin/server

# Запустить (требует работающий PostgreSQL)
DB_DSN="postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable" \
JWT_SECRET="changeme" \
./bin/server
```

Переменные окружения:

| Переменная                 | Default                                     | Описание                                  |
|----------------------------|---------------------------------------------|-------------------------------------------|
| `DB_DSN`                   | `postgres://...@localhost:5432/marketplace` | DSN подключения к PostgreSQL              |
| `HTTP_PORT`                | `8080`                                      | Порт HTTP-сервера                         |
| `JWT_SECRET`               | `dev-secret-change-in-production`           | Секрет для подписи JWT                    |
| `JWT_ACCESS_TTL`           | `15m`                                       | Время жизни access-токена                 |
| `JWT_REFRESH_TTL`          | `168h`                                      | Время жизни refresh-токена                |
| `ORDER_RATE_LIMIT_MINUTES` | `1`                                         | Минимальный интервал между заказами (мин) |

---

## Makefile-цели

| Цель          | Описание                                                          |
|---------------|-------------------------------------------------------------------|
| `make tools`  | Установить `oapi-codegen` и `sqlc`                               |
| `make generate` | Сгенерировать `internal/api/` и `internal/db/sqlc/` из spec/SQL |
| `make build`  | Собрать `bin/server` (автоматически вызывает `generate`)         |
| `make run`    | `docker-compose up --build`                                      |
| `make test`   | Unit-тесты (без e2e)                                             |
| `make test-e2e` | E2E-тесты (запускает PostgreSQL через testcontainers)          |
| `make migrate`| Применить миграции к локальной БД                                |
| `make tidy`   | `go mod tidy`                                                    |
| `make clean`  | Удалить сгенерированные файлы и бинарь                           |

> **Важно**: `internal/api/` и `internal/db/sqlc/` добавлены в `.gitignore`.
> Перед первой сборкой всегда выполняйте `make generate`.

---

## Тесты

### Unit-тесты

```bash
make test
```

Тестируют бизнес-логику (service/) с мок-репозиторием: auth, products, orders (rate limit, stock, промокоды), apierr.

### E2E-тесты

```bash
make test-e2e
```

Поднимают реальный PostgreSQL в Docker через [testcontainers-go](https://golang.testcontainers.org/),
применяют миграции и прогоняют 17 HTTP-сценариев:

- Регистрация / вход / refresh-токен / дублирующий email
- CRUD товаров с проверками RBAC
- Создание заказов (happy path, нехватка стока, rate limit)
- Промокоды (процентная скидка, невалидный код)
- Отмена заказа, двойная отмена (`INVALID_STATE_TRANSITION`)
- Проверки владения (`ORDER_OWNERSHIP_VIOLATION`)
- Ролевая модель (SELLER не может создать заказ; USER не может создать промокод)

Требуют работающий Docker.

---

## Структура проекта

```
api/
  openapi.yaml            # OpenAPI 3.0 — source of truth
cmd/server/main.go        # Точка входа
internal/
  api/                    # СГЕНЕРИРОВАНО (oapi-codegen), в .gitignore
  app/app.go              # Сборка роутера и RunMigrations
  apierr/                 # Типизированные бизнес-ошибки с HTTP-статусами
  config/                 # Конфигурация из env-переменных
  db/                     # pgxpool
  db/sqlc/                # СГЕНЕРИРОВАНО (sqlc), в .gitignore
  db/queries/             # SQL-запросы для sqlc
  handler/                # HTTP-обработчики (реализуют StrictServerInterface)
  middleware/             # Logger (JSON) + Auth (JWT)
  migrations/             # SQL-миграции (embedded FS, golang-migrate)
  service/                # Бизнес-логика
  e2e/                    # E2E-тесты (testcontainers-go)
```

---

## API

Полная спецификация — [api/openapi.yaml](api/openapi.yaml).

### Аутентификация

```
POST /auth/register    # Регистрация → {access_token, refresh_token}
POST /auth/login       # Вход        → {access_token, refresh_token}
POST /auth/refresh     # Обновить access-токен по refresh-токену
```

Все остальные эндпоинты требуют заголовок:
```
Authorization: Bearer <access_token>
```

### Товары

```
GET    /products           # Список (?page=0&size=20&status=ACTIVE&category=...)
POST   /products           # Создать (SELLER, ADMIN)
GET    /products/{id}      # Получить
PUT    /products/{id}      # Обновить (SELLER — свои, ADMIN — любые)
DELETE /products/{id}      # Мягкое удаление → ARCHIVED (SELLER — свои, ADMIN — любые)
```

### Заказы

```
POST   /orders              # Создать (USER, ADMIN)
GET    /orders/{id}         # Получить (USER — свои, ADMIN — любые)
PUT    /orders/{id}         # Обновить позиции (только статус CREATED)
POST   /orders/{id}/cancel  # Отменить (CREATED / PAYMENT_PENDING → CANCELED)
```

### Промокоды

```
POST /promo-codes    # Создать (SELLER, ADMIN)
```

---

## Коды ошибок

| Код                         | HTTP | Когда                                       |
|-----------------------------|------|---------------------------------------------|
| `VALIDATION_ERROR`          | 400  | Невалидные входные данные                   |
| `TOKEN_EXPIRED`             | 401  | JWT истёк                                   |
| `TOKEN_INVALID`             | 401  | JWT невалиден                               |
| `REFRESH_TOKEN_INVALID`     | 401  | Refresh-токен недействителен                |
| `INVALID_CREDENTIALS`       | 401  | Неверный email или пароль                   |
| `ACCESS_DENIED`             | 403  | Нет прав                                    |
| `ORDER_OWNERSHIP_VIOLATION` | 403  | Заказ принадлежит другому пользователю      |
| `PRODUCT_NOT_FOUND`         | 404  | Товар не найден                             |
| `ORDER_NOT_FOUND`           | 404  | Заказ не найден                             |
| `PRODUCT_INACTIVE`          | 409  | Товар неактивен                             |
| `ORDER_HAS_ACTIVE`          | 409  | Уже есть активный заказ                     |
| `INVALID_STATE_TRANSITION`  | 409  | Недопустимый переход состояния              |
| `INSUFFICIENT_STOCK`        | 409  | Не хватает товара на складе                 |
| `USER_ALREADY_EXISTS`       | 409  | Email уже занят                             |
| `PROMO_CODE_INVALID`        | 422  | Промокод недействителен / истёк / исчерпан  |
| `PROMO_CODE_MIN_AMOUNT`     | 422  | Сумма заказа ниже минимальной для промокода |
| `ORDER_LIMIT_EXCEEDED`      | 429  | Слишком частые операции с заказами          |

---

## Демонстрация через curl

Полный сценарий для защиты. Запустите сервис (`make run`), затем выполняйте команды по порядку.

```bash
BASE=http://localhost:8080
```

### 1. Аутентификация

```bash
# Регистрация продавца
curl -s -X POST $BASE/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"seller@demo.com","password":"password123","role":"SELLER"}' | jq .

# Регистрация покупателя
curl -s -X POST $BASE/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@demo.com","password":"password123","role":"USER"}' | jq .

# Сохранить токены в переменные
SELLER_TOKEN=$(curl -s -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"seller@demo.com","password":"password123"}' | jq -r .access_token)

USER_TOKEN=$(curl -s -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@demo.com","password":"password123"}' | jq -r .access_token)

echo "SELLER: $SELLER_TOKEN"
echo "USER:   $USER_TOKEN"

# Refresh token
REFRESH=$(curl -s -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@demo.com","password":"password123"}' | jq -r .refresh_token)

curl -s -X POST $BASE/auth/refresh \
  -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}" | jq .
```

### 2. Товары

```bash
# Создать товар (SELLER)
PRODUCT_ID=$(curl -s -X POST $BASE/products \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{"name":"Laptop Pro","price":1299.99,"stock":10,"category":"Electronics","description":"Мощный ноутбук"}' \
  | jq -r .id)
echo "Product ID: $PRODUCT_ID"

# Список товаров (пагинация + фильтр по категории)
curl -s "$BASE/products?page=0&size=10&category=Electronics" \
  -H "Authorization: Bearer $USER_TOKEN" | jq .

# Список товаров с фильтром по статусу
curl -s "$BASE/products?status=ACTIVE" \
  -H "Authorization: Bearer $USER_TOKEN" | jq '{totalElements, items: [.items[] | {id, name, status, stock}]}'

# Получить товар по ID
curl -s $BASE/products/$PRODUCT_ID \
  -H "Authorization: Bearer $USER_TOKEN" | jq .

# Обновить товар (SELLER — только свои)
curl -s -X PUT $BASE/products/$PRODUCT_ID \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{"name":"Laptop Pro Max","price":1499.99,"stock":8,"category":"Electronics","status":"ACTIVE"}' | jq .

# Мягкое удаление → статус ARCHIVED
curl -s -X DELETE $BASE/products/$PRODUCT_ID \
  -H "Authorization: Bearer $SELLER_TOKEN" | jq '{id, name, status}'

# Убедиться что статус ARCHIVED (физически запись осталась)
curl -s $BASE/products/$PRODUCT_ID \
  -H "Authorization: Bearer $USER_TOKEN" | jq '{id, name, status}'

# Создать новый товар для заказов
PRODUCT_ID=$(curl -s -X POST $BASE/products \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{"name":"Wireless Mouse","price":49.99,"stock":20,"category":"Accessories"}' \
  | jq -r .id)
echo "Product ID: $PRODUCT_ID"
```

### 3. Промокоды

```bash
# Создать процентный промокод (SELLER)
curl -s -X POST $BASE/promo-codes \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{
    "code": "SAVE20",
    "discount_type": "PERCENTAGE",
    "discount_value": 20,
    "min_order_amount": 0,
    "max_uses": 100,
    "valid_from": "2024-01-01T00:00:00Z",
    "valid_until": "2099-12-31T23:59:59Z"
  }' | jq .

# Создать промокод с фиксированной скидкой
curl -s -X POST $BASE/promo-codes \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{
    "code": "FLAT10",
    "discount_type": "FIXED_AMOUNT",
    "discount_value": 10,
    "min_order_amount": 30,
    "max_uses": 50,
    "valid_from": "2024-01-01T00:00:00Z",
    "valid_until": "2099-12-31T23:59:59Z"
  }' | jq .
```

### 4. Заказы

```bash
# Создать заказ (USER) — фиксируется CREATE_ORDER, запускается rate limit (1 мин)
ORDER_ID=$(curl -s -X POST $BASE/orders \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER_TOKEN" \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}" \
  | jq -r .id)
echo "Order ID: $ORDER_ID"

# Получить заказ
curl -s $BASE/orders/$ORDER_ID \
  -H "Authorization: Bearer $USER_TOKEN" | jq .

# Обновить позиции заказа (только в статусе CREATED)
curl -s -X PUT $BASE/orders/$ORDER_ID \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER_TOKEN" \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":3}]}" | jq '{status, total_amount, items}'

# Отменить заказ (CREATED → CANCELED, остатки возвращаются)
curl -s -X POST $BASE/orders/$ORDER_ID/cancel \
  -H "Authorization: Bearer $USER_TOKEN" | jq '{id, status}'

# Убедиться что сток вернулся
curl -s $BASE/products/$PRODUCT_ID \
  -H "Authorization: Bearer $USER_TOKEN" | jq '{name, stock}'
```

### 5. Проверка ошибок и промокоды

```bash
# 409 — дублирующийся email
curl -s -X POST $BASE/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@demo.com","password":"password123","role":"USER"}' | jq .

# 401 — неверный пароль (INVALID_CREDENTIALS)
curl -s -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@demo.com","password":"wrongpassword"}' | jq .

# 401 — запрос без токена (TOKEN_INVALID)
curl -s $BASE/products | jq .

# 403 — USER не может создать товар (ACCESS_DENIED)
curl -s -X POST $BASE/products \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER_TOKEN" \
  -d '{"name":"Hack","price":1.0,"stock":1,"category":"X"}' | jq .

# 403 — USER не может создать промокод (ACCESS_DENIED)
curl -s -X POST $BASE/promo-codes \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER_TOKEN" \
  -d '{"code":"HACK1","discount_type":"FIXED_AMOUNT","discount_value":1,"max_uses":1,"valid_from":"2024-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z"}' | jq .

# 404 — товар не найден
curl -s $BASE/products/00000000-0000-0000-0000-000000000000 \
  -H "Authorization: Bearer $USER_TOKEN" | jq .

# 400 — ошибка валидации (цена <= 0)
curl -s -X POST $BASE/products \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{"name":"Bad","price":-1,"stock":0,"category":"X"}' | jq .

# 429 — rate limit: USER создал заказ в разделе 4, повторная попытка < 1 мин
curl -s -X POST $BASE/orders \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER_TOKEN" \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}" | jq .

# 409 — повторная отмена уже отменённого заказа (INVALID_STATE_TRANSITION)
curl -s -X POST $BASE/orders/$ORDER_ID/cancel \
  -H "Authorization: Bearer $USER_TOKEN" | jq .

# --- Свежий пользователь USER2 (нет активных заказов, нет rate limit) ---
USER2_TOKEN=$(curl -s -X POST $BASE/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user2@demo.com","password":"password123","role":"USER"}' | jq -r .access_token)

# Товар с малым стоком для демонстрации INSUFFICIENT_STOCK
PRODUCT2_ID=$(curl -s -X POST $BASE/products \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -d '{"name":"Rare Item","price":999.99,"stock":1,"category":"Rare"}' | jq -r .id)

# 409 — нехватка стока (INSUFFICIENT_STOCK); заказ не создаётся, rate limit не ставится
curl -s -X POST $BASE/orders \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER2_TOKEN" \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT2_ID\",\"quantity\":100}]}" | jq .

# 422 — несуществующий промокод (PROMO_CODE_INVALID); заказ не создаётся
curl -s -X POST $BASE/orders \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER2_TOKEN" \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}],\"promo_code\":\"NOSUCH\"}" | jq .

# Заказ с промокодом SAVE20 (скидка 20%): discount_amount=10, total_amount=39.99
ORDER2_ID=$(curl -s -X POST $BASE/orders \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $USER2_TOKEN" \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}],\"promo_code\":\"SAVE20\"}" \
  | jq -r .id)

curl -s $BASE/orders/$ORDER2_ID \
  -H "Authorization: Bearer $USER2_TOKEN" | jq '{status, total_amount, discount_amount}'
```

### 6. Проверка данных в БД

```bash
# Подключиться к PostgreSQL и посмотреть таблицы
docker compose exec db psql -U marketplace -d marketplace -c "SELECT id, name, status, stock FROM products;"
docker compose exec db psql -U marketplace -d marketplace -c "SELECT id, status, total_amount, discount_amount FROM orders;"
docker compose exec db psql -U marketplace -d marketplace -c "SELECT id, operation_type, created_at FROM user_operations ORDER BY created_at DESC LIMIT 10;"
docker compose exec db psql -U marketplace -d marketplace -c "SELECT code, current_uses, max_uses FROM promo_codes;"
```
