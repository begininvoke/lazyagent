# Loaded by install.tape via VHS to render a clean, accurate
# recreation of `brew install illegalstudio/tap/lazyagent`.

# Minimal, branded prompt.
PS1='\[\e[1;35m\]❯\[\e[0m\] '

# Override `brew` so the recording shows a tidy install instead of the
# noisy local environment. Output mirrors the real cask install for the
# binary cask (download -> progress -> link binary -> success).
brew() {
  if [ "$1" = "install" ]; then
    local v="0.12.2"
    local url="https://github.com/illegalstudio/lazyagent/releases/download/v${v}/lazyagent_${v}_darwin_arm64.zip"

    echo "==> Downloading ${url}"
    sleep 0.5
    echo "==> Downloading from https://objects.githubusercontent.com/github-production-release-asset"
    sleep 0.4

    # Animated progress bar (single line, like real brew).
    local total=40 filled
    for p in 8 23 41 58 74 89 100; do
      filled=$(( p * total / 100 ))
      printf '\r'
      printf '#%.0s' $(seq 1 "$filled")
      printf '%*s' $(( total - filled )) ''
      printf ' %3d%%' "$p"
      sleep 0.12
    done
    printf '\n'
    sleep 0.3

    echo "==> Installing Cask lazyagent"
    sleep 0.4
    echo "==> Linking Binary 'lazyagent' to '/opt/homebrew/bin/lazyagent'"
    sleep 0.5
    echo "🍺  lazyagent was successfully installed!"
    return 0
  fi
  command brew "$@"
}
