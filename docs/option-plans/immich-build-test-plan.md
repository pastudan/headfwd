# Immich Build and Test Plan (Backend, Web, Mobile)

Date: 2026-02-04
Scope: Build and basic verification for Immich backend (`server`), web UI (`web`), and mobile (`mobile`) independently.

## Prereqs (one-time)

1. From the `immich` repo root, trust and install toolchain via mise:

```bash
cd /Users/dan/repos/headfwd/immich
mise trust
mise install
```

2. Verify tool versions (expected):

```bash
node -v     # should be 24.13.x
pnpm -v     # should be 10.28.x
flutter --version  # should show 3.35.7
```

3. Install JS dependencies for the monorepo:

```bash
pnpm install --frozen-lockfile
```

If `pnpm` is not installed, use mise to install it (step 1), or install via your preferred system method.

## Backend Build (server)

Goal: Compile the NestJS backend.

1. Build:

```bash
cd /Users/dan/repos/headfwd/immich
pnpm --filter immich build
```

2. Optional type/lint checks:

```bash
pnpm --filter immich check:code
```

3. Expected output:

- `immich/server/dist` exists with compiled output.
- No build errors.

## Web UI Build (web)

Goal: Build the SvelteKit web app.

1. Build:

```bash
cd /Users/dan/repos/headfwd/immich
pnpm --filter immich-web build
```

2. Optional checks/tests:

```bash
pnpm --filter immich-web check:code
pnpm --filter immich-web test
```

3. Expected output:

- `immich/web/build` (or `.svelte-kit` and `build/`) contains build artifacts.
- No build errors.

## Mobile Build (Flutter)

Goal: Ensure Flutter app compiles for the chosen target.

1. Install Flutter deps and generate translations:

```bash
cd /Users/dan/repos/headfwd/immich/mobile
flutter pub get
make translation
```

2. Build Android (debug APK):

```bash
flutter build apk --debug
```

3. Build iOS (simulator):

```bash
flutter build ios --simulator
```

Notes:
- iOS builds require Xcode and a configured simulator.
- If you only need one target, run only the matching command.

## Troubleshooting Quick Hits

1. If `pnpm` scripts refuse to run before install:

```bash
pnpm install --frozen-lockfile
```

2. If `node` version is too old:

```bash
mise use -g node@24.13.0
```

3. If Flutter fails to locate Xcode:

```bash
xcode-select -p
```

## What I Need From You

1. Confirm which targets you want me to build here:

- Backend + Web only
- Backend + Web + Android
- Backend + Web + iOS
- All three (Backend, Web, Android + iOS)

2. Confirm it is OK for me to install missing tools (pnpm, flutter) via mise in this environment.
