# Public Egress Extension

The public egress extension is an HTTP client middleware that restricts an
exporter to public HTTPS destinations. It is intended for exporters whose
endpoint is controlled by a less-trusted user.

For every new connection, the extension resolves the destination hostname,
rejects the complete result if any address is non-public, and dials one of the
validated IP addresses directly. This prevents a second DNS lookup from changing
the destination between validation and connection. It also disables proxies and
rejects HTTP redirects before the client follows them.

```yaml
extensions:
  public_egress/customer: {}

exporters:
  otlphttp/customer:
    endpoint: https://otlp.example.com
    middlewares:
      - id: public_egress/customer

service:
  extensions: [public_egress/customer]
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [otlphttp/customer]
```

The extension must be the last middleware in the exporter's `middlewares` list,
which makes it the innermost middleware around the HTTP transport.
