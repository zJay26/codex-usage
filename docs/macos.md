# macOS installation and updates

Choose `darwin-arm64` for Apple Silicon or `darwin-amd64` for Intel. Install as the logged-in user; no `sudo` is needed.

```bash
set -e
arch=arm64 # use amd64 on Intel
curl -fL "https://github.com/zJay26/codex-usage/releases/latest/download/codex-usage-darwin-$arch" -o codex-usage
curl -fL https://github.com/zJay26/codex-usage/releases/latest/download/SHA256SUMS -o SHA256SUMS
expected=$(awk -v name="codex-usage-darwin-$arch" '$2 == name { print $1 }' SHA256SUMS)
test -n "$expected" && test "$(shasum -a 256 codex-usage | awk '{print $1}')" = "$expected"
chmod +x codex-usage
./codex-usage install
```

Use a normal macOS graphical login session for installation. The service is a per-user LaunchAgent, registered with `launchctl bootstrap gui/<uid>`, and runs while that user is logged in. In a headless SSH session, use `./codex-usage serve` or install from a graphical login session. The installer reports launchctl errors instead of claiming that a service started.

Default locations:

| Content | Location |
|---|---|
| State and database | `~/Library/Application Support/codex-usage/` |
| Installed executable | `~/Library/Application Support/codex-usage/bin/codex-usage` |
| Login agent | `~/Library/LaunchAgents/com.zjay.codex-usage.plist` |
| Codex source | `~/.codex` or `CODEX_HOME` |

`CODEX_USAGE_HOME` selects a dedicated alternative state directory. The LaunchAgent saves this path and explicit Codex Home settings. The installed executable opens the browser with macOS `open`.

```bash
"$HOME/Library/Application Support/codex-usage/bin/codex-usage" uninstall
```

Uninstall unloads the agent and removes the installed executable, retaining the database. `uninstall --purge` additionally deletes only a validated, marked state directory. Optional in-app updates use a separate launchd helper so stopping the main agent does not kill the updater. Downloads are checked against SHA256SUMS; program and data backups are retained, with startup-failure rollback as on Windows/Linux.

These command-line binaries do not have an Apple Developer ID signature or Apple notarization. If macOS blocks a browser-downloaded binary, verify the checksum and follow [Apple's instructions for opening a trusted app](https://support.apple.com/en-us/102445). Do not disable Gatekeeper globally. Native CI covers macOS Intel and Apple Silicon; this is not a notarized `.app`/`.pkg` distribution.

The service follows [Apple's LaunchAgent model](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html). macOS includes `launchctl` for loading and unloading these jobs, as described in [Apple's Terminal guide](https://support.apple.com/guide/terminal/script-management-with-launchd-apdc6c1077b-5d5d-4d35-9c19-60f2397b2369/mac).
