# ike demo cast. Regenerate from the repo root with:
#   go build -ldflags "-X main.version=<version>" -o ./ike . && vhs docs/demo.tape
# Requires vhs (brew install vhs). Runs ./ike explicitly so a stale `ike` on
# PATH can never sneak into the recording.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1280
Set Height 800
Set Padding 16
Set TypingSpeed 90ms

# Launch demo mode: shows the splash, then the monitors view.
Type "./ike --demo"
Enter
Sleep 3s

# Every command at a glance — the command palette (enter runs one).
Type ":menu"
Enter
Sleep 2500ms

# Contexts: the orgs you're connected to (auth type per row); activate a
# second org with space — active rows highlight and views start merging.
Type ":ctx"
Enter
Sleep 1500ms
Down
Sleep 400ms
Space
Sleep 1800ms

# Cross-org triage: open incidents + alerting monitors, worst first.
Type ":overview"
Enter
Sleep 2500ms

# Bulk actions: mark monitors with space, then mute them all at once.
Type ":monitors"
Enter
Sleep 1500ms
Space
Sleep 400ms
Down
Space
Sleep 600ms
Type "m"
Sleep 1500ms
Right
Enter
Sleep 2s

# Datadog spend: estimated + projected, trend, anomalies.
Type ":cost"
Enter
Sleep 2500ms

# On-call: who's paged now + the escalation ladder.
Type ":oncall"
Enter
Sleep 1500ms
Enter
Sleep 2500ms
Escape
Sleep 600ms

# Security signals (Cloud SIEM) and notebooks (runbooks / postmortems).
Type ":security"
Enter
Sleep 2s
Type ":notebooks"
Enter
Sleep 1500ms
Enter
Sleep 2500ms
Escape
Sleep 600ms

# The debugging loop: logs, narrowed by a Datadog search query.
Type ":logs"
Enter
Sleep 1500ms
Type "/"
Sleep 300ms
Ctrl+U
Type "status:error"
Enter
Sleep 2s

# Watch mode: leave a view refreshing hands-off for a wall display.
Type ":overview"
Enter
Sleep 1200ms
Type ":watch"
Enter
Sleep 2500ms

# Out.
Type ":watch"
Enter
Sleep 500ms
Type "q"
Sleep 1s
