# README media

The Chinese and English tours use the actual Dashboard with the in-memory synthetic API in `scripts/demo-api.js`. The default fixture has no data-quality warnings and uses fully priced GPT-6 Sol, Astra, and Luna records. No warning UI is hidden or removed. Append `&scenario=diagnostics` to the demo URL to exercise the separate incomplete-data fixture and its warning dialog.

## Reproduce

Install the repository's Node dependencies, Playwright Chromium, and FFmpeg / ffprobe on `PATH`, then run:

```sh
npm ci
npx playwright install chromium
npm run capture:media
```

`PLAYWRIGHT_CHANNEL` optionally selects an installed browser. `MEDIA_LOCALE=zh-CN` or `MEDIA_LOCALE=en` records one language when iterating. Capture launches its own loopback static server on a free port and stops it on completion; it never opens the local usage database.

Outputs:

- `codex-usage-demo.gif` and `codex-usage-demo-en.gif`: complete 1120 × 778 tours, with up to 20 fps motion and held static frames, for each README.
- `codex-usage-demo-zh.mp4` and `codex-usage-demo-en.mp4`: 1440 × 1000, 25 fps video alternatives.
- `social-preview.png` and `../images/dashboard*.png`: refreshed social, desktop, and mobile images.
- `../../dist/media-review/<locale>/`: ignored chapter screenshots and `chapters.json` with actual timing and recording checks.

The tour covers overview ranges, minute-precision queries, hourly drill-down, daily calendar, projects, expandable task trees, Session search, Fast filtering, JSON / CSV export, model prices, themes, and languages. Pointer movement takes 160–340 ms with easing; most results remain visible for 350–850 ms. There is no global time compression and no truncated tail. Capture fixes the clock and timezone for reproducible dates, waits for pricing before recording, and checks the entire tour for visible warning/error banners, JavaScript errors, and external requests.

The export dialog is demonstrated without downloading files. Software installation and updates are documented in the project README; the synthetic demo does not install software.
