#!/usr/bin/env python3
"""Build the README architecture diagram, light and dark.

The layout is defined once below; the palette is a token table per mode. After
editing either, re-run:

    python3 docs/diagrams/build.py

Writes the HTML sources to docs/diagrams/src/ and exports standalone SVGs to
docs/images/ via the diagram-design skill's export helper
(.agents/skills/diagram-design/scripts/export_svg.py, from
github.com/cathrynlavery/diagram-design; not committed here).
"""

from __future__ import annotations

import math
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SRC_DIR = ROOT / "docs" / "diagrams" / "src"
OUT_DIR = ROOT / "docs" / "images"
EXPORT = ROOT / ".agents" / "skills" / "diagram-design" / "scripts" / "export_svg.py"

# Semantic roles per the diagram-design style guide. `node` is the backend
# node fill (white in light, paper-2 in dark).
PALETTES: dict[str, dict[str, dict[str, str]]] = {
    "default": {
        "light": dict(paper="#f5f5f5", node="#ffffff", ink="#2d3142", muted="#4f5d75",
                      soft="#5a6580", accent="#bf4520", link="#2e5aa8"),
        "dark": dict(paper="#2d3142", node="#393e53", ink="#f5f5f5", muted="#bfc0c0",
                     soft="#949eb2", accent="#f08a59", link="#739fdf"),
    },
}

W, H = 960, 504
TOP = 24  # viewBox starts here; nothing is drawn above it
SANS = "'Geist', system-ui, -apple-system, 'Segoe UI', sans-serif"
MONO = "'Geist Mono', ui-monospace, SFMono-Regular, Menlo, monospace"
SERIF = "'Instrument Serif', Georgia, serif"


def rgba(hex_color: str, alpha: float) -> str:
    r, g, b = (int(hex_color[i:i + 2], 16) for i in (1, 3, 5))
    return f"rgba({r},{g},{b},{alpha:g})"


def luminance(hex_color: str) -> float:
    def ch(c: int) -> float:
        c /= 255
        return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = (int(hex_color[i:i + 2], 16) for i in (1, 3, 5))
    return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b)


def contrast(a: str, b: str) -> float:
    la, lb = sorted((luminance(a), luminance(b)), reverse=True)
    return (la + 0.05) / (lb + 0.05)


def r4(v: float) -> int:
    return int(math.ceil(v / 4) * 4)


