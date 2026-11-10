# Codex terminal-scoped primary session discovery

Relay codexfix.18 / Pocket 0.2.30 applies this policy **only to `agent == codex`**.
All other providers keep the existing Herdr Relay discovery, session identifiers,
history parsers, and interaction authorization. No Codex runtime or hook report
is required for them.

## Shared app-server selection order

1. Resolve the exact Herdr terminal to its outer native Codex TUI process. Verify
   the foreground process group, tty/session, process ancestry, owner and birth
   identity. Node launchers and helper/zombie descendants are not extra TUI roots.
2. For shared mode, read this process's owned loopback listeners and its configured, allowed Codex
   store. Query the local app-server's loaded-thread metadata and `codex_tui` MCP
   origin associations. **Only origins owned by this TUI process become candidates**;
   the daemon's global loaded list is never a terminal's candidate list by itself.
3. Reduce this terminal's candidates using explicit parent/subagent metadata.
   Every excluded child must reach a root within the same association, without
   missing links, cycles or contradictory session-tree metadata. An independently
   forked conversation remains a root. A single connected root is selected without
   requiring or trusting Herdr's reported ID.
4. If the verified native TUI argv explicitly configures the supported ID-first
   terminal-title layout, select using the fresh title ID as described below.
   Otherwise, if multiple roots remain, validate the fresh Herdr report's
   provider/kind/UUID and match it against those roots. A matching connected root
   is selected; unrelated or child IDs cannot supply replacement candidates.
5. If still unresolved, retain no chosen current ID and show a reason. Suggest
   trying `codex --no-daemon` and checking the Herdr hook where appropriate. This
   is guidance, **not a guarantee**. Explicit standalone startup uses the separate
   writer-ownership path below, never this shared-server endpoint.

Names, human-readable title text, creation origin, recent activity, direct-input
flags, transcript length and whether a conversation has materialized do not
choose a root. Only the explicitly configured title ID can break a tie. Discovery never sends `/status`, reads a footer ID, installs a dynamic hook,
attaches a debugger, resumes a thread, or invokes a model/tool. Manual pins and
associated-history substitutions are not part of this policy.

Without the title-ID signal, in a multiple-root tie the matching Herdr report is
a trusted input, not an
independent TUI selection getter. If Herdr incorrectly names another otherwise
valid root in this same candidate set, these checks cannot establish the actual
TUI selection. The policy must not be described as eliminating that trust boundary.

## Explicit terminal-title ID signal

Relay-launched built-in Codex agents default to
`-c 'tui.terminal_title=["thread-id","thread-name"]'`. The inspected native TUI argv
must contain that effective layout (or `["thread-id"]`); Node wrappers, positional
prompt text after `--`, unknown options, alternative layouts and disabling or
unrecognized overrides do not enable this path. Merely changing a Tab name, a
UUID appearing in output, or a global config file is not enough. No global
configuration or existing TUI is modified by Relay.

Read `terminal_title` from fresh Herdr `pane.get` responses. In the supported
layout, ID is the first ` | `-separated segment. Accept a complete UUID or Codex
0.166.0's exact 29-character UUID prefix plus `...`; match it against the full IDs
of this terminal's verified runtime roots in shared mode. In embedded mode,
match against **all** this TUI's verified exclusive writer IDs first (including
unmaterialized IDs for prefix collision checks), then validate the selected
thread's exact identity, store, project and primary/non-subagent classification.
The selected thread must have a valid metadata header. Other retained writer IDs
without history do not block it; they cannot be selected as a substitute. A child
title is never mapped to its parent. No thread from another terminal can be
introduced by title text.

When this signal is enabled, it is required even for a single root. Empty,
unrecognized, conflicting or ambiguous titles do not fall back to a retained
hook. A valid title takes precedence over a stale hook report, which is retained
only for diagnostics. This intentionally solves the case where `/new` or `/resume`
leaves old roots loaded and Herdr's old report has not updated.

