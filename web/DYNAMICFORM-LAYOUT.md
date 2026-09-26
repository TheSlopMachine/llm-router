# DynamicForm layout-деревья: Lua → backend → REST → frontend → values обратно

Lifecycle одного дерева: `Lua manifest → register → handlerCall → parseUINodes/parseAuthResult → providerSvc → dashboard REST → api.ts → DynamicForm → POST values → Lua`.

## 1. Lifecycle

### 1.1 Lua: manifest + регистрация слотов
- Манифест `--- @` в байте 0, валидация: `internal/services/luaplugin/manifest.go`.
- Слоты: `internal/services/luaplugin/handler_names.go:9-27` — `complete` required, опциональные `config_schema`, `credential_schema`, `auth_initiate`, `auth_step` (+ остальные).
- Регистрация проверяет только "есть функция": `internal/services/luaplugin/sandbox.go:225-235` — каждый опциональный слот из `OptionalHandlerNames()` обязан быть `*lua.LFunction`, если объявлен. Неизвестные имена игнорируются (док: `docs/PLUGIN-API.md:76-79`).
- UI-слоты вызываются без сетевого контекста; свежий песочный `LState` на каждый вызов: `internal/services/luaplugin/exec.go:131-145`.

### 1.2 Backend: вызов + валидация (`internal/services/luaplugin/handlers.go`, `uinodes.go`)
- `Schema(typeKey, handler)` (`handlers.go:592-622`): только `config_schema`/`credential_schema`; `nil` от Lua = `nil, nil` (нет схемы); отсутствие хендлера = `notFoundError` → `ErrHandlerNotFound`. Нарушение = `recordCrash` + `PluginInternalError`.
- `AuthInitiate(ctx: {flow_id})` (`handlers.go:626-650`), `AuthStep(ctx, {action, values})` (`handlers.go:653-684`): `values==nil` → `{}`; `values` конвертируется `toLuaValue` (`convert.go:24-77`).
- `parseAuthResult` (`uinodes.go:12-54`): ровно один из `render` / `redirect_url` (non-empty string) / `credentials` (string-keyed table через `marshalLua`+`unmarshalTo`). `render` валидируется через `parseUINodes`.
- `parseUINodes` (`uinodes.go:66-96`): корень — array-table; `depth>8` → ошибка; `n>200` **на один список** (не на всё дерево, см. §4.7).
- `parseUINode` (`uinodes.go:98-333`): строковые поля читаются только как `lua.LString` (иначе `""`); неизвестные ключи Lua молча отбрасываются (в `models.UINode` есть `Extra json:"-"`, `internal/models/models.go:1152`, никуда не сериализуется).
- Дефолты, проставляемые backend: `button.form_action="submit"` (`uinodes.go:231-233`); `flow`: `direction=vertical gap=md align=stretch justify=start wrap=true` (`uinodes.go:257-283`, `128-134`); `grid.gap=md` (`uinodes.go:294-296`); `spacer.grow=true` (`uinodes.go:139-141`).
- Любая ошибка валидации = запись в crash-буфер (последние 50, `service.go:644-653`) + `PluginInternalError` → на REST превращается в `500 {"error":...}`.

### 1.3 provider.Service: Go-деревья и fallback (`internal/services/provider/schemas.go`)
- `SupportsAuthFlow` (`schemas.go:51-59`): `true` только если Lua-тип объявляет `auth_initiate`. Go-адаптеры — всегда `false`.
- `ConfigSchema` (`schemas.go:71-92`): `custom` — статическое дерево (text + `base_url` input); `virtual` — `nil, nil`; Lua — делегация; `ErrHandlerNotFound` → `nil, nil` = "raw JSON fallback".
- `CredentialSchema` (`schemas.go:95-121`): `custom` — text + `api_key` (password) + Save; `virtual` — text + `agent_id` + Save; Lua — делегация; `ErrHandlerNotFound` → `nil, nil`.

