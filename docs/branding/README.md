# Codex Usage · by zJay

The selected D8 mark combines four uninterrupted teal usage bars with a copper Z:
a thicker rising trend line and thinner horizontal guides. The original product
name stays **Codex Usage**; **by zJay** is the author signature.

- `d8-reference.png` is the selected image-generation concept, retained unchanged.
- `generation.json` records the original built-in image-generation prompt.
- `../../internal/web/static/icon.svg` is the production vector interpretation.
  It keeps the D8 geometry and uses the frontend teal/copper palette, with clean
  rounded edges and restrained shading for small sizes.
- `icon.png` is a 512px preview of that vector asset.
- The 32px browser fallback and 180px touch icon are exported from the same SVG.

The dashboard uses the SVG at 48px on desktop and 36–40px on small screens. The
SVG favicon scales to the browser's tab size. Both light and dark themes retain
the selected fog-white tile; bars have no white or mint top segments.

After changing the SVG, run `npm run build:icons` with Playwright Chromium
installed, or set `PLAYWRIGHT_CHANNEL=msedge` to use installed Edge. Generated PNGs
are committed so normal Go builds do not require a browser. The local server
versions icon URLs by content; the static demo rewrites them to relative paths.

The Chinese and English repository READMEs use `icon.png` at 112px, followed by
**Codex Usage** and a linked **by zJay** signature. The repository slug, executable
name and installation paths remain `codex-usage`.

Run `npm run capture:media` after visual changes to refresh the bilingual tours,
desktop/mobile screenshots and `../media/social-preview.png`. The social card
uses this same SVG; GitHub's repository social-preview setting is managed
separately from the committed image.
