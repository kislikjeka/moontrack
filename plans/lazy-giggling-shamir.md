# Plan: Интеграция Grafana Loki MCP Server

## Context

В проекте уже есть Grafana+Loki+Promtail стек (docker-compose, profile "logs"), структурированное JSON-логирование на бэкенде с полями `level`, `component`, `request_id`, `user_id`, `tx_id`, `msg`, `duration_ms`. Нужно подключить [Loki MCP Server](https://github.com/grafana/loki-mcp) чтобы Claude Code мог напрямую запрашивать логи через LogQL — для отладки, трассировки запросов и поиска ошибок.

## Подход

**Docker stdio**: Собираем loki-mcp-server в Docker-образ, Claude Code запускает его через `docker run --rm -i` (stdio transport). Loki уже экспонирует порт 3100 на хост, контейнер подключается через `host.docker.internal:3100` (macOS).

## Шаги

### 1. Создать `infra/loki-mcp/Dockerfile`

Multi-stage build: клонируем `github.com/grafana/loki-mcp`, собираем Go-бинарник, минимальный alpine runtime.

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git && \
    git clone --depth 1 https://github.com/grafana/loki-mcp.git .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o loki-mcp-server ./cmd/server

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/loki-mcp-server .
ENTRYPOINT ["./loki-mcp-server"]
```

### 2. Создать `.mcp.json` в корне проекта

```json
{
  "mcpServers": {
    "loki": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "-e", "LOKI_URL=http://host.docker.internal:3100",
        "moontrack-loki-mcp:latest"
      ]
    }
  }
}
```

### 3. Добавить рецепты в `justfile`

Новая секция "Observability":
- **`loki-mcp-build`** — `docker build -t moontrack-loki-mcp:latest ./infra/loki-mcp`
- **`loki-mcp-check`** — проверяет наличие образа и доступность Loki на localhost:3100

Файл: `/Users/kislikjeka/projects/moontrack/justfile`

### 4. Создать скилл `.claude/skills/observability-debugging/SKILL.md`

Содержание:
- Пререквизиты (`just dev-logs`, `just loki-mcp-build`)
- LogQL quick reference с готовыми запросами для MoonTrack
- Таблица компонентов (`http`, `ledger`, `wallet`, `sync`, `asset`, `price`, etc.)
- Типичные сценарии отладки: 500 ошибки, проваленные транзакции, проблемы sync, медленные запросы
- Workflow: start broad → narrow down → trace request → check timing

### 5. Обновить `CLAUDE.md`

Добавить секцию "Observability (Loki MCP)" с:
- Пререквизитами
- Доступными MCP инструментами (`loki_query`)
- Ключевыми лейблами и полями логов
- Ссылкой на скилл для деталей

Файл: `/Users/kislikjeka/projects/moontrack/CLAUDE.md`

## Файлы

| Файл | Действие |
|------|----------|
| `infra/loki-mcp/Dockerfile` | Создать |
| `.mcp.json` | Создать |
| `.claude/skills/observability-debugging/SKILL.md` | Создать |
| `justfile` | Изменить (добавить секцию Observability) |
| `CLAUDE.md` | Изменить (добавить секцию Observability) |

## Проверка

1. `just loki-mcp-build` — образ собирается без ошибок
2. `just dev-logs` — поднимается Loki стек
3. `just loki-mcp-check` — оба чека проходят (образ + Loki ready)
4. Перезапустить Claude Code, `/mcp` показывает `loki` как connected
5. Выполнить тестовый запрос: `loki_query` с `{service="backend"} | json | level="error"` — возвращает результаты (или пустой список если ошибок нет)
