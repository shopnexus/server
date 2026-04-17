# LLM Interface Refactoring — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the concrete `internal/infras/embedding` package with a pluggable `internal/infras/llm` package exposing a `Client` interface for embedding, text generation, chat, and structured output.

**Architecture:** Single `llm.Client` interface with all four capabilities. The existing Python HTTP embedding service becomes `PythonClient`, implementing only `Embed`. Unimplemented methods return `ErrNotSupported`. Consumers depend on the interface, not the concrete type.

**Tech Stack:** Go, net/http, sonic (JSON), encoding/json (for RawMessage), uber/fx (DI)

**Spec:** `docs/superpowers/specs/2026-03-25-llm-interface-design.md`

---

### Task 1: Create `llm` package — interface and models

**Files:**
- Create: `internal/infras/llm/client.go`
- Create: `internal/infras/llm/model.go`

- [ ] **Step 1: Create `client.go` with `Client` interface and `ErrNotSupported`**

```go
// internal/infras/llm/client.go
package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNotSupported is returned when a provider does not implement a capability.
var ErrNotSupported = errors.New("llm: operation not supported by this provider")

// Client defines the interface for LLM providers.
type Client interface {
	Embed(ctx context.Context, texts []string) ([]EmbedResult, error)
	GenerateText(ctx context.Context, params GenerateTextParams) (string, error)
	Chat(ctx context.Context, params ChatParams) (ChatMessage, error)
	GenerateStructured(ctx context.Context, params GenerateStructuredParams) (json.RawMessage, error)
}
```

- [ ] **Step 2: Create `model.go` with all public types**

```go
// internal/infras/llm/model.go
package llm

import "encoding/json"

// EmbedResult holds the dense and sparse vectors produced by an embedding provider.
type EmbedResult struct {
	Dense  []float32          `json:"dense"`
	Sparse map[uint32]float32 `json:"sparse"`
}

// GenerateTextParams holds parameters for text generation.
type GenerateTextParams struct {
	Prompt        string   `json:"prompt"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Temperature   float64  `json:"temperature,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// Role represents the role of a chat message sender.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatParams holds parameters for chat completion.
type ChatParams struct {
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// GenerateStructuredParams holds parameters for structured output generation.
type GenerateStructuredParams struct {
	Prompt      string          `json:"prompt"`
	Schema      json.RawMessage `json:"schema"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}
```

- [ ] **Step 3: Verify package compiles**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./internal/infras/llm/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/infras/llm/client.go internal/infras/llm/model.go
git commit -m "feat: add llm package with Client interface and models"
```

---

### Task 2: Create `PythonClient` implementation

**Files:**
- Create: `internal/infras/llm/python_client.go`

- [ ] **Step 1: Create `python_client.go`**

Move the logic from `internal/infras/embedding/embedding.go` into `python_client.go`, renaming types and updating error prefixes. Private HTTP types (`embedRequest`, `embedResponseEntry`, `embedResponse`) stay in this file.

```go
// internal/infras/llm/python_client.go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
)

// Compile-time interface check.
var _ Client = (*PythonClient)(nil)

// PythonConfig holds configuration for the Python embedding service HTTP client.
type PythonConfig struct {
	URL     string        `yaml:"url"`
	Timeout time.Duration `yaml:"timeout"`
}

// PythonClient is an HTTP client for the Python embedding service.
// It implements only the Embed method of the Client interface.
type PythonClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPythonClient creates a new PythonClient.
func NewPythonClient(cfg PythonConfig) *PythonClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &PythonClient{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// embedRequest is the JSON request body sent to the /embed endpoint.
type embedRequest struct {
	Texts []string `json:"texts"`
}

// embedResponseEntry is a single embedding entry from the service response.
type embedResponseEntry struct {
	Dense  []float32          `json:"dense"`
	Sparse map[string]float32 `json:"sparse"`
}

// embedResponse is the JSON response body from the /embed endpoint.
type embedResponse struct {
	Embeddings []embedResponseEntry `json:"embeddings"`
}

// Embed sends texts to the Python embedding service and returns dense+sparse vectors.
func (c *PythonClient) Embed(ctx context.Context, texts []string) ([]EmbedResult, error) {
	body, err := sonic.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("llm/python: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm/python: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm/python: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm/python: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm/python: service returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result embedResponse
	if err := sonic.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("llm/python: unmarshal response: %w", err)
	}

	out := make([]EmbedResult, len(result.Embeddings))
	for i, entry := range result.Embeddings {
		sparse := make(map[uint32]float32, len(entry.Sparse))
		for k, v := range entry.Sparse {
			idx, err := strconv.ParseUint(k, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("llm/python: invalid sparse index %q in result %d: %w", k, i, err)
			}
			sparse[uint32(idx)] = v
		}
		out[i] = EmbedResult{
			Dense:  entry.Dense,
			Sparse: sparse,
		}
	}

	return out, nil
}

// GenerateText is not supported by the Python embedding service.
func (c *PythonClient) GenerateText(ctx context.Context, params GenerateTextParams) (string, error) {
	return "", ErrNotSupported
}

// Chat is not supported by the Python embedding service.
func (c *PythonClient) Chat(ctx context.Context, params ChatParams) (ChatMessage, error) {
	return ChatMessage{}, ErrNotSupported
}

// GenerateStructured is not supported by the Python embedding service.
func (c *PythonClient) GenerateStructured(ctx context.Context, params GenerateStructuredParams) (json.RawMessage, error) {
	return nil, ErrNotSupported
}
```

