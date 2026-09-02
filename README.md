# llm-router

Single-binary, bloat-free, multi-provider OpenAI-compatible LLM gateway. Go on the backend, Svelte on the front, compiled into one file.

```bash
llm-router localhost --web 8080 --api 8081 --db ./llm-router.db

```

Zero external dependencies.

---

## What It Does

Sits between your applications and LLM providers, exposing a single OpenAI-compatible `/v1` API.

* **Load distribution:** Split traffic across multiple providers and provider accounts.
* **Access control:** Issue separate API keys with individual model, provider, and rate limits.
* **Built-in dashboard:** Full admin UI embedded into the binary (no extra deployment needed).

---

## Architecture & Philosophy

* **Robust stack:** Go + Svelte instead of heavy JS runtime environments.
* **Modular design:** Providers are isolated Go modules defined at compile time. Core repo remains lightweight and maintainable.
* **Curated core:** Zero dead code, experimental unmoderated features, or sprawl.
* **Human-designed UI:** Fully reviewed Svelte dashboard.

---

## Key Features

### Modular Builds

Include only the adapters you need via `adapters.conf`:

```text
github.com/TheSlopMachine/llm-router-adapter-demo 01286aaf5620fb7b4a0f108f96ac7751ae3d7040

```

Run `make build` to compile everything into a single binary.

### Embedded Svelte Admin Panel

* **Chat:** Test models directly in the browser.
* **Providers:** Connect accounts via guided setup wizards.
* **API Keys:** Issue/revoke tokens with fine-grained model permissions.
* **Metrics:** Live request, error, and token counts by provider and model.
* **Model Browser:** View all available models across active providers.
* **Agents Editor:** Configure multi-model routing rules visually.

### Zero-Config CLI

No YAML or JSON runtime configs. Configure everything through flags:

```bash
llm-router localhost --web 8080 --api 8081 --db ./llm-router.db --max-retries 7

```

---

## Quick Start

1. **Build and run:**
```bash
make build
llm-router localhost --web 8080 --api 8081 --db ./llm-router.db

```


2. **Setup (`http://localhost:8080`):**
* Create the initial admin account.
* Add a provider and credential.
* Issue a router token.


3. **Use the API:**
Pass the endpoint URL and your newly generated API key directly to your harness, framework, or agent runner:
* **Base URL:** `http://localhost:8081/v1`
* **API Key:** `<your-api-key>`

---

## License

MIT