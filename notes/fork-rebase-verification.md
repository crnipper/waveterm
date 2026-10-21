# Fork rebase verification

Fork: https://github.com/crnipper/waveterm
Branch: durable-remote-browser
Upstream base: 9f1c9670d407be3b93cf090319a63ef30bf54d0e
Original base: 97e560027f494d20fed347b2d6b72b6bcb3e50e0 (71 upstream commits behind)
Original history: archive/durable-browser-pre-rebase, tip 97410c46158e1df9acc5eb495bf080b9d5d46956.
The five durable-fix/repro commits plus the browser feature rebased without conflicts.

Passed: `go test ./pkg/jobmanager ./pkg/wshutil ./pkg/util/logutil`
(jobmanager and wshutil tests; logutil currently has no test files), five Vitest
widget tests, three Python resolver tests, schema/binding generation.
`tsc --noEmit` still reports 17 historical preview-fixture errors (version/buildtime,
getPathForFile, numthreads); no modified feature files report new errors.
Windows packaging and click/session tests must be repeated from this branch.
The historical Windows result was BUILD PASSED only; no cutover/live tests.

Public source includes portable config fragments rather than credentials, full
profiles, private repository paths/state, package binaries, or data backups.
Original history also has a verified local Git bundle under ~/sources for recovery.

Production frontend build (`npm run build:prod`) also passed after the rebase.
