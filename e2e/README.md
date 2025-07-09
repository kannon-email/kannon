# Kannon E2E Testing Suite

This directory contains comprehensive end-to-end tests for the Kannon email system.

## Overview

The e2e tests verify the complete email pipeline from API submission to delivery verification using real infrastructure components:

- **PostgreSQL** database via Docker
- **NATS** messaging system via Docker
- **Test SMTP server** for email capture and verification
- **All Kannon services** running concurrently (API, Sender, Dispatcher, Validator, Stats)

The only mocked service is the real SMTP client that is used to capture and verify emails.
