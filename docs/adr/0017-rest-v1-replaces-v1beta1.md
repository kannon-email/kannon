# REST v1 replaces v1beta1 with the same contract

The REST API shipped as `/v1beta1`. We promote its contract unchanged to `/v1` and
deprecate `/v1beta1`, rather than first making the breaking changes a beta would
allow, so clients move by changing a base URL and nothing else. v1 carries the
stability promise v1beta1 had: compatible evolution only, and breaking changes need
`/v2`. This supersedes the `/v1beta1` prefix and compatibility promise of ADR 0014
and point 20 of `docs/rest-api-design.md`.

Until it is removed, `/v1beta1` is served by the same handlers as `/v1`, not by a copy
of the code. Removal is planned soon, with no fixed date. Deprecation is stated in the
v1beta1 OpenAPI contract and in the docs only; responses carry no `Deprecation` or
`Sunset` header, because the alias is short-lived.
