VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0)
LDFLAGS := -s -w -X github.com/ra5on/TitanOS/backend/internal/version.Version=$(VERSION)

.PHONY: build web titand titanosctl test test-release lint dev dev-backend dev-web zip zip-dev qemu clean

# titand with the web UI embedded: backend/bin/titand
build: web titand titanosctl

web: web/node_modules
	cd web && npm run build

titand:
	cd backend && CGO_ENABLED=0 go build -trimpath -tags embedui -ldflags '$(LDFLAGS)' -o bin/titand ./cmd/titand

titanosctl:
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/titanosctl ./cmd/titanosctl

test: web/node_modules
	cd backend && go vet ./... && go test ./...
	cd web && npm test
	cd web && npm run check

# Static checks. The versions are pinned; go run fetches them into the module
# cache. shellcheck comes from the distribution (apt/dnf install shellcheck).
STATICCHECK_VERSION := 2026.2.2
GOVULNCHECK_VERSION := v1.9.0
ACTIONLINT_VERSION := v1.7.12

lint:
	cd backend && go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	shellcheck scripts/*.sh
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

# Real miniature images/signatures exercise release packaging and tamper rejection.
test-release:
	@command -v mksquashfs >/dev/null && command -v unsquashfs >/dev/null && command -v minisign >/dev/null && command -v zip >/dev/null
	python3 -m unittest discover -s scripts/tests -v

# Backend on :8080 and the Vite dev server (with /api proxied to the backend) side by side.
dev:
	$(MAKE) -j2 dev-backend dev-web

# -dev: plain HTTP, no reboot or poweroff; the configuration lives in test/config.
dev-backend:
	cd backend && go run ./cmd/titand -dev -config ../test/config

dev-web: web/node_modules
	cd web && npm run dev

# The stick contents (out/stick) and the release zip; needs mkosi and squashfs-tools.
zip: build
	VERSION=$(VERSION) scripts/make-zip.sh

# Same with root autologin on the console, for testing.
zip-dev: build
	VERSION=$(VERSION) scripts/make-zip.sh --dev

# Boots out/stick in QEMU (UEFI) from an emulated USB stick.
qemu:
	scripts/run-qemu.sh

web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch $@

# The image tree contains files of other users (via /etc/subuid), which only
# a user namespace may delete.
clean:
	rm -rf backend/bin backend/internal/webui/dist web/.svelte-kit out
	$(if $(filter 0,$(shell id -u)),,unshare --user --map-auto --map-root-user) rm -rf build/mkosi.output build/mkosi.cache
