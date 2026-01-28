# Observer Baseline Metrics

**Recorded:** 2026-01-28
**Before:** Phase 1 fixes

## Environment
- Go version: 1.24.12
- Platform: linux/amd64
- CGO: Not available (gcc missing) - race detection limited

## Database
- Path: ~/.observer/observer.db
- Size: 70,516,736 bytes (67 MB)
- Backup: observer.db.pre-fix-20260128

## Test Environment Baseline
- Goroutines (test): 2
- Memory: Not measured (requires running app)

## Notes
- Race detection (`go test -race`) requires CGO/gcc which is not available
- Tests run without race flag pass successfully
- Unsubscribe mechanism exists and works (verified by test)
- Stop() does NOT wait for workers (known bug, Phase 1.1 will fix)

## Phase 0 Verification
- [x] Work tests pass: `go test ./internal/work/...` OK
- [x] App tests pass: `go test ./internal/app/...` OK
- [x] Debug server compiles: `go build ./internal/debug/...` OK
- [x] Main compiles: `go build ./cmd/observer/...` OK
- [x] Database backup taken
- [x] Unsubscribe prevents leaks (test passes)
- [x] Unsubscribe closes channel (test passes)
- [x] Unsubscribe is idempotent (test passes)
- [x] Race detection - ENABLED (gcc 13.3.0 installed)
- [x] Race in copyItem() fixed (heapIndex was being copied unsafely)

## Database Stats
- Backup verified: MD5 checksums match (1ba0fb5103085f64eb6ad5118416f82d)
- Rollback procedure tested and working

## Known Issues Before Fix
1. `go p.execute(item)` at pool.go:283 has no wg tracking
2. TestStopWaitsForActiveWorkers fails (documents the bug)

## Rollback Test
```
Date: 2026-01-28
Result: PASSED
- Created test file
- Modified test file
- Restored from backup
- Verified MD5 match
- Cleaned up
```
