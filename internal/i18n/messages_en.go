package i18n

var english = map[string]string{
	"root.short":       "llm-router — minimalist LLM routing gateway",
	"group.run":        "Run:",
	"group.configure":  "Configure:",
	"group.maintain":   "Maintain:",
	"help.short":       "Help about any command",
	"completion.short": "Generate the autocompletion script for the specified shell",
	"flag.help":        "help for %s",
	"version.short":    "Print version, commit and build time",

	"usage.usage":       "Usage:",
	"usage.aliases":     "Aliases:",
	"usage.examples":    "Examples:",
	"usage.commands":    "Available Commands:",
	"usage.additional":  "Additional Commands:",
	"usage.flags":       "Flags:",
	"usage.globalFlags": "Global Flags:",
	"usage.topics":      "Additional help topics:",
	"usage.subcommand":  "[command]",
	"usage.more":        `Use "{{.CommandPath}} [command] --help" for more information about a command.`,

	"serve.short": "Run the server in the foreground",
	"serve.long": `Run the server in the foreground until SIGINT or SIGTERM.

Use this command in systemd units and containers. Configuration comes from
the environment and the .env file (see "llm-router config").`,
	"serve.initFailed": "init server",

	"daemon.short": "Run the server in the background (Linux, macOS, FreeBSD)",
	"daemon.long": `Run the server in the background.

A lock file next to the database records the running process, so the
commands also find a server started with "llm-router serve". For systemd
and containers use "llm-router serve" instead: a supervisor wants the
process in the foreground.`,
	"daemon.start.short":    "Start the server in the background",
	"daemon.stop.short":     "Stop the running server and wait for a clean exit",
	"daemon.restart.short":  "Stop the server if it runs, then start it",
	"daemon.status.short":   "Show whether the server runs",
	"daemon.unsupported":    "daemon is not supported on %s",
	"daemon.started":        "started (pid %d), listening on %s",
	"daemon.stopped":        "stopped (pid %d)",
	"daemon.notRunning":     "not running",
	"daemon.startTimeout":   "daemon (pid %d) did not answer on %s within %s; see %s",
	"daemon.startFailed":    "daemon %s during start:\n%s",
	"daemon.startFailedLog": "daemon %s during start; see %s",
	"daemon.exited":         "exited",
	"daemon.maintenance":    "a maintenance command (pid %d) holds the database; wait for it to finish",
	"daemon.noPID":          "the lock holder recorded no pid; try again",
	"daemon.stopPID":        "stop pid %d",
	"daemon.didNotExit":     "pid %d did not exit within %s",
	"daemon.resolve":        "resolve executable",
	"daemon.start":          "start %s",

	"instance.held":        "another llm-router instance holds the database (pid %d, %s)",
	"instance.heldUnknown": "another llm-router instance holds the database",

	"status.short": "Show the state of the running server",
	"status.long": `Show the state of the running server: process, uptime, address, version
and whether the dashboard answers and has a password.

Exit code 3 means the server is not running.`,
	"status.label.status":    "status",
	"status.label.mode":      "mode",
	"status.label.pid":       "pid",
	"status.label.uptime":    "uptime",
	"status.label.listen":    "listen",
	"status.label.version":   "version",
	"status.label.dashboard": "dashboard",
	"status.label.password":  "password",
	"status.value.running":   "running",
	"status.value.stopped":   "stopped",
	"status.value.answering": "answering",
	"status.value.silent":    "not answering",
	"status.value.set":       "set",
	"status.value.notSet":    "not set (run: llm-router set-dashboard-password)",

	"logs.short": "Print server events",
	"logs.long": `Print the events the server persisted. The command reads the events
database directly, so the server does not need to run, and it works from any
terminal.

--severity is a minimum: debug, info, warn, error or fatal. The server
persists only events at or above its LOG_LEVEL, so "--severity debug" shows
nothing below that level.

--subsystem narrows to one part of the server, for example providers,
virtual_models, plugins, plugin_repos, plugin_storage, tokens, credentials,
proxy or doctor. --filter and --exclude match any other event key exactly:
"--filter code=rate_limit" matches the value, "--filter token_id" matches
events that carry the key. Both repeat. Keys are listed in docs/EVENT-KEYS.md.

--from and --to take an RFC 3339 timestamp or an age: 30m, 6h, 2d.`,
	"logs.flag.subsystem": "only events of this subsystem",
	"logs.flag.severity":  "minimum severity: debug, info, warn, error, fatal",
	"logs.flag.filter":    "only events with key[=value]; repeatable",
	"logs.flag.exclude":   "skip events with key[=value]; repeatable",
	"logs.flag.id":        "print one event by id",
	"logs.flag.from":      "only events since RFC 3339 time or age (30m, 6h, 2d)",
	"logs.flag.to":        "only events until RFC 3339 time or age",
	"logs.flag.limit":     "newest events to print",
	"logs.flag.follow":    "keep waiting for new events",
	"logs.flag.json":      "print one JSON object per line",
	"logs.reading":        "reading %s",
	"logs.noDatabase":     "no events database at %s: the server has not run yet",
	"logs.followID":       "--follow and --id cannot be combined",
	"logs.limit":          "--limit must be at least 1",
	"logs.openEvents":     "open events",
	"logs.queryEvents":    "query events",

	"config.short": "Show and change the configuration",
	"config.long": `Show and change the deployment settings stored in the .env file.

Without a subcommand it opens an interactive menu. The file is ENV_FILE or
the default under the XDG config directory. Environment variables win over
the file; "config list" shows where each value comes from. Changes apply
after a restart of the server.`,
	"config.init.short":     "Write a .env file with the default values",
	"config.init.force":     "overwrite an existing file",
	"config.get.short":      "Print the effective value of a key",
	"config.set.short":      "Write a value to the .env file",
	"config.unset.short":    "Remove a key from the .env file and restore its default",
	"config.list.short":     "List all keys with value, source and meaning",
	"config.needTerminal":   "the menu needs a terminal: use config get, set, unset or list",
	"config.unknownKey":     "unknown key %q: see llm-router config list",
	"config.invalidEnvFile": "invalid ENV_FILE",
	"config.wrote":          "wrote %s",
	"config.exists":         "%s exists: use --force to overwrite it",
	"config.writeDefault":   "write default configuration",
	"config.file":           "file %s",
	"config.notSet":         "%s is not set in %s",
	"config.reset":          "%s reset to default (%s)",
	"config.envWins":        "%s is also set in the environment and wins over the file",
	"config.restart":        "the server is running: restart it to apply the change",
	"config.prompt.number":  "Number (empty to exit): ",
	"config.noItem":         "no such item",
	"config.prompt.value":   "%s [%s] (empty keeps, - resets): ",
	"config.error":          "error: %v",

	"key.HOST":                  "listen address",
	"key.PORT":                  "listen port for dashboard, /api and /v1",
	"key.DB_PATH":               "main database file",
	"key.CACHE_PATH":            "rebuildable cache database file",
	"key.EVENTS_PATH":           "events (logs) database file",
	"key.EVENTS_RETENTION_DAYS": "days to keep events",
	"key.LOG_LEVEL":             "debug, info, warn or error",
	"key.LUA_POOL_BUCKET_SIZE":  "idle Lua states kept per plugin, 0 disables the pool",

	"password.short": "Set the password of the dashboard user admin",
	"password.long": `Set the password of the dashboard user "admin". The user is created on
first use.

Without a source flag the command asks for the password twice on a terminal.
The change applies at once, no restart needed.

--if-absent leaves an existing password untouched. Use it in container
entrypoints: the password from a secret is only the first value, so a
password changed in the dashboard survives restarts. A password in
--password is visible in the process list and the shell history; prefer
--password-stdin.`,
	"password.flag.stdin":     "read the password from stdin",
	"password.flag.value":     "password as an argument (visible in ps and shell history)",
	"password.flag.generate":  "generate a random password and print it to stdout",
	"password.flag.ifAbsent":  "do nothing when the admin password is already set",
	"password.empty":          "--password must not be empty",
	"password.checkAccounts":  "check dashboard accounts",
	"password.listAccounts":   "list dashboard accounts",
	"password.alreadySet":     "admin password is already set",
	"password.setFailed":      "set admin password",
	"password.created":        "admin password created",
	"password.updated":        "admin password updated",
	"password.warnArgument":   "warning: a password in the command line is visible in the process list and the shell history",
	"password.noSource":       "no password source: use --password-stdin or --generate, or run in a terminal",
	"password.prompt.new":     "New password: ",
	"password.prompt.repeat":  "Repeat password: ",
	"password.mustNotBeEmpty": "password must not be empty",
	"password.mismatch":       "passwords do not match",
	"password.tooLong":        "password is longer than %d bytes",
	"prompt.readInput":        "read input",
	"prompt.readPassword":     "read password",
	"cli.generatePassword":    "generate password",
	"cli.severity":            "invalid severity %q: want debug, info, warn, error or fatal",
	"cli.time":                "invalid time %q: want RFC 3339 (2026-10-21T09:00:00Z) or an age like 30m, 6h, 2d",
	"cli.filter":              "invalid filter %q: want key or key=value",

	"doctor.short": "Check the database and fix what can be fixed",
	"doctor.long": `Check the database for broken references and inconsistent records, the
same checks as the dashboard doctor.

The command needs the server stopped: it takes the database lock for the
whole run. Exit code 1 means issues remain.`,
	"doctor.flag.fix":     "apply every available fix",
	"doctor.stopFirst":    "stop it first: llm-router daemon stop",
	"doctor.openDatabase": "open sqlite database",
	"doctor.build":        "build doctor",
	"doctor.inspect":      "inspect",
	"doctor.none":         "no issues found",
	"doctor.fixed":        "fixed %d",
	"doctor.inspectAfter": "inspect after fix",
	"doctor.remain":       "%d issues remain:",
	"doctor.fix":          "fix",

	"update.short": "Update to the latest GitHub release",
	"update.long": `Update the binary to the latest release of %s.

The archive is checked against the release checksums and the new binary
must report the release version before it replaces the old one. The old
binary stays next to it as llm-router.prev, and the database is copied to
<database>%s (migrations do not roll back).

A server running in the background is stopped and started again on the new
binary. A server running in the foreground blocks the update: stop it
first. Containers are not updated in place: pull a new image.`,
	"update.flag.check":    "only report whether an update exists",
	"update.flag.force":    "allow a downgrade, a reinstall or an update of a dev build",
	"update.container":     "update is disabled in containers: pull a new image instead",
	"update.devBuild":      "build %q has no release version: use --force to install the latest release",
	"update.noRelease":     "no release is published yet",
	"update.badTag":        "release tag %q is not a version",
	"update.available":     "update available: %s -> %s",
	"update.upToDate":      "up to date (%s)",
	"update.newer":         "%s is newer than the latest release %s",
	"update.newerForce":    "%s is newer than the latest release %s: use --force to downgrade",
	"update.latestDev":     "latest release: %s (build %q has no release version)",
	"update.noAsset":       "release %s has no %s",
	"update.noChecksums":   "release %s has no %s: refusing to install an unverified binary",
	"update.running":       "a %s instance (pid %d) runs: stop it before updating",
	"update.downloading":   "downloading %s",
	"update.done":          "updated %s -> %s",
	"update.previous":      "previous binary: %s",
	"update.copy":          "database copy:   %s",
	"update.restarted":     "server restarted (pid %d)",
	"update.restartFailed": "the binary is updated, the server does not start",
	"update.binaryNoRun":   "the downloaded binary does not run",
	"update.binaryVersion": "the downloaded binary reports %q, want version %s",
	"update.backup":        "back up %s",
}
