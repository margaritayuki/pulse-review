# 0002. Providers статистики diff

Статус: accepted  
Дата: 2026-08-29

## Контекст

Для объёма изменений нужны additions, deletions и files по итоговому diff MR.
REST `changes_count` неточен и ограничивается значением `1000+`; сумма diff
коммитов двойным счётом учитывает промежуточные правки и откаты.

## Решение

Основной источник — GitLab GraphQL `diffStatsSummary`. Совместимый fallback —
пагинированный REST `/diffs` с разбором unified diff. Выбор скрыт за
`mrDiffStatsProvider`; fallback выполняется при `errDiffStatsNotSupported`, но
не при авторизации, rate limit или временной сетевой ошибке.

## Альтернативы

- `changes_count` — отвергнут из-за семантики и лимита;
- сумма commit stats — отвергнута из-за двойного счёта;
- только REST parsing — отвергнут как более дорогой основной путь.

## Последствия

- GraphQL и REST должны давать совпадающие значения на контрольной выборке;
- incomplete diff нельзя выдавать за полный ноль;
- новые фильтры по путям или generated files потребуют более детального provider;
- provider и политика `changedLines` остаются заменяемыми независимо.

## Миграция и проверка

Требуются unit-тесты GraphQL, unsupported capability, REST pagination, служебных
`+++`/`---`, binary/collapsed/too-large случаев и запрета fallback на временные
ошибки.
