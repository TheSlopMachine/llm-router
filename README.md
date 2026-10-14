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
llm-router --generate-config
llm-router --bootstrap
llm-router
```

This starts one public listener on `PORT` (default `8080`) for the dashboard and `/v1`.

## Usage

1. Install plugins.
2. Add credentials.
3. Create a new token.
4. Pick a model.
5. Connect your harness to the router. Set the base URL to `http://HOST:PORT/v1` and the API key to the token you've just created.
