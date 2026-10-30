# Vendored browser libraries

The files in this directory are served verbatim by the Go server (`/ui/assets/vendor/**`), so the
pages do not fetch any script, stylesheet or font from a CDN and keep working on a LAN without
internet access. Do not edit them: upgrade by replacing the file, updating the tags in the pages
and the instructions in [docs/FRONTEND.md](../../../docs/FRONTEND.md).

| File | Project | Version | License |
| ---- | ------- | ------- | ------- |
| `alpine-3.17.5.min.js` | [Alpine.js](https://github.com/alpinejs/alpine) | 3.17.5 | MIT |
| `alpine-collapse-3.17.5.min.js` | [Alpine collapse plugin](https://github.com/alpinejs/alpine) | 3.17.5 | MIT |
| `htmx-1.9.10.min.js` | [htmx](https://github.com/bigskysoftware/htmx) | 1.9.10 | BSD 2-Clause |
| `tailwind-2.2.19.min.css` | [Tailwind CSS](https://github.com/tailwindlabs/tailwindcss) | 2.2.19 | MIT |

License texts:

- Alpine.js: <https://github.com/alpinejs/alpine/blob/main/LICENSE.md>
- htmx: <https://github.com/bigskysoftware/htmx/blob/master/LICENSE>
- Tailwind CSS: <https://github.com/tailwindlabs/tailwindcss/blob/v2.2.19/LICENSE>
