# Phase 2 UX Review

**Verdict: Acceptable with issues**

## Visual Design

**Good:** Clean color palette, clear visual hierarchy with time bands, read/unread distinction works.

**Problems:**
- `colorMuted` (240) vs `colorSecondary` (241) are nearly identical. Pick one.
- No `colorError` defined but ErrorStyle hardcodes `196`. Inconsistent.
- SourceBadge on dark background (236) with purple text (62) has poor contrast.

## Key Bindings

**Good:** vim-style j/k, sensible defaults for g/G, Enter for action.

**Problems:**
- `r` for refresh vs `f` for fetch is confusing. Users won't understand the difference.
- Status bar shows `r:refresh` but `f` isn't documented anywhere in the UI.
- No `?` for help screen. Users are left guessing about g/G/f.
- `Enter` marks as read but doesn't actually DO anything visible. No browser open? No detail view?

## Error States

**Bad:** When `a.err != nil`, the entire UI is replaced with just the error message. User loses all context, can't see their items, can't navigate, can only quit. This is hostile.

**Fix:** Show error in a dismissible banner at the top while keeping the item list visible.

## Edge Cases

**Empty list:** Handled with "No items to display. Press 'r' to refresh." Good.

**Long titles:** Truncated at `titleWidth-3` with "...". But uses `len(title)` which is BYTE length, not rune length. Unicode titles will be butchered.

**Narrow terminals:** `titleWidth` floors at 20, but if terminal is 40 chars and badge is 25 chars, math goes negative then clamps. The layout will overflow and wrap ugly.

**Scroll offset bug:** If cursor is past visible area AND a time band header appears, the header line counts toward `renderedLines` but not toward scroll offset calculation. Items can disappear from view.

## Status Feedback

**Missing:**
- No "Loading..." indicator when refreshing (only on initial load)
- No "Fetching feeds..." during background fetch
- No "N new items" after fetch completes
- FetchComplete sets error but never clears it on success cases with 0 new items

## Must Fix

1. Error display: Don't replace entire UI with error
2. Unicode truncation: Use `runewidth` package
3. Fetch feedback: Show loading/success states
4. Document `f` key or remove it

## Should Fix

1. Add `?` help overlay
2. Consolidate muted/secondary colors
3. Make `Enter` actually open the item (browser or detail view)
