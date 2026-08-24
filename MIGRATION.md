# Миграция backend на Go — v0.2.0

Ruby-реализация `local_server.rb` остаётся эталоном поведения до прохождения
матрицы CI на Windows и macOS. Пользовательские JSON-файлы и frontend не
требуют миграции.

## Контракт совместимости

Go-версия сохраняет:

- адрес `127.0.0.1` и порт `4567` по умолчанию;
- переменные `GITLAB_URL`, `GITLAB_TOKEN`, `GITLAB_PROJECTS`,
  `GITLAB_PROJECT_ID`, `PORT` и `MATTERMOST_WEBHOOK_URL`;
- файлы `data/routes.json`, `data/groups.json` и `data/projects.json`;
- API `/api/config`, `/api/report`, `/api/progress` и `/api/send`;
- включительные UTC-границы отчёта;
- правила подсчёта approvals, likes, комментариев и MR;
- 15-минутный кэш и максимум шесть параллельных обработчиков MR;
- JSON-ошибки с HTTP 500, как в Ruby-сервере.

## Переходный запуск

- Go: `bin/setup` + `bin/start` или `setup.cmd` + `start.cmd`;
- явные Go-команды `bin/setup-go`, `bin/start-go`, `setup-go.cmd` и
  `start-go.cmd` оставлены как алиасы;
- Ruby временно доступен напрямую через `bundle exec ruby local_server.rb`.

Основной запуск будет переключён на Go только после зелёного CI и проверки
результатов обеих реализаций на одинаковых GitLab fixtures. До этого Ruby-файлы,
Gemfile и старые установщики не удаляются.

## Критерии завершения

1. `go test ./...`, `go test -race ./...` и `go vet ./...` проходят.
2. Go-бинарник собирается и запускается на Windows и macOS.
3. Контракты API и результаты метрик совпадают с Ruby oracle.
4. Существующие `data/*.json` читаются обеими реализациями.
5. Frontend работает без изменений HTTP-контракта.
6. Ruby удаляется только отдельным последующим изменением после релиза.
