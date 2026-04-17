# LLM Interface Refactoring

## Summary

Refactor `internal/infras/embedding/` into `internal/infras/llm/` — a pluggable interface supporting embedding, text generation, chat, and structured output. The current Python HTTP service becomes one implementation (`PythonClient`) that only implements `Embed`.

## Motivation

The existing `embedding.Client` is a concrete struct tightly coupled to the Python embedding service. The codebase needs broader LLM capabilities (text generation, chat, structured output) and the ability to swap providers via config.

## Package: `internal/infras/llm/`

### Files

| File | Purpose |
|------|---------|
| `client.go` | `Client` interface definition + `ErrNotSupported` sentinel |
| `model.go` | All request/response types |
| `python_client.go` | `PythonClient` — HTTP client for the Python embedding service (implements `Embed` only) |

### Interface (`client.go`)

```go
package llm

type Client interface {
    Embed(ctx context.Context, texts []string) ([]EmbedResult, error)
    GenerateText(ctx context.Context, params GenerateTextParams) (string, error)
    Chat(ctx context.Context, params ChatParams) (ChatMessage, error)
    GenerateStructured(ctx context.Context, params GenerateStructuredParams) (json.RawMessage, error)
}
```

`ErrNotSupported` is returned by providers that don't implement a capability.

### Models (`model.go`)

Public types shared across providers. Private HTTP-layer types (e.g., `embedRequest`) stay in their respective provider files.

**Embedding:**
- `EmbedResult` — `Dense []float32`, `Sparse map[uint32]float32` (unchanged from current)

**Text generation:**
- `GenerateTextParams` — `Prompt string`, `MaxTokens int`, `Temperature float64`, `StopSequences []string`

**Chat:**
- `Role` — string type (`RoleSystem`, `RoleUser`, `RoleAssistant`)
- `ChatMessage` — `Role Role`, `Content string`
- `ChatParams` — `Messages []ChatMessage`, `MaxTokens int`, `Temperature float64`

**Structured output:**
- `GenerateStructuredParams` — `Prompt string`, `Schema json.RawMessage`, `MaxTokens int`, `Temperature float64`

### PythonClient (`python_client.go`)

Moved from `internal/infras/embedding/embedding.go`. Contains the `PythonClient` struct, its config, constructor, and all private HTTP-layer types (`embedRequest`, `embedResponseEntry`, `embedResponse`).

Changes:
- Struct renamed from `Client` to `PythonClient`
- Config struct renamed from `Config` to `PythonConfig` (retains both `URL string` and `Timeout time.Duration` fields)
- Constructor renamed from `NewClient` to `NewPythonClient`
- `Embed` method preserved as-is (HTTP POST to `/embed`)
- `GenerateText`, `Chat`, `GenerateStructured` return `ErrNotSupported`
- Error message prefix changed from `embedding:` to `llm/python:`
- Compile-time interface check: `var _ Client = (*PythonClient)(nil)`

## Consumer Changes

### `CatalogBizHandler` (`internal/module/catalog/biz/interface.go`)

- Field: `embedding *embedding.Client` → `llm llm.Client`
- Import: `embedding` → `llm`
- Constructor param: `embeddingClient *embedding.Client` → `llmClient llm.Client`

### `search.go` (`internal/module/catalog/biz/search.go`)

- All occurrences of `b.embedding.Embed(...)` → `b.llm.Embed(...)` (two call sites: `Search` method and `UpdateProducts` method)

### `catalog/fx.go` (`internal/module/catalog/fx.go`)

- No code changes needed — fx resolves `llm.Client` automatically since `app.go` provides the interface type. Listed here to document the dependency chain.

### `app.go` (`internal/app/app.go`)

- `NewEmbeddingClient` → `NewLLMClient`
- Returns `llm.Client` (interface, not `*llm.PythonClient`) — deliberate change from concrete to interface return type so fx wires consumers to the interface
- Import: `embedding` → `llm`

### Config (`config/struct.go` + `config/config.default.yml`)

- Struct field: `Embedding Embedding` → `LLM LLM`
- Struct type: `Embedding` → `LLM`
- YAML key: `embedding:` → `llm:`
- `mapstructure` tag: `mapstructure:"embedding"` → `mapstructure:"llm"`

## Deleted

- `internal/infras/embedding/` — entire directory removed

## Out of Scope

- Concrete implementations for GenerateText/Chat/GenerateStructured
- Streaming support
- Provider selection via config (only one provider exists today)
