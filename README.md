# Mail Sender

Cloud Native SMTP mail sender

> You need a k8s environment in a private cluster. Actually, due to limitations of AWS, GCP etc. on port 25 this project will not work on cloud providers.

## TODO

- Add GUI and User in React
- Manage Statistics
- Manage Templates

## Server Configuration

The SMTP server need to be configured in order to work properly.

1. Choose a SENDER_NAME setting an env variable in [./k8s/sender.yaml](./k8s/sender.yaml). In my example, this is `mailer.ludusrusso.space`. This should be an subdomain in your posses (you need to set some DNS records).
2. Set a reverse DNS record FROM your server IP -> TO SERVER_NAME
3. Set a A record FROM your SENDER_NAME domaint -> TO your server IP

## Create a New Sender Domain

Using `api` service and [api.proto](./proto/api.proto) you can create a New Domain in the system.
A domain should be a subdomain of the main domain you want to send the emails.
E.g., if you want to send email form `ludovico@test.space` you can choose a subdomain of `test.space`, e.g. `mail.test.space`.

Create the domain using the `createDomain` method

```
# Request
{
  "domain": "sending.ludusrusso.space"
}

# Response
{
  "domain": "sending.ludusrusso.space",
  "key": "xxxxxxxxxx",
  "dkimPubKey": "xxxxxxx"
}
```

This will generate a domain, with an api access key and a dkimPublicKey.

DNS record:

- TXT record for DKIM `smtp._domainkey.<YOUR_DOMAIN>` -> `k=rsa; p=<YOUR DKIM KEY HERE>`
- TXT record for SPF `<YOUR_DOMAIN>` -> `v=spf1 include:<SENDER_NAME> ~all`

When DNS record will be propagated, you are ready to start sending emails.

## Sending Mail

You can send emails using the mailer api and the [mailer.proto](./proto/mailer.proto) file.
You need to authenticate to this api using Basic authentication.

Create token for authentication:

`token = base64(<your domain>:<your domain key>)`

Then, in the `SendHTML` endpoint, pass a Metadata with

```json
{
  "Authorization": "Basic <your token>"
}
```

Example CALL

```
# Request
{
  "sender": {
    "email": "no-reply@ludusrusso.space",
    "alias": "Ludovico"
  },
  "to": [
    "ludus.russo@gmail.com",
    "ludovico@ludusrusso.space"
  ],
  "subject": "Test",
  "html": "<h1>ciao</h1><p>prova</p>"
}

# Reponse

{
  "messageID": "message/6ca47968-3b3a-401b-ad05-a816ce19d69c@sending.ludusrusso.space",
  "templateID": "tmp-template/fc0e8313-5800-4e82-91fe-940cfad21d19@sending.ludusrusso.space",
  "scheduled_time": {
    "seconds": "1609668627",
    "nanos": 905726068
  }
}
```