class Canvas:
    def __init__(self, t: dict[str, str]):
        self.t = t
        self.parts: list[str] = []

    def add(self, s: str) -> None:
        self.parts.append(s)

    # -- primitives -------------------------------------------------------
    def zone(self, x, y, w, h, label):
        t = self.t
        lw = r4(len(label) * 5.4 + 16)
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="8" fill="{rgba(t["ink"], 0.02)}" '
                 f'stroke="{rgba(t["ink"], 0.14)}" stroke-width="0.8"/>')
        self.add(f'<rect x="{x + 16}" y="{y + 4}" width="{lw}" height="12" rx="2" fill="{t["paper"]}"/>')
        self.add(f'<text x="{x + 16 + lw / 2:g}" y="{y + 13}" fill="{t["soft"]}" font-size="7" font-weight="500" '
                 f'font-family="{MONO}" text-anchor="middle" letter-spacing="0.14em">{label}</text>')

    def group(self, x, y, w, h, label):
        """Dashed inner grouping with its label chip sitting on the bottom edge."""
        t = self.t
        lw = r4(len(label) * 5.4 + 16)
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="6" fill="none" '
                 f'stroke="{rgba(t["ink"], 0.28)}" stroke-width="0.8" stroke-dasharray="4,3"/>')
        self.add(f'<rect x="{x + 16}" y="{y + h - 6}" width="{lw}" height="12" rx="2" fill="{t["paper"]}"/>')
        self.add(f'<text x="{x + 16 + lw / 2:g}" y="{y + h + 3}" fill="{t["soft"]}" font-size="7" font-weight="500" '
                 f'font-family="{MONO}" text-anchor="middle" letter-spacing="0.14em">{label}</text>')

    def label(self, cx, line_y, text, color, side="above", step=None):
        """Arrow label on a horizontal segment, 8px clear of the stroke."""
        t = self.t
        mw = r4(len(text) * 5.6 + 12)
        my = line_y - 20 if side == "above" else line_y + 8
        mx = cx - mw / 2
        self.add(f'<rect x="{mx:g}" y="{my}" width="{mw}" height="12" rx="2" fill="{t["paper"]}"/>')
        self.add(f'<text x="{cx:g}" y="{my + 9}" fill="{color}" font-size="8" font-family="{MONO}" '
                 f'text-anchor="middle" letter-spacing="0.06em">{text}</text>')
        if step is not None:
            self.badge(mx - 10, my + 6, step, color)

    def side_label(self, line_x, cy, text, color, step=None):
        """Arrow label beside a vertical segment, 8px clear of the stroke."""
        t = self.t
        mw = r4(len(text) * 5.6 + 12)
        mx = line_x + 8 + (20 if step is not None else 0)
        self.add(f'<rect x="{mx}" y="{cy - 6}" width="{mw}" height="12" rx="2" fill="{t["paper"]}"/>')
        self.add(f'<text x="{mx + mw / 2:g}" y="{cy + 3}" fill="{color}" font-size="8" font-family="{MONO}" '
                 f'text-anchor="middle" letter-spacing="0.06em">{text}</text>')
        if step is not None:
            self.badge(line_x + 18, cy, step, color)

    def badge(self, cx, cy, n, color):
        t = self.t
        self.add(f'<circle cx="{cx:g}" cy="{cy}" r="7" fill="{t["paper"]}" stroke="{color}" stroke-width="1"/>')
        self.add(f'<text x="{cx:g}" y="{cy + 3}" fill="{color}" font-size="8" font-weight="600" '
                 f'font-family="{MONO}" text-anchor="middle">{n}</text>')

    def node(self, x, y, w, h, kind, tag, name, sub, sub2=None):
        t = self.t
        fill, stroke, tag_ink = {
            "focal": (rgba(t["accent"], 0.09), t["accent"], t["accent"]),
            "backend": (t["node"], t["ink"], t["ink"]),
            "external": (rgba(t["ink"], 0.03), rgba(t["ink"], 0.34), t["soft"]),
            "input": (rgba(t["muted"], 0.10), t["soft"], t["soft"]),
        }[kind]
        tag_stroke = rgba(t["accent"], 0.5) if kind == "focal" else rgba(t["ink"], 0.3)
        tw = r4(len(tag) * 4.9 + 10)
        cx = x + w / 2
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="6" fill="{t["paper"]}"/>')
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="6" fill="{fill}" stroke="{stroke}" stroke-width="1"/>')
        self.add(f'<rect x="{x + 8}" y="{y + 8}" width="{tw}" height="12" rx="2" fill="none" stroke="{tag_stroke}" stroke-width="0.8"/>')
        self.add(f'<text x="{x + 8 + tw / 2:g}" y="{y + 17}" fill="{tag_ink}" font-size="7" font-weight="500" '
                 f'font-family="{MONO}" text-anchor="middle" letter-spacing="0.08em">{tag}</text>')
        name_y = y + h / 2 + (2 if sub2 is None else -6)
        self.add(f'<text x="{cx:g}" y="{name_y:g}" fill="{t["ink"]}" font-size="12" font-weight="600" '
                 f'font-family="{SANS}" text-anchor="middle">{name}</text>')
        self.add(f'<text x="{cx:g}" y="{name_y + 16:g}" fill="{t["muted"]}" font-size="9" '
                 f'font-family="{MONO}" text-anchor="middle">{sub}</text>')
        if sub2:
            self.add(f'<text x="{cx:g}" y="{name_y + 29:g}" fill="{t["muted"]}" font-size="9" '
                     f'font-family="{MONO}" text-anchor="middle">{sub2}</text>')


