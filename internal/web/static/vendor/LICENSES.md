# Vendored Web Asset Licences

The `rmp web` interface embeds the following third-party assets, served only
from `/static/...` and never from a remote origin (see `SPEC/BUILD.md
§ Vendored Web Assets`). Each remains under its upstream licence.

| Asset | Location | Project | Licence |
|-------|----------|---------|---------|
| Tabler (CSS framework + JS) | `tabler/tabler.min.css`, `tabler/tabler.min.js` | Tabler | MIT |
| Tabler Icons (webfont + CSS) | `tabler-icons/tabler-icons.min.css`, `tabler-icons/fonts/*` | Tabler Icons | MIT |
| Inter (variable webfont) | `inter/inter.css`, `inter/files/inter-latin-wght-normal.woff2` | Inter (Rasmus Andersson) | SIL Open Font License 1.1 |
| D3.js (graph library) | `d3/d3.min.js` | D3 (Mike Bostock) | ISC |
| d3-sankey (Sankey layout plugin) | `d3/d3-sankey.min.js` | d3-sankey (Mike Bostock) | ISC |

These attributions are recorded as good practice for vendored assets; the full
upstream licence texts ship with each project's distribution.
