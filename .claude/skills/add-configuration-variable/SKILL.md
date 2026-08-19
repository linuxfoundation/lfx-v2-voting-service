# Skill: add-configuration-variable

## Purpose

Ensure a new environment variable is consistently wired across all the places the repository expects it: application loading, `.env.example`, README documentation, and (for secrets) the Helm chart ExternalSecret.

## When to use

When introducing a new environment variable — feature flag, external service URL, timeout, credential, or any other runtime configuration.

---

## Workflow

### Step 1 — Load in `cmd/voting-api/main.go`

Locate the env loading block near the top of `main.go`. Follow the existing patterns:

**Optional with default:**
```go
port := os.Getenv("MY_VAR")
if port == "" {
    port = "default-value"
}
```

**Required (fail fast):**
```go
myVal := os.Getenv("MY_VAR")
if myVal == "" {
    logger.Error("MY_VAR is required but not set")
    os.Exit(1)
}
```

**Feature flag (enable/disable pattern):**
```go
myFeatureEnabled := os.Getenv("MY_FEATURE_ENABLED") != "false"
```
or
```go
myDisabled := os.Getenv("MY_FEATURE_DISABLED") == "true"
```

Use the same style as the nearest existing flag. Check `ID_MAPPING_DISABLED` and `EVENT_PROCESSING_ENABLED` as canonical examples of the feature-disable pattern.

### Step 2 — Add to `.env.example`

Add the new variable in the relevant section of `.env.example`. Include:
- A comment explaining what the variable does
- A safe local-dev default that doesn't require external services where possible

```bash
# MY_VAR: description of what it does and valid values.
# Set to "true" to disable when running without <service>.
MY_VAR=local-default
```

### Step 3 — Add to `README.md` configuration table

Find the configuration table in `README.md` under `### Configuration`. Add a row:

```markdown
| `MY_VAR` | Description | `default` |
```

Match the table's existing column order (Variable / Description / Default).

### Step 4 — For secrets: update the Helm ExternalSecret

If the variable holds a credential that should never appear in version control:

1. **Do not** add it to `.env.example` with a real value. Use a placeholder: `MY_SECRET=<see 1Password: LFX V2 vault>`.
2. Add it to `charts/lfx-v2-voting-service/templates/externalsecret.yaml` under `spec.data` following the existing entries.
3. Add it to `charts/lfx-v2-voting-service/values.yaml` if it needs a configurable AWS Secrets Manager path.

### Step 5 — Validate

```bash
make ci
```

Additionally, verify the service starts cleanly with the new variable absent (if optional) and with the `.env.example` default value:

```bash
source .env && make run
```

---

## Checklist

- [ ] Variable loaded in `cmd/voting-api/main.go`
- [ ] `.env.example` has the variable with a comment and safe local default
- [ ] README configuration table updated
- [ ] For secrets: ExternalSecret updated; `.env.example` uses a placeholder
- [ ] `make ci` passes
- [ ] Service starts with the `.env.example` default
