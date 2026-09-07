# llm-router: переход на Lua-плагины

Замена компилируемой adapter-системы (`llm-router-sdk`, Go-модули в `.workspace/`, `adapters.conf`) на однофайловые Lua-плагины, устанавливаемые из магазина в дашборде или вручную. Полная замена, без переходного периода с двумя системами.

---

## 1. Принципы

- **Один источник правды на каждую систему.** Retry/fallback — один движок, а не копия логики в `router` и в `agents`. Регистрация провайдеров — один сервис, а не "runtime providers из адаптеров" + "custom providers из БД" как два разных пути. Рендеринг любого lua-driven UI в дашборде — один компонент дерева, а не RenderHTML в одном месте и формы в другом.
- **Ядро agnostic к содержимому плагина.** Go не знает про OAuth, api-key или что угодно ещё — он знает про набор функций-хендлеров и универсальный (result, err)-контракт.
- **Безопасность — по умолчанию deny.** Никаких stdlib-глобалов, никакого сетевого доступа без явного `@allow_host`, никакого сырого HTML от третьей стороны в админке.

---

## 2. Новые сервисы и системы

### 2.1 `internal/services/luaplugin` — ядро исполнения плагинов

Отвечает за весь жизненный цикл плагина как кода: парсинг манифеста, статическую+sandboxed валидацию, хранение, реестр type key → плагин, исполнение хендлеров.

```
internal/services/luaplugin/
  service.go     LuaPluginService: Install/Update/Rollback/Delete/Enable/Disable, реестр type key -> PluginRecord
  manifest.go    парсинг заголовка "--- @tag value", проверка обязательных тегов, semver-сравнение @router_version
  sandbox.go     сборка _G: SkipOpenLibs=true + курируемый набор safe base/table/string/math, без io/os.execute/require/debug/load
  exec.go        компиляция в *lua.FunctionProto (кэш в памяти по PluginID+version), запуск в свежем *lua.LState на каждый вызов
  convert.go     маршалинг Go <-> Lua: ChatCompletionRequest/Response, StreamChunk, Credential, ModelInfo, UI-tree
  httpclient.go  create_http_client + SSRF-safe transport, привязанный к AllowHosts конкретного плагина
  storage.go     llm_router.storage.* поверх BucketPluginStorage
  errors.go      PluginInternalError, конвертация lua-side error-таблиц в Go
```

Хранилище: bbolt-бакет `BucketPlugins`, ключ — составной ID плагина (см. §5.2), значение — `PluginRecord`:

```go
type PluginRecord struct {
    ID            string    // "repo-name/author/plugin-name"
    DisplayName   string    // @plugin
    Author        string    // @author
    Version       string    // @version (версия плагина)
    RouterVersion string    // @router_version (минимальная требуемая версия роутера)
    Description   string
    AllowHosts    []string  // из @allow_host; ["*"] == wildcard
    Unsafe        bool      // true если wildcard
    TypeKeys      []string  // обнаружены при выполнении top-level кода на этапе установки
    Source        []byte    // текущий активный .lua файл
    History       []PluginVersionSnapshot // предыдущие версии для rollback (source+manifest+typekeys)
    Origin        PluginOrigin            // {RepoID, RepoPath} | {Manual: true}
    Enabled       bool
    InstalledAt   time.Time
    UpdatedAt     time.Time
}

type PluginVersionSnapshot struct {
    Version  string
    Source   []byte
    TypeKeys []string
}

type PluginOrigin struct {
    RepoID  string // пусто при ручной загрузке
    Path    string // путь внутри llm-router-plugins/
    Manual  bool
}
```

Реестр type key → плагин строится в памяти при старте (по всем `Enabled == true` записям) и обновляется при Install/Update/Rollback/Delete/Enable/Disable — тот же паттерн `onChanged`, что уже используется в `provider`/`credential` для инвалидации кэша моделей.

**Важное уточнение к решению "валидация только статическая, без исполнения" (см. переписку):** обнаружить `TypeKeys` можно только реально выполнив top-level код плагина — вызовы `llm_router.register(...)` заполняют таблицу регистрации во время исполнения, это не AST-анализ. Это не противоречит принципу: сам top-level код плагина исполняется в уже полностью настроенном sandbox (никаких `io`/`os.execute`, HTTP-клиент уже привязан к `AllowHosts`, извлечённым из манифеста заранее). Значит выполнение top-level кода на шаге установки безопасно по построению — оно ничего не может сделать за пределами того, что плагину и так будет разрешено при обычной работе. Отдельно: сами хендлеры (`complete`, `auth_initiate` и т.д.) на этом шаге не вызываются — только сам факт загрузки модуля и вызовы `register()`.

