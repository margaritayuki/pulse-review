# Миграция backend на Go — v0.2.0

Миграция завершена. Pulse Review собирается и запускается без Ruby и Bundler;
целевые платформы — Windows и macOS.

## Сохранённая совместимость

- адрес `127.0.0.1` и порт `4567` по умолчанию;
- переменные `GITLAB_URL`, `GITLAB_TOKEN`, `GITLAB_PROJECTS`,
  `GITLAB_PROJECT_ID`, `PORT` и `MATTERMOST_WEBHOOK_URL`;
- файлы `data/routes.json`, `data/groups.json` и `data/projects.json`;
- API `/api/config`, `/api/report`, `/api/progress` и `/api/send`;
- включительные UTC-границы отчёта;
- правила подсчёта approvals, likes, комментариев и MR;
- 15-минутный кэш и максимум шесть параллельных обработчиков MR;
- JSON-ошибки с HTTP 500.

## Запуск

- macOS: `bin/setup`, затем `bin/start`;
- Windows: `setup.cmd`, затем `start.cmd`.

Команды с суффиксом `-go` оставлены как совместимые алиасы.

## Проверка

- пользовательская проверка отчётов на существующей конфигурации выполнена;
- `go test ./...`, `go test -race ./...` и `go vet ./...` проходят;
- CI успешно прошёл на Windows и macOS;
- release workflow собирает Windows x64, Mac Apple Silicon и Mac Intel.
