# Native agent management (local extension)

Android **0.2.9 / versionCode 12**, computer Relay **0.23.5+codexfix.7**.
The existing native TCP Gateway remains codexfix.6; pairing/encryption are unchanged.

## Phone entry points

- Agent card **⋯ → 重命名 Agent**, or the same item directly in conversation **⋯**.
  This renames the **Herdr Tab**. Split panes in the same tab share its label.
  It does not restart an agent or rename the underlying Codex conversation.
- Workspace heading **＋ Agent**. Empty workspaces remain visible. The top-level
  new-agent action now chooses a workspace before showing installed agent profiles.
  The fixed workspace ID, server session and directory are included in the request;
  equal directory paths or labels do not merge workspace identities.
- Agent/conversation **⋯ → 模型 / 思考强度**. Currently **Codex only**, and only when
  the exact active session and local daemon can be verified. Readers can inspect
  settings but cannot modify them; other agent kinds show an explicit unsupported message.

The launch name follows Herdr's existing `[a-z][a-z0-9_-]{0,31}` restriction.
Chinese display labels can be applied afterward with Rename. Initial prompts may
be left empty. Launch controls wait for the result, and uncertain outcomes require
manual inspection before creating again. Neither launch nor model changes are
silently retried after a lost acknowledgement.

## Project directories (after v0.3.4)

A controller can create a workspace or start an Agent in an existing local
absolute directory accessible to the Relay's OS account. It need not be under
that account's home: `D:\platform`, `/mnt/projects` and `/Volumes/Projects` are
valid examples. No allowlist or Gateway change is needed. Android and desktop
send the remote path unchanged; drive-relative paths such as `D:platform` and
UNC/device paths are not supported. When starting in a selected workspace, the
Relay checks its ID and current directory before creating the tab, resolving
links and Windows junctions. Refresh the workspace list if it has moved. This
changes neither reader permissions nor file browsing/download scope.

## Per-session model settings

New encrypted Relay commands:

- `get_agent_settings` (authenticated read): includes the exact normal pane target.
  Returns `available`, `model`, `effort`, `provider`, `settings_revision`, `models`,
  and `next_turn`; unavailable targets return `available:false` with a reason.
- `set_agent_settings` (authenticated, audited controller write): includes the exact
  pane target and **only** `model`, `effort`, `settings_revision` as setting values.

The local Codex daemon is reached via the configured transcript root's private Unix
control socket. Calls are limited to initialization, loaded-thread inventory,
`thread/read` with `includeTurns:false`, `model/list`, and (explicit writes only)
`thread/settings/update`. The last call sends **threadId/model/effort only**.
There is no thread creation/resume, terminal typing, model execution, permission
change, global config write or credential forwarding.

Model and effort values come from the running daemon's catalog, not an APK allow-list.
The exact loaded thread ID and working directory must match. Herdr pane, terminal,
generation, process-bound conversation selection and current device authorization
are rechecked before dispatch. Settings are re-read after fetching the catalog to
reject stale selections; the model/effort result is read back before claiming success.
The protocol has no atomic compare-and-swap against unrelated desktop clients; a
later desktop change can still supersede a phone choice.

Settings apply to **subsequent turns** in this session, not the already-running model
call. A missing acknowledgement or inconsistent readback is `dispatched_unknown`:
refresh actual settings before another write. The original creator can be Codex
Desktop even when the thread is now running in a Herdr TUI; originator metadata is
not used to reject an otherwise exactly verified live thread.

## Validation and operational boundary

- Whole Go tests and vet; race checks for codexruntime/protocol/app/coordinator.
- Runtime fixture verifies live catalog, exact three-field updates, effective readback,
  stale settings, revoked target, unadvertised model/effort, hidden model, wrong
  thread/CWD, unloaded threads, lost acknowledgement and no replay.
- Android tests cover exact workspace payloads, empty/duplicate workspaces, Chinese
  tab labels, invalid launch names, real native menus/forms, progress/validation,
  no duplicate submission, dynamic efforts, reader permissions and mismatched receipts.
- Live QA reads settings for all five current Codex agents without changing any
  model, effort, agent name or creating an agent. Mutation tests use isolated mocks,
  not the user's working agents. No physical phone is attached.
- Cover-install the APK using its existing signature. **No re-pairing is needed.**
  Only the computer Relay is restarted; Herdr, agents, Gateway and TCP credentials
  are left intact.