The title is re-read before accepting a result and during protected history and
conversation operations. A changed terminal/process/layout/selected ID invalidates
old leases. A human-readable name change with the same ID does not switch history.
The existing read-only diagnostic interface reports
`selection_evidence: "terminal_title_id"`; raw titles, names and transcript text
are not added to diagnostics. No phone-side candidate selection is introduced.

**Trust boundary:** this is a terminal display signal, not authenticated Codex
selection RPC. Herdr cannot prove which writer emitted OSC or that a retained
title has already refreshed after a switch. Re-reading the same stale title does
not prove freshness; a short switch-before-redraw window and deliberately spoofed
OSC output are not eliminated. It must not be described as cryptographic or
atomic selection proof. This feature never parses recent conversation text or
sends `/status`, `/title`, or rename probes into user terminals.

## Embedded / standalone writer path

An explicit `--no-daemon` option on the verified **native TUI** selects this path.
From codexfix.18, an explicitly configured title-ID TUI can also use it **only
when its own live exclusive writer locks are positively verified**. Codex
0.166.0 excludes ordinary `-c` overrides such as `tui.terminal_title` from shared
daemon reuse; the flag alone is therefore not a reliable mode detector. A failed
shared RPC, absent lock file, another PID's lock, or title alone never proves
embedded mode. Without positive writer evidence, shared discovery stays separate.
Unknown CLI options, prompt text and non-TUI commands do not enable title trust.

1. Preserve the terminal, foreground group, owner, PID birth and allowlisted
   `CODEX_HOME` checks. Do not require a loopback listener or a shared socket.
2. Read `/proc/<TUI-pid>/fd` and `fdinfo` for this store's
   `thread-writer-locks/<thread-id>.lock`. Require a live, exclusive, whole-file
   kernel **FLOCK** owned by that PID, matching the regular file's device/inode.
   Lock-file existence, another process's locks, read locks, open history files,
   removed/replaced files and symlinked lock namespaces are not proof. Relay never
   opens/acquires/releases/deletes these lock files.
3. With the supported title-ID signal, first match the complete/prefix ID against
   every verified writer. Require exactly one match; read and validate only that
   exact ID's metadata, with primary classification, no delegated parent, matching
   project/store/session identity. A child, corrupt header or conflicting project
   stays unconfirmed. Re-check the whole writer set and selected metadata before
   accepting the result.
4. Without title-ID evidence, preserve the original complete-set metadata and
   parent-tree reduction. Missing metadata or ambiguous roots do not become a
   hook-only fallback. Independent forks remain roots.
5. A selected unmaterialized new thread still waits for its first metadata
   header, normally after the first user turn (location misses have a bounded
   five-second cache). A `/new` placeholder retained beside a valid resumed
   target no longer blocks that target. This was the production failure missed
   by the earlier explicit-`--no-daemon` experiment.
6. Old hook reports cannot select between embedded roots. Title display signal
   limits above still apply; this does not establish an atomic selection getter.

Ownership and identity/ancestry metadata are rechecked, including after the final
metadata read. Process, mode, writer inode, store or selected-ID changes revoke
old leases. Bound history/send/approval still work; no mobile selection API is
added. Mode and proof source are included in the existing diagnostic snapshot.

Standalone has no externally queryable settings/turn endpoint in the tested
version: model changes must be made in the computer TUI. Run status (codexfix.19) reads lifecycle metadata from the exact verified primary
rollout after revalidating ownership. Start/complete/abort records provide whole-turn
elapsed time; absent, partial or conflicting records keep timing unknown. Herdr
idle is not completion proof. Neither operation may query or write a same-store
shared server just because that socket exists. See [run state](agent-run-state.md).

To clear retained multiple roots, the user can normally exit the old TUI and run
`codex --no-daemon resume <thread-id>`. A session still owned by another backend
can display an ownership-conflict dialog; Relay does not kill that backend,
interact with the dialog, fork the conversation or alter Codex Desktop settings.

## Windows x64 process and runtime evidence

