# Unit Tests

Tests that run **without a database**. Cover middleware, cryptography, and pure validation logic.

## Files in this directory

| File | Package | Source of truth |
|------|---------|-----------------|
| `middleware_test.go` | `middleware` | `internal/middleware/middleware_test.go` |
| `crypto_test.go` | `services` | `internal/services/crypto_test.go` |

> **Note:** Go requires `_test.go` files to live alongside their package source for `go test` to work. The files here are **copies** for organizational reference. The canonical (runnable) versions are in `internal/`. Always edit the originals.

## Running

```bash
bash run_tests.sh --unit
```

## Test Count: 26

### Middleware — 21 tests

| Test | What it verifies |
|------|------------------|
| `TestRequireRole_Allowed` | Admin passes admin-required guard |
| `TestRequireRole_Denied` | Student blocked from admin route |
| `TestRequireRole_MultipleRoles` | Clinician or admin both pass clinician+admin guard |
| `TestDataScope_StudentSelf` | Student sees own record, blocked from other users |
| `TestDataScope_AdminOrg` | Admin can access any record in their org |
| `TestDataScope_ClinicianDepartment_DenyCrossUser` | Clinician denied cross-user via self-scope |
| `TestEnforceDeptScope_NilDepartment_DenyCrossUser` | Nil department fails closed |
| `TestEnforceOrgScope_SameOrg` | Same org passes, different org blocked |
| `TestHMACAuth_ValidSignature` | Correct signature + timestamp passes |
| `TestHMACAuth_MissingHeaders` | Missing HMAC headers → 401 |
| `TestHMACAuth_WrongSignature` | Tampered signature → 401 |
| `TestHMACAuth_ExpiredTimestamp` | 10-min-old timestamp rejected |
| `TestHMACAuth_FutureTimestamp` | 10-min-future timestamp rejected |
| `TestHMACAuth_TamperedBody` | Changed body with stale signature fails |
| `TestHMACAuth_MismatchedBodyHashHeader` | Claimed hash ≠ real body hash |
| `TestHMACAuth_EnforcesOnAnyRoute` | HMAC enforced wherever middleware applied |
| `TestRateLimit` | 4th request in window → 429 |
| `TestCSRF_ValidToken` | Matching cookie + form token passes |
| `TestCSRF_MissingToken` | Missing form token → 403 |
| `TestCSRF_SkippedForTokenAuth` | API token auth skips CSRF |
| `TestCSRF_GETRequestsSkipped` | GET requests bypass CSRF |

### Crypto — 5 tests

| Test | What it verifies |
|------|------------------|
| `TestEncryptDecryptField` | Round-trip encrypt/decrypt for 5 subtypes |
| `TestEncryptField_DifferentCiphertextEachTime` | Random nonce → unique output |
| `TestDecryptField_InvalidData` | Bad base64 and invalid ciphertext rejected |
