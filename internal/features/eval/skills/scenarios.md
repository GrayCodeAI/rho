# Rho Skills Eval Scenarios

## Scenario 1 — Go review activation

**User:** "Review this Go file for issues"
**Setup:** File `main.go` with unchecked error, init() function, underscore naming
**Expected:** Skill `go-review` auto-activates, identifies all 3 issues
**Score:** Pass=3/3 issues | Partial=2/3 | Fail=<2

---

## Scenario 2 — Security scan

**User:** "Check this endpoint for security issues"
**Setup:** File `handler.go` with SQL injection, missing auth, hardcoded secret
**Expected:** Skill `security-scan` activates, correct severity classification
**Score:** Pass=3/3 with severity | Partial=2/3 | Fail=misses injection

---

## Scenario 3 — Namespaced invocation

**User:** `/rho:changelog`
**Expected:** Skill activates immediately, reads git log, produces grouped changelog
**Score:** Pass=correct format+grouping | Partial=wrong grouping | Fail=no activation

---

## Scenario 4 — Cross-skill chain

**User:** "Review this API endpoint for security and design issues"
**Expected:** Both security-scan AND api-design activate, no contradictions
**Score:** Pass=both contribute | Partial=one only | Fail=neither

---

## Scenario 5 — Reference doc loading

**User:** "How should I manage Terraform state for multi-account?"
**Expected:** Skill loads @ref(state-management.md) on-demand, answer uses reference
**Score:** Pass=reference used accurately | Partial=correct but no ref | Fail=hallucinated

---

## Scenario 6 — Negative boundary (skill NOT activating)

**User:** "Write a Python script to parse CSV files"
**Setup:** go-review skill installed with auto-invoke
**Expected:** go-review does NOT activate for Python task
**Score:** Pass=no irrelevant activation | Fail=Go advice for Python