### 1.4 Dashboard REST (`internal/dashboard/handler.go:93-95,141-142`)
| Метод | Хендлер | Ответ |
|---|---|---|
| `GET .../providers/{id}/config-schema` | `providers.go:359-375` | `{nodes}` или `{nodes:null, fallback:"raw_json"}` |
| `GET .../providers/{id}/credential-schema` | `providers.go:438-454` | то же |
| `GET .../type-schemas?kind=config\|credential&type_key=` | `providers.go:390-425` | то же; `kind` иное → 400 |
| `POST .../auth/initiate {provider_id}` | `authflow.go:29-67` | `renderAuthResult`; Go-тип или нет `auth_initiate` → **409** |
| `POST .../auth/step {provider_id,flow_id,action,values}` | `authflow.go:82-120` | `renderAuthResult`; нет `auth_step` → 400; `flow_id==""` → 400 |
- `renderAuthResult` (`authflow.go:122-160`): `render` → `{status:"render",nodes,flow_id,provider_id}`; `redirect` → `{status:"redirect",redirect_url,flow_id,provider_id}`; `credentials` → `credSvc.Add` + `{status:"complete",message,credential_id}` (ошибка сохранения → 500). Пустой результат → 500.
- OpenAPI отражён в `web/openapi.yaml:662,850,906,2276,2312,3034`; сгенерированные типы — `web/src/lib/generated/api-types.ts` (компоненты используют ручные `web/src/lib/types.ts:214-256`).

### 1.5 Frontend: fetch + render (`web/src/lib/api.ts`, `types.ts`, `DynamicForm.svelte`)
- `api.providers.configSchema/credentialSchema/configSchemaForType` (`api.ts:122-130`, raw `fetch`+`assertOk`); `api.auth.initiate/step` (`api.ts:133-139`, `postJson`).
- Типы: `UINode` (`types.ts:214-239`), `SchemaResponse {nodes: UINode[]|null, fallback?}` (`types.ts:241-244`), `AuthStepResponse {status: render|redirect|complete, nodes?, redirect_url?, flow_id?, provider_id?, message?, credential_id?}` (`types.ts:248-256`).
- `DynamicForm.svelte:1-22` — props `nodes`, `values=$bindable({})`, `busy`. `setValue` (`:24-26`) пишет `values[name]`. Кнопки inline **никогда не рендерятся**: `collectButtons` (`:61-71`) рекурсивно собирает `type==="button"` в порядке дерева; `buttonVariant` (`:73-79`): явный `variant` иначе `cancel|restart→secondary`, всё остальное → `primary`.
- Хосты владеют футером:
  - `ProviderCredentialWizard.svelte:66-115` — `loadFlow` (`auth.initiate`) → `wizard`, `redirect` (`window.open` + текст-баннер), `complete` → `onComplete`, иначе fallback `loadSingleStep` (`credentialSchema` → `single` | `raw`). `syncFooter` (`:140-166`): кнопки дерева → Cancel авто (если дерево не объявило `cancel`) → дефолтный submit (`Continue`/`Save`), если кнопок нет вообще. `submitWizard` (`:168-195`) шлёт `{provider_id,flow_id,action,values}`; `submitSingle` (`:197-214`) шлёт `POST /credentials {provider_id, data}`; `submitRaw` (`:216-228`) — JSON-вставка.
  - `CustomProviderWizard.svelte:60-109` — только **config**-схема по `type_key` (`configSchemaForType`); кнопка дерева = "сохранить всё" (`action` display-only, `:87-88`); дерево без кнопок → авто Add/Save + Cancel.
- Обратный путь single-step: `dashboard/credentials.go:84-117` → `credSvc.Add` (`internal/services/credential/service.go:62-105`) → `ValidateCredentials(typeKey, data)` (`handlers.go:427-465`; нет хендлера = accept). Обратный путь wizard-complete: тот же `Add` внутри `renderAuthResult` (`authflow.go:143-147`).

## 2. Node kinds (15): поля и рендер

Легенда: **Б** — валидация `uinodes.go`, **Ф** — `DynamicForm.svelte`.

