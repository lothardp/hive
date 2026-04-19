# Cell Creation Progress Feedback

## Overview

Surface which step cell creation is currently on (cloning, hooks, tmux, layout) in the dashboard's create overlay, instead of the current static `"Cloning <project>..."` line. The value is mostly psychological: long hooks like `yarn install` no longer look like a frozen UI, and if something is actually stuck, the user can see where.

**Design constraints** (stated by the user):

- Best-effort, nice-to-have — lossy is fine, missed updates are fine.
- Must not deeply interfere with cell creation logic. The service's happy path and error paths stay structurally identical; instrumentation is opt-in and nil-safe.
- No new config, no new DB columns, no new types in user-visible APIs.

---

## Part 1: Progress callback on the service layer

### What it is

A single optional `func(string)` callback on `CreateOpts` / `MultiOpts` (and on the internal `ProvisionOpts`). When non-nil, the service calls it with a short human-readable line at each milestone. When nil, the service behaves exactly as today — zero runtime difference.

Nothing else changes in the service layer: same return types, same error wrapping, same rollback semantics.

### Data model

`internal/cell/service.go`:

```go
type CreateOpts struct {
    Project   string
    Name      string
    RepoPath  string
    OnProgress func(string) // NEW; optional
}

type MultiOpts struct {
    Name       string
    Projects   []config.DiscoveredProject
    OnProgress func(string) // NEW; optional
}

type ProvisionOpts struct {
    Project    string
    SourceRepo string
    TargetPath string
    CellName   string
    AllocPorts bool
    ExtraEnv   map[string]string
    OnProgress func(string) // NEW; optional
}
```

No other struct changes. `CreateResult` stays as-is.

### Callback contract

- Called synchronously from the service goroutine.
- **Must be non-blocking.** The TUI's implementation drops messages when its channel is full. The service does not care whether the message was received.
- Called with short, already-formatted lines like `"cloning myapp"`, `"running hook 3/8: yarn install"`, `"creating tmux session"`. Callers display the latest line verbatim.
- Never called with an empty string. The absence of a call means "no change".
- Not called from rollback paths — progress is about forward motion. On failure the error message is surfaced through the existing return path.

### Emission points

Inside `Service.provisionClone` (wraps clone + ports + env + hooks):

```
emit("cloning " + projectLabel)           // before CloneMgr.CloneInto
emit("allocating ports")                  // before allocator.Allocate (only if AllocPorts && PortVars > 0)
// Hooks: see Part 2 — per-hook granularity, not just "running hooks"
```

Inside `Service.Create` (around `provisionClone`):

```
// provisionClone emits its own steps; Create only adds post-provision ones
emit("creating tmux session")             // before TmuxMgr.CreateSession
emit("applying layout")                   // before layout.Apply (only if default layout exists)
emit("saving cell record")                // before CellRepo.Create
```

Inside `Service.CreateMulti`:

```
emit("creating coordinator session")      // before TmuxMgr.CreateSession for coordinator
// For each child project p:
emit("[" + p.Name + "] cloning")          // child's OnProgress wraps the parent emitter with a prefix
// ... etc via provisionChildCell → provisionClone
```

### How `provisionClone` forwards the callback

`Create` populates `ProvisionOpts.OnProgress` by simply forwarding `opts.OnProgress`. `CreateMulti` wraps the parent callback with a project-name prefix before passing it to `provisionChildCell` → `provisionClone`:

```go
childOpts := ProvisionChildOpts{ /* existing fields */ }
// wrap for per-project prefix
if opts.OnProgress != nil {
    prefix := "[" + p.Name + "] "
    childOnProgress := func(s string) { opts.OnProgress(prefix + s) }
    // plumbed through to provisionClone via ProvisionOpts.OnProgress
}
```

`ProvisionChildOpts` also gains an optional `OnProgress func(string)` field, forwarded into `ProvisionOpts`.

### Emit helper

To keep the service code uncluttered, a local helper at the top of `provisionClone` / `Create`:

```go
emit := func(s string) {
    if opts.OnProgress != nil {
        opts.OnProgress(s)
    }
}
```

Every emission site is then a single line: `emit("cloning " + opts.Project)`. This keeps the "nil-safe" check in one place and makes the diff small.

---

## Part 2: Per-hook progress in `hooks.Runner`

### Why

`yarn install` / `npm install` / `bundle install` are where most wall time lives. Seeing `running hook 3/8: yarn install` is what actually tells the user the system is alive. Hook-level granularity requires a small change in `internal/hooks/hooks.go`.