Windows uses the same root reduction, configured-title selection and binding
leases. It replaces `/proc` with read-only native process queries: current-user
SID, creation time, session, parent PID, image, command line, working directory,
and the allowlisted `CODEX_HOME` environment key. Herdr reports the foreground
root (often the npm Node launcher); only its unique native Codex child is selected.
Daemon/exec/remote commands, foreign parents, ambiguous children and reused parent
PIDs are rejected. Process identity is rechecked after discovery.

Shared-runtime origins come from `GetExtendedTcpTable` owner-module listener
records, including endpoint creation time. Only IPv4 loopback listeners owned by
the selected TUI qualify. The existing control socket and runtime metadata APIs
work on Windows; global loaded threads remain insufficient by themselves.

For embedded runtimes, Relay opens existing writer files read-only, queries
`FileProcessIdsUsingFileInformation`, and requires the TUI to be the sole external
file user. Reads at offset 0, 4096 and beyond EOF must return the kernel's lock
violation expected from Codex's exclusive whole-file lock. Ordinary open files,
shared locks, first-byte-only locks, reparse files and changed file identities are
rejected. Owner/file/process identity is checked again. Relay neither takes nor
releases these locks; its own concurrent inspection handles are excluded from
external ownership. Additional external handles can conservatively prevent proof.
These probes rely on Codex's lock contract, not a Windows API enumerating exact
lock ranges. At most 4096 lock-directory entries and 64 owned threads are examined.

Windows working-directory aliases (including packaged-app redirection) are
accepted only when both paths identify the same directory object. No prefix or
basename matching is used. Native amd64 process layouts are currently supported;
other architectures fail closed. Embedded model settings remain unavailable when
Codex exposes no control endpoint; never use another shared runtime as a substitute.

## Phone/API boundary

The phone does not enumerate processes, analyze candidate trees or choose a report.
It consumes the final `agent_session_id`, `binding_state` and `binding_reason` from
normal inventory messages. There is no separate manual-confirmation or candidate-
selection API. The optional [diagnostic snapshot API](session-binding-diagnostics.md) exposes existing inventory metadata as bounded, extensible JSON; it cannot select or confirm a candidate.

Keep normal history/stream/send/approval APIs and the target's `binding_epoch`:
identity and cache scope can stay the same while an old request lease is revoked.
`get_agent_run_state` remains for turn status/timing; `get_agent_settings` and
`set_agent_settings` remain for model/reasoning controls. These features use the
Relay's resolved exact ID, not a requirement that Herdr report the same ID. Store,
process and ancestry data are resolved on the computer, not by the Android app.

## Validation and lifecycle

Runtime association/root metadata is read twice. Child lifecycle changes are
allowed only if the verified root set remains unchanged. Terminal identity,
process birth, store and owned listeners are rechecked after discovery. The
fresh title ID, when enabled, must still select the same candidate. Without it,
the fresh Herdr report must agree when needed to break a tie; an irrelevant
report change does not veto a unique runtime root.

The selected exact ID is checked against available history ID/project metadata.
Conflicting metadata fails closed. In shared mode, missing or unmaterialized
history does not erase identity established by live runtime metadata. Standalone
requires an available header for the title-selected thread, or for every owned thread when selecting without a title. Historical
creator names do not establish the current executor.

History reads and protected conversation operations revalidate the binding.
Existing device/role authorization and exact terminal request leases remain in
force; this does not replace them. Workspace/file authorization remains separate
from Codex conversation identity. Changing terminal, process instance, selected
ID, runtime endpoint or proof state invalidates old requests and subscriptions.

## Terminal recovery is not conversation authority

Starting with Pocket 0.2.28 / Relay codexfix.15, an unresolved conversation does
not disable explicit terminal control. These use terminal scope:

- Literal terminal text/input, raw text/keys, constrained filter input and masked
  terminal input; existing character/no-echo validation still applies.
- Explicit size lease and Stop/Restart/Clear. Restart/Clear still require an owned
  launch profile; this does not guess a profile or resume an unknown conversation.

