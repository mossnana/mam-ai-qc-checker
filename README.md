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

`AI_QC_AGENT` selects exactly one implementation. All configuration comes from
the deployment environment; the browser cannot change the active agent or
credentials. API agents use a provider API key; CLI agents use the matching
host sign-in directory, mounted read-only.

| Value | Implementation | Required configuration |
| --- | --- | --- |
| `codex-api` | OpenAI API through the Agent Framework OpenAI provider | `OPENAI_API_KEY` |
| `codex-cli` | Codex CLI custom agent | `CODEX_AUTH_DIR` |
| `claude-code-api` | Anthropic Messages API through the Agent Framework Anthropic provider | `ANTHROPIC_API_KEY` |
| `claude-code-cli` | Claude Code CLI custom agent | `CLAUDE_AUTH_DIR` |

Set `AI_QC_MODEL` to override the selected agent's model. API agents otherwise
use `gpt-4.1-mini` and `claude-sonnet-4-5`; CLI agents preserve their own
configured default.

### Configure a Codex CLI mount

For `codex-cli`, set `CODEX_AUTH_DIR` to a host directory containing the
file-based Codex login profile, including `auth.json`. The Compose service mounts
that directory read-only at `/auth/codex`, then copies it only into the
container's disposable profile before starting the checker. The browser cannot
read or replace these credentials.

On a headless customer host, create a file-backed profile with `codex login`
or `codex login --device-auth`, then set `CODEX_AUTH_DIR` to that profile. If
the host normally stores Codex credentials in a system keychain, create a
file-based profile first; a directory mount cannot carry OS keychain entries.

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