### Change

Add an optional callback to `Runner.Run`:

```go
// internal/hooks/hooks.go

func (r *Runner) Run(
    ctx context.Context,
    workDir string,
    hooks []string,
    env map[string]string,
    onHook func(index, total int, cmd string), // NEW; optional
) *Result {
    result := &Result{Total: len(hooks)}
    for i, cmd := range hooks {
        if onHook != nil {
            onHook(i+1, len(hooks), cmd)
        }
        result.Ran = i + 1
        // ... existing body unchanged ...
    }
    return result
}
```

### Wiring in `provisionClone`

```go
onHook := func(idx, total int, cmd string) {
    preview := cmd
    if len(preview) > 60 {
        preview = preview[:57] + "..."
    }
    emit(fmt.Sprintf("running hook %d/%d: %s", idx, total, preview))
}
result := runner.Run(ctx, opts.TargetPath, projectCfg.Hooks, envVars, onHook)
```

Truncation is intentional — some hooks are long (`cp -r ${HIVE_REPO_DIR}/node_modules .`) and the TUI line is width-limited.

### Callers to update

Only `provisionClone` calls `Runner.Run` today. One call site, one signature change. Nil is valid and covers any future callers that don't care about progress.

---

## Part 3: TUI wiring

### Bubble Tea pattern: channel + listener cmd

Bubble Tea's update loop runs on a single goroutine, and `tea.Msg` is the only way to feed it state. The service runs in a goroutine (already does, inside `m.createCell`'s `tea.Cmd`). To get strings from that goroutine into the update loop:

1. Model owns a buffered `chan string` of progress messages.
2. The `OnProgress` callback is a closure that does a non-blocking send on that channel — `select { case ch <- s: default: }`. Messages are dropped if the channel is full. That's fine — we only care about the latest line.
3. A second `tea.Cmd` (the "listener") blocks on reading from the channel and emits a `progressMsg`, then re-schedules itself. This runs in parallel with the create goroutine.
4. When create finishes, the service goroutine closes the channel (via a deferred close from the code that owns the callback). The listener sees the zero-value-with-ok=false and returns a terminating message (or just returns nothing — the `cellCreated`/`cellCreateFailed` message will supersede the progress state anyway).

### `internal/tui/create.go` changes

New fields on `CreateModel`:

```go
type CreateModel struct {
    // ... existing fields ...

    progressLine string       // NEW; latest step label
    progressCh   chan string  // NEW; buffered, capacity 16
}
```

New message type:

```go
type progressMsg struct{ line string }
type progressDone struct{} // listener terminates on channel close
```

`createCell` now kicks off two commands:

```go
func (m *CreateModel) createCell(opts cell.CreateOpts) (tea.Cmd, tea.Cmd) {
    m.progressCh = make(chan string, 16)
    ch := m.progressCh // capture

    opts.OnProgress = func(s string) {
        select {
        case ch <- s:
        default: // drop on full
        }
    }

    createCmd := func() tea.Msg {
        ctx := context.Background()
        defer close(ch)
        result, err := m.cellService.Create(ctx, opts)
        if err != nil {
            return cellCreateFailed{err: err}
        }
        return cellCreated{result: result}
    }

    return createCmd, m.listenProgress()
}

func (m *CreateModel) listenProgress() tea.Cmd {
    ch := m.progressCh
    return func() tea.Msg {
        s, ok := <-ch
        if !ok {
            return progressDone{}
        }
        return progressMsg{line: s}
    }
}
```

And the call-site in `updateNameInput` becomes:

```go
m.step = stepCreating
opts := cell.CreateOpts{ /* existing fields */ }
createCmd, listenCmd := m.createCell(opts)
return m, tea.Batch(createCmd, listenCmd)
```

`Update` handles the two new messages:

```go
case progressMsg:
    m.progressLine = msg.line
    return m, m.listenProgress() // re-arm the listener

case progressDone:
    return m, nil
```

`View` for `stepCreating` uses `m.progressLine` with a fallback:

```go
case stepCreating:
    line := m.progressLine
    if line == "" {
        line = fmt.Sprintf("Cloning %s...", m.selectedProject.Name) // fallback until first emit
    }
    b.WriteString("  " + titleStyle.Render("Creating cell...") + "\n\n")
    b.WriteString("  " + progressStyle.Render(line) + "\n")
```

### `internal/tui/create_multi.go` changes

