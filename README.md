# llm-router

A **zero-bloat**, self-contained OpenAI-compatible LLM routing gateway.

```
llm-router localhost -p 8080 --db ./router.db
```

---

## For AI Agents

See [README-CLAUDE.md](README-CLAUDE.md) for coding guidelines and rules when working on this project.

---

## Philosophy

| Principle | How |
|-----------|-----|
| **Zero external dependencies** (runtime) | Only `cobra` (CLI) + `bbolt` (embedded DB) |
| **No config files** | CLI flags only |
| **Easy to extend** | Register a new provider adapter in one `init()` call |
| **Self-sufficient** | Single binary, embedded UI, embedded DB |

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         llm-router                               │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │  Dashboard  (Svelte SPA)          :  GET /               │   │
│  │  Overview · Providers · Agents · Tokens · Credentials    │   │
│  └───────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │  Swagger UI                       :  GET /swagger/        │   │
│  │  Interactive API documentation for all endpoints           │   │
│  └───────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │  OpenAI-compatible API  :  POST /v1/chat/completions      │   │
│  │                             GET  /v1/models               │   │
│  └──────────────┬────────────────────────────────────────────┘   │
│                 │                                                 │
│    ┌────────────▼───────────┐   ┌──────────────────────────┐    │
│    │  Token Service         │   │  Router Service           │    │
│    │  Validate bearer token │──▶│  Resolve ModelId →        │    │
│    │  Enforce token rules   │   │  Provider + Credential    │    │
│    └────────────────────────┘   └──────────┬───────────────┘    │
│                                            │                     │
│    ┌───────────────────────────────────────▼───────────────┐    │
│    │  Provider Adapter Registry                             │    │
│    │  adapter.Complete() / CompleteStream()                 │    │
│    └────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │  Maintenance Service  (background goroutine)              │   │
│  │  Scans credentials  ·  Calls adapter.RefreshCredential()  │   │
│  └───────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │  bbolt embedded DB                                        │   │
│  │  buckets: meta · admin · tokens · token_index · providers │   │
│  │           credentials · auth · agents                     │   │
│  └───────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

---

## Services

| # | Service | Package | Responsibility |
|---|---------|---------|----------------|
| 0 | **Bootstrap UI** | `dashboard` | First-run admin account creation |
| A | **Dashboard** | `dashboard` | Admin UI for system overview, providers, agents, tokens, and credentials |
| B | **Swagger UI** | `dashboard` | Interactive API documentation at `/swagger/` |
| C | **OpenAI API** | `api/v1` | `/v1/chat/completions`, `/v1/models` |
| 1 | **Token Service** | `services/token` | Issue / validate / revoke router-tokens |
| 2 | **Router Service** | `services/router` | Resolve ModelId → Adapter + Credential with LRU selection and intelligent retry |
| 3 | **Provider Service** | `services/provider` | CRUD for Provider records |
| 4 | **Credential Pool** | `services/credential` | Store, retrieve, update credentials with LRU-based load balancing |
| 5 | **Agent Service** | `services/agent` | CRUD for Agent records with validation |
| 6 | **Auth Service** | `services/auth` | Ephemeral state for auth flows |
| 7 | **Maintenance** | `services/maintenance` | Background credential refresh + auth flow cleanup |

---

## Key Concepts

### ModelId

Format: `provider/model-name[:version]`

```
openai/gpt-4o
anthropic/claude-3-opus:20240229
ollama/llama3
agents/my-agent
```

The prefix (`openai`, `anthropic`, `agents`, etc.) maps directly to the **adapter TypeKey** used to look up the right `provider.Adapter`.

### Agents

**Agents** are virtual models that orchestrate requests across multiple real providers with custom instructions and intelligent routing.

Format: `agents/agent-name`

```
agents/research-assistant
agents/code-reviewer
```

**Key Features:**
- **Multi-model orchestration**: Define a list of models to try in priority order
- **Instruction injection**: Add system prompts at the beginning or end of conversations
- **Model-specific instructions**: Fine-tune instructions for each model
- **Decision-based routing**: Use a cheap model to intelligently select the best model for each request
- **Automatic fallback**: On rate limits, automatically tries the next model in priority order

**Example Use Case:**
```json
{
  "name": "Research Assistant",
  "models": [
    {
      "model_id": "openai/gpt-4o",
      "priority": 0,
      "description": "Best for complex reasoning and analysis"
    },
    {
      "model_id": "openai/gpt-4o-mini",
      "priority": 1,
      "description": "Fast and cost-effective for simple tasks"
    }
  ],
  "instructions": {
    "content": "You are a research assistant. Provide detailed, well-sourced answers.",
    "injection": "beginning"
  },
  "decision_model": {
    "model_id": "openai/gpt-4o-mini",
    "system_prompt": "Choose the best model based on query complexity."
  }
}
```

**Usage:**
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <your-router-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "agents/research-assistant",
    "messages": [{"role": "user", "content": "Explain quantum computing"}]
  }'
```

### Token Disambiguation (IMPORTANT)

There are **two completely separate token systems**:

| Token | Where used | Managed by |
|-------|-----------|------------|
| **Router Token** | `Authorization: Bearer <token>` on `/v1/` API | Token Service |
| **Provider Credentials** | `api_key`, `access_token`, etc. sent to upstream | Credential Pool |

Router Tokens are _our own_ bearer tokens. Provider Credentials are what we use to call upstream LLMs.

### Token Rules

Each Router Token carries a `TokenRules` struct:

```json
{
  "allowed_models": ["openai/gpt-4o", "anthropic/claude-3-opus"]
}
```

Empty `allowed_models` = **all models permitted**.

---

## Adding a New Provider

1. Create a package under `providers/`:

```go
// providers/myprovider/adapter.go
package myprovider

