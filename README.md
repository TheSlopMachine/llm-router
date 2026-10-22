# llm-router

Single-binary OpenAI-compatible routing gateway. Go backend, Svelte dashboard, SQLite storage, one binary to run.

## Problems it solves

One central endpoint for every model. Seamlessly route through multiple providers and accounts. Never hit rate limits. Bypass region blocks with automatic proxying.

## What makes it different

Go plus Svelte, low memory footprint. Self-contained. No JavaScript, no dependencies, quick installation.

Free providers via Lua plugins. Install them from the catalog and use immediately.

Speaks OpenAI- and Anthropic-compatible formats. Connect your harness to the router and you're done.

## Install from releases

Download the latest release: https://github.com/TheSlopMachine/llm-router/releases/latest

## Install from source

```bash
make install
```

## Configure

Configure via .env file or environment variables.

```bash
llm-router config init
llm-router set-dashboard-password
llm-router daemon start
```

This starts one public listener on `PORT` (default `8080`) for the dashboard and `/v1`. The dashboard login is `admin`.

`llm-router help` lists every command. The main ones:

| Command | Purpose |
|---|---|
| `serve` | Run in the foreground (systemd, Docker). |
| `daemon start\|stop\|restart\|status` | Run in the background (Linux, macOS, FreeBSD). |
| `config` | Interactive settings menu; `config get\|set\|unset\|list\|init` for scripts. |
| `set-dashboard-password` | Set the `admin` password. |
| `logs` | Print server events (`-f`, `--subsystem`, `--severity`, ...). |
| `status`, `doctor` | Inspect the running server, check the database. |
| `update` | Install the latest release. |

Only one instance runs per database.

Output is Russian or English: `LLM_ROUTER_LANG` (`ru` or `en`), else `LC_ALL`, `LC_MESSAGES`, `LANG` or the Windows user locale; anything but Russian gives English. Event messages, `--json`, config values and command names stay English.

### systemd and Docker

Run `llm-router serve` in the foreground; the supervisor restarts it. Create the first password before the server starts. `--if-absent` keeps a password changed in the dashboard across restarts:

```bash
llm-router config init
llm-router set-dashboard-password --password-stdin --if-absent < /run/secrets/admin_password
exec llm-router serve
```

## Usage

1. Install plugins.
2. Add credentials.
3. Create a new token.
4. Pick a model.
5. Connect your harness to the router. Set the base URL to `http://HOST:PORT/v1` and the API key to the token you've just created.
