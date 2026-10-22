# Conformance suite

Differential tests: scenarios run against a live Keygen CE reference server
and against a running backplane-lic; normalised transcripts must match.

- `make conformance-record` — run the scenarios against the reference
  (`LIC_CONFORMANCE_REFERENCE`, default `http://localhost:3900`) and write
  `testdata/<scenario>.golden.json`.
- `make conformance` — run the scenarios against our server
  (`LIC_CONFORMANCE_TARGET`, e.g. `http://localhost:8080`) and diff every step
  against its golden. Skips when the variable is unset.

Each scenario is a self-contained list of steps (`scenarios_test.go`): method,
path, auth (none / admin token / token or license from a captured var / basic),
JSON body and headers — all `{{var}}`-interpolated — plus captures
(`data.id` style paths). Embed the built-in `{{run}}` suffix in generated
names/fingerprints/emails; the normaliser masks it, UUIDs, token secrets,
timestamps and long hex/base64 blobs, keeping statuses, selected headers,
error codes and all other values byte-comparable. Response signatures are
verified with the account's ed25519 key fetched from `GET /v1/accounts/{id}`.

To add a scenario: append one to `scenarios()` in `scenarios_test.go` (create
whatever it needs, never rely on global counts), then `make conformance-record`
and check the new golden into git.

The harness lives in `harness.go` (`Target` interface; `HandlerTarget` wraps an
in-process `http.Handler` for socketless boots), `normalise.go`, `signature.go`
and `diff.go`.
