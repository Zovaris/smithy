# 0002 · GitHub CLI is optional

- Status: proposed
- Date: 2026-11-19

## Context and Problem Statement

The binary and launcher required `gh` for install (attestation), PRs, checks and waits, so people
without the GitHub CLI could not use smithy at all. smithy is built for GitHub; other forges are
out of scope for now.

## Considered Options

- Keep `gh` required — excludes users for no functional reason.
- Support GitLab/Bitbucket now — large surface, no users asking yet.
- Keep the install blocked without `gh` — safest, but the same exclusion.

## Decision Outcome

Chosen option: "three modes": with `gh`, the full experience (attested install, gh for PRs);
without `gh` but with `GH_TOKEN`/`GITHUB_TOKEN`, the binary calls the GitHub API itself; with
neither, smelt ends with a pushed branch and a compare URL, and warden/tend say they need access.
Without `gh`, the launcher verifies the checksum and warns that the build's origin is unverified
(the maintainer's call, 2026-11-19). Other forges go on the roadmap.

## Consequences

- Good: smithy runs anywhere git does; `gh` stays the recommended path.
- Bad: a checksum from the same release is weaker than attestation; two code paths to test.
