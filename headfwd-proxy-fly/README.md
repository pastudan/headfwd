# HeadFwd Proxy (Fly.io)

This is a Fly.io deployment target for the HeadFwd proxy that supports custom HTTP upgrades (required for TS2021).

## Prereqs

- `flyctl` installed and logged in

## Quick Start

```bash
cd headfwd-proxy-fly

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