- [ ] **Step 2: Verify package compiles**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./internal/infras/llm/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/infras/llm/python_client.go
git commit -m "feat: add PythonClient implementing llm.Client Embed method"
```

---

### Task 3: Update config types

**Files:**
- Modify: `config/struct.go:17,97-99`
- Modify: `config/config.default.yml:71-72`

- [ ] **Step 1: Rename `Embedding` to `LLM` in `config/struct.go`**

Line 17 — change the field on the `Config` struct:
```go
// Before:
Embedding Embedding `yaml:"embedding" mapstructure:"embedding" validate:"required"`
// After:
LLM LLM `yaml:"llm" mapstructure:"llm" validate:"required"`
```

Lines 97-99 — rename the type:
```go
// Before:
type Embedding struct {
	URL string `yaml:"url" mapstructure:"url" validate:"required,url"`
}
// After:
type LLM struct {
	URL string `yaml:"url" mapstructure:"url" validate:"required,url"`
}
```

- [ ] **Step 2: Update `config/config.default.yml`**

Lines 71-72 — rename the YAML key:
```yaml
# Before:
embedding:
  url: "http://localhost:5007"
# After:
llm:
  url: "http://localhost:5007"
```

- [ ] **Step 3: Verify config compiles**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./config/`
Expected: no errors (config package itself should compile; downstream errors are expected until Task 4)

- [ ] **Step 4: Commit**

```bash
git add config/struct.go config/config.default.yml
git commit -m "refactor: rename Embedding config to LLM"
```

---

### Task 4: Update `app.go` wiring

**Files:**
- Modify: `internal/app/app.go:16,40,138-142`

- [ ] **Step 1: Update import**

Line 16 — change:
```go
// Before:
"shopnexus-server/internal/infras/embedding"
// After:
"shopnexus-server/internal/infras/llm"
```

- [ ] **Step 2: Update fx.Provide**

Line 40 — change:
```go
// Before:
NewEmbeddingClient,
// After:
NewLLMClient,
```

- [ ] **Step 3: Rewrite the constructor function**

Lines 138-142 — replace the function:
```go
// Before:
func NewEmbeddingClient(cfg *config.Config) *embedding.Client {
	return embedding.NewClient(embedding.Config{
		URL: cfg.Embedding.URL,
	})
}
// After:
func NewLLMClient(cfg *config.Config) llm.Client {
	return llm.NewPythonClient(llm.PythonConfig{
		URL: cfg.LLM.URL,
	})
}
```

Note: Return type changes from `*embedding.Client` (concrete) to `llm.Client` (interface). This is required for fx to wire the interface type to consumers.

- [ ] **Step 4: Verify app package compiles**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./internal/app/`
Expected: may fail until Task 5 completes (catalog still imports old embedding package)

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go
git commit -m "refactor: wire llm.Client instead of embedding.Client in app"
```

---

### Task 5: Update catalog module consumers

**Files:**
- Modify: `internal/module/catalog/biz/interface.go:10,84,106,119`
- Modify: `internal/module/catalog/biz/search.go:33,269`

- [ ] **Step 1: Update `interface.go` import**

Line 10 — change:
```go
// Before:
"shopnexus-server/internal/infras/embedding"
// After:
"shopnexus-server/internal/infras/llm"
```

- [ ] **Step 2: Update struct field**

Line 84 — change:
```go
// Before:
embedding    *embedding.Client
// After:
llm          llm.Client
```

- [ ] **Step 3: Update constructor parameter**

Line 106 — change:
```go
// Before:
embeddingClient *embedding.Client,
// After:
llmClient llm.Client,
```

- [ ] **Step 4: Update constructor body assignment**

Line 119 — change:
```go
// Before:
embedding:    embeddingClient,
// After:
llm:          llmClient,
```

- [ ] **Step 5: Update `search.go` call sites**

Line 33 — change:
```go
// Before:
embeddings, err := b.embedding.Embed(ctx, []string{params.Query})
// After:
embeddings, err := b.llm.Embed(ctx, []string{params.Query})
```

Line 269 — change:
```go
// Before:
embeddings, err := b.embedding.Embed(ctx, texts)
// After:
embeddings, err := b.llm.Embed(ctx, texts)
```

- [ ] **Step 6: Verify full project compiles**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./...`
Expected: PASS (all references to old `embedding` package are now gone)

- [ ] **Step 7: Commit**

```bash
git add internal/module/catalog/biz/interface.go internal/module/catalog/biz/search.go
git commit -m "refactor: update catalog module to use llm.Client"
```

---

### Task 6: Delete old embedding package

**Files:**
- Delete: `internal/infras/embedding/embedding.go`

- [ ] **Step 1: Delete the old package**

```bash
rm -rf internal/infras/embedding/
```

- [ ] **Step 2: Verify full project compiles**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add -A internal/infras/embedding/
git commit -m "refactor: remove old embedding package, replaced by llm"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run full build**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go build ./...`
Expected: PASS

- [ ] **Step 2: Run tests (if any exist)**

Run: `cd /home/beanbocchi/shopnexus/shopnexus-remastered && go test ./internal/infras/llm/... ./internal/module/catalog/... 2>&1 | head -30`
Expected: PASS or no test files found

- [ ] **Step 3: Verify no remaining references to old package**

Run: `grep -r "infras/embedding" --include="*.go" .`
Expected: no output (no remaining references)
