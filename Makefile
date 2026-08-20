.PHONY: all build tui frontend bindings clean dev install deploy app

all: build

# Build with frontend (TUI + GUI support)
build: frontend
	rm -rf internal/assets/dist/*
	cp -r frontend/dist/. internal/assets/dist/
	go build -o lazyagent .

# Build TUI only (no frontend or Wails needed)
tui:
	go build -tags notray -o lazyagent .

# Build the frontend
frontend: bindings
	cd frontend && npm run build

# Generate Wails bindings
bindings:
	wails3 generate bindings -d frontend/src/bindings -ts .

# Install frontend dependencies
install:
	cd frontend && npm install

# Dev mode: rebuild frontend and run GUI app
dev: bindings
	cd frontend && npm run build
	rm -rf internal/assets/dist/*
	cp -r frontend/dist/. internal/assets/dist/
	go run . --gui

# Interactive release: propose next semver tag, then tag + push to origin.
deploy:
	@scripts/deploy.sh

# Assemble an unsigned Lazyagent.app locally (for testing the bundle)
app: build
	scripts/make-app.sh ./lazyagent dev dist-app

# Clean build artifacts
clean:
	rm -f lazyagent
	rm -rf frontend/dist internal/assets/dist/*
	rm -rf dist-app
