# Gossipper Control UI

Локальная веб-панель для HTTP API gossipper (`internal/api`): health, stats, сценарий XML, горячий apply, rate/pause.

## Лицензии и заимствование стиля

Проект **Gossipper** и этот UI распространяются под **AGPL-3.0-or-later** (см. корневой `LICENSE`).

Тема оформления (Tailwind 4 + токены shadcn, `App.css`) **производная от** веб-UI проекта [Homer](https://github.com/sipcapture/homer) (`src/ui`), также AGPL — в начале `src/App.css` указан SPDX и ссылка на источник.

## Запуск в разработке

Требуется Node **20.19+** или **22.12+** (см. `package.json` → `engines`).

```bash
cd web/control-ui
npm install
```

По умолчанию Vite слушает порт **5174** и проксирует префикс `/api` на целевой backend (переменная **`VITE_API_TARGET`**, по умолчанию `http://127.0.0.1:8080`). Запросы из UI идут на **`VITE_API_BASE`** (по умолчанию **`/api/v1`**, то есть через прокси полный путь вида `/api/v1/health`).

```bash
# пример: API на другом хосте/порту
VITE_API_TARGET=http://10.0.0.5:9090 npm run dev
```

Запустите gossipper с включённым API, например:

```text
-api_addr :8080
# при необходимости:
-api_token <секрет>
```

и сценарий из файла, если нужны `PUT` на диск и apply «с диска» без тела:

```text
-sf /path/to/scenario.xml
```

## Сборка для статики

```bash
npm run build
```

Артефакты в каталоге `dist/`. Раздавайте их любым статическим сервером и проксируйте `/api/v1` на тот же хост/порт, где слушает gossipper (или соберите с `VITE_API_BASE=https://gossipper.example:8080/api/v1` для прямых запросов в браузере — учитывайте CORS).

## Эндпоинты (для справки)

| Метод | Путь | Назначение |
|--------|------|------------|
| GET | `/api/v1/health` | `{ "status": "ok" }` |
| GET | `/api/v1/stats` | JSON сводки движка |
| GET | `/api/v1/scenario` | метаданные + XML (или встроенный сценарий) |
| PUT | `/api/v1/scenario` | запись XML в `-sf`; `?apply=true` — сразу hot-reload |
| POST | `/api/v1/scenario/apply` | тело `application/xml` или без тела (читает файл `-sf`) |
| GET / POST | `/api/v1/control` | чтение / изменение `rate`, `paused` |

Ошибки: JSON `{"error":"..."}` и HTTP-код.
