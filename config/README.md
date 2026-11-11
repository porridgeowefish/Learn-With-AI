# Learn-With-AI Configuration

Status: active
Owner: project maintainer
Last reviewed: 2026-10-13
Source of truth: `config.example.json` (shape) and `backend-go/internal/platform/config` (loader behavior). Current configuration knowledge is maintained in `docs/harness-skill/facts/personalization.md`; the workspace boundary is in `docs/harness-skill/facts/workspace.md`.

## File

`config.local.json` in the workspace root is the local configuration file. The
workspace root defaults to `<repo>/workspace/` (ADR-0023); create the file by
copying `config/config.example.json`. It is gitignored and is never rewritten
during load.

## Precedence

```text
compiled defaults < config.local.json < LWA_* environment < explicit CLI flags
```

Key environment variables:

```text
LWA_SERVER_PORT          server.port
LWA_WORKSPACE_ROOT       workspace.root
WORKSPACE                workspace.root (compatibility alias)
LWA_PRIMARY_AI_API_KEY   AI provider key when apiKeyEnv names it
LWA_WEB_SEARCH_API_KEY   web search key when apiKeyEnv names it
CLAUDE_BIN / CODEBUDDY_BIN / HERMES_BIN / CODEX_BIN / TRAE_BIN   runtime binaries
```

CLI flags (added in iteration 14): `--port`, `--workspace`, `--config`.

## Legacy flat keys (read-compatible)

`askAiProviders`, `agentRuntime`, `agentRuntimeBins`, `imageApiKey`,
`imageBaseURL`, `imageModel`, `imageBackupApiKey`, `imageBackupBaseURL`,
`imageBackupModel`, `imagePromptModel`, `pythonBin` remain readable. When
both a sectioned key and its legacy alias exist, the sectioned key wins and
loading emits a non-secret warning. Existing inline `apiKey` values stay
readable; the environment variable named by `apiKeyEnv` wins over inline
keys. The committed example contains no secrets.
Existing environment names with the former prefix remain readable.
