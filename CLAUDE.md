# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

**Observer is starting fresh.** The v0.4 codebase has been archived to `archive/v0.4/`. New development should not reference or build upon the archived code without explicit instruction.

## What is Observer?

Observer is an ambient news aggregation TUI (Terminal User Interface) built with Go. The goal is to let users "watch the world go by" - aggregating content from many sources with radical transparency and user control over curation.

### Core Philosophy

- **You Own Your Attention** - No algorithm stands between you and information
- **Curation By Consent** - Every filter is visible and adjustable
- **AI as Tool, Never Master** - AI assists when asked, never decides secretly

## Build Commands

```bash
# Build
go build -o observer ./cmd/observer

# Test
go test ./...

# Test with race detector
go test -race ./...

# Run single test
go test -run TestName ./path/to/package

# Run
./observer
```

## Architecture

This is a greenfield project. Architecture will be documented as it develops.

Reference the archived v0.4 CLAUDE.md at `archive/v0.4/CLAUDE.md` for historical context on previous architecture decisions, but do not assume that architecture applies to new development.

## Workflow Requirements

**Always use subagents.** For any non-trivial task, use the Task tool to spawn subagents. This preserves context and prevents the main conversation from hitting compaction limits. Use parallel subagents when tasks are independent.

**Ask before assuming.** Previous conversation context may be lost after compaction. If working on a significant task, confirm the current direction before proceeding with implementation.
