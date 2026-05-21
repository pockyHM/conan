# Model Management CLI Design

**Goal:** Add CLI-based model configuration management so users can add and switch Anthropic and OpenAI-compatible models without manually editing `~/.conan/config.yaml`.

**Status:** Draft approved for planning.

## Background

Conan already loads global model configuration from `~/.conan/config.yaml` into `configschema.GlobalConfig`:

```yaml
default_model: qwen-prod
models:
  - name: qwen-prod
    type: openai
    endpoint: https://example-compatible-endpoint/v1
    model: qwen-max
    api_key: sk-...
```

The existing LLM factory consumes `models` and `default_model`, so model management should write the existing config shape instead of introducing another storage layer.

## Commands

Add a `model` command group to `cmd/conan`:

```text
conan model add
conan model list
conan model use <name>
conan model remove <name>
```

### `conan model add`

Starts an interactive terminal wizard.

Flow:

1. Select provider preset:
   - Anthropic
   - OpenAI
   - GLM
   - MiniMax
   - Qwen
   - Kimi
   - Custom OpenAI-compatible
2. Enter config name, e.g. `qwen-prod`.
3. Enter API key.
4. Determine endpoint:
   - Preset providers use a built-in endpoint constant.
   - Custom OpenAI-compatible prompts for endpoint.
5. Try to discover models when the provider supports listing models.
6. If discovery succeeds, let the user choose from the returned model IDs.
7. If discovery fails or is unsupported, prompt for a model name manually.
8. Ask whether to set the new model as `default_model`.
9. Save the updated `~/.conan/config.yaml`.

API keys are saved directly into the config file, per product decision.

### `conan model list`

Print configured models in a simple table:

```text
NAME          TYPE       MODEL             ENDPOINT                         DEFAULT
qwen-prod     openai     qwen-max          https://.../v1                   *
claude-main   anthropic  claude-sonnet...  https://api.anthropic.com
```

The command should not print API keys.

### `conan model use <name>`

Sets `default_model` to an existing model config name and writes the global config file.

If `<name>` does not exist, return a clear error and leave the file unchanged.

### `conan model remove <name>`

Removes the named model config.

If removing the current default model, clear `default_model` unless another model remains and the user confirms a replacement in interactive mode. For the first implementation, keep this command non-interactive and clear `default_model`.

## Provider Presets

Represent presets as data, not switch-heavy command logic:

```go
type ModelPreset struct {
    ID              string
    DisplayName     string
    Type            string
    Endpoint        string
    SupportsList    bool
    ListMode        string
    DefaultModelHint string
}
```

Rules:

- `Type` must be either `anthropic` or `openai`, matching existing `configschema.ModelConfig.Type`.
- GLM, MiniMax, Qwen, and Kimi should use `type: openai` because they are OpenAI-compatible integrations.
- Endpoint constants must be verified against official provider documentation during implementation before being hardcoded.
- If an endpoint is not confidently verified, the preset should still exist but prompt for endpoint confirmation/editing before saving.

Initial preset set:

```text
anthropic  -> type anthropic, official Anthropic API endpoint
openai     -> type openai, official OpenAI API endpoint
qwen       -> type openai, DashScope OpenAI-compatible endpoint
kimi       -> type openai, Moonshot/Kimi OpenAI-compatible endpoint
glm        -> type openai, Zhipu/BigModel OpenAI-compatible endpoint
minimax    -> type openai, MiniMax OpenAI-compatible endpoint
custom     -> type openai, endpoint entered by user
```

## Model Discovery

Add a small model discovery helper for interactive `model add`.

Interface:

```go
type ModelLister interface {
    ListModels(ctx context.Context, endpoint string, apiKey string) ([]string, error)
}
```

For OpenAI-compatible providers, call:

```text
GET <endpoint>/models
Authorization: Bearer <api_key>
```

Expected response shape:

```json
{
  "data": [
    {"id": "model-a"},
    {"id": "model-b"}
  ]
}
```

Discovery behavior:

- Use a short timeout.
- Sort model IDs alphabetically for stable display.
- If the request fails, returns non-2xx, or response cannot be parsed, show a concise warning and fall back to manual model input.
- Never fail the whole `model add` flow just because model discovery failed.

Anthropic model discovery should only be enabled if implementation verifies the correct official endpoint and response shape. Otherwise Anthropic uses manual model entry with a default hint.

## Config Persistence

Add global config writing next to the existing loader logic, preferably in `internal/config`.

Required behavior:

- Load existing `~/.conan/config.yaml` if present.
- Preserve unrelated global config fields.
- Create parent directory if missing.
- Write YAML with file mode `0600`, because API keys are stored directly.
- Use atomic write where practical:
  - write to a temp file in the same directory,
  - chmod `0600`,
  - rename over the target.
- Trim trailing slashes from endpoints before saving, matching current load normalization.
- Reject duplicate model names in `model add` unless an explicit replace flow is added later.

No separate model storage file should be introduced.

## Interactive UI

Use a minimal terminal prompt flow rather than the main Bubble Tea TUI.

Design constraints:

- It must work from `conan model add` in a normal shell.
- It should be testable by injecting stdin/stdout.
- API key input should avoid echoing when possible; if the terminal does not support hidden input, fall back to normal input with a warning.
- Prompts should be short and deterministic so command tests can assert output.

Example session:

```text
$ conan model add
Select provider:
  1) Anthropic
  2) OpenAI
  3) GLM
  4) MiniMax
  5) Qwen
  6) Kimi
  7) Custom OpenAI-compatible
Provider [1-7]: 5
Config name: qwen-prod
API key: ********
Fetching available models...
Select model:
  1) qwen-max
  2) qwen-plus
  3) Enter manually
Model [1-3]: 1
Set as default model? [y/N]: y
Saved model qwen-prod to ~/.conan/config.yaml
```

## Testing

Add tests for:

- Config writer creates `config.yaml` with `0600` permissions.
- Config writer preserves unrelated fields such as cluster, security, memory, and logging settings.
- `model list` hides API keys and marks the default model.
- `model use` updates `default_model` only when the model exists.
- `model remove` removes the target and clears default when needed.
- OpenAI-compatible model lister parses `/models` response.
- Model lister falls back cleanly on HTTP errors or malformed JSON.
- `model add` scripted stdin flow saves expected config.

## Non-goals

- No encrypted secret store in the first version.
- No OAuth/device login flows.
- No automatic provider account validation beyond optional model listing.
- No main TUI settings screen yet.
- No remote syncing of model configs.
