GO ?= go
export GOCACHE ?= $(CURDIR)/.cache/go-build

.PHONY: build app icon release test run clean
build:
	mkdir -p dist
	$(GO) build -trimpath -ldflags="-s -w" -o dist/ccrouter ./cmd/ccrouter
	$(GO) build -trimpath -ldflags="-s -w" -o dist/ccrouter-tray ./cmd/ccrouter-tray
app: build
	mkdir -p dist/CCRouter.app/Contents/MacOS dist/CCRouter.app/Contents/Resources
	cp dist/ccrouter dist/CCRouter.app/Contents/MacOS/ccrouter
	cp dist/ccrouter-tray dist/CCRouter.app/Contents/MacOS/ccrouter-tray
	cp packaging/Info.plist dist/CCRouter.app/Contents/Info.plist
	cp packaging/CCRouter.icns dist/CCRouter.app/Contents/Resources/CCRouter.icns
	codesign --force --sign - dist/CCRouter.app
# Rebuilds the app icon from designs/icon/ccrouter.png.
icon:
	rm -rf dist/CCRouter.iconset
	$(GO) run ./tools/icongen designs/icon/ccrouter.png dist/CCRouter.iconset
	iconutil -c icns dist/CCRouter.iconset -o packaging/CCRouter.icns
# Apple Silicon app for macOS 13+, zipped with its SHA-256.
# Ad-hoc signed: without a Developer ID it cannot be notarized.
VERSION := $(shell sed -n 's/^const version = "\(.*\)"/\1/p' cmd/ccrouter/main.go)
REL := dist/release
release:
	@grep -q "<string>$(VERSION)</string>" packaging/Info.plist || { echo "Info.plist version differs from $(VERSION)"; exit 1; }
	rm -rf $(REL) && mkdir -p $(REL)/CCRouter.app/Contents/MacOS $(REL)/CCRouter.app/Contents/Resources
	for cmd in ccrouter ccrouter-tray; do \
		MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 GOARCH=arm64 \
			$(GO) build -trimpath -ldflags="-s -w" -o $(REL)/CCRouter.app/Contents/MacOS/$$cmd ./cmd/$$cmd || exit 1; \
	done
	cp packaging/Info.plist $(REL)/CCRouter.app/Contents/Info.plist
	cp packaging/CCRouter.icns $(REL)/CCRouter.app/Contents/Resources/CCRouter.icns
	codesign --force --sign - $(REL)/CCRouter.app
	cd $(REL) && ditto -c -k --keepParent CCRouter.app CCRouter-$(VERSION)-macos-arm64.zip
	cd $(REL) && shasum -a 256 CCRouter-$(VERSION)-macos-arm64.zip > CCRouter-$(VERSION)-macos-arm64.zip.sha256
	@cat $(REL)/CCRouter-$(VERSION)-macos-arm64.zip.sha256
test:
	$(GO) test -race ./internal/... ./cmd/...
run: build
	dist/ccrouter gui
clean:
	$(GO) clean
