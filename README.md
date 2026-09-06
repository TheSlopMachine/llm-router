# llm-router

Single-binary, bloat-free, extensible multi-provider OpenAI-compatible LLM gateway. Go on the backend, Svelte on the front, compiled into one file.

```bash
llm-router localhost --web 8080 --api 8081 --db ./llm-router.db

```

Zero external dependencies.

---

## What It Does

Sits between your applications and LLM providers, exposing a single OpenAI-compatible `/v1` API.

* **Load distribution:** Split traffic across multiple providers and provider accounts.
* **Access control:** Issue separate API keys with individual model, provider, and rate limits.
* **Built-in dashboard:** Full admin UI embedded into the go binary.

---

## Architecture & Philosophy

* **Blazingly fast:** Go + Svelte, no heavy JS runtime.
* **Modular:** Core repo remains lightweight and maintainable. Providers and plugins live in separate repositories.
* **Everything works:** Zero dead code, all features are tested by real people.
* **Clean UI:** Fully reviewed Svelte dashboard polished by a human.
* **UX with common sense:** No unmoderated AI slop that nobody knows how to use.

---

## Key Features

### Admin Panel

* **Chat:** Test models directly in the browser.
* **Metrics:** Inspect model and provider usage.
* **Providers:** Connect new providers and accounts.
* **Models:** Browse available models across active providers.
* **Agents:** Configure multi-model routing rules visually.
* **Tokens:** Manage access tokens with fine-grained model permissions.

### Modular Builds

Include only the providers you need via `adapters.conf`:

```text
# Example: github.com/TheSlopMachine/llm-router-adapter-demo

github.com/TheSlopMachine/llm-router-adapter-google
github.com/TheSlopMachine/llm-router-adapter-kiro
github.com/TheSlopMachine/llm-router-adapter-opencode-zen
```

Workspace setup is fully handled by the Makefile.

### Zero-Config

No config files. Minimal set of optional CLI flags and you are done.

```bash
llm-router --help

llm-router — minimalist LLM routing gateway

Usage:
  llm-router [host] [flags]

Examples:
  llm-router
  llm-router localhost --web 8080 --api 8081 --db ./llm-router.db

Flags:
      --api Port             port for /v1 OpenAI-compatible API (default 8081)
      --db string            path to the database file (default "llm-router.db")
  -h, --help                 help for llm-router
      --log-level LogLevel   log level: debug, info, warn, error (default info)
      --testing-key string   path to file with bearer token (auto-generated)
  -v, --version              print version information and exit
      --web Port             port for dashboard UI (default 8080)
```

---

## Quick Start

### Development server

```bash
make start
make start HOST=localhost WEB_PORT=8080 API_PORT=8081
```

### Production builds

```bash
make publish
make publish PUBLISH_PLATFORMS="windows/amd64 linux/amd64 darwin/amd64 freebsd/amd64"
```

### Dashboard setup

1. Create the initial admin account.
2. Go to `Providers` and authenticate in any of listed providers, or add a new one.
3. Go to `Tokens` and issue a new token. In wizard, select `Allow all` if you don't care about limits.
4. The token will be shown to you once, afterwards you won't be able see it again.

### API usage

Pass your token and the endpoint URL to your harness or any other AI-powered software:
- **Base URL:** `http://localhost:8081/v1`
- **Token:** `10b7****************************************************4fd7`

---

## License

MIT
