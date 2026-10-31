# REST is the primary API, with explicit Domain resources

Accepted during the REST API design interview.

REST becomes the primary application contract for administration, sending, and
statistics. Existing gRPC APIs are deprecated but remain available during a
transition whose end date is not yet decided. This allows the REST contract to
use the established domain vocabulary without immediately breaking existing
integrations; it accepts the temporary cost of maintaining two transports.

Global administrators and Domain credentials use the same resource paths under
`/v1beta1/domains/{domain}` rather than separate administrative and customer APIs.
Authority determines which operations a credential can perform and on which
Domains. An administrator acting for a Domain retains its authenticated identity
in the audit record, consistent with ADR 0008; selecting a Domain does not replace
the Principal with a Domain credential.

Domain credentials support selected capabilities or full administration within
their Domain. Issued authority cannot exceed the issuer's authority, and existing
send-only keys do not gain new permissions. The approved role catalogue and
credential contract are recorded in `docs/rest-api-design.md`.

The beta catalogue is limited to admin, sender, batch-reader, analyst and
metrics-reader. Template and credential management require admin within the
relevant Domain. Dedicated template-editor and key-manager roles are deferred:
their extra granularity and limited-authority issuance are not needed for the
initial contract. The existing authorization engine is reused.

Changes within v1beta1 remain compatible; breaking changes require a new API
version. New functionality goes to REST, while gRPC receives necessary fixes.
There is no fixed gRPC removal date.

Existing API Keys remain in the same api_keys table with their existing secrets,
hashes and identifiers. New role metadata extends that table additively; existing
rows need no rewrite or reissuance and continue resolving to sender when the new
metadata is absent. Choosing a parallel credential table would create a migration
the design explicitly excludes.
