# Git Workflow

This document defines the repository's branching, integration, release, and rollback rules.

## Branch roles

- `main` is the stable release branch. It only receives merged work; the repository's initial baseline commit is the sole exception.
- `develop` is the day-to-day integration branch. Only maintainers may commit directly to it.
- All other contributors and agents work on short-lived topic branches and request maintainer review before integration.

## Creating and naming branches

- Create ordinary topic branches from the latest `develop`.
- Create urgent production fixes from the latest `main`.
- Use `<type>/<short-kebab-case-description>` for branch names.
- Allowed types are `feature`, `fix`, `hotfix`, `docs`, `refactor`, and `chore`.
- Include an issue number at the start of the description when one exists; it is not required.

Examples:

```
feature/import-boundaries
fix/invalid-geometry
hotfix/map-tile-timeout
docs/git-workflow
```

## Working and integrating changes

- Before merging an ordinary topic branch, rebase it onto the latest `develop`, resolve any conflicts, and run relevant checks.
- Do not rebase a branch already shared by multiple contributors. Merge the latest `develop` into that branch instead, so shared history is not rewritten.
- The source-branch author resolves merge conflicts. After resolving them, rerun relevant checks and have the maintainer review the affected area.
- If the intended resolution is unclear, stop the merge and ask the maintainer to decide.
- Maintainers may commit directly to `develop`, but must run relevant checks and use a clear commit message.
- Contributors and agents must obtain maintainer review before their topic branch is integrated into `develop`.

## Merge strategy

- Merge ordinary topic branches into `develop` using squash merge. Each completed unit of work becomes one reversible integration commit.
- Merge `develop` into `main` with a merge commit, preserving the release boundary and integration history.
- After a successful topic-branch merge, delete the merged branch locally and remotely. Do not delete unmerged branches that are still needed for investigation or further work.

## Commit messages

Use simplified Conventional Commits:

```
feat: import boundaries
fix: reject invalid geometry
docs: add git workflow
```

Use a concise imperative description. A scope is optional and should be added only when it improves clarity.

## Releasing from `develop`

Before merging `develop` into `main`, the maintainer confirms that:

- relevant automated checks have passed;
- the included changes have been reviewed;
- the working tree is clean; and
- the release scope is understood.

When no automated check exists for a change, record the manual verification performed before merging it into `main`. For ordinary integration into `develop`, at minimum state that no automated check exists.

After merging to `main`, create an annotated Semantic Versioning tag such as `v0.1.0`.

## Release branches

Do not create release branches by default. Create a short-lived `release/<version>` branch only when a version must be stabilised while `develop` continues with the next version's work.

A release branch may contain only release preparation, bug fixes, and release-documentation changes. When the release is ready:

1. Merge it into `main`.
2. Create the annotated release tag.
3. Merge the release result back into `develop`.
4. Delete the release branch.

## Hotfixes

For an urgent production fix:

1. Create `hotfix/<description>` from `main`.
2. Review and validate the fix.
3. Merge it into `main` with a merge commit and create the annotated release tag.
4. Merge `main` back into `develop` so the fix is not lost.
5. Delete the hotfix branch.

## Rollbacks and shared history

Never rewrite history on `develop` or `main`. To undo a change that has entered either shared branch, create a new `revert` commit. Rewriting history is allowed only on an unshared personal topic branch, such as during the required rebase before integration.
