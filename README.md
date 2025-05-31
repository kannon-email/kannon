# Kannon 💥

[![CI](https://github.com/gyozatech/kannon/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/gyozatech/kannon/actions/workflows/ci.yaml)

![Kannon Logo](assets/kannonlogo.png?raw=true)

A **Cloud Native SMTP mail sender** for Kubernetes and modern infrastructure.

> [!NOTE]
> Due to limitations of AWS, GCP, etc. on port 25, this project will not work on cloud providers that block port 25.

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quickstart](#quickstart)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Configuration](#configuration)
- [Database Schema](#database-schema)
- [API Overview](#api-overview)
- [Domain & DNS Setup](#domain--dns-setup)
- [Sending Mail](#sending-mail)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- Cloud-native, scalable SMTP mail sending
- gRPC API for sending HTML and templated emails
- DKIM and SPF support for deliverability
- Multi-node sending (planned)
- Template management (planned)
- Statistics and analytics (planned)
- Kubernetes-ready deployment
- Postgres-backed persistence

## Architecture

Kannon is composed of several microservices and workers:

- **API**: gRPC server for mail and domain management
- **SMTP**: Handles SMTP protocol and relays mail
- **Sender/Dispatcher**: Queues and sends emails
- **Verifier/Bounce/Stats**: (Optional) for validation, bounce handling, and analytics

All components can be enabled/disabled via CLI flags or config.

## Quickstart

### Prerequisites

- Go 1.22+
- Docker (optional, for containerized deployment)
- PostgreSQL database

### Local Run (for development)

```sh
git clone https://github.com/kannon-email/kannon.git
cd kannon
go build -o kannon .
./kannon --run-api --run-smtp --run-sender --run-dispatcher --config ./config.yaml
```

### Docker

```sh
docker pull ghcr.io/kannon-email/kannon/kannon:latest
docker run --env-file .env ghcr.io/kannon-email/kannon/kannon:latest --run-api --run-smtp --run-sender --run-dispatcher --config /etc/kannon/config.yaml
```

## Kubernetes Deployment

You can deploy Kannon using the manifests in the [`k8s/`](./k8s/deployment.yaml) folder.

Example config (YAML):

```yaml
nats_url: nats://nats:4222
debug: true
database_url: postgres://postgres:password@postgres:5432/kannon?sslmode=disable
api:
  port: 50051
sender:
  hostname: your-hostname
  max_jobs: 100
```

## Configuration

Kannon can be configured via YAML file, environment variables, or CLI flags. Key options:

- `database_url`: PostgreSQL connection string
- `nats_url`: NATS server for internal messaging (if used)
- `debug`: Enable debug logging
- `sender.hostname`: Hostname for outgoing mail
- `api.port`: gRPC API port
- `run-*`: Enable/disable components (api, smtp, sender, dispatcher, verifier, bounce, stats)

See [`cmd/root.go`](./cmd/root.go) for all flags and options.

## Database Schema

Kannon requires a PostgreSQL database. The main tables are:

- `domains`: Registered sender domains, DKIM keys
- `messages`: Outgoing messages
- `sending_pool_emails`: Email queue and status
- `templates`: Email templates

See [`db/migrations/`](./db/migrations/) for full schema and migrations.

## API Overview

Kannon exposes a gRPC API for sending mail and managing domains. See the proto definitions in [`./proto/kannon/`](./proto/kannon/):

- **Mailer API**: [`mailerapiv1.proto`](./proto/kannon/mailer/apiv1/mailerapiv1.proto)
  - `SendHTML`: Send a raw HTML email
  - `SendTemplate`: Send an email using a stored template
- **Admin API**: Domain management (see proto files)

Authentication is via Basic Auth using your domain and API key.

## Domain & DNS Setup

To send mail, you must register a sender domain and configure DNS:

1. Register a domain via the API (see Admin API/proto)
2. Set up DNS records:
   - **A record**: `<SENDER_NAME>` → your server IP
   - **Reverse DNS**: your server IP → `<SENDER_NAME>`
   - **SPF TXT**: `<SENDER_NAME>` → `v=spf1 ip4:<YOUR SENDER IP> -all`
   - **DKIM TXT**: `smtp._domainkey.<YOUR_DOMAIN>` → `k=rsa; p=<YOUR DKIM KEY HERE>`

## Sending Mail

Authenticate using Basic Auth:

```
token = base64(<your domain>:<your domain key>)
```

Pass this in the `Authorization` metadata for gRPC calls:

```json
{
  "Authorization": "Basic <your token>"
}
```

Example `SendHTML` request:

```json
{
  "sender": {
    "email": "no-reply@yourdomain.com",
    "alias": "Your Name"
  },
  "recipients": ["user@example.com"],
  "subject": "Test",
  "html": "<h1>Hello</h1><p>This is a test.</p>"
}
```

See the [proto file](./proto/kannon/mailer/apiv1/mailerapiv1.proto) for all fields and options.

![Signed Email](assets/email-sign.png)

## Contributing

We welcome contributions! Please:

- Use [feature request](.github/ISSUE_TEMPLATE/feature_request.md) and [bug report](.github/ISSUE_TEMPLATE/bug_report.md) templates for issues
- Follow the [pull request template](.github/PULL_REQUEST_TEMPLATE.md)
- See the [Apache 2.0 License](./LICENSE)

## License

Kannon is licensed under the Apache 2.0 License. See [LICENSE](./LICENSE) for details.
