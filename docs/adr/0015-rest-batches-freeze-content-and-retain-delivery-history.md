# REST Batches freeze content and retain Delivery history

Accepted for the new REST contract; implementation is pending.

Creating a Batch freezes the Template content used by that Batch. Subsequent
Template edits or deletion do not change an accepted send, including one scheduled
for later. The current dispatcher reads mutable Template content at build time;
the new contract accepts the storage cost of a snapshot so an accepted request
has a stable meaning. Template identity remains available as provenance.

Deliveries receive stable public identifiers and retain a queryable history after
workers finish processing them. Today the operational pool deletes terminal rows;
that behavior cannot define the lifetime of the new public Delivery resource.
History retention is fixed at 30 days from Batch processing completion in the
first REST version; pending or scheduled Deliveries prevent expiration. Engagement events
remain distinct from delivery outcomes, and a later asynchronous bounce can update
a previously Delivered outcome.

Use simple persistent Delivery history updated by lifecycle transitions, with
queries for Batch state counts initially. This requirement does not introduce
event sourcing, a generic projection framework or synchronized duplicate counters.
The existing Stats pipeline and aggregate counters remain distinct.
