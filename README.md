# llm-router

Single-binary OpenAI-compatible routing gateway. Go backend, Svelte dashboard, SQLite storage, one binary to run.

## Problems it solves

One central endpoint for every model. Seamlessly route through multiple providers and accounts. Never hit rate limits. Bypass region blocks with automatic proxying.

## What makes it different

Go plus Svelte, low memory footprint. One self-contained binary: no runtime, no external services, quick installation.

Free providers via Lua plugins. Install them from the catalog and use immediately.

Speaks OpenAI- and Anthropic-compatible formats. Connect your harness to the router and you're done.

## Install from releases

Download the latest release: https://github.com/TheSlopMachine/llm-router/releases/latest

## Install with a package manager

Scoop, Homebrew, Nix, APT, DNF, Zypper, APK and FreeBSD pkg: https://github.com/TheSlopMachine/llm-packages

## Install from source

```bash
make install
```

## Configure

Configure via .env file or environment variables.

```bash
llm-router config init
llm-router passwd
llm-router daemon start
```

This starts the server on the configured port (default is `8080`).

`llm-router help` lists every command. The main ones:

| Command | Purpose |
|---|---|
| `serve` | Run in the foreground. |
| `daemon start\|stop\|restart\|status` | Run in the background. |
| `daemon enable\|disable` | Start the package service at boot. |
| `config` | Interactive settings menu. |
| `passwd` | Set the dashboard password. |
| `logs` | Print server events. |
| `status`, `doctor` | Inspect the running server and database. |
| `update` | Install the latest release. |

### systemd and Docker

Systemd and Docker should use this initialization logic:

```bash
llm-router config init
llm-router passwd --password-stdin --if-absent < /run/secrets/dashboard_password
exec llm-router serve
```

## Usage

1. Install plugins.
2. Add credentials.
3. Create a new token.
4. Pick a model.
5. Connect your harness to the router. Set the base URL to `http://localhost:8080/v1` and the API key to the token you've just created.