def svg_body(t: dict[str, str], slug: str) -> str:
    c = Canvas(t)
    accent, link, muted, ink = t["accent"], t["link"], t["muted"], t["ink"]

    c.add(f'<rect x="0" y="{TOP}" width="{W}" height="{H - TOP}" fill="{t["paper"]}"/>')

    # Zones (bg -> zones -> arrows -> labels -> nodes)
    c.zone(40, 64, 304, 352, "YOUR NETWORK · BEHIND NAT")
    c.zone(424, 168, 208, 144, "UNTRUSTED RELAY")
    c.zone(712, 176, 208, 128, "ANYWHERE")
    # Headscale, the sidecar and the app itself can be embedded in one binary.
    c.group(72, 86, 240, 312, "CAN SHIP AS ONE APP")

    # Arrows. The two tunnels point at the proxy: both sides dial out to it.
    # 1. Sidecar dials out: the persistent tunnel (focal)
    c.add(f'<line x1="298" y1="240" x2="446" y2="240" stroke="{accent}" stroke-width="1.6" marker-end="url(#arrow-accent)"/>')
    # 2. Client dials the proxy, which splices it into (1); Noise E2E to Headscale
    c.add(f'<line x1="734" y1="240" x2="610" y2="240" stroke="{link}" stroke-width="1.2" marker-end="url(#arrow-link)"/>')
    # Sidecar hands control traffic to Headscale locally
    c.add(f'<line x1="192" y1="208" x2="192" y2="162" stroke="{muted}" stroke-width="1.2" marker-end="url(#arrow)"/>')
    # Optional: your app drives the sidecar's user + device management API, so
    # onboarding (create user, preauth key, QR) can live in your app's own UI.
    c.add(f'<line x1="144" y1="318" x2="144" y2="274" stroke="{muted}" stroke-width="1.2" '
          f'stroke-dasharray="4,3" marker-end="url(#arrow)"/>')
    # 3. WireGuard, direct where possible (bypasses the relay); traffic flows both ways
    c.add(f'<path d="M 816,282 V 344 Q 816,352 808,352 H 298" fill="none" stroke="{ink}" stroke-width="1.2" '
          f'marker-start="url(#arrow-ink)" marker-end="url(#arrow-ink)"/>')

    # Labels
    c.label(372, 240, "WSS TUNNEL", accent, "above", step=1)
    c.label(684, 240, "WSS TUNNEL", link, "above", step=2)
    c.side_label(192, 184, "LOCAL", muted)
    c.side_label(144, 296, "USER + DEVICE MGMT", muted)
    c.label(560, 352, "DIRECT CHANNEL VIA WIREGUARD", ink, "above", step=3)

    # Nodes
    c.node(88, 96, 208, 64, "backend", "CTRL", "Headscale", "control plane")
    c.node(88, 208, 208, 64, "focal", "SIDECAR", "HeadFwd sidecar", "headfwd-sidecar")
    c.node(88, 320, 208, 64, "backend", "APP", "Your app", "e.g. Frigate · Immich")
    c.node(448, 200, 160, 80, "external", "PROXY", "HeadFwd proxy", "&lt;fingerprint&gt;.headfwd.net", "hosted on Fly.io")
    c.node(736, 200, 160, 80, "input", "CLIENT", "Client", "iOS / Android app", "libtailscale baked in")

    # Legend strip
    ly = 440
    c.add(f'<line x1="40" y1="{ly}" x2="{W - 40}" y2="{ly}" stroke="{rgba(ink, 0.12)}" stroke-width="0.8"/>')
    c.add(f'<text x="40" y="{ly + 22}" fill="{muted}" font-size="8" font-family="{MONO}" letter-spacing="0.18em">LEGEND</text>')
    lx, iy = 112, ly + 16
    items = [
        ("box", rgba(accent, 0.09), accent, None, "HeadFwd sidecar"),
        ("box", t["node"], ink, None, "Self-hosted"),
        ("box", rgba(ink, 0.03), rgba(ink, 0.34), None, "Untrusted relay"),
        ("line", accent, "arrow-accent", "", "Outbound tunnel"),
        ("line", link, "arrow-link", "", "Client tunnel · Noise E2E"),
        ("line", ink, "arrow-ink", "both", "Direct WireGuard"),
        ("line", muted, "arrow", "dash", "Optional in-app onboarding"),
    ]
    for kind, a, b, style, text in items:
        # Wrap to a second row rather than run past the right margin.
        if (lx + (20 if kind == "box" else 34) + r4(len(text) * 4.9)) > W - 40:
            lx, iy = 112, iy + 18
        if kind == "box":
            c.add(f'<rect x="{lx}" y="{iy}" width="14" height="10" rx="2" fill="{a}" stroke="{b}" stroke-width="1"/>')
            tx = lx + 20
        else:
            start = f' marker-start="url(#{b})"' if style == "both" else ""
            dash = ' stroke-dasharray="4,3"' if style == "dash" else ""
            c.add(f'<line x1="{lx + 2}" y1="{iy + 5}" x2="{lx + 26}" y2="{iy + 5}" stroke="{a}" stroke-width="1.2"'
                  f'{dash}{start} marker-end="url(#{b})"/>')
            tx = lx + 34
        c.add(f'<text x="{tx}" y="{iy + 8.5:g}" fill="{muted}" font-size="8.5" font-family="{SANS}">{text}</text>')
        lx = tx + r4(len(text) * 4.9) + 20

    markers = "\n      ".join(
        f'<marker id="{mid}" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto-start-reverse">'
        f'<polygon points="0 0, 8 3, 0 6" fill="{col}"/></marker>'
        for mid, col in (("arrow", muted), ("arrow-accent", accent), ("arrow-link", link), ("arrow-ink", ink))
    )
    body = "\n      ".join(c.parts)
    return f'''<svg viewBox="0 {TOP} {W} {H - TOP}" width="{W}" height="{H - TOP}" xmlns="http://www.w3.org/2000/svg" role="img" aria-labelledby="{slug}-title {slug}-desc">
      <title id="{slug}-title">HeadFwd architecture</title>
      <desc id="{slug}-desc">Headscale, the HeadFwd sidecar and your app run behind NAT and can ship as a single app. The sidecar dials out a persistent WSS tunnel to an untrusted HeadFwd proxy. Clients open their own WSS tunnel to the proxy at their fingerprint subdomain, and the proxy splices it into the sidecar's tunnel; the control traffic inside is Noise-encrypted end to end, so only Headscale can read it. Devices then reach your app over a secure direct WireGuard channel. Optionally, your app can call the sidecar's user and device management API, so onboarding lives in your app's own UI.</desc>
      <defs>
      {markers}
      </defs>
      {body}
    </svg>'''


