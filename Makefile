# IP-Beamer build targets. Pure Go, no cgo, no external modules — every target
# is a single static binary that runs with zero runtime dependencies.
#
# All release artifacts land under dist/:  dist/server/ and dist/client/.
# Filenames carry the version, e.g. dist/server/ipbeamd-1.0.0-openwrt-arm64.
# Override the version with, e.g., `make packages VERSION=1.1.0`.

VERSION ?= 1.0.2
DIST := dist
SRV := $(DIST)/server
CLI := $(DIST)/client
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all test fmt vet clean server client \
        server-openwrt server-ubuntu clients deb openwrt-pkg packages android

all: test server-openwrt server-ubuntu clients

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# Local host builds (scratch, not versioned in the name).
server:
	go build -ldflags="$(LDFLAGS)" -o $(DIST)/ipbeamd ./cmd/ipbeamd
client:
	go build -ldflags="$(LDFLAGS)" -o $(DIST)/ipbeam ./cmd/ipbeam

# Server for the GL.iNet Flint 2 (MediaTek Filogic 880 — ARM64).
server-openwrt:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" \
		-o $(SRV)/ipbeamd-$(VERSION)-openwrt-arm64 ./cmd/ipbeamd

# Server for Ubuntu 24.04 x86-64.
server-ubuntu:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" \
		-o $(SRV)/ipbeamd-$(VERSION)-ubuntu-amd64 ./cmd/ipbeamd

# Reference CLI client for every desktop OS.
clients:
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(CLI)/ipbeam-$(VERSION)-windows-amd64.exe ./cmd/ipbeam
	GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(CLI)/ipbeam-$(VERSION)-macos-arm64      ./cmd/ipbeam
	GOOS=darwin  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(CLI)/ipbeam-$(VERSION)-macos-amd64      ./cmd/ipbeam
	GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(CLI)/ipbeam-$(VERSION)-linux-amd64      ./cmd/ipbeam

# Ubuntu .deb (needs nfpm: https://nfpm.goreleaser.com/install/).
deb: server-ubuntu
	cp $(SRV)/ipbeamd-$(VERSION)-ubuntu-amd64 $(SRV)/ipbeamd-ubuntu-amd64
	VERSION=$(VERSION) nfpm package -f nfpm.yaml -p deb -t $(SRV)/ipbeamd_$(VERSION)_amd64.deb
	rm -f $(SRV)/ipbeamd-ubuntu-amd64
	@echo "built $(SRV)/ipbeamd_$(VERSION)_amd64.deb"

# OpenWrt install tarball: static arm64 binary + config + init + install.sh.
openwrt-pkg: server-openwrt
	rm -rf $(SRV)/openwrt-pkg && mkdir -p $(SRV)/openwrt-pkg
	cp $(SRV)/ipbeamd-$(VERSION)-openwrt-arm64 $(SRV)/openwrt-pkg/ipbeamd
	cp deploy/openwrt/config.json   $(SRV)/openwrt-pkg/config.json
	cp deploy/openwrt/ipbeamd.init  $(SRV)/openwrt-pkg/ipbeamd.init
	cp deploy/openwrt/install.sh    $(SRV)/openwrt-pkg/install.sh
	cp deploy/openwrt/uninstall.sh  $(SRV)/openwrt-pkg/uninstall.sh
	chmod +x $(SRV)/openwrt-pkg/install.sh $(SRV)/openwrt-pkg/uninstall.sh
	tar -C $(SRV)/openwrt-pkg -czf $(SRV)/ipbeamd-$(VERSION)-openwrt-arm64.tar.gz .
	rm -rf $(SRV)/openwrt-pkg
	@echo "built $(SRV)/ipbeamd-$(VERSION)-openwrt-arm64.tar.gz"

# Android APK -> dist/client/ipbeamer-$(VERSION).apk.
# Requires JAVA_HOME (JDK 17) and the Android SDK (ANDROID_HOME / local.properties).
android:
	cd android && ./gradlew assembleDebug
	mkdir -p $(CLI)
	cp android/app/build/outputs/apk/debug/app-debug.apk $(CLI)/ipbeamer-$(VERSION).apk
	@echo "built $(CLI)/ipbeamer-$(VERSION).apk"

packages: deb openwrt-pkg clients

clean:
	rm -rf $(DIST)
