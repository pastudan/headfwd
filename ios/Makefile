LIBTAILSCALE_REPO := https://github.com/tailscale/libtailscale.git
LIBTAILSCALE_DIR  := vendor/libtailscale
FRAMEWORKS_DIR    := Frameworks
XCFRAMEWORK       := $(FRAMEWORKS_DIR)/TailscaleKit.xcframework

.PHONY: help vendor frameworks generate build-sim build-device clean-frameworks clean

help:
	@echo ""
	@echo "HeadFwd iOS — available targets:"
	@echo "  make vendor       Clone tailscale/libtailscale source"
	@echo "  make frameworks   Build TailscaleKit.xcframework (requires Xcode + Go)"
	@echo "  make generate     Run xcodegen to regenerate HeadFwdApp.xcodeproj"
	@echo "  make build-sim    Build for iOS Simulator"
	@echo "  make build-device Build for iOS device (requires provisioning)"
	@echo "  make setup        vendor + frameworks + generate  (first-time setup)"
	@echo ""

## ── Vendor ──────────────────────────────────────────────────────────────────

vendor: $(LIBTAILSCALE_DIR)/.git

$(LIBTAILSCALE_DIR)/.git:
	@echo "→ Cloning tailscale/libtailscale..."
	git clone --depth=1 $(LIBTAILSCALE_REPO) $(LIBTAILSCALE_DIR)
	@echo "→ Applying HeadFwd key-pinning patch..."
	cd $(LIBTAILSCALE_DIR) && git apply $(CURDIR)/patches/libtailscale-pinned-key.patch
	@echo "→ Applying HeadFwd portal-proxy patch..."
	cd $(LIBTAILSCALE_DIR) && git apply $(CURDIR)/patches/libtailscale-portal-proxy.patch
	@echo "✓ Patches applied"

## ── Frameworks ───────────────────────────────────────────────────────────────

frameworks: $(XCFRAMEWORK)

$(XCFRAMEWORK): $(LIBTAILSCALE_DIR)/.git
	@echo "→ Building libtailscale_ios.a (device)..."
	cd $(LIBTAILSCALE_DIR) && $(MAKE) libtailscale_ios.a
	@echo "→ Building libtailscale_ios_sim fat binary (sim arm64 + x86_64)..."
	cd $(LIBTAILSCALE_DIR) && $(MAKE) c-archive-ios-sim
	@echo "→ Building TailscaleKit.xcframework..."
	cd $(LIBTAILSCALE_DIR)/swift && $(MAKE) ios-fat
	@echo "→ Copying xcframework into $(FRAMEWORKS_DIR)/..."
	mkdir -p $(FRAMEWORKS_DIR)
	rm -rf $(XCFRAMEWORK)
	cp -R $(LIBTAILSCALE_DIR)/swift/build/Build/Products/Release-iphonefat/TailscaleKit.xcframework \
	      $(XCFRAMEWORK)
	@echo "✓ TailscaleKit.xcframework ready"

clean-frameworks:
	rm -rf $(FRAMEWORKS_DIR)
	cd $(LIBTAILSCALE_DIR)/swift && $(MAKE) clean 2>/dev/null || true

## ── Xcode project ────────────────────────────────────────────────────────────

generate:
	xcodegen generate

## ── Build ────────────────────────────────────────────────────────────────────

SCHEME      := HeadFwdApp
SIM_DEST    := platform=iOS Simulator,name=iPhone 16
DEVICE_UDID := YOUR_DEVICE_UDID
# Known teams:
#   YOUR_APPLE_TEAM_ID  dan@hexial.com (personal)
#   YOUR_APPLE_TEAM_ID  the app publisher (via dan@hexial.com)
TEAM        ?= YOUR_APPLE_TEAM_ID

build-sim: generate
	xcodebuild build \
	  -scheme $(SCHEME) \
	  -destination "$(SIM_DEST)" \
	  -derivedDataPath build/DerivedData \
	  CODE_SIGNING_ALLOWED=NO

build-device:
	xcodebuild build \
	  -scheme $(SCHEME) \
	  -destination "platform=iOS,id=$(DEVICE_UDID)" \
	  -derivedDataPath build/DerivedData \
	  -allowProvisioningUpdates \
	  -allowProvisioningDeviceRegistration

## ── Help ──────────────────────────────────────────────────────────────────────
# Note: build-device intentionally does NOT call generate -- xcodegen rewrites
# the project file and resets signing. Run 'make generate' only when you change
# project.yml, then re-set signing in Xcode once.

run-device: build-device
	APP_PATH=$$(find build/DerivedData -name "HeadFwdApp.app" -path "*/Debug-iphoneos/*" | head -1) && \
	xcrun devicectl device install app --device $(DEVICE_UDID) "$$APP_PATH" && \
	xcrun devicectl device process launch --device $(DEVICE_UDID) net.headfwd.app

## ── First-time setup ─────────────────────────────────────────────────────────

setup: vendor frameworks generate
	@echo ""
	@echo "✓ Setup complete. Run 'make build-sim' to build."