Only an authenticated controller with a ready inventory can perform them. Relay
retains exact server/pane/terminal/generation/session-field/binding-epoch request
checks (the session field may be empty). It freshly reads `pane.get` to verify
terminal incarnation, provider and cwd both at admission and just before writes,
including after queue/capacity waits. Discovery and global loaded-thread matching
are not part of this check. A stale request is not silently retargeted. Lifecycle
confirmations on Android are captured to the original exact target.

This permits manual keystrokes in the currently displayed terminal—including
keys that the program may interpret as a confirmation—but does **not** authorize
Relay-generated approval decisions based on an unverified history. `submit_prompt`,
structured question/approval actions, model changes and conversation reads retain
their primary checks. Unknown mutations remain primary-bound by default. Readers,
unauthenticated clients and uncertain/pending sends do not gain a bypass.

The browser keeps the normal conversation composer and structured controls gated,
but exposes the explicit Keyboard path and Stop/Clear. Android's Terminal composer
sends literal text with Enter separate; its Conversation composer stays gated.
Terminal access neither confirms a primary ID nor discards the user's draft.

## Resume and cache behavior

- Same selected ID: cached content can display immediately, labelled as cached,
  while the source location/current tail refreshes. Old pagination cursors,
  subscriptions and sequence positions are not restored as current.
- Different or now-unresolved ID: revoke old requests and change/isolate the
  history and draft scope. Never display one conversation as another.
- Conflicting metadata: display the error, not cached content as current proof.
- A transient source failure is not proof that Herdr supplied a bad ID.

## Failures and platform boundary

- `runtime_ambiguous_no_report`: multiple roots with no report to break the tie.
- `reported_session_not_candidate`: report does not match these terminal-scoped roots.
- `herdr_session_invalid`: malformed/provider-mismatched report needed for a tie.
- `runtime_ancestry_unavailable`: missing, unclassified or cyclic ancestry.
- `runtime_metadata_conflict` / `process_project_conflict`: conflicting scope data.
- `runtime_unavailable` / `runtime_disconnected`: no usable runtime association.
- `runtime_changed`: terminal/process/association/selection changed during validation.
- `runtime_platform_unsupported`: process proof is not implemented on that platform.
- `standalone_runtime_unavailable`: no readable, valid writer ownership proof.
- `standalone_metadata_unavailable`: owned thread metadata is not readable yet.
- `standalone_ambiguous`: multiple roots without title-ID evidence; retained hooks cannot break this tie.
- `terminal_title_missing` / `terminal_title_invalid`: expected ID-first title is not readable.
- `terminal_title_ambiguous`: truncated ID matches multiple verified roots.
- `terminal_title_conflict`: ID does not match a verified primary root.
- `standalone_ancestry_unavailable` / `standalone_metadata_conflict`: incomplete or
  conflicting standalone identity/parent/project/store metadata.

Process/socket proof currently targets Linux; no root or process injection is used.
Do not promise that `--no-daemon` repairs malformed metadata or enables unsupported
platforms. The Relay does not enable `--no-daemon`, stop a daemon, restart an agent or
change Codex Desktop settings automatically. Relay `codexfix.17` adds a default
terminal-title argument only when creating or explicitly restarting a built-in
Codex agent; it does not change already-running processes. The title ID only
selects within that terminal's verified primary candidates as described above.
See [startup arguments](development.md#agent-startup-arguments).

## Verification and release

Synthetic tests cover terminal isolation, unique roots without reports, child
ancestry, independent forks, multiple-root report matching, incomplete/cyclic
metadata, changing reports/processes, missing history, lease replacement and the
non-Codex bypass. Standalone coverage includes real kernel locks, first-turn
metadata, unrelated shared sockets, child/fork reduction, stale reports, process/
mode/inode changes, settings isolation and unknown timing. Runtime reads are metadata-only; private live audits never enter
public fixtures. Same-ID source/cache refresh has separate reader/mobile coverage.

Source and published versions are separate. Distribution remains CI-only; this
change does not deploy or restart Relay, Gateway, Herdr, Codex or the desktop app.
