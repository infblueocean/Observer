# Phase 2 Code Review

**Verdict: ACCEPTABLE with minor issues.**

## Go Idioms

**Good:**
- Filter functions are pure; idiomatic `[]Item in, []Item out` pattern.
- Consistent nil-safe returns (`[]store.Item{}` not `nil`).
- App uses value receiver for `tea.Model` - correct Bubble Tea pattern.

**Issues:**
- `filter.go:93-94`: `map[string]bool` should be `map[string]struct{}` (zero-cost set). Minor.
- `stream.go:100-101`: Truncation by byte count, not runes. Will break on Unicode titles. Use `[]rune(title)` instead.

## Error Handling

**Good:**
- `ItemsLoaded` carries `Err` field; properly checked in Update (line 57-61).
- Edge cases handled: empty slices, nil inputs, out-of-bounds cursor.

**Missing:**
- `app.go:160`: Error display is bare. Consider showing which operation failed.

## API Design

**Good:**
- `NewApp` signature with function dependencies is clean and testable.
- Messages are simple structs; no footguns.

**Concerns:**
- `app.go:27-34`: Three `func() tea.Cmd` parameters look identical. Consider a struct:
  ```go
  type AppDeps struct {
      LoadItems    func() tea.Cmd
      MarkRead     func(string) tea.Cmd
      TriggerFetch func() tea.Cmd
  }
  ```
  Prevents argument swapping bugs.

## Bubble Tea Patterns

**Correct:**
- Value receivers throughout (`App` not `*App`).
- Update returns new model; state mutations happen before return.
- Init returns a Cmd; doesn't block.

**Issue:**
- `app.go:128-135`: Enter key marks read but doesn't open the URL. Plan says "mark read" but users expect "open". Document this or implement URL opening.

## Plan Compliance

**Compliant:**
- Messages match spec exactly (lines 207-234).
- Filter functions match spec (lines 318-337).
- Key bindings match spec (lines 374-382).
- App struct matches spec except `viewport` field is omitted (acceptable simplification).

**Deviation:**
- `styles.go`: Not in plan. Fine, but should be documented.

## Test Coverage

**Good:**
- Filter edge cases covered thoroughly (nil, empty, boundary conditions).
- Navigation tested including bounds.
- Mock pattern for command functions is clean.

**Missing:**
- No tests for `RenderStream` or `RenderStatusBar`.
- `stream.go:47-49`: Scroll offset logic untested.

## Specific Line Issues

| File | Line | Issue |
|------|------|-------|
| `stream.go` | 100 | Byte truncation breaks Unicode |
| `filter.go` | 137 | Result order is non-deterministic (map iteration) |
| `app.go` | 63-65 | Cursor reset to last item on reload; should stay at current position if valid |

## Summary

Solid implementation. Fix the Unicode truncation bug and add stream rendering tests. The map iteration ordering in `LimitPerSource` could cause flaky behavior in sorted views downstream.
