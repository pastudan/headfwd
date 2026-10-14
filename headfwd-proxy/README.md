# HeadFwd Proxy (Fly.io)

This is the HeadFwd proxy, deployed on Fly.io. It supports custom HTTP upgrades (required for TS2021).

The earlier Cloudflare Workers + Durable Objects version is parked on the [`cloudflare-proxy`](https://github.com/pastudan/headfwd/tree/cloudflare-proxy) branch until Workers can proxy TS2021's custom HTTP Upgrade.

## Prereqs

- `flyctl` installed and logged in

## Quick Start

```bash
cd headfwd-proxy

# Create app (choose a name)
fly launch --no-deploy

# Optional: set your apex domain for correct subdomains
fly secrets set PUBLIC_HOST=headfwd.net

# Deploy
fly deploy
```

## Custom Domains

After deployment, attach your domain to the Fly app:

```bash
fly domains add headfwd.net
fly domains add '*.headfwd.net'
```

Then configure the DNS records Fly gives you.

## Notes

- This proxy stores registrations in memory. If the app restarts, the sidecar will re-register automatically.
- Run a single instance to keep tunnel state consistent:
  ```bash
  fly scale count 1
  ```
