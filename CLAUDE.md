# Repository Instructions

- This is one Git monorepo. The root `.git/` is the only Git metadata directory; do not run `git init` in `apps/`, `services/`, or their subdirectories.
- The top-level layout is `apps/`, `services/`, and `docs/`. `apps/web/` is reserved for the Web project. `services/geodata-serve/` is the local Go + DuckDB data service; other service directories require a real service seam and name before creation.
- Start Claude Code or Codex from `apps/web/` for Web-only work or from `services/geodata-serve/` for data-service-only work. Start from the repository root for changes that cross projects.
- Root instructions define repository-wide invariants. Subproject instructions define only local technology, build, test, and other project-specific constraints.
- Commit only credential-free, reproducible team AI configuration. Keep local paths, tokens, and personal experiments in developer-local files.
