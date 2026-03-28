# OpenCode Phase 0 Proof

Generated on 2026-03-28 from `task/20260328-1917-opencode-phase0-proof`.

Machine-readable report:
- `docs/proofs/2026-03-28-opencode-phase0-proof.json`

## Goal

Verify the external OpenCode plugin runtime contract before implementing the full agent-deck integration:

- plugin can read `AGENTDECK_INSTANCE_ID`
- plugin receives usable session events
- plugin can extract a real session ID
- plugin can write hook-compatible files on the host
- the same write path works across the sandbox boundary

## Commands Run

Host + sandbox proof:

```bash
GOCACHE=/tmp/agentdeck-go-build go run ./cmd/agent-deck \
  opencode-plugin phase0-proof \
  --mode both \
  --sandbox-image agent-deck-opencode-phase0:latest \
  --report-file docs/proofs/2026-03-28-opencode-phase0-proof.json
```

Sandbox proof image build:

```bash
docker build -t agent-deck-opencode-phase0:latest \
  -f sandbox/opencode-phase0-proof.Dockerfile \
  sandbox/
```

## Result

Overall result: PASS

Host mode:
- Plugin initialized and observed `AGENTDECK_INSTANCE_ID`
- Observed session ID: `ses_2cb149b5fffeK5xKWZgyIzvpmI`
- Extracted session ID field: `properties.sessionID`
- Host-visible hook JSON and `.sid` anchor were written successfully

Sandbox mode:
- Plugin initialized and observed `AGENTDECK_INSTANCE_ID`
- Observed session ID: `ses_2cb14884dffeulPA52fBUYAEPI`
- Extracted session ID field: `properties.sessionID`
- Host-visible hook JSON and `.sid` anchor were written successfully through the container mount

## Key Findings

- In these proof runs, the useful OpenCode events were `session.updated`, `session.idle`, `session.status`, `message.updated`, and `message.part.*`.
- The proof did not need a `session.created` event to bind the session reliably.
- The stable session ID location observed in both host and sandbox runs was `properties.sessionID`.
- Direct plugin file writes to a host-visible hooks directory work in both host and sandbox modes.
- Sandbox mode needs an explicit writable state path when the container runs as the host UID. The proof command now mounts and exports `XDG_STATE_HOME` accordingly.
- Concurrent plugin events need unique temporary filenames for atomic writes. The proof harness now uses per-write temp paths instead of a shared `*.tmp` file to avoid rename races.

## Environment Note

The repo's full `agent-deck-sandbox:latest` image build did not complete in this environment because `npm install -g @google/gemini-cli@0` failed with `ECONNRESET`. That failure is unrelated to the OpenCode plugin contract under test, so the proof used the dedicated `sandbox/opencode-phase0-proof.Dockerfile` image to validate the container boundary directly.
