.PHONY: build build-frontend build-server dev clean

build: build-frontend build-server

build-frontend:
	cd portal/frontend && npm ci && npx vite build

build-server:
	go build -o headfwd-sidecar .

dev:
	@./dev.sh

clean:
	rm -rf headfwd-sidecar portal/frontend/dist portal/frontend/node_modules
