# Coragent V2

Coragent M1 is a line-oriented, read-only repository companion. It can inspect
workspace files through built-in `list`, `search`, and `read` tools, cite the
source lines it used, preserve sessions, resume them after process exit, and
cancel active Provider or tool work. M1 cannot edit files, run commands, or give
tools network access.

## Build

Coragent requires Go 1.24 or newer because M1 uses Go's descriptor-backed
`os.Root` workspace confinement. Mercury itself remains a Go 1.22 fixture.

```sh
CORAGENT_COMMIT="$(git rev-parse HEAD)"
go build -trimpath -buildvcs=false \
  -ldflags "-X main.version=${CORAGENT_COMMIT}" -o coragent ./cmd/coragent
```

The embedded commit must describe a clean checkout for official benchmark
evidence. A normal development build may omit `-ldflags` and reports `m1-dev`.

## Configure a Provider

Create `~/.coragent/settings.json`:

```json
{
  "provider": {
    "endpoint": "https://provider.example/v1/chat/completions",
    "model": "an-immutable-or-configured-model-id",
    "context_window": 32000,
    "max_output_tokens": 8000,
    "api_key_env": "OPENAI_API_KEY",
    "tool_choice": "auto"
  }
}
```

Set the named environment variable in your shell. The value is loaded only by
the Provider transport and is never placed in model messages, tools,
Transcripts, Events, logs, or artifacts.

A workspace-local `.coragent/settings.json` may override the model, explicit
limits, sampling settings, tool choice, and user preferences. It may not set or
override `endpoint` or `api_key_env`; those transport-authority values must come
from the trusted home settings file so an untrusted repository cannot redirect
an ambient credential. A saved session is bound to the non-secret digest of its
Provider, credential-source identity, and runtime profile. Resume fails closed
if that profile changes.

## Use the line-oriented CLI

Start a new session in a repository:

```sh
./coragent -C /path/to/repository
```

One input line submits one turn. Press Ctrl-C during a run to cancel it. Press
Ctrl-D while idle to exit without closing the saved session.

Session lifecycle commands are deliberately small:

```sh
./coragent sessions
./coragent resume <session-id>
./coragent close <session-id>
```

For scripts or a quick setup check, submit one prompt and exit:

```sh
./coragent -C /path/to/repository --prompt "Explain the configuration precedence and cite the implementing lines."
```

Durable sessions live directly under `~/.coragent/sessions/`. Closing is
non-destructive: it preserves replay but rejects new prompts.

## M1 benchmark

Copy `benchmarks/reference-profile.example.json` to
`benchmarks/reference-profile.json`, replace the placeholder with an immutable
model snapshot, and run the three I01-I04 rounds at different times:

```sh
go run ./cmd/m1bench round --suite <suite-id> --round 1 \
  --endpoint <fixed-chat-completions-url> --api-key-env OPENAI_API_KEY \
  --coragent-bin ./coragent --coragent-commit "${CORAGENT_COMMIT}" \
  --source-root .
# Run rounds 2 and 3 later with the same suite ID and comparison manifest.
go run ./cmd/m1bench report --suite <suite-id>
```

Attempt artifacts are retained under `artifacts/benchmarks/` and excluded from
Git. The M1 report passes only with at least 10 of 12 slots, every investigation
task passing at least twice, and no physical `safety_fail` execution.
Each logical slot contains `result.json`; retained physical executions live in
`execution-1/` and, after one replaceable infrastructure failure only,
`execution-2/`.

The official runner requires a clean source tree at the pinned commit and
rebuilds the binary with the fixed flags above; its SHA-256 and the Provider
endpoint SHA-256 are retained in every round. Report generation revalidates the
physical transcript, events, tool-call/result subsets, citations, workspace
digest, terminal outcome, and frontend artifacts instead of trusting only the
logical `result.json` summaries.
