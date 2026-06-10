# VISSR Repository & PR Process Guide

Repo hygiene, PR practices, release management, and content organization for VISSR.

## PR Practices

### PR Scope & Size

- One concern per PR (don't mix features, fixes, and refactors)
- Target reasonable lines changed (excluding generated files, testdata)
- Large changes: split into stacked PRs (A then B then C)
- PR description must explain: what, why, and how to test

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
- **Rebase** for stacked PRs (A then B then C)
- Never force-push to shared branches
- Delete branch after merge

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