def page(t: dict[str, str], slug: str) -> str:
    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>HeadFwd · Architecture</title>
  <link href="https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    *, *::before, *::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
    body {{
      font-family: {SANS};
      background: {t["paper"]};
      color: {t["ink"]};
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 3rem 1rem;
    }}
    .frame {{ max-width: 1100px; width: 100%; }}
    .diagram-container {{ width: 100%; overflow-x: auto; }}
    .eyebrow {{
      font-family: {MONO}; font-size: 0.66rem; font-weight: 500; letter-spacing: 0.18em;
      text-transform: uppercase; color: {t["muted"]}; margin-bottom: 0.5rem;
    }}
    h1 {{
      font-family: {SERIF}; font-size: clamp(1.5rem, 2.4vw + 0.75rem, 2rem); font-weight: 400;
      letter-spacing: -0.02em; line-height: 1.15; color: {t["ink"]}; margin-bottom: 1.5rem;
    }}
    svg {{ width: 100%; min-width: {W}px; display: block; }}
  </style>
</head>
<body>
  <div class="frame">
    <p class="eyebrow">Architecture · HeadFwd</p>
    <h1>Reaching a Headscale server behind NAT</h1>
    <div class="diagram-container">
    {svg_body(t, slug)}
    </div>
  </div>
</body>
</html>
'''


def preview() -> str:
    rows = "\n".join(
        f'<h2>{name}</h2><div class="row">'
        + "".join(f'<img src="../images/architecture-{name}-{mode}.svg" alt="{name} {mode}">' for mode in modes)
        + "</div>"
        for name, modes in PALETTES.items()
    )
    return f'''<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><title>Architecture palettes</title>
<!-- Generated by build.py. Shows each exported SVG as <img>, the way GitHub renders it. -->
<style>
body{{margin:0;font:14px system-ui,sans-serif;background:#ddd;color:#222}}
h2{{margin:24px 16px 8px;font-weight:600}}
.row{{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:0 16px}}
img{{width:100%;height:auto;display:block;border:1px solid #bbb}}
</style></head><body>
{rows}
</body></html>
'''


def main() -> int:
    if not EXPORT.is_file():
        print(f"error: SVG exporter not found at {EXPORT.relative_to(ROOT)}\n"
              "It ships with the diagram-design agent skill (github.com/cathrynlavery/diagram-design),\n"
              "which is not committed to this repo. Install that skill into .agents/skills/ and re-run.",
              file=sys.stderr)
        return 2
    SRC_DIR.mkdir(parents=True, exist_ok=True)
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    failed = False
    for name, modes in PALETTES.items():
        for mode, t in modes.items():
            for role in ("ink", "muted", "soft", "accent", "link"):
                ratio = contrast(t[role], t["paper"])
                if ratio < 4.5:
                    print(f"warn: {name}/{mode} {role} {ratio:.2f}:1 < 4.5:1", file=sys.stderr)
                    failed = True
            slug = f"architecture-{name}-{mode}"
            src = SRC_DIR / f"{slug}.html"
            src.write_text(page(t, slug))
            subprocess.run([sys.executable, str(EXPORT), str(src), str(OUT_DIR / f"{slug}.svg")], check=True)
    (ROOT / "docs" / "diagrams" / "preview.html").write_text(preview())
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
