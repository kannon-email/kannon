# Kannon E2E Testing Suite

This directory contains comprehensive end-to-end tests for the Kannon email system.

## Overview

The e2e tests verify the complete email pipeline from API submission to delivery verification. Kannon is booted through the production startup module (`x/startup`) from a real config file written to a temporary directory, with every listener on port 0, and the suite waits on the instance's readiness signal:

- **PostgreSQL** via the shared test helper (`internal/tests`), prepared by the real migrations
- **NATS** in-process
- **All Kannon services** running in one instance (API, Sender, Dispatcher, Validator, Stats, Tracker, inbound SMTP, Audit)

The outbound Sender is replaced through a startup option by a capturing double, and the retry backoff and Retry Window are compressed the same way.
