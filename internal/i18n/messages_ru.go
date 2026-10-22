package i18n

var russian = map[string]string{
	"root.short":       "llm-router — минималистичный шлюз маршрутизации LLM",
	"group.run":        "Запуск:",
	"group.configure":  "Настройка:",
	"group.maintain":   "Обслуживание:",
	"help.short":       "Справка по любой команде",
	"completion.short": "Создать скрипт автодополнения для выбранной оболочки",
	"flag.help":        "справка по %s",
	"version.short":    "Показать версию, коммит и время сборки",

	"usage.usage":       "Использование:",
	"usage.aliases":     "Псевдонимы:",
	"usage.examples":    "Примеры:",
	"usage.commands":    "Доступные команды:",
	"usage.additional":  "Дополнительные команды:",
	"usage.flags":       "Флаги:",
	"usage.globalFlags": "Глобальные флаги:",
	"usage.topics":      "Дополнительные разделы справки:",
	"usage.subcommand":  "[команда]",
	"usage.more":        `Подробнее о команде: {{.CommandPath}} [команда] --help`,

	"serve.short": "Запустить сервер на переднем плане",
	"serve.long": `Запускает сервер на переднем плане до SIGINT или SIGTERM.

Эта команда нужна для юнитов systemd и контейнеров. Настройки берутся из
окружения и файла .env (см. "llm-router config").`,
	"serve.initFailed": "инициализация сервера",

	"daemon.short": "Запускать сервер в фоне (Linux, macOS, FreeBSD)",
	"daemon.long": `Запускает сервер в фоне.

Lock-файл рядом с базой хранит данные о запущенном процессе, поэтому
команды находят и сервер, запущенный через "llm-router serve". Для systemd
и контейнеров используй "llm-router serve": супервизору нужен процесс
на переднем плане.`,
	"daemon.start.short":    "Запустить сервер в фоне",
	"daemon.stop.short":     "Остановить сервер и дождаться корректного завершения",
	"daemon.restart.short":  "Остановить сервер, если он запущен, и запустить снова",
	"daemon.status.short":   "Показать, запущен ли сервер",
	"daemon.unsupported":    "daemon не поддерживается на %s",
	"daemon.started":        "запущен (pid %d), слушает %s",
	"daemon.stopped":        "остановлен (pid %d)",
	"daemon.notRunning":     "не запущен",
	"daemon.startTimeout":   "daemon (pid %d) не ответил на %s за %s; см. %s",
	"daemon.startFailed":    "daemon %s при запуске:\n%s",
	"daemon.startFailedLog": "daemon %s при запуске; см. %s",
	"daemon.exited":         "завершился",
	"daemon.maintenance":    "команда обслуживания (pid %d) держит базу; дождись её завершения",
	"daemon.noPID":          "владелец lock-файла не записал pid; повтори попытку",
	"daemon.stopPID":        "остановка pid %d",
	"daemon.didNotExit":     "pid %d не завершился за %s",
	"daemon.resolve":        "определение пути к бинарю",
	"daemon.start":          "запуск %s",

	"instance.held":        "другой экземпляр llm-router уже работает с этой базой (pid %d, %s)",
	"instance.heldUnknown": "другой экземпляр llm-router уже работает с этой базой",

	"status.short": "Показать состояние запущенного сервера",
	"status.long": `Показывает состояние запущенного сервера: процесс, время работы, адрес,
версию, отвечает ли дашборд и задан ли пароль.

Код выхода 3 означает, что сервер не запущен.`,
	"status.label.status":    "статус",
	"status.label.mode":      "режим",
	"status.label.pid":       "pid",
	"status.label.uptime":    "работает",
	"status.label.listen":    "адрес",
	"status.label.version":   "версия",
	"status.label.dashboard": "дашборд",
	"status.label.password":  "пароль",
	"status.value.running":   "запущен",
	"status.value.stopped":   "остановлен",
	"status.value.answering": "отвечает",
	"status.value.silent":    "не отвечает",
	"status.value.set":       "задан",
	"status.value.notSet":    "не задан (выполни: llm-router set-dashboard-password)",

	"logs.short": "Показать события сервера",
	"logs.long": `Показывает события, которые сохранил сервер. Команда читает базу событий
напрямую, поэтому сервер может не работать, и запускается из любого
терминала.

--severity задаёт минимальный уровень: debug, info, warn, error или fatal.
Сервер сохраняет только события не ниже своего LOG_LEVEL, поэтому
"--severity debug" не покажет то, что ниже этого уровня.

--subsystem оставляет одну часть сервера, например providers,
virtual_models, plugins, plugin_repos, plugin_storage, tokens, credentials,
proxy или doctor. --filter и --exclude точно сравнивают любой другой ключ
события: "--filter code=rate_limit" сравнивает значение, "--filter token_id"
находит события, где есть этот ключ. Оба флага можно повторять. Список
ключей в docs/EVENT-KEYS.md.

--from и --to принимают время RFC 3339 или возраст: 30m, 6h, 2d.`,
	"logs.flag.subsystem": "только события этой подсистемы",
	"logs.flag.severity":  "минимальный уровень: debug, info, warn, error, fatal",
	"logs.flag.filter":    "только события с ключом key[=value]; можно повторять",
	"logs.flag.exclude":   "пропустить события с ключом key[=value]; можно повторять",
	"logs.flag.id":        "показать одно событие по id",
	"logs.flag.from":      "только события с момента RFC 3339 или возраста (30m, 6h, 2d)",
	"logs.flag.to":        "только события до момента RFC 3339 или возраста",
	"logs.flag.limit":     "сколько последних событий показать",
	"logs.flag.follow":    "ждать новые события",
	"logs.flag.json":      "один JSON-объект на строку",
	"logs.reading":        "читаю %s",
	"logs.noDatabase":     "нет базы событий в %s: сервер ещё не запускался",
	"logs.followID":       "--follow и --id нельзя использовать вместе",
	"logs.limit":          "--limit должен быть не меньше 1",
	"logs.openEvents":     "открытие событий",
	"logs.queryEvents":    "запрос событий",

	"config.short": "Показать и изменить настройки",
	"config.long": `Показывает и меняет настройки развёртывания из файла .env.

Без подкоманды открывает интерактивное меню. Файл — это ENV_FILE или путь
по умолчанию в каталоге конфигурации XDG. Переменные окружения сильнее
файла; "config list" показывает, откуда взято каждое значение. Изменения
применяются после перезапуска сервера.`,
	"config.init.short":     "Записать файл .env со значениями по умолчанию",
	"config.init.force":     "перезаписать существующий файл",
	"config.get.short":      "Показать действующее значение ключа",
	"config.set.short":      "Записать значение в файл .env",
	"config.unset.short":    "Удалить ключ из файла .env и вернуть значение по умолчанию",
	"config.list.short":     "Показать все ключи со значением, источником и смыслом",
	"config.needTerminal":   "меню требует терминал: используй config get, set, unset или list",
	"config.unknownKey":     "неизвестный ключ %q: см. llm-router config list",
	"config.invalidEnvFile": "неверный ENV_FILE",
	"config.wrote":          "записан %s",
	"config.exists":         "%s уже есть: --force перезапишет его",
	"config.writeDefault":   "запись конфигурации по умолчанию",
	"config.file":           "файл %s",
	"config.notSet":         "%s не задан в %s",
	"config.reset":          "%s сброшен к значению по умолчанию (%s)",
	"config.envWins":        "%s также задан в окружении и перекрывает файл",
	"config.restart":        "сервер запущен: перезапусти его, чтобы применить изменение",
	"config.prompt.number":  "Номер (пусто — выход): ",
	"config.noItem":         "нет такого пункта",
	"config.prompt.value":   "%s [%s] (пусто — оставить, - — сбросить): ",
	"config.error":          "ошибка: %v",

	"key.HOST":                  "адрес прослушивания",
	"key.PORT":                  "порт для дашборда, /api и /v1",
	"key.DB_PATH":               "основной файл базы данных",
	"key.CACHE_PATH":            "файл базы кэша, который можно пересоздать",
	"key.EVENTS_PATH":           "файл базы событий (логов)",
	"key.EVENTS_RETENTION_DAYS": "сколько дней хранить события",
	"key.LOG_LEVEL":             "debug, info, warn или error",
	"key.LUA_POOL_BUCKET_SIZE":  "свободных Lua-состояний на плагин, 0 отключает пул",

	"password.short": "Задать пароль пользователя дашборда admin",
	"password.long": `Задаёт пароль пользователя дашборда "admin". При первом вызове
пользователь создаётся.

Без флага-источника команда дважды спрашивает пароль в терминале.
Изменение действует сразу, перезапуск не нужен.

--if-absent не трогает уже заданный пароль. Используй его в entrypoint
контейнера: пароль из секрета — только первое значение, поэтому пароль,
изменённый в дашборде, переживает перезапуски. Пароль в --password виден
в списке процессов и истории shell; лучше --password-stdin.`,
	"password.flag.stdin":     "прочитать пароль из stdin",
	"password.flag.value":     "пароль аргументом (виден в ps и истории shell)",
	"password.flag.generate":  "сгенерировать случайный пароль и вывести его в stdout",
	"password.flag.ifAbsent":  "ничего не делать, если пароль admin уже задан",
	"password.empty":          "--password не должен быть пустым",
	"password.checkAccounts":  "проверка учётных записей дашборда",
	"password.listAccounts":   "список учётных записей дашборда",
	"password.alreadySet":     "пароль admin уже задан",
	"password.setFailed":      "установка пароля admin",
	"password.created":        "пароль admin создан",
	"password.updated":        "пароль admin обновлён",
	"password.warnArgument":   "внимание: пароль в командной строке виден в списке процессов и истории shell",
	"password.noSource":       "нет источника пароля: используй --password-stdin или --generate, либо запусти в терминале",
	"password.prompt.new":     "Новый пароль: ",
	"password.prompt.repeat":  "Повтори пароль: ",
	"password.mustNotBeEmpty": "пароль не должен быть пустым",
	"password.mismatch":       "пароли не совпадают",
	"password.tooLong":        "пароль длиннее %d байт",
	"prompt.readInput":        "чтение ввода",
	"prompt.readPassword":     "чтение пароля",
	"cli.generatePassword":    "генерация пароля",
	"cli.severity":            "неверный уровень %q: нужен debug, info, warn, error или fatal",
	"cli.time":                "неверное время %q: нужен RFC 3339 (2026-10-21T09:00:00Z) или возраст вроде 30m, 6h, 2d",
	"cli.filter":              "неверный фильтр %q: нужен key или key=value",

	"doctor.short": "Проверить базу данных и исправить, что можно",
	"doctor.long": `Проверяет базу на битые ссылки и несогласованные записи, теми же
проверками, что и doctor в дашборде.

Серверу нужно быть остановленным: команда держит lock базы всё время работы.
Код выхода 1 означает, что проблемы остались.`,
	"doctor.flag.fix":     "применить все доступные исправления",
	"doctor.stopFirst":    "сначала останови его: llm-router daemon stop",
	"doctor.openDatabase": "открытие базы sqlite",
	"doctor.build":        "создание doctor",
	"doctor.inspect":      "проверка",
	"doctor.none":         "проблем не найдено",
	"doctor.fixed":        "исправлено %d",
	"doctor.inspectAfter": "проверка после исправления",
	"doctor.remain":       "осталось проблем: %d",
	"doctor.fix":          "исправление",

	"update.short": "Обновить до последнего релиза на GitHub",
	"update.long": `Обновляет бинарь до последнего релиза %s.

Архив сверяется с контрольными суммами релиза, а новый бинарь должен
сообщить версию релиза до того, как заменит старый. Старый бинарь остаётся
рядом как llm-router.prev, а база копируется в <база>%s
(миграции не откатываются).

Сервер, работающий в фоне, останавливается и запускается снова на новом
бинаре. Сервер на переднем плане блокирует обновление: сначала останови
его. Контейнеры на месте не обновляются: возьми новый образ.`,
	"update.flag.check":    "только сообщить, есть ли обновление",
	"update.flag.force":    "разрешить откат, переустановку или обновление dev-сборки",
	"update.container":     "обновление отключено в контейнерах: возьми новый образ",
	"update.devBuild":      "у сборки %q нет версии релиза: --force установит последний релиз",
	"update.noRelease":     "релизов пока нет",
	"update.badTag":        "тег релиза %q не является версией",
	"update.available":     "доступно обновление: %s -> %s",
	"update.upToDate":      "актуальная версия (%s)",
	"update.newer":         "%s новее последнего релиза %s",
	"update.newerForce":    "%s новее последнего релиза %s: --force разрешит откат",
	"update.latestDev":     "последний релиз: %s (у сборки %q нет версии релиза)",
	"update.noAsset":       "в релизе %s нет %s",
	"update.noChecksums":   "в релизе %s нет %s: непроверенный бинарь не устанавливаю",
	"update.running":       "экземпляр %s (pid %d) работает: останови его перед обновлением",
	"update.downloading":   "загружаю %s",
	"update.done":          "обновлено %s -> %s",
	"update.previous":      "прежний бинарь: %s",
	"update.copy":          "копия базы:     %s",
	"update.restarted":     "сервер перезапущен (pid %d)",
	"update.restartFailed": "бинарь обновлён, но сервер не запускается",
	"update.binaryNoRun":   "загруженный бинарь не запускается",
	"update.binaryVersion": "загруженный бинарь сообщает %q, ожидалась версия %s",
	"update.backup":        "копирование %s",
}
