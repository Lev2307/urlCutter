# urlCutter

Сокращатель ссылок на Go. Учебный проект: чистый `net/http`, без фреймворков.

**Статус:** скелет сервиса. Сокращения ссылок пока нет.

## Запуск

```sh
cp .env.example .env
go run ./cmd/urlcutter
curl.exe -i http://localhost:8080/healthz
```

## Конфигурация

Переменные необязательны, у каждой есть дефолт. Длительности — в формате `time.ParseDuration` (`10s`, `500ms`).

| Переменная | Дефолт |
|---|---|
| `PORT` | `8080` |
| `READ_TIMEOUT` | `5s` |
| `WRITE_TIMEOUT` | `5s` |
| `IDLE_TIMEOUT` | `5s` |
| `READ_HEADER_TIMEOUT` | `5s` |
| `SHUTDOWN_TIMEOUT` | `5s` |
| `LOG_LEVEL` | `INFO` |

## Ручки

- `GET /healthz` — проверка живости
- `GET /slow` — временная, спит 15 с, для проверки graceful shutdown

## Структура

```
cmd/urlcutter/main.go   запуск и выключение сервера
internal/api/           маршруты и хендлеры
internal/config/        разбор переменных окружения
```
