<!--
  Thanks for your contribution! Please fill out this template so reviewers can
  understand the change quickly. Anything that does not apply can be left in
  place; do not delete unanswered sections — write "n/a".
-->

## Summary

<!--
  One paragraph describing what this PR does and why. Link the related
  issue(s) with `Fixes #N` or `Refs #N` if applicable.
-->

## Changes

<!--
  Bullet list of what changed, grouped by area (engine, tools, CLI, CI, etc.).
  Reviewers should be able to skim this and know what to look at first.
-->

-

## Testing

<!--
  Describe how you tested. Paste output of `make test` and `make lint`. If you
  added new tests, list them. If you could not run something locally (for
  example, a platform-specific integration test), call that out.
-->

```text
$ make test
...
$ make lint
...
```

## Checklist

- [ ] My commits follow [Conventional Commits](https://www.conventionalcommits.org/)
      (`feat(scope): …`, `fix(scope): …`, `docs(scope): …`, etc.)
- [ ] `make build` passes
- [ ] `make lint` passes (no new lint findings, no `nolint:…` without justification)
- [ ] `make test` passes locally with `-race` enabled
- [ ] New or changed code has tests (table-driven where appropriate)
- [ ] Public APIs have godoc comments and runnable examples where helpful
- [ ] `CHANGELOG.md` updated under `## [Unreleased]` if user-visible
- [ ] No secrets, tokens, or PII added to the repo
- [ ] No `Co-authored-by:` trailers (this is individual-developer work)