| Kind | Поля (Б) | Рендер (Ф) |
|---|---|---|
| `text` | `text` (пустой разрешён, проверки нет) | `:84-85` `<Text tone=soft>{text}</Text>` |
| `input` | `name!`; `label,placeholder,required`; `input_type ∈ {text,password,number}` (`:208-214`, пустой = text); `value: any` (через `fromLuaValue`, `:172-178`); `options` игнорируются | `:88-102` `TextEdit type = password/secret→secret иначе text`; значение **только** `values[name] ?? ''` — `node.value` не используется; `placeholder→hint`; `label + *` |
| `select` | `name!`; `label,required`; `options: string[]` (`:179-191`); `option_labels: {k:v}` non-empty strings, ключи ⊆ `options` (`:142-165,:215-225`); `value: any` | `:103-114` `selectValue`: `values[name]` → `node.value` → `options[0]`; `optionLabel`: `option_labels[o] ?? o` |
| `checkbox` | `name!` (`:204-207`); `required` парсится, но ни на что не влияет | `:115-121` `Switch checked = values[name]===true`, `label=node.label` |
| `button` | `text` (не проверяется); `form_action` default `submit` (`:231-233`), любое непустое; `variant ∈ {primary,secondary,danger}` (`:234-240`), пустой разрешён | inline нет; только `collectButtons`+`buttonVariant` (`:61-79`) → футер хоста |
| `link` | `url!` non-empty (`:226-229`); `text` | `:122-123` `<a target=_blank rel="noopener noreferrer">{text\|\|url}</a>` |
| `banner` | `text`; `variant ∈ {info,error,success}` (`:241-248`), пустой = info | `:86-87` `div.banner-{variant\|\|info}` |
| `secret` | `name!` (`:324-327`); `label,required,placeholder`; `input_type` не проверяется | `:124-138` всегда `TextEdit type=secret`, значение `values[name] ?? ''` |
| `code` | `text!` non-empty (`:328-331`); `label` | `:139-140` `CodeBlock {text,label}` |
| `group` | `content!` non-empty (`:249-252`) | `:166-169` вложенный `div` с левой границей |
| `flow` | `content!`; `direction h\|v`; `gap sm\|md\|lg`; `align start\|center\|end\|stretch`; `justify start\|center\|end\|between`; `wrap bool` | `:141-144` flex через `flowStyle` (`:44-53`); `wrap ?? true → flex-wrap: wrap` |
| `grid` | `content!`; `columns: number 1..6` **обязателен** (`:291-293`, строка → ошибка); `gap sm\|md\|lg` | `:145-151` `grid-template-columns: repeat(columns\|\|1,1fr)`; ≤560px схлопывается в 1 колонку (`:228-232`) |
| `section` | `content!`; `title!` ≤120; `subtitle` ≤240 (`:300-312`) | `:152-157` `section > h3 + subtitle + content` |
| `spacer` | content запрещён (`:313-316`); `size ∈ {sm,md,lg}`; `grow bool` | `:158-163` `grow ?? true → <Spacer/>` иначе фиксированный `div height=gap(size)` |
| `divider` | content запрещён (`:320-323`) | `:164-165` `<Divider/>` |

Глобальные ограничения: глубина 8 (`uinodes.go:74-77`), ≤200 узлов **на один список** (`:78-81`), неизвестный `type` → ошибка (`:166-168`), `content` обязан быть array-table (`:192-202`), `columns` — только `LNumber` (`:123-127`), `wrap`/`grow` — только `LBool` (`:128-141`), `value` — любой конвертируемый Lua→JSON. Полный пример всех видов: `web/src/pages/UiTest.svelte:169-198`; минимальный живой пример: `scripts/smoke/testdata/mock.lua:21-29` (section + secret + button).

Ограничение "no raw HTML": `docs/PLUGIN-API.md:409-417` + `AGENTS.md`. Исполнение — allowlist `uiNodeTypes` (`uinodes.go:56-61`): всё вне 15 видов отклоняется как violation. Текст рендерится только через компоненты (`Text`, `CodeBlock`, `TextEdit`), кнопки — только через футер хоста.

## 3. Расхождения дока / backend / frontend

