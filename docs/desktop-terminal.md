# Desktop direct terminal

The desktop is an independent Go module in `desktop/`, using MyGo v0.8.1 native UI and its Ghostty terminal plugin. It reuses Kotlin Android's TCP invitation, E2EE v2, exact target and uncertain-delivery contracts. The main interface has no JavaScript frontend; PDF previews use a private embedded system WebView with the Go bridge disabled.

## Relay extension

`desktop_terminal_v1` is advertised only on Linux/macOS when the Herdr server reports private protocol 22. The adapter connects to the sibling `*-client.sock` Unix socket, negotiates `TerminalAnsi`, and sends `AttachTerminal` with takeover disabled. It does not spawn a shell, send a prompt, or change the PTY through ioctl.

| Request | Fields beyond the existing exact target | Result |
| --- | --- | --- |
| `terminal_open` | `columns`, `rows` | `stream_id` |
| `terminal_input` | `stream_id`, `terminal_sequence`, base64 `data` | Command result |
| `terminal_resize` | `stream_id`, `terminal_sequence`, `columns`, `rows` | Command result |
| `terminal_scroll` | `stream_id`, `terminal_sequence`, signed `lines` | Command result |
| `terminal_close` | `stream_id` | Owner-scoped cleanup |

Mutations require a paired controller and an exact live pane/terminal target. Terminal operations do not require a confirmed Codex conversation; they cannot grant conversation authority. Every operation rechecks device authorization and the live `pane.get` result. Input sequences start at 1 and cannot skip or repeat. Failed writes close the stream and never replay. Open is audited; raw terminal input is not included in write audit records.

`terminal_output` events carry `stream_id`, sequence starting at 1, geometry and base64 ANSI bytes. Native render frames must start with a complete frame and consecutive sequence. Mouse and keyboard modes are translated into terminal escapes; unsupported notifications/clipboard messages are not acted on. The desktop disallows remote clipboard writes. Output and packet buffers are bounded; overload fails closed. `terminal_closed` permits reconnecting, leaving the remote Agent alive.

One stream is allowed per transport client, at most 64 per Relay. Disconnect, target replacement and revoked device authorization release it. The desktop creates `terminal.Terminal` with `Conn` and `ResizeByConn`, so Herdr owns the final dimensions. Scroll is forwarded semantically to Herdr's scrollback.

The desktop writes input, resize and scroll requests in one sequence, allowing at most 32 unacknowledged operations. A write returns after transport dispatch; confirmations are checked asynchronously with an eight-second deadline. Rejections, unknown results and disconnection close the stream without replay. This avoids one network round trip per key while preserving the Relay's existing ordered admission and per-operation target/permission checks. It requires no Relay protocol change.

The encrypted receive loop routes terminal frames directly to the bounded terminal queue, bypassing UI update scheduling. Entering terminal mode cancels conversation paging and its update subscription; returning to chat recovers the previously loaded history. File transfers still share the encrypted transport, so this is not a guarantee of identical performance to SSH under all workloads.

## Files

The desktop uses existing `workspace_list`, `workspace_file_read` and `workspace_file_download` actions with `file_scope=workspace` by default and the captured exact target on every request. Controllers may explicitly authorize `filesystem` reads for their own device through `file_access_set`; revocation cancels readers and clears previews. Directory responses are checked for path containment, duplicate rows, cursor progress and unchanged revisions. Preview chunks require a single complete SHA-256 digest. Original downloads require an unchanged download revision, exact offset/size/path and a matching per-chunk digest. A local temporary file is renamed into the user-selected destination only after successful completion.

## Validation

Windows runs the desktop's headless native UI, Ghostty stream, transport and file tests, plus native Relay/Herdr integration and Codex process-ownership checks. Linux/WSL retains the Unix socket adapter and existing Codex authority tests. Windows Relay details and remaining runtime limits are in [Windows Relay](windows-relay.md). A socket fixture is not a deployment test: validate against a live protocol-22 Herdr before distributing this preview.

## Retained terminal panels

Exiting an Agent (for example with `/exit`) does not close its Herdr terminal.
Relay combines the active `agent.list` with terminal-backed entries from
`pane.list` / the session topology. Panels without an active Agent remain in
inventory with `terminal_only: true`, their original pane/terminal/workspace IDs,
and no conversation session or binding. Missing Agent kind alone does not mean
terminal-only: an untyped entry in `agent.list` remains an Agent.

Android and desktop label these entries as terminals and open the terminal view.
Terminal reads and input remain available under the existing device permissions.
Exiting or restarting an Agent advances the target generation, so old prompts,
approvals and drafts cannot be applied to the shell or the next Agent. A closed
panel disappears; a temporary terminal-inventory failure retains the last list
and marks it stale until a successful refresh.
