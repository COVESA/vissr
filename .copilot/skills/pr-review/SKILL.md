---
name: vissr-pr-review
description: Comprehensive PR review guidelines for VISSR (Vehicle Information and Signals Server). Modular guides covering architecture, security, testing, and best practices for all COVESA/vissr PRs.
applyTo: "**"
---

# VISSR Pull Request Review Skill

Modular review guidance for **all pull requests** to the COVESA/vissr repository.

## Quick Start

**Use this skill for any VISSR PR:**
```
review PR https://github.com/COVESA/vissr/pull/XXX with #file:vissr-pr-review skill
```

Then follow the **[PR Review Checklist](CHECKLIST.md)** in your review.

## Complete Guides

| Guide | Purpose | Use When |
|-------|---------|----------|
| **[CHECKLIST.md](../../guides/generic/CHECKLIST.md)** | Copy-paste PR review checklist | Reviewing every PR |
| **[ARCHITECTURE.md](../../guides/generic/ARCHITECTURE.md)** | Architectural patterns, SOLID, design | Evaluating new packages or design |
| **[SECURITY.md](../../guides/generic/SECURITY.md)** | Secrets, TLS, authorization | Reviewing security-related changes |
| **[TESTING.md](../../guides/generic/TESTING.md)** | Test requirements, commands | Verifying test coverage |
| **[REFERENCE.md](../../guides/generic/REFERENCE.md)** | Useful commands, examples, resources | Quick lookup |

## Core Principles

1. **Registry Pattern** — All tree sources use `RegisterServiceTree()` / `DeregisterServiceTree()`
2. **Mutual Exclusivity** — Conflicting flags (e.g., `--vdm` / `--him`) validated early
3. **Input Validation** — All user-provided data validated before use
4. **Concurrency Safety** — Shared state protected by mutex; tests pass `-race`
5. **Graceful Degradation** — Optional components fail non-fatally
6. **No Secrets in Code** — All credentials via environment variables
7. **Goroutine Lifecycle** — Every goroutine has a known exit condition
8. **Atomic I/O** — Network writes are atomic frames

## Common Issues to Flag

- **Missing input validation** on user-provided JSON, GraphQL, or flags
- **Mutually exclusive flags** not validated at startup
- **Hardcoded secrets** or credentials in code
- **Goroutine leaks** — loops without exit conditions
- **Race conditions** — shared maps/slices accessed without mutex
- **Silent errors** — discarded error returns or failed type assertions
- **Non-atomic I/O** — multiple writes when one atomic write needed

## Approval Criteria

**Approve:** All tests pass, validates input, no secrets, follows patterns  
**Request changes:** Test coverage gaps, validation missing, concurrency concerns  
**Reject:** Test failures, hardcoded secrets, race detector failures, TLS weakened

---

See each guide for detailed patterns, examples, and checklists.
