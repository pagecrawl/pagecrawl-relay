#!/usr/bin/env bash
# Build the macOS release assets on a Mac:
#
#   pagecrawl-relay-darwin-arm64, pagecrawl-relay-darwin-amd64
#       The command-line program, as before. Signed and notarized, so a browser
#       download opens without the "cannot check it for malware" warning.
#   pagecrawl-relay-macos.dmg
#       PageCrawl Relay.app, the menu-bar build (-tags tray) as one universal app,
#       signed, notarized and stapled, so it also opens offline on first launch.
#
# Usage, from the relay-client directory:
#
#   packaging/macos/build.sh v0.1.7 dist
#
# MACOS_SIGN_IDENTITY picks the certificate ("Developer ID Application: ..."). Left
# unset, everything is signed ad hoc, which is enough to try the app on this Mac and
# nothing else. MACOS_KEYCHAIN points codesign at a keychain other than the default.
# Notarization runs only when NOTARY_KEY_FILE, NOTARY_KEY_ID and NOTARY_ISSUER (an
# App Store Connect API key) are all set.
set -euo pipefail

TAG="${1:?usage: packaging/macos/build.sh <tag> [outdir]}"
OUT="${2:-dist}"
IDENTITY="${MACOS_SIGN_IDENTITY:--}"
BUNDLE_ID="io.pagecrawl.relay"
APP_NAME="PageCrawl Relay"

# Info.plist wants plain dotted numbers; a local "dev" build gets 0.0.0.
PLIST_VERSION="${TAG#v}"
[[ "$PLIST_VERSION" =~ ^[0-9]+(\.[0-9]+){0,2}$ ]] || PLIST_VERSION="0.0.0"

cd "$(dirname "$0")/../.."
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# The oldest macOS the Go toolchain supports (macOS 13 for Go 1.27), so cgo objects
# in the app are built for it too rather than for whatever this Mac runs.
export MACOSX_DEPLOYMENT_TARGET=13.0
# Go honours the deployment target for its own code, but the cgo objects (systray's
# and the login-item code) are compiled against whatever SDK this Mac has, and the
# linker says so: "built for newer macOS version". Passing the minimum to clang too
# is what actually makes the app run on the macOS its Info.plist claims.
export CGO_CFLAGS="-mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET}"
export CGO_LDFLAGS="-mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET}"
LDFLAGS="-s -w -X main.Version=${TAG}"

sign() {
    local args=(--force --options runtime --sign "$IDENTITY")
    if [ "$IDENTITY" = "-" ]; then
        args+=(--timestamp=none)
    else
        # A secure timestamp is required for notarization.
        args+=(--timestamp)
    fi
    [ -n "${MACOS_KEYCHAIN:-}" ] && args+=(--keychain "$MACOS_KEYCHAIN")
    codesign "${args[@]}" "$@"
}

notarizing() {
    [ -n "${NOTARY_KEY_FILE:-}" ] && [ -n "${NOTARY_KEY_ID:-}" ] && [ -n "${NOTARY_ISSUER:-}" ]
}

# notarytool exits 0 for a rejected submission too, so read the verdict, and print
# Apple's log on a rejection because that is the only place the reason is given.
notarize() {
    local file="$1" result id status
    result="$(xcrun notarytool submit "$file" \
        --key "$NOTARY_KEY_FILE" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" \
        --wait --timeout 30m --output-format json)"
    id="$(plutil -extract id raw - <<<"$result")"
    status="$(plutil -extract status raw - <<<"$result")"
    echo "notarization of $(basename "$file"): $status ($id)"
    if [ "$status" != "Accepted" ]; then
        xcrun notarytool log "$id" \
            --key "$NOTARY_KEY_FILE" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER" || true
        exit 1
    fi
}

# --- Command-line binaries -------------------------------------------------------

for arch in arm64 amd64; do
    bin="$OUT/pagecrawl-relay-darwin-$arch"
    CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "$bin" .
    sign --identifier "$BUNDLE_ID.cli" "$bin"
    echo "built $(basename "$bin")"
done

if notarizing; then
    # A bare executable cannot be stapled; Gatekeeper looks its ticket up online,
    # which is fine for a file that was just downloaded.
    mkdir -p "$WORK/cli"
    cp "$OUT/pagecrawl-relay-darwin-arm64" "$OUT/pagecrawl-relay-darwin-amd64" "$WORK/cli/"
    ditto -c -k "$WORK/cli" "$WORK/cli.zip"
    notarize "$WORK/cli.zip"
fi

# --- Menu-bar app ----------------------------------------------------------------

# The tray build needs cgo, so each architecture is built here and joined with lipo.
# Go passes clang the matching -arch, so an Apple Silicon Mac builds both.
for arch in arm64 amd64; do
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" go build -tags tray -trimpath \
        -ldflags "$LDFLAGS" -o "$WORK/relay-$arch" .
done

APP="$WORK/app/$APP_NAME.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/pagecrawl-relay" "$WORK/relay-arm64" "$WORK/relay-amd64"

ICONSET="$WORK/AppIcon.iconset"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
    sips -z "$size" "$size" packaging/macos/icon-1024.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
    double=$((size * 2))
    sips -z "$double" "$double" packaging/macos/icon-1024.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"

# LSUIElement: a menu-bar app, with no Dock icon and no app menu.
cat >"$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleDisplayName</key>
    <string>$APP_NAME</string>
    <key>CFBundleExecutable</key>
    <string>pagecrawl-relay</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>$BUNDLE_ID</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>$APP_NAME</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$PLIST_VERSION</string>
    <key>CFBundleVersion</key>
    <string>$PLIST_VERSION</string>
    <key>LSApplicationCategoryType</key>
    <string>public.app-category.utilities</string>
    <key>LSMinimumSystemVersion</key>
    <string>$MACOSX_DEPLOYMENT_TARGET</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHumanReadableCopyright</key>
    <string>MIT licensed. Source: github.com/pagecrawl/pagecrawl-relay</string>
</dict>
</plist>
PLIST
plutil -lint "$APP/Contents/Info.plist" >/dev/null

sign --identifier "$BUNDLE_ID" "$APP"
codesign --verify --strict --verbose=2 "$APP"

# Notarize and staple the app itself before it goes in the image, so the copy people
# drag to Applications carries its own ticket.
if notarizing; then
    ditto -c -k --keepParent "$APP" "$WORK/app.zip"
    notarize "$WORK/app.zip"
    xcrun stapler staple "$APP"
fi

ln -s /Applications "$WORK/app/Applications"
DMG="$OUT/pagecrawl-relay-macos.dmg"
rm -f "$DMG"
hdiutil create -quiet -volname "$APP_NAME" -srcfolder "$WORK/app" -fs HFS+ -format UDZO -ov "$DMG"
sign --identifier "$BUNDLE_ID.dmg" "$DMG"

if notarizing; then
    notarize "$DMG"
    xcrun stapler staple "$DMG"
    spctl --assess --type open --context context:primary-signature --verbose=2 "$DMG"
fi

echo "built $(basename "$DMG")"