import "llm-router/internal/services/provider"

func init() {
    provider.Register(&Adapter{})
}

type Adapter struct{}

func (a *Adapter) TypeKey() string              { return "myprovider" }
func (a *Adapter) AuthType() models.AuthType    { return models.AuthTypeAPIKey }
func (a *Adapter) ValidateCredentials(...) error { ... }
func (a *Adapter) Complete(...) (...)            { ... } // Return *provider.ProviderError for rate limits
func (a *Adapter) CompleteStream(...) error      { ... } // Return *provider.ProviderError for rate limits
func (a *Adapter) NeedsRefresh(...) bool         { return false }
func (a *Adapter) RefreshCredential(...) (...)   { return nil, provider.ErrNoRefreshNeeded }
func (a *Adapter) GetAuthFlow() provider.AuthFlowHandler { return nil } // Optional: automated auth
```

2. Blank-import it in `main.go`:

```go
import _ "llm-router/providers/myprovider"
```

3. Done. The dashboard's "Add Provider" dropdown will include `myprovider` automatically.

### Authentication Flow (Optional)

Implement `GetAuthFlow()` to enable wizard-based credential acquisition in the dashboard.
Instead of users manually entering JSON credentials, they can authenticate through a
guided flow (API key input, OAuth, multi-step auth, etc.).

See `providers/demo/doc.go` for comprehensive documentation and examples.

---

## Credential Load Balancing

llm-router implements intelligent credential load balancing to avoid rate limits:

- **LRU Selection**: Credentials are selected based on least-recently-used order
- **Priority Ordering**: Never-used credentials prioritized, then normal, then quota-exceeded
- **Intelligent Retry**: On rate limit (429), automatically rotates to next credential
- **Exponential Backoff**: When all credentials exhausted, backs off exponentially (1s→2s→4s→8s→16s→32s→64s)
- **Automatic Recovery**: Quota-exceeded credentials automatically recover after reset time

Configure retry cycles with `--max-retries` flag (default: 7).

---

## Usage

```bash
# First run — starts the bootstrap UI at localhost:8080
make run

# Build
make build

# Release build
make release
```

Then:

1. Open `http://localhost:8080` → Create admin account
2. Dashboard → **Overview** → Review system statistics
3. Dashboard → **Providers** → Add a provider (e.g. type `demo`, base URL can be empty for demo)
4. Dashboard → **Credentials** → Click "Add New Credential" → Select provider → Follow wizard
5. Dashboard → **Agents** → Create an agent to orchestrate multiple models (optional)
6. Dashboard → **Tokens** → Issue a router token, note the secret value
7. **Swagger UI** → Visit `http://localhost:8080/swagger/` for interactive API documentation
8. Use the token with any OpenAI-compatible client:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <your-router-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "demo/hello-model",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /` | Dashboard UI (Svelte SPA) |
| `GET /swagger/` | Interactive API documentation (Swagger UI) |
| `POST /v1/chat/completions` | OpenAI-compatible chat completions |
| `GET /v1/models` | List available models |
| `GET /api/llm-router/status` | System status |
| `POST /api/llm-router/login` | Admin login |
| `POST /api/llm-router/logout` | Admin logout |
| `POST /api/llm-router/bootstrap` | Create initial admin account |
| `GET /api/llm-router/dashboard/*` | Dashboard API endpoints (providers, agents, tokens, credentials, models) |

---

## Tech Stack

| Layer | Choice |
|-------|--------|
| CLI | `spf13/cobra` |
| HTTP Server | Go stdlib `net/http` (1.22+ routing) |
| Embedded DB | `go.etcd.io/bbolt` (pure Go, zero CGO) |
| Serialisation | Go stdlib `encoding/json` |
| Frontend | Svelte SPA |
| API Documentation | Swagger/OpenAPI 2.0 via `swaggo/swag` |

---

## Project Layout

```
llm-router/
├── main.go
├── cmd/
│   └── root.go                    # cobra CLI
├── docs/
│   └── generated/                 # auto-generated Swagger docs (gitignored)
│       ├── docs.go
│       ├── swagger.json
│       └── swagger.yaml
├── providers/
│   ├── demo/
│   │   ├── adapter.go             # reference implementation
│   │   └── doc.go                 # comprehensive adapter guide
│   └── agents/
│       ├── adapter.go             # virtual provider for agents
│       ├── decision.go            # decision model routing
│       └── injection.go           # instruction injection
└── internal/
    ├── config/config.go           # runtime config struct
    ├── db/db.go                   # bbolt open + bucket init
    ├── models/models.go           # all shared data structures
    ├── api/v1/handler.go          # OpenAI-compatible API
    ├── dashboard/
    │   ├── handler.go             # dashboard HTTP handler + Swagger UI
    │   ├── agents.go              # agent API endpoints
    │   └── ui/                    # Svelte SPA source
    ├── server/server.go           # composition root
    └── services/
        ├── admin/service.go       # bootstrap + session auth
        ├── token/service.go       # router-token CRUD + validation
        ├── provider/
        │   ├── adapter.go         # Adapter interface + registry
        │   └── service.go         # Provider CRUD
        ├── credential/service.go  # Credential Pool
        ├── agent/service.go       # Agent CRUD + validation
        ├── auth/service.go        # ephemeral auth flow state
        ├── router/service.go      # request routing
        ├── modelcache/service.go  # model list caching
        └── maintenance/service.go # background credential refresh
```
