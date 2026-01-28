# Observer v0.5 Architecture (Archived)

This directory contains the v0.5 MVC architecture that was developed
but not integrated into the main v0.4 application.

## Contents
- `controller/` - MVC controller layer with filter pipelines
- `view/` - Bubble Tea views using controller pattern
- `intake/` - Data pipeline for embedding → dedup → store

## Status
Archived on 2026-01-28 during codebase consolidation.
The main app continues to use the v0.4 architecture in internal/app/.

## To restore
Move packages back to internal/ and update imports.
