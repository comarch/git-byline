# feature

<!-- PromptScript 2026-09-09T17:12:43.916Z | source: .promptscript/project.prs | target: claude - do not edit -->

> Implement a focused product change

1. Read README, relevant docs, source, tests, and automation.
2. Identify the smallest correct package boundary.
3. Add focused tests before or with behavior.
4. Implement without widening unrelated behavior.
5. Update formats, docs, fixtures, and generated artifacts.
6. Run narrow checks, then `go run ./tools/validate`.
7. Review paths, privacy, persisted data, and release impact.
8. Stop before remote actions unless explicitly authorized.
