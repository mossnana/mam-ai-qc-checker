# AI QC Checker — MVP

This repository is a runnable foundation for the event-driven QC platform in
`REQUIREMENT.md`. This MVP accepts PNG/JPEG/GIF images, shows a browser
preview, and runs real `IMAGE_DIMENSION`, `FILE_SIZE_LIMIT`, and sampled
`COLOR_PALETTE_COMPLIANCE` checks against the uploaded file, plus a visual-AI
check implemented with custom Microsoft Agent Framework for Go agents.

## Run

```bash
cp .env.example .env
# Edit .env: select AI_QC_AGENT and provide its required credential.
docker compose up --build
```

`AI_QC_AGENT` selects exactly one implementation. API agents use a provider API
key; CLI agents use the matching host sign-in directory, mounted read-only.

| Value | Implementation | Required configuration |
| --- | --- | --- |
| `codex-api` | OpenAI API through the Agent Framework OpenAI provider | `OPENAI_API_KEY` |
| `codex-cli` | Codex CLI custom agent | `CODEX_AUTH_DIR` |
| `claude-code-api` | Anthropic Messages API through the Agent Framework Anthropic provider | `ANTHROPIC_API_KEY` |
| `claude-code-cli` | Claude Code CLI custom agent | `CLAUDE_AUTH_DIR` |

Set `AI_QC_MODEL` to override the selected agent's model. API agents otherwise
use `gpt-4.1-mini` and `claude-sonnet-4-5`; CLI agents preserve their own
configured default.

### Configure the AI connection in the app

The **AI connection** button lets an authorized operator change the active
agent without rebuilding the stack. For an API agent, paste the provider key;
for a CLI agent, upload a ZIP of the *contents* of the CLI profile folder
(`.codex` or `.claude`). The checker extracts it into its private persistent
volume and runs the selected CLI with that directory as its profile. The key is
never returned to the browser or saved in browser storage.

The settings API is on port `8084`. Before putting it on a real host, set both
of these deployment variables and expose the API only through your authenticated
application/proxy:

```bash
SETTINGS_ADMIN_TOKEN=use-a-long-random-secret
SETTINGS_ALLOWED_ORIGIN=https://qc.example.com
# The browser must be able to reach the settings API through this URL.
VITE_AI_SETTINGS_API=https://qc.example.com/ai-settings
```

Operators enter the settings token in the dialog; it is retained only for the
current browser session. The checker stores credentials with owner-only file
permissions in the `ai-settings` Docker volume. Treat that volume as a secret,
back it up accordingly, and do not expose port 8084 directly to the internet.
The current MVP has no tenant identity model, so this is a deployment-wide
connection—not a per-user or per-project credential store.

Open [http://localhost:5173](http://localhost:5173), select an image, add one
or more rules from the registry, and choose **อัปโหลดและตรวจ**. The dev intake
adapter receives the file directly and writes it to an isolated Compose volume;
the colour checker reads its actual pixels from that volume.

## Current topology

```text
frontend/mam-ai-qc-checker ──HTTP──> intake-svc (8081)
          │                         ruleset-svc (8082)
          │                         inspection-svc (8083)
          │                                  │
          └─────────────────────── NATS JetStream ──> checker-dimension
                                                   ├─> checker-filesize
                                                   ├─> checker-color
                                                   └─> checker-ai-visual ──> selected custom Agent Framework agent
```

Each checker announces a manifest to `qc.registry.announce` at startup and
every 60 seconds, consumes its own `qc.check.*.v1` subject in a queue group,
and emits `qc.inspection.task.completed.v1`. The inspection service keeps the
rule snapshot with the job and decides `PASSED`, `FAILED`, or `NEEDS_REVIEW`.

## Visual AI QC configuration

The visual service owns a conservative default system prompt in
`backend/checker-ai-visual/system-prompt.md`. It sends the primary and optional
reference image as Agent Framework data attachments, and uses the framework's
structured-output support for API agents to enforce `summary` and `findings[]`
(category, severity, confidence, message) before its normalized result returns
via NATS. CLI agents receive the same attachments as temporary local input files
and validate their schema-conforming JSON response before publishing it. To
tailor what gets checked or the wording, mount a replacement prompt file and set
`AI_QC_SYSTEM_PROMPT_FILE` to that path.

This is a local *service*, not a local model: the artwork is sent either to the
provider API or to the selected CLI's configured provider. Use a local model
adapter if air-gapped inference is required.

## Intentional next increments

The SRS is larger than a first vertical slice. Before a production rollout,
replace the local development intake adapter with RustFS/S3 presigned uploads,
add persistent repositories and transactional outbox, JetStream durable pull
consumers/DLQ, IAM/project/review/report services, SSE, and the Python OCR/CV
checkers. The common contracts in `backend/internal/contracts` are the
extension point for those services.
