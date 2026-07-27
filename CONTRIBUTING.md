# Contributing

## Commits & PR titles (Conventional Commits)

This repo uses [Conventional Commits](https://www.conventionalcommits.org/) with
[release-please](https://github.com/googleapis/release-please) for versioning and
the changelog. PRs are **squash-merged**, so the **PR title** becomes the commit
on `main` and drives the release — it MUST be a valid Conventional Commit (CI
enforces this):

```
<type>[optional scope]: <description>
```

- `feat:` → minor bump · `fix:` → patch bump
- `feat!:` / `fix!:` or a `BREAKING CHANGE:` footer → major bump
- `docs:` `chore:` `refactor:` `test:` `ci:` `perf:` → no release on their own

Examples: `feat: idempotent Present`, `fix: split zone on trailing dot`,
`feat!: require login+password Secret keys`.

## Branches

Branch from `main`; name it `<type>/<slug>` (e.g. `fix/cleanup-index`). Branches
are deleted automatically on merge.

## Releases — automated, do not hand-edit

Do **not** tag manually or edit `CHANGELOG.md`. release-please opens a *release
PR* that maintains the version and changelog from merged commits; merging it
tags and publishes (image + Helm chart). Refine wording in that release PR if
needed.

## Sign-off (DCO)

Every commit must carry a `Signed-off-by` line — certify the
[Developer Certificate of Origin](https://developercertificate.org/):

```sh
git commit --signoff
```

## Local checks

`mise run ci` (lint + test) must pass before you push. `pre-commit install`
mirrors the CI hooks. Unit tests need no credentials; the gated conformance
suite (`TEST_ZONE_NAME` + live SpaceWeb creds) is opt-in — see the README.
