# HeadFwd Proxy (Fly.io)

This is the HeadFwd proxy, deployed on Fly.io. It supports custom HTTP upgrades (required for TS2021).

The earlier Cloudflare Workers + Durable Objects version is parked on the [`cloudflare-proxy`](https://github.com/pastudan/headfwd/tree/cloudflare-proxy) branch until Workers can proxy TS2021's custom HTTP Upgrade.

You don't need to run this to use HeadFwd. The public proxy at `headfwd.net` is the default. This guide is for running your own proxy on your own domain. Every sidecar that uses it is then reachable at `https://<fingerprint>.<your-domain>`.

The examples below use `example.com`. Replace it with your domain everywhere.

## Prereqs

- `flyctl` installed and logged in (`fly auth login`)
- A domain whose DNS you control. Either the apex (`example.com`) or a subdomain you dedicate to HeadFwd (`relay.example.com`) works. Sidecars will live one level below it.

## 1. Create and deploy the app

```bash
cd headfwd-proxy

# Create the app. fly.toml ships with app = 'headfwd-proxy'; pick your own unique name when prompted.
fly launch --no-deploy

# The public base domain. The proxy builds every tunnel/public URL as
# <fingerprint>.$PUBLIC_HOST, so this MUST equal the domain your sidecars expect.
fly secrets set PUBLIC_HOST=example.com

fly deploy

# Tunnel state lives in memory, so run exactly one machine.
fly scale count 1
```

Always set `PUBLIC_HOST`. Without it, the proxy guesses the domain from the request's `Host` header. Then a sidecar that registers via `<app>.fly.dev`, or any other alias, would be handed URLs on that domain, and it would refuse them.

`PUBLIC_SCHEME` is already `https` in `fly.toml`. Leave it there; sidecars only accept `https://` / `wss://` URLs.

## 2. Attach the apex and the wildcard

Clients reach a sidecar at `<fingerprint>.example.com`, and sidecars register at `example.com`, so you need a certificate for both names:

```bash
# Make sure the app has public IPs (fly launch usually allocates them).
fly ips list
# If missing:  fly ips allocate-v4 --shared   and   fly ips allocate-v6

fly certs add example.com
fly certs add "*.example.com"     # quote it so your shell doesn't expand the *
```

## 3. Configure DNS

At your DNS provider, create:

| Name | Type | Value |
|---|---|---|
| `example.com` | `A` | the app's IPv4 from `fly ips list` |
| `example.com` | `AAAA` | the app's IPv6 from `fly ips list` |
| `*.example.com` | `A` / `AAAA` | same as above (or `CNAME` → `<app>.fly.dev`) |
| `_acme-challenge.example.com` | `CNAME` | the target printed by `fly certs setup "*.example.com"` |

You need the `_acme-challenge` record. Let's Encrypt only issues wildcard certificates through the DNS-01 challenge, so `*.example.com` stays pending until that CNAME exists. Run `fly certs setup <hostname>` at any time to see the exact records Fly expects.

If you dedicated a subdomain such as `relay.example.com`, use that name in place of `example.com` throughout this table and in `PUBLIC_HOST`. If you use Cloudflare, leave these records DNS-only (grey cloud). See Fly's [custom domain docs](https://fly.io/docs/networking/custom-domain/).

Wait for both certificates to show as issued:

```bash
fly certs check example.com
fly certs check "*.example.com"
```

## 4. Verify the proxy

```bash
curl https://example.com/health
# → OK

# A fingerprint subdomain with no sidecar behind it should answer over valid TLS
# (proving the wildcard cert works) with 503 "Headscale not connected":
curl -i "https://0123456789abcdef0123456789abcdef.example.com/key?v=96"
```

## 5. Point sidecars at it

The sidecar works out the expected public domain locally from `--proxy`, or from `--proxy-domain` if that is set. It then rejects any tunnel or public URL from the proxy that isn't exactly `<its own fingerprint>.<that domain>`.

```bash
# CLI
./headfwd-sidecar --proxy https://example.com

# docker compose (repo root)
PROXY_URL=https://example.com docker compose up -d
```

Use `--proxy-domain` / `PROXY_DOMAIN` only when the address you register at isn't the public base domain. One example is registering at `https://api.example.com` while sidecars live at `<fingerprint>.example.com`:

```bash
./headfwd-sidecar --proxy https://api.example.com --proxy-domain example.com
```

On success the sidecar logs `public domain: example.com` and `✓ Registered at: https://<fingerprint>.example.com`. It then rewrites Headscale's `server_url` to that URL. Restart Headscale after this so the change takes effect, or set `RESTART_HEADSCALE=true`.

If the proxy's `PUBLIC_HOST` doesn't match, registration fails with:

```
proxy issued public URL on "other.net" but this sidecar expects "example.com" ...
```

To fix it, set `PUBLIC_HOST` on the proxy to the domain the sidecar expects.

## Moving off headfwd.net (or between proxies)

Your Headscale Noise key, and so your fingerprint, doesn't change when you switch proxies. To move:

1. Deploy your proxy (steps 1–4).
2. Restart the sidecar with `--proxy https://example.com` (or `PROXY_URL=...`). It re-registers, and it updates a HeadFwd-written `server_url` from `https://<fp>.headfwd.net` to `https://<fp>.example.com` automatically. A `server_url` you set by hand is only replaced with `--force-update`.
3. Restart Headscale.
4. Have devices scan a fresh QR code from the portal, because their saved login server still points at the old domain.

## Security notes

- The proxy is untrusted by design. It never sees keys or plaintext. A sidecar only accepts URLs that match the fingerprint it computes from Headscale's Noise key and the domain *it* was configured with. A rogue or misconfigured proxy can't move a sidecar onto another domain.
- Clients pin the fingerprint from the QR code and verify Headscale's Noise key against it, whichever proxy is in the path. See [`docs/ios-key-verification.md`](../docs/ios-key-verification.md).

## Notes

- This proxy stores registrations in memory. If the app restarts, the sidecar will re-register automatically.
- Run a single instance to keep tunnel state consistent (`fly scale count 1`).