### 2.2 `internal/services/pluginrepo` — магазин плагинов

Отвечает за работу с внешними репозиториями плагинов, независимо от `luaplugin` (тот ничего не знает про GitHub).

```go
type RepoProvider interface {
    Kind() string // "github", "generic-index"
    ListPluginFiles(ctx context.Context, ref RepoRef) ([]RepoFile, error) // сканирует llm-router-plugins/
    FetchFile(ctx context.Context, ref RepoRef, path string) ([]byte, error)
    ReadMe(ctx context.Context, ref RepoRef) (content string, ok bool, err error)
    License(ctx context.Context, ref RepoRef) (content string, ok bool, err error)
}
```

- **GitHub-реализация**: REST Contents API (`GET /repos/{owner}/{repo}/contents/{path}`), рекурсивный обход `llm-router-plugins/`. Никакого `git clone`, никакой зависимости от установленного `git` в системе.
- **Generic-реализация**: репозиторий — произвольный HTTP(S)-эндпоинт, отдающий JSON-индекс файлов (`{"plugins": [{"path": "...", "url": "..."}], "readme_url": "...", "license_url": "..."}`). Позволяет подключать self-hosted сборники без привязки к GitHub.
- Приватные репозитории — вне первой версии: только публичные, без хранения токенов доступа.

Хранилище: bbolt-бакет `BucketPluginRepos` — список добавленных пользователем репозиториев `{ID, Kind, Owner, Repo, IndexURL, AddedAt}`. Поиск по магазину — агрегирует `ListPluginFiles` по всем добавленным репозиториям + встроенный дефолтный список (опционально).

Install-flow: `pluginrepo` скачивает файл → передаёт байты в `LuaPluginService.Install(source []byte, origin PluginOrigin)` → тот делает манифест-парсинг + sandboxed dry-run + запись в `BucketPlugins`.

### 2.3 `internal/services/retry` — единый retry/fallthrough движок

