# VISSR Repository & PR Process Guide

Repo hygiene, PR practices, release management, and content organization for VISSR.

## PR Practices

### PR Scope & Size

- One concern per PR (don't mix features, fixes, and refactors)
- Target < 500 lines changed (excluding generated files, testdata)
- Large changes: split into stacked PRs (A → B → C)
- PR description must explain: what, why, and how to test

### PR Lifecycle

1. **Draft PR** — WIP, not ready for review (use for early feedback)
2. **Ready for Review** — All tests pass, description complete
3. **Approved** — Merge after CI green + 1 approval
4. **Merged** — Squash-merge to master (clean linear history)

### Labeling

| Label | When |
|-------|------|
| `feat` | New feature |
| `fix` | Bug fix |
| `breaking` | Breaks backward compatibility |
| `docs` | Documentation only |
| `needs-review` | Awaiting reviewer |

### Commit Hygiene

- Each commit should build and pass tests independently
- Squash fixup commits before marking ready
- Commit messages follow `type(scope): description` convention
- Atomic commits: one logical change per commit

## Branch Management

### Branch Naming

```
feat/<short-description>       — New feature
fix/<issue-number>-<desc>      — Bug fix
chore/<description>            — Maintenance
docs/<description>             — Documentation
```

### Merge Strategy

- **Squash merge** to master (default) — clean history
- **Rebase** for stacked PRs (A → B → C)
- Never force-push to shared branches
- Delete branch after merge

### Protected Branch: `master`

- All PRs must pass CI before merge
- At least 1 approval required
- No direct commits to master

## File Organization

### Where Things Go

```
server/vissv2server/           — Server implementation
├── <package>/                 — One package per concern
├── <package>/testdata/        — Test fixtures for that package
├── viss.him                   — HIM forest configuration
└── forest/                    — Compiled binary trees

utils/                         — Shared utilities (Node_t, crypto, logging)
client/                        — Client implementations
spec/                          — Specification documents (HTML, Quickstart.md)
tools/                         — CLI tools (vdminfo, etc.)
resources/                     — VSS spec files, Makefile
.copilot/                      — Copilot skills and guides
.github/workflows/             — CI/CD pipeline definitions
```

### Naming Conventions

| Item | Convention | Example |
|------|-----------|---------|
| Packages | lowercase, single word | `vdmloader`, `webdash` |
| Go files | lowercase, underscore | `service_reg.go`, `tree_utils.go` |
| Test files | `*_test.go` | `vdmloader_test.go` |
| Testdata | `testdata/` directory | `vdmloader/testdata/vehicle.graphql` |
| Specs | `VISSv<version>_<topic>` | `VISSv4.0_VDM.html` |
| Scripts | lowercase, descriptive | `runstack.sh`, `runtest.sh` |

### Stale File Cleanup

Flag in reviews:
- Dead code (unreachable functions, commented-out blocks)
- Orphaned test files (tests for deleted code)
- Unused dependencies in `go.mod`
- Generated files committed without regeneration instructions
- Temporary/debug files (`*.log`, `*.tmp`, `*.bak`)

## Release Management

### Versioning

VISSR follows semantic versioning aligned with spec versions:
- **Major**: VISSv2 → VISSv3 → VISSv4 (breaking protocol changes)
- **Minor**: New features within a spec version
- **Patch**: Bug fixes, no new features

### Tagging

```bash
git tag -a v4.0.0 -m "VISSv4.0: VDM GraphQL SDL support"
git push origin v4.0.0
```

### Changelog

- Maintain `CHANGELOG.md` or use GitHub Releases
- Group by: Added, Changed, Fixed, Removed, Security
- Reference PR numbers and issue numbers

### Release Checklist

- [ ] All tests pass on master (including smoke)
- [ ] `go.mod` dependencies are pinned (no floating versions)
- [ ] Spec docs updated (`spec/VISSv*.html`)
- [ ] README updated if public API changed
- [ ] Tag created and pushed
- [ ] GitHub Release created with changelog

## CI/CD Practices

### Workflow File Review

When reviewing `.github/workflows/` changes:
- Jobs run only necessary steps (no wasted compute)
- Secrets accessed via GitHub secrets (not hardcoded)
- Cache configured for Go modules (`actions/setup-go` with `cache: true`)
- Timeouts set on all jobs (prevent hanging builds)
- Build matrix covers target platforms

### Required CI Checks

| Check | Blocks Merge? |
|-------|--------------|
| `go build ./...` | Yes |
| `go vet ./...` | Yes |
| Unit tests with `-race` | Yes |
| Smoke test | Yes (when relevant) |
| Lint (if configured) | Advisory |

## Documentation Standards

### README

- Build/run instructions always current
- Quick start example works out of the box
- Links to specs and guides for deeper info

### Code Documentation

- Package comment on every package (first line of any `.go` file)
- Doc comment on all exported functions, types, constants
- Non-obvious algorithms get inline comments explaining "why"
- No commented-out code (use version control instead)

### Spec Documents

- `spec/` contains canonical HTML specs
- Quickstart `.md` files for developer onboarding
- Keep specs in sync with implementation (flag drift in reviews)

## Dependency Management

### Adding Dependencies

Before adding a new `go.mod` dependency:
1. Check if stdlib has an alternative
2. Verify license compatibility (MIT, Apache-2.0, BSD preferred)
3. Check maintenance status (last commit, open issues)
4. Pin to specific version (not `latest`)
5. Run `go mod tidy` to clean up

### Updating Dependencies

- Update regularly (security patches)
- One dependency update per PR (easy to revert)
- Run full test suite after updates
- Check for breaking changes in changelogs

### Red Flags

- Dependency with no commits in 2+ years
- License change in new version
- Dependency that pulls in 50+ transitive deps
- `replace` directives pointing to local paths (test only)