Same shape. The multicell flow is a separate `tea.Model` file; it will gain the same `progressLine` / `progressCh` fields, the same listener cmd, and `MultiOpts.OnProgress` is wired identically. Per-project prefixing is already handled on the service side (Part 1).

---

## CLI output

### Before

```
Creating cell...

Cloning myapp...
```

(Stays like that for the whole `yarn install`.)

### After

Over the life of one normal cell creation, the `stepCreating` pane will display in sequence:

```
Creating cell...

cloning myapp
```

```
Creating cell...

allocating ports
```

```
Creating cell...

running hook 1/8: cp -cR ${HIVE_REPO_DIR}/node_modules .
```

```
Creating cell...

running hook 7/8: yarn install
```

```
Creating cell...

creating tmux session
```

```
Creating cell...

applying layout
```

Then the existing `stepDone` → `switchToSession` transition.

### For a multicell

```
Creating cell...

[api] cloning
```

```
Creating cell...

[api] running hook 4/6: bundle install
```

```
Creating cell...

[web] running hook 2/5: npm ci
```

---

## Integration points

Inside the service flow (before vs after):

```
Create:
  1. check existence        (existing)
  2. provisionClone:
     a. orphan cleanup      (existing)
     b. clone                (existing)  ← emit "cloning <project>"        [NEW]
     c. allocate ports       (existing)  ← emit "allocating ports"         [NEW]
     d. build env            (existing)
     e. run hooks            (existing)  ← per-hook emit                   [NEW]
  3. kill orphan tmux        (existing)
  4. create tmux session     (existing)  ← emit "creating tmux session"    [NEW]
  5. apply layout            (existing)  ← emit "applying layout"          [NEW]
  6. marshal ports           (existing)
  7. create DB row           (existing)  ← emit "saving cell record"       [NEW]
```

No existing step is moved, renamed, split, or reordered. Every new emission is a single line of the form `if opts.OnProgress != nil { opts.OnProgress("...") }` (via the `emit` helper).

---

## Implementation order

### Step 1: Service-layer callback (no TUI changes yet)

- Add `OnProgress func(string)` to `CreateOpts`, `MultiOpts`, `ProvisionOpts`, `ProvisionChildOpts`.
- Add an `emit` helper at the top of `provisionClone`, `Create`, `CreateMulti`, `provisionChildCell`.
- Insert the emission points listed in Part 1.
- **Smoke test:** nothing calls the field yet — existing behavior is unchanged and all tests pass.

### Step 2: Hook-level progress

- Change `hooks.Runner.Run` to take `onHook func(int, int, string)`.
- Update the sole call site in `provisionClone` to pass a callback that emits `"running hook N/M: <preview>"`.
- Pass `nil` from any test or future caller that doesn't care.

### Step 3: TUI wiring for single-cell create

- Add `progressLine string`, `progressCh chan string` to `CreateModel`.
- Add `progressMsg` / `progressDone` types.
- Refactor `createCell` to return two commands; batch them.
- Add a `listenProgress` cmd and re-arm it on each `progressMsg`.
- Update `stepCreating` view to render `m.progressLine` with fallback.
- **Test:** create a cell on `mobile-app` and watch the line change through `yarn install`.

### Step 4: TUI wiring for multicell create

- Mirror step 3 in `create_multi.go`.
- `CreateMulti` already wraps per-project prefixes on the service side, so the TUI only has to display strings verbatim.
- **Test:** create a multicell spanning two projects and verify prefixes.

### Step 5: Polish

- Truncate progress lines to available width in `View` (not just hook preview — the full line). `lipgloss.Width` to measure, slice if over.
- Optional: a trailing animated dot (`running hook 7/8: yarn install .`, `..`, `...`) via `tea.Tick` every ~400ms. Skip if it feels like scope creep.
- Manual test on `mobile-app` config where hooks dominate wall time.

### What is intentionally out of scope

- **Timing / ETA.** No per-step timers, no spinners with elapsed time. Best-effort means "what step", not "how long".
- **Structured events.** Plain strings, no event types, no JSON schema. If the UX needs richer data later, we change it then.
- **Progress in `cmd/` non-TUI paths.** Only the dashboard consumes progress. CLI paths that invoke `Service.Create` directly (if any) keep `OnProgress` nil.
- **Cancellation mid-step.** The existing context plumbing already handles cancel; this plan adds no new cancellation points.
- **Tests for progress emission.** Not worth the mocking overhead for a cosmetic feature. The feature is visibly verified during manual cell creation.