1. `input_type`: дока (`PLUGIN-API.md:409-411`) — `text|password|number`. Frontend (`DynamicForm.svelte:95`) понимает ещё `secret` (`password|secret→secret`). Backend (`uinodes.go:208-214`) `secret` **отвергает**. Плагин не может объявить `input_type="secret"` — только отдельный kind `secret`. `number` backend принимает, но frontend рисует обычным текстом (числового контрола нет).
2. `node.value` для `input`/`secret` игнорируется frontend (только `values[name]`); префилл из Lua работает лишь для `select` (`selectValue`, `:28-33`). Backend `value` при этом принимает и хранит (`uinodes.go:172-178`, `types.ts:227`).
3. `select` с пустым `options` backend разрешает (проверки длины нет) — frontend рисует пустой `Select`; `selectValue` вернёт `""`.
4. `checkbox.required` парсится (`uinodes.go:169-171`) и нигде не используется; дефолта/preset для чекбокса нет (`undefined→false`).
5. `button`: backend разрешает любой непустой `form_action` (проверяется только `variant`). Frontend: `cancel` закрывает модалку, `restart` закрывает (single) / уходит в `auth_step` (wizard), всё остальное постится в Lua как `action` как есть (`ProviderCredentialWizard.svelte:124-138`). Пустой `text` кнопки backend пропускает — подпись подставляет хост (`Continue`/`Save`, `Add`/`Save`).
6. Лимит "200 nodes": дока говорит про дерево, код проверяет `tbl.Len()>200` **каждого списка отдельно** (`uinodes.go:78-81`) — глубокое дерево может легально превысить 200 суммарно. Глубина `depth>8` считается от 0 на корне (`:74-77`).
7. Лимиты `section.title≤120`, `subtitle≤240` (`uinodes.go:307-312`) в доке не упомянуты.
8. Строгость типов полей неоднородна: `columns` строкой / `wrap`/`grow` не-bool → ошибка; `required` не-bool → молча `false` (`uinodes.go:169-171`); неизвестные ключи → молча дропаются (`Extra json:"-"`, `models.go:1152`).
9. `api.ts:122-130` — `configSchema/credentialSchema` идут через raw `fetch`, вне `apiCall`/generated-клиента; `credentialSchemaForType` отсутствует (только `configSchemaForType`) — создание провайдера грузит только config-схему (`CustomProviderWizard.svelte:60-76`).
10. Пустое дерево `[]` truthy в JS: `if (schema.nodes)` (`ProviderCredentialWizard.svelte:104`, `CustomProviderWizard.svelte:66`) принимает `[]` за "есть форма" — рисуется пустая форма; авто-кнопка в credential-визарде при `nodes==[]` не добавляется (`tree.length===0 && nodes.length>0`, `:157`), остаётся только Cancel.
11. `ctx` auth-хендлеров: дока (`PLUGIN-API.md:107,260-261`) обещает `provider_config` в `ctx`; код всегда передаёт `nil` (`handlers.go:632-634`, `662-667`) — `ctxTable` (`handlers.go:16-25`) поле `provider_config` для auth не ставит никогда. У `get_model_infos` наоборот: `provider_config` — 3-й аргумент, не часть `ctx` (дока здесь точна).
12. `SupportsAuthFlow` = только наличие `auth_initiate` (`schemas.go:51-59`); одинокий `auth_step` не рекламируется. Отсутствие `auth_initiate` → 409 → silent fallback на single-step (`ProviderCredentialWizard.svelte:88-93`); отсутствие `auth_step` на шаге → 400 без fallback (`authflow.go:111-114`).
13. `credentials`-финал проходит повторную `ValidateCredentials` через `credSvc.Add` (`authflow.go:143-147` → `credential/service.go:72-78`); отказ → 500 `save credentials: ...`, credential не создаётся.
14. `redirect`-статус: frontend делает `window.open` и оставляет wizard с пустыми `nodes` (`ProviderCredentialWizard.svelte:75-80,181-184`) — продолжения по poll нет, пользователь закрывает диалог сам; в этом состоянии в футере только Cancel.
15. `link.url` backend требует лишь non-empty (`uinodes.go:226-229`); frontend подставляет в `href` без санитизации (`DynamicForm.svelte:123`) — `javascript:` не отсекается на этом слое.
16. `docs/PLUGIN-API.md` содержит дубль секции `Manifest reference` (`:42-60` и `:506-511`). `google.lua` в корне — пустой файл (0 байт); каталога `providers/*.lua` нет; живые примеры схем: `mock.lua:21-29`, статические Go-деревья `custom`/`virtual` (`schemas.go:72-109`), полигон `UiTest.svelte:169-198`, тесты `uinodes_test.go`.
