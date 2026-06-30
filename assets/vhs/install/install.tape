# VHS tape: `brew install illegalstudio/tap/lazyagent`
# Render with:  vhs install.tape   ->   install.gif

Output install.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 22
Set Width 1400
Set Height 460
Set Padding 30
Set Margin 20
Set BorderRadius 12
Set Theme "Catppuccin Mocha"
Set TypingSpeed 65ms
Set PlaybackSpeed 1.0

# --- Setup (not recorded): clean prompt + tidy `brew` output ---
Hide
Type "source demo.bashrc" Enter
Type "clear" Enter
Show

# --- Recorded ---
Sleep 600ms
Type "brew install illegalstudio/tap/lazyagent"
Sleep 700ms
Enter
Sleep 4s
Sleep 2500ms