Заменяет два независимых цикла (`router.Service` — перебор credential'ов; `agents.Adapter` — перебор моделей агента).

```go
package retry

type Classifiable interface{ Retryable() bool }

func Classify(err error) bool // errors.As(err, &Classifiable{}) -> .Retryable()

type Candidate[T any] struct {
    Label string
    Run   func(ctx context.Context) (T, error)
}

func Run[T any](ctx context.Context, candidates []Candidate[T], logger *slog.Logger) (T, error)

type StreamCandidate struct {
    Label string
    Run   func(ctx context.Context, w io.Writer) error
}

func RunStream(ctx context.Context, candidates []StreamCandidate, w io.Writer, logger *slog.Logger) error
```

Единая точка, что считается retryable: `ProviderError{Type: RateLimit|QuotaExceeded}` и `PluginInternalError` (падение плагина retryable — переход на следующего candidate). Всё остальное — terminal, немедленный возврат ошибки.

- `router.Service.Complete/CompleteStream` строит `[]Candidate` = credential'ы одного provider/model, отдаёт `retry.Run`/`retry.RunStream`.
- `agents.Adapter.Complete/CompleteStream` строит `[]Candidate` = модели агента (в порядке priority/decision-model), каждый `Run` внутри вызывает `router.Service.Complete` (у которого свой вложенный `retry.Run` по credential'ам). Decision-model остаётся отдельным шагом *до* построения списка кандидатов — он только определяет порядок, в сам retry-движок не входит.
- Сообщение "все кандидаты исчерпаны" формируется retry-движком один раз, одинаково для обоих потребителей.

### 2.4 Унификация регистрации провайдеров (в `internal/services/provider`)

`GetDefaultProviders()` удаляется. Провайдер — всегда явная БД-запись, независимо от того, `custom` это, `agents` или lua-плагин.

```go
type ProviderInstance struct {
    ID        string
    Name      string
    TypeKey   string         // plugin type key | "custom" | "agents"
    Qualifier string
    Config    map[string]any // произвольная конфигурация, форма для неё описывается config_schema плагина
    IconURL   string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

Единый `repository.Repository[ProviderInstance]` вместо раздельных "runtime providers" и `CustomProvider`. CRUD (`Create/Update/Delete/Get/List/GetByType`) — один путь для всех типов, с тем же `SetOnChanged` → `modelInfoSvc.InvalidateProvider`, что уже работает для `custom`. Для типов, у которых нет `config_schema` (в том числе встроенный `custom`), дашборд использует статичную Go-side схему (см. §6.2) — интерфейс тот же.

`internal/adapters/generic` (`custom`) остаётся Go-кодом как есть по сути (простой OpenAI-совместимый passthrough не выигрывает от переписывания на Lua), но переключается на `ProviderInstance`/`Config["base_url"]` вместо отдельного `CustomProvider`-репозитория и `generic.SetResolver`.

---

## 3. Что удаляется

| Элемент | Причина |
|---|---|
| `.workspace/llm-router-sdk` как отдельный Go-модуль | сливается в основной модуль (типы уходят в `internal/models`, retry-типы — в `internal/services/retry`) |
| `sdk.Adapter` интерфейс, `sdk.Register/Lookup/Registered` | заменяется реестром type key в `LuaPluginService` |
| `AuthType`, `sdk.AuthTypeAPIKey/OAuth2/Basic`, поле `Provider.AuthType` | auth — implementation detail плагина, ядру не нужен |
| `GetDefaultProviders()` | провайдеры всегда явные БД-записи (см. §2.4) |
| `GetAuthFlow`, `AuthFlowHandler`, `AuthFlowContext`, `AuthFlowState.RenderHTML` | заменяется `auth_initiate`/`auth_step` + UI-дерево (§4.8); `RenderHTML` как сырой HTML от третьей стороны — недопустимый вектор XSS в админке |
| `.workspace/llm-router-adapter-google`, `-kiro`, `-opencode-zen` как Go-модули | переписываются в Lua-плагины, поставляются как встроенные (bundled) записи в `BucketPlugins` при первом запуске новой версии |
| `adapters.conf`, генерация `go.work` + `adapters.go` | Go-модульная регистрация адаптеров не нужна |
| `make start`/`make publish` шаги regen workspace для адаптеров | тот же аргумент |
| `models.CustomProvider`, `db.BucketCustomProviders` | заменяется `ProviderInstance` / `BucketProviderInstances` |
| `Credential.Data map[string]string` | заменяется `map[string]any` (нужно для нестроковых значений: expiry-таймстампы и т.п. в произвольных auth-схемах) |

---

## 4. Затронутые сервисы: план рефакторинга

| Сервис | Изменение |
|---|---|
| `internal/services/provider` | `ProviderInstance` вместо runtime+custom split; `ResolveAdapter` → `ResolvePlugin` (возвращает `*luaplugin.PluginRecord` + `*ProviderInstance`); удаление `sdk`-реэкспортов |
| `internal/services/credential` | `Data map[string]string` → `map[string]any`; `ValidateCredentials` теперь зовёт lua-хендлер через `LuaPluginService.Call(typeKey, "validate_credentials", data)` |
| `internal/services/router` | цикл по credential'ам заменяется на построение `[]retry.Candidate` + `retry.Run`; вызов адаптера заменяется вызовом `LuaPluginService.Complete/CompleteStream`; классификация ошибок идёт через `retry.Classify` |
| `providers/agents` | цикл по моделям заменяется на `[]retry.Candidate`/`retry.StreamCandidate` + `retry.Run`/`retry.RunStream`; остальная логика (decision-model, инъекция инструкций) не меняется |
| `internal/services/modelinfo` | без структурных изменений; источник ошибок при `GetModelInfos` теперь включает `PluginInternalError` |
| `internal/services/maintenance` | `NeedsRefresh`/`RefreshCredential` вызываются через `LuaPluginService`; отсутствие хендлера в плагине трактуется как "рефреш не поддерживается" |
| `internal/adapters/generic` | остаётся Go-кодом; переключается на `ProviderInstance.Config["base_url"]`, регистрирует статичную `config_schema`-эквивалентную форму на Go-стороне (см. §6.2) |
| `internal/dashboard` | новые файлы: `plugins.go` (CRUD установленных плагинов), `plugin_store.go` (поиск/установка из репозиториев); `providers.go`/`credentials.go` переписываются под generic `ProviderInstance` + UI-дерево вместо хардкод-полей |
| `internal/repository`, `internal/db` | новые бакеты: `BucketPlugins`, `BucketPluginRepos`, `BucketPluginStorage`, `BucketProviderInstances` (замена `BucketCustomProviders`) |
| `internal/models` | вносятся wire-типы из бывшего sdk (`ChatCompletionRequest/Response`, `StreamChunk`, `ModelInfo`, `ProviderError`+`ErrorType` с методом `Retryable()`), `PluginInternalError`, `UINode` (§4.8) |
| `cmd/root.go`, `Makefile`, `scripts/` | убираются шаги, завязанные на `adapters.conf`/workspace-регенерацию Go-модулей адаптеров |
| `AGENTS.md` §3, §7 | секция про `.workspace/` adapter-модули заменяется описанием `.workspace/`-эквивалента для разработки Lua-плагинов (или убирается, если разработка плагинов не требует workspace вообще — плагин это один файл, который можно писать где угодно) |

---

## 5. Спецификация Lua API

### 5.1 Sandbox

- Движок: `gopher-lua` (`github.com/yuin/gopher-lua`), `lua.NewState(lua.Options{SkipOpenLibs: true})`.
- Открываются вручную только: `base` (курированное подмножество — `pairs, ipairs, next, select, type, tostring, tonumber, error, pcall, xpcall, setmetatable, getmetatable, rawget, rawset, rawequal, rawlen, assert`, **без** `dofile, loadfile, load, loadstring, require, collectgarbage, print` в обычном виде), `table.*`, `string.*`, `math.*`, ограниченный `os` (`os.time, os.clock, os.date` — без `execute, getenv, remove, rename, tmpname, exit`).
- `debug` — не открывается никогда (позволяет обходить sandbox через upvalue-манипуляции).
- `print` переопределён: пишет в структурированный лог плагина (виден в дашборде на странице плагина), с обрезкой по размеру.
- Дополнительно в `_G`: `json` (`json.encode`, `json.decode`) — плагинам постоянно нужно собирать/разбирать payload'ы, которые не совпадают 1:1 с внутренними wire-типами (см. пример в §7).

### 5.2 Регистрация

```lua
llm_router.register(type_key, {
  complete           = function(ctx, credential, request) ... end,         -- обязателен
  complete_stream    = function(ctx, credential, request, emit) ... end,   -- опционален
  validate_credentials = function(data) ... end,                          -- опционален
  get_model_infos    = function(ctx, credential, provider_config) ... end, -- опционален
  needs_refresh      = function(credential) ... end,                      -- опционален
  refresh_credential = function(ctx, credential) ... end,                 -- опционален
  config_schema      = function() ... end,                                -- опционален
  credential_schema  = function() ... end,                                -- опционален
  auth_initiate      = function(ctx) ... end,                             -- опционален
  auth_step          = function(ctx, input) ... end,                      -- опционален
})
```

Один файл может вызвать `register` несколько раз с разными `type_key` — все они принадлежат одному `PluginID`.

Поведение при отсутствии опциональных хендлеров — фиксированное, единое для всех плагинов (не настраивается):

| Хендлер | Если не объявлен |
|---|---|
| `complete_stream` | роутер эмулирует стрим: вызывает `complete`, отдаёт результат одним chunk'ом + `[DONE]` |
| `validate_credentials` | любые данные принимаются как есть |
| `get_model_infos` | у провайдера нет моделей в кэше метаданных; прямая маршрутизация по `type/model-name` по-прежнему работает |
| `needs_refresh`/`refresh_credential` | credential считается не обновляемым (аналог старого `ErrNoRefreshNeeded`) |
| `config_schema`/`credential_schema` | дашборд показывает fallback: одно текстовое поле "raw JSON" |
| `auth_initiate` | создание credential идёт только через одношаговую `credential_schema`-форму, без визарда |

### 5.3 Контракт ошибок

Каждый request-time хендлер возвращает `(result, err)`. `err` — таблица:

```lua
{ type = "rate_limit" | "quota_exceeded" | "auth" | "upstream" | "timeout" | "invalid_request",
  message = "человекочитаемое сообщение",
  retry_after = <unix_timestamp> }  -- обязателен для quota_exceeded
```

Любой не пойманный runtime-сбой (реальная Lua-паника, `error()` без соблюдения формата, обращение к nil, синтаксическая поломка) перехватывается на границе Go/Lua (protected call) и превращается в `PluginInternalError{PluginID, TypeKey, Cause}` — `Retryable() == true`, обрабатывается retry-движком (§2.3) как транзиентная ошибка.

### 5.4 Формы данных (Go ↔ Lua)

- `credential` = `{ id = "...", data = { ...произвольные ключи... } }` (значения — string/number/boolean/nested table).
- `request` (аргумент в `complete`/`complete_stream`) — таблица, зеркалящая JSON `ChatCompletionRequest`: `model, messages, tools, tool_choice, max_tokens, temperature, top_p, ...`. `stream`-флаг отсутствует — стрим/не-стрим определяется тем, какой хендлер вызван.
- `messages[i]` = `{ role=, content=, tool_calls=, tool_call_id=, name= }`.
- Возврат из `complete` — таблица той же формы, что `ChatCompletionResponse` (`id, model, choices[1].message.{role,content,tool_calls}, choices[1].finish_reason, usage.{prompt_tokens,completion_tokens,total_tokens}`). Несоответствие формы = `PluginInternalError` (schema violation), без попытки угадать/докрутить руками.
- `emit(chunk)` в `complete_stream` — `chunk` той же формы, что `StreamChunk`. Go сам форматирует `data: ...\n\n` и завершающий `data: [DONE]\n\n` — плагин никогда не пишет сырой SSE-текст.
- `get_model_infos` возвращает массив `{ name=, display_name=, rpm=, tpm=, rpd=, context_window=, max_tokens= }`.

### 5.5 HTTP-клиент

```lua
local client = llm_router.create_http_client({
  timeout_ms = 60000,          -- опционально, дефолт применяется автоматически
  proxy_settings = {           -- зарезервировано, сейчас no-op (прямое соединение)
    use_proxy = false,
    country_whitelist = { "US", "GB" },  -- взаимоисключимо с blacklist
    country_blacklist = { "RU" },
  },
})

-- буферизованный запрос
local resp, err = client:request({
  method = "POST",
  url = "https://api.example.com/v1/chat/completions",
  headers = { ["Content-Type"] = "application/json" },
  body = json.encode(payload),
})
-- resp = { status = 200, headers = { ... }, body = "..." }

-- построчное чтение сырого стрима (для нестандартных SSE/event-форматов, напр. AWS event-stream у Kiro)
local err = client:stream({
  method = "POST", url = "...", headers = { ... }, body = "...",
  on_line = function(line) ... end,
})
```

Каждый URL перед dial проходит SSRF-проверку (§5.6), основанную на `AllowHosts` данного плагина. Дополнительно: лимит на размер тела ответа (`io.LimitReader`), таймаут по умолчанию, если не задан.

### 5.6 SSRF-защита

Применяется на каждом hop'е (не только к исходному URL):

1. Hostname из URL сверяется с `@allow_host` плагина (точное совпадение, case-insensitive). При wildcard (`@allow_host *`) эта проверка пропускается.
2. DNS резолвится вручную внутри `DialContext`; каждый полученный IP проверяется на принадлежность заведомо небезопасным диапазонам — `10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16 (включая 169.254.169.254), 100.64.0.0/10, ::1, fc00::/7, fe80::/10`. **Эта проверка не отключается wildcard'ом** — "полный доступ в интернет" не означает доступ к внутренней сети хоста или облачным metadata-эндпоинтам.
3. Dial идёт на уже проверенный IP напрямую (TLS `ServerName` — по исходному hostname), не на hostname повторно — закрывает DNS rebinding между проверкой и подключением.
4. `CheckRedirect` повторяет проверки (1)+(2)+(3) на каждый `Location` редиректа; при отказе — явная ошибка в лог-контракте (`type = "upstream"`), не паника.
5. Кастомный `Host`-заголовок (если API это вообще позволяет) сверяется с тем же allow-list, что и URL — иначе это обход через сервер, доверяющий заголовку `Host`.
6. Разрешённые схемы — только `http`/`https`.

### 5.7 Persistent storage

```lua
llm_router.storage.set(scope, key, value)          -- value сериализуется в JSON
local value, err = llm_router.storage.get(scope, key)
llm_router.storage.delete(scope, key)
```

`scope` — явная строка, которую задаёт сам плагин (типично `"credential:" .. credential.id`), а не скрытое ambient-состояние. Нужен, потому что state пересоздаётся на каждый вызов (см. §5.1) — единственный способ для плагина сохранить что-то между запросами (например обменянный access token) — явно положить в storage. Бэкенд — `BucketPluginStorage`, ключ `PluginID + scope + key`.

### 5.8 UI-дерево (`config_schema`, `credential_schema`, `auth_initiate`/`auth_step`)

Единый формат для всего lua-driven UI в дашборде — заменяет и формы конфигурации провайдера, и старый `RenderHTML` из auth-flow. Корень — список детей; вложенность — через атрибут `content`, тоже список.

```lua
{
  { type = "text", text = "Вставьте ключ из личного кабинета провайдера." },
  { type = "input", name = "api_key", input_type = "password", label = "API Key", required = true },
  { type = "banner", variant = "error", text = "Ключ слишком короткий" },  -- опционально, для повторного рендера с ошибкой
  {
    type = "group",
    content = {
      { type = "select", name = "region", label = "Регион", options = { "us", "eu" } },
      { type = "checkbox", name = "enable_beta", label = "Beta-модели" },
    },
  },
  { type = "link", text = "Получить ключ", url = "https://provider.example.com/keys" },
  { type = "button", text = "Продолжить", form_action = "submit" },
}
```

Минимальный набор `type`: `text` (label), `input` (`input_type`: text/password/number), `select`, `checkbox`, `button` (`form_action`: `submit`/`cancel`/произвольное имя действия), `link` (открывает `url` в новой вкладке — замена `ExternalURL`), `banner` (`variant`: info/error/success), `group` (чистый контейнер).

Submit собирает `{ [name] = value, ... }` по всем `input/select/checkbox` на экране и вызывает `auth_step(ctx, { action = <form_action кнопки>, values = {...} })`. Возврат `auth_step`/`auth_initiate` — один из трёх вариантов, различаемых по форме таблицы:

```lua
return { render = { ...UI-дерево... } }             -- показать следующий экран
return { redirect_url = "https://oauth-provider/..." } -- открыть внешний URL (OAuth), ждать колбэк
return { credentials = { access_token = "...", refresh_token = "..." } } -- флоу завершён, сохранить credential
```

Промежуточное состояние многошагового флоу (например `code_verifier` в PKCE) — через `llm_router.storage` с `scope = "auth_flow:" .. ctx.flow_id`, а не через отдельный `AuthStore`-интерфейс — тот же механизм, что и для рефреша токенов.

---

## 6. Спецификация метаданных и репозиториев

### 6.1 Манифест

Сплошной блок строк с префиксом `---` в самом начале файла (до первой строки без этого префикса):

```lua
--- @plugin OpenCode Zen
--- @author denchInside
--- @version 1.2.0
--- @router_version 0.0.4
--- @description OpenAI/Anthropic/Google совместимый провайдер OpenCode Zen
--- @allow_host opencode.ai
```

| Тег | Обязателен | Повторяемость | Значение |
|---|---|---|---|
| `@plugin` | да | нет | отображаемое имя |
| `@author` | да | нет | автор |
| `@version` | да | нет | semver, версия плагина (для апдейтов в магазине) |
| `@router_version` | да | нет | semver, минимальная требуемая версия llm-router |
| `@description` | нет | нет | одна строка |
| `@allow_host` | да, ≥1 | да | конкретный хост, либо единственное значение `*` (wildcard = unsafe) |
| `@license` | нет | нет | SPDX-идентификатор, информационно |

Проверка `@router_version`: `router.CurrentVersion >= manifest.RouterVersion` (сравнение major.minor.patch), иначе установка отклоняется с явной ошибкой.

### 6.2 Идентичность плагина

Составной ID: `<repo-name>/<author>/<plugin-name>` (ровно в этом порядке), например `llm-router-plugins/AnatolyDmitrievich/kiro-oauth`. Уникален в рамках инсталляции; конфликт между двумя репозиториями с одинаковым `author/plugin-name` невозможен, потому что `repo-name` — часть ключа. Ручная загрузка файла (`Manual: true`) использует `manual/<author>/<plugin-name>` как `repo-name`.

`ProviderInstance`/`Credential` привязаны к этому составному ID, не к конкретной версии — откат на предыдущую версию плагина (§2.1, `History`) не требует пересоздания credential'ов.

### 6.3 Структура репозитория

```
README.md                  опционально, показывается в карточке репозитория в магазине
LICENSE.md                 опционально, показывается там же
llm-router-plugins/
  kiro-oauth.lua
  opencode-zen.lua
  ...
```

Файлы — только плоско внутри `llm-router-plugins/`, без подпапок (плагин принципиально однофайловый, вложенность не нужна).

### 6.4 Обновления

Магазин периодически (и по кнопке "проверить обновления") сравнивает `@version` установленного плагина с версией того же пути в репозитории-источнике (`Origin.RepoID + Origin.Path`). При расхождении — бейдж "доступно обновление" в списке установленных плагинов; апдейт — обычный `Install` поверх существующего `PluginID`, старая версия уходит в `History`.

---

## 7. Обновление фронтенда

Новые/переписываемые компоненты в `web/src`:

| Компонент | Назначение |
|---|---|
| `pages/PluginStore.svelte` | поиск по добавленным репозиториям + дефолтному списку, карточка репозитория (README/LICENSE), кнопка установки, кнопка "добавить свой репозиторий" (URL), кнопка "загрузить файл вручную" |
| `pages/PluginManager.svelte` | список установленных плагинов: версия, allow_hosts (с явным бейджем "небезопасный — неограниченный доступ в интернет" при wildcard), enable/disable, удаление, откат на предыдущую версию, лог последних крашей (`PluginInternalError`) |
| `components/ui/DynamicForm.svelte` | рендерер UI-дерева из §5.8 — единственное место, которое превращает lua-таблицу в реальные Svelte-компоненты; используется и для `config_schema` (создание провайдера), и для `credential_schema`/`auth_initiate`/`auth_step` (добавление credential) |
| `components/wizards/CustomProviderWizard.svelte` | переписывается: вместо хардкод-полей (`name/base_url/icon_url`) рендерит `DynamicForm` по `config_schema` конкретного type key (для `custom` — статичная Go-side схема с теми же тремя полями, отдаётся тем же API-контрактом) |
| `components/CredentialWizard` (новый, вместо текущей логики в `Providers.svelte`) | цикл `auth_initiate` → `DynamicForm` → submit → `auth_step` → (следующий экран \| redirect \| сохранение credential), заменяет прежний RenderHTML-based flow |

`web/openapi.yaml` — новые эндпоинты: `GET/POST /plugins`, `GET /plugins/{id}`, `POST /plugins/{id}/enable|disable|rollback`, `DELETE /plugins/{id}`, `GET/POST /plugin-repos`, `GET /plugin-repos/{id}/search`, `POST /plugins/install-from-repo`, `POST /plugins/install-file`; существующие `/providers`, `/credentials` меняют форму ответа под `ProviderInstance`/`Config`. `web/src/lib/generated/` — регенерируется, руками не трогается (см. AGENTS.md).

---

## 8. Пример: переписываем `opencode-zen` на Lua-плагин

Исходный Go-адаптер (`.workspace/llm-router-adapter-opencode-zen`) — ~550 строк: два upstream-эндпоинта (`/chat/completions` для большинства моделей, `/responses` для `gpt-*`/`muse-spark`/`grok-*`), ручная сборка SSE для не-стримингового fallback, кастомные заголовки, классификация ошибок по статус-коду. Ниже — эквивалент на новом API (стриминг для `/responses`-веток сокращён до основных событий ради читаемости примера; полная версия следует тому же паттерну).

```lua
--- @plugin OpenCode Zen
--- @author TheSlopMachine
--- @version 1.0.0
--- @router_version 0.0.4
--- @description OpenAI/Anthropic/Google совместимый бесплатный провайдер OpenCode Zen
--- @allow_host opencode.ai

local BASE_URL = "https://opencode.ai/zen/v1"

local function endpoint_for_model(model)
  local m = model:lower()
  if m:match("^gpt%-") or m:match("muse%-spark") or m:match("^grok%-") then
    return "/responses"
  end
  return "/chat/completions"
end

local function opencode_headers(extra)
  local h = {
    ["User-Agent"] = "llm-router-opencode-zen-plugin/1.0.0",
    ["x-opencode-client"] = "opencode",
    ["x-opencode-project"] = "proj_llm-router",
  }
  for k, v in pairs(extra or {}) do h[k] = v end
  return h
end

-- статус-код -> наш error-контракт (эквивалент errors.go оригинала)
local function classify_error(status, body)
  local message = body
  local ok, parsed = pcall(json.decode, body)
  if ok and parsed and parsed.error and parsed.error.message then
    message = parsed.error.message
  end
  if status == 401 or status == 403 then
    return nil, { type = "auth", message = message }
  elseif status == 429 then
    local t = message:lower():find("quota")
    return nil, { type = t and "quota_exceeded" or "rate_limit", message = message, retry_after = os.time() + 60 }
  elseif status == 408 or status == 504 then
    return nil, { type = "timeout", message = message }
  elseif status >= 500 then
    return nil, { type = "upstream", message = message }
  end
  return nil, { type = "invalid_request", message = message }
end

local function api_key_of(credential)
  return credential.data.api_key or credential.data.access_token or ""
end

llm_router.register("opencode-zen", {

  credential_schema = function()
    return {
      { type = "text", text = "Бесплатные модели работают без ключа. Ключ нужен только для платных моделей." },
      { type = "input", name = "api_key", input_type = "password", label = "API Key (опционально)" },
      { type = "button", text = "Сохранить", form_action = "submit" },
    }
  end,

  validate_credentials = function(data)
    local key = data.api_key
    if key ~= nil and key ~= "" and #key < 20 then
      return false, { type = "invalid_request", message = "api_key: минимум 20 символов" }
    end
    return true
  end,

  get_model_infos = function(ctx, credential, provider_config)
    local client = llm_router.create_http_client({})
    local resp, err = client:request({
      method = "GET", url = BASE_URL .. "/models",
      headers = opencode_headers({ Authorization = api_key_of(credential) ~= "" and ("Bearer " .. api_key_of(credential)) or nil }),
    })
    if err then return nil, err end
    if resp.status ~= 200 then return classify_error(resp.status, resp.body) end

    local parsed = json.decode(resp.body)
    local infos = {}
    for _, m in ipairs(parsed.data or {}) do
      table.insert(infos, {
        name = m.id, display_name = m.id,
        context_window = 200000, max_tokens = 32000,
        rpm = 60, tpm = 100000, rpd = 500,
      })
    end
    return infos
  end,

  complete = function(ctx, credential, request)
    local model = request.model:match("([^/]+)$") -- короткое имя без "opencode-zen/"
    local endpoint = endpoint_for_model(model)
    local client = llm_router.create_http_client({})

    local payload = { model = model, messages = request.messages, stream = false }
    if request.max_tokens and request.max_tokens > 0 then payload.max_tokens = request.max_tokens end
    if request.temperature and request.temperature > 0 then payload.temperature = request.temperature end

    local api_key = api_key_of(credential)
    local resp, err = client:request({
      method = "POST", url = BASE_URL .. endpoint,
      headers = opencode_headers({
        ["Content-Type"] = "application/json",
        Authorization = api_key ~= "" and ("Bearer " .. api_key) or nil,
      }),
      body = json.encode(payload),
    })
    if err then return nil, err end
    if resp.status ~= 200 then return classify_error(resp.status, resp.body) end
    return json.decode(resp.body)
  end,

  -- complete_stream не объявлен: роутер эмулирует стрим поверх complete() одним chunk'ом —
  -- для бесплатных моделей это ровно то, что делал оригинальный Go-клиент вручную
  -- (upstream-стриминг у free-моделей был признан ненадёжным и заменён синтетическим SSE).
})
```

Что упрощается по сравнению с оригиналом:
- SSE-кодирование (`data: ...\n\n`, `[DONE]`) больше не пишется руками — либо через `emit()` в `complete_stream`, либо (как в этом примере) через встроенный fallback "один chunk поверх complete".
- Ветка `/responses` с полным разбором `output_item.added`/`function_call_arguments.delta`/`response.completed` опущена ради длины примера, но реализуется тем же способом: `client:stream({..., on_line = function(line) ... end})` для чтения построчного SSE от `/responses`, разбор события через `json.decode`, вызов `emit(...)` на каждый значимый event — один в один методика, что и в оригинальном `chatToResponses`.

### 8.1 Как API покрывает более сложный случай — Kiro

`llm-router-adapter-kiro` — OAuth2 (`authflow.go`) + бинарный AWS event-stream формат (`eventstream.go`), не обычный SSE. На новом API:
- OAuth-визард — `auth_initiate`/`auth_step` (§5.8) вместо `RenderHTML`; промежуточные значения (`code_verifier`, `state`) — через `llm_router.storage` с `scope = "auth_flow:" .. ctx.flow_id`; финальный шаг возвращает `{ credentials = { access_token=, refresh_token=, expires_at= } }`.
- Проактивный рефреш — `needs_refresh`/`refresh_credential`, сверяющие `credential.data.expires_at` с текущим временем.
- Бинарный event-stream — `client:stream({..., on_line = nil, on_chunk = function(bytes) ... end})` (сырые байты, не построчно — для этого случая HTTP-клиенту нужен режим "raw chunk", а не только "raw line"; учтено в §5.5 как второй режим `on_chunk` наравне с `on_line`). Разбор AWS event-stream framing (length-prefixed бинарные фреймы) — обычная Lua-арифметика над `string.byte`, ровно тот же алгоритм, что сейчас в `eventstream.go`, просто на Lua.

Полная построчная переписка (~700 строк между `adapter.go`/`authflow.go`/`eventstream.go`) в объём этого документа не входит — API уже показал на opencode-zen, что все примитивы (HTTP-клиент, error-контракт, UI-дерево, storage) достаточны для этого случая; сама переписка — отдельная задача при реализации.

---

## 9. Порядок реализации

1. `internal/models` — новые wire-типы (перенос из sdk), `ProviderError.Retryable()`, `PluginInternalError`, `UINode`.
2. `internal/services/retry` — движок + unit-тесты на классификацию.
3. `internal/services/luaplugin` — sandbox, манифест, конвертация типов, HTTP-клиент с SSRF, storage. Наибольший по объёму кусок.
4. `internal/services/provider` — `ProviderInstance`, слияние runtime+custom путей.
5. Переключение `router`/`agents`/`credential`/`maintenance` на `luaplugin` + `retry`.
6. `internal/services/pluginrepo` — GitHub Contents API + generic-индекс.
7. `internal/dashboard` — новые эндпоинты (`plugins.go`, `plugin_store.go`), правки `providers.go`/`credentials.go`.
8. Фронтенд: `DynamicForm`, `PluginStore.svelte`, `PluginManager.svelte`, переписка визардов.
9. Переписка `google`/`kiro`/`opencode-zen` на Lua, поставка как bundled-плагины.
10. Удаление `.workspace/llm-router-sdk`, `.workspace/llm-router-adapter-*`, `adapters.conf`, соответствующих шагов в `Makefile`/`scripts/`, правка `AGENTS.md`.
