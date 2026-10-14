# Harness validation

`make harness HARNESS=claude|codex|opencode` drives one real CLI harness through the
router, three resumed phases in one session, and checks the outcome. A matrix job per
harness runs it in CI (`.github/workflows/harness.yml`).

## What runs where

- `test/harness/plugins/harness-mock.lua` is the deterministic mock plugin. It answers
  every harness with a scripted sequence of shell tool calls. It makes no network calls.
- `scripts/harness/` is the driver. It configures each CLI for an isolated run
  directory, runs the three phases, checks the marker files, and checks the plugin's
  events.
- `internal/testutil/harnessmock/` tests the plugin under gopher-lua, the VM the router
  embeds.

## The scenario

The driver sends one prompt per phase. Each prompt carries `PHASE <n>` and `run=<nonce>`.

| Phase | Plugin action | Expected reply |
|-------|---------------|----------------|
| 1 | shell call writes `phase1.txt` and reads it back | `PHASE 1 COMPLETE` |
| 2 | shell call reads `phase1.txt`, writes and reads `phase2.txt` | `PHASE 2 COMPLETE` |
| 3 | shell call reads both files | `HARNESS-MOCK-OK` |

The plugin decides each turn from the message history alone. It keeps no state of its own:

- Without tools, or without a phase marker, the request is auxiliary and gets plain text.
  Title generation takes this path.
- A user turn with a phase marker and no tool result yet gets one shell tool call. The
  shell tool is the first candidate the harness advertises: `Bash` for Claude Code,
  `exec_command` for Codex, `bash` for OpenCode.
- A tool result gets the phase's expected output checked, then the reply. A result from
  another phase fails `tool_result_phase_mismatch`. Missing output fails
  `expected_output_missing`.
- A third call for the same conversation and phase fails `tool_call_repeated`. A healthy
  phase makes one call, so a repeat means the router withheld the tool result from history.
  Completion is idempotent: a user turn that follows an already valid tool result for the
  phase returns the reply instead of issuing another call.

Each phase logs two events, `Harness phase issues tool call` and `Harness phase completes`,
with the phase, step and history size. The driver reads them back through
`GET /api/events?filter.type=harness-mock&filter.run=<nonce>` and checks the sequence
`1:call 1:complete 2:call 2:complete 3:call 3:complete`, with the history growing on each call.
That is what proves the session history carried across the resumes.

## Per-harness wiring

- **Claude Code** runs with `-p`, a pinned `--session-id`, `--resume` on later phases,
  `--tools` and `--allowedTools`, and `ANTHROPIC_BASE_URL` set to the admin root. The router
  serves `/v1/messages`.
- **Codex CLI** runs `codex exec` with `--json` and `exec resume <thread>`. Its config sets
  `wire_api = "responses"`, because Codex 0.162 rejects the chat wire API. Sandbox and
  approval policy go in config, since `exec resume` does not accept `-s`. The sandbox is
  `danger-full-access`: the `workspace-write` bubblewrap sandbox cannot set up loopback
  in CI containers, so every tool call fails before the command runs.
- **OpenCode** runs `opencode run --format json` with `-s <session>` on later phases. The
  provider is inline config (`OPENCODE_CONFIG_CONTENT`) under `provider.<id>.options.baseURL`.

Every harness gets its own run directory under `HARNESS_RUN_ROOT`. Its home, XDG and
config directories all point inside that tree, so no run reads or writes the runner's
own state. Each phase's stdout and stderr are saved there as artifacts.

## Running locally

Start the stack and upload the plugin, then run one harness:

```
make start SCOPE=backend
make upload-plugins UPLOAD_STORE_DIR=/absolute/path/to/a/dir/holding/harness-mock.lua
make harness HARNESS=codex
make stop
```

The harness CLI must be on `PATH`. CI pins the versions in `harness.yml`; use the same
versions locally to reproduce a CI failure.

## Guards

- `TestPhaseReplyTokensMatchPlugin` fails if the driver's reply tokens drift from the plugin.
- `TestEventMessagesMatchPlugin` and `TestPluginScenarioWritesTheMarkerFiles` do the same
  for the log messages and the marker file names.
- The parser tests use outputs recorded from the real CLIs in `scripts/harness/testdata/`.
  A change in a CLI's output format fails them.

## Known gaps

- The Responses path is exercised by the Codex job. Stream tool-call translation through
  the router is only covered by that job, not by a unit test.
- The Claude job depends on Claude Code recognizing the model name. The plugin declares
  `claude-sonnet-4-5` because Claude Code rejects names it does not recognize.
- `previous_response_id` history is not loaded by the router, so a harness that relies on
  server-side response chaining is not covered.
