package route

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/logger"
)

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// firstExistingDir returns the first candidate that is an existing directory,
// or "" when none is. Kept separate from resolveUIDir so the preference order
// can be tested without relying on the layout around the test binary.
func firstExistingDir(candidates ...string) string {
	for _, candidate := range candidates {
		if isDir(candidate) {
			return candidate
		}
	}
	return ""
}

// resolveUIDir returns the directory holding the UI assets. It prefers ./ui
// (the layout of the container image and of a checkout) and falls back to a
// "ui" directory next to the executable, so a binary started from another
// working directory still serves the pages and real cache-busting hashes
// instead of silently falling back to the "dev" placeholder.
//
// Resolved on every call rather than cached: the value depends on the working
// directory, which the process (or a test) can change after startup, and a
// stale cache would pin every later read to the wrong directory.
func resolveUIDir() string {
	if dir := firstExistingDir("./ui"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		if dir := firstExistingDir(filepath.Join(filepath.Dir(exe), "ui")); dir != "" {
			return dir
		}
	}
	// Debug level: the fallback mirrors the historical behaviour, and the
	// unreadable file paths are already reported by fileHash.
	logger.WithComponent("ui_router").Debugf("UI directory not found at ./ui or next to the executable, using ./ui")
	return "./ui"
}

// uiPath joins name to the resolved UI directory.
func uiPath(name string) string {
	return filepath.Join(resolveUIDir(), name)
}

// assetVersion returns the content hash used as the ?v= cache-busting value
// for a bundle. Computed per request: the files are small, and a cached value
// would go stale if the UI directory changes under a running process.
func assetVersion(name string) string {
	return fileHash(uiPath(name))
}

// fileHash returns the first 8 hex characters of the SHA-256 hash of the
// file at path, or "dev" if the file cannot be read.
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.WithComponent("ui_router").Warnf("cannot read %s for cache-busting hash: %v", path, err)
		return "dev"
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:4])
}

// cacheBustTag appends the content hash of an asset to the attribute that
// references it, turning href="/ui/assets/icon.svg" into
// href="/ui/assets/icon.svg?v=<hash>". The assets are served with a one-year
// cache lifetime, so the hash is what makes a deployment pick up a new file
// even when the service worker cache is still warm. The files are small, so
// hashing them on every page request keeps a deploy live without a restart.
func cacheBustTag(html, attr, asset string) string {
	target := attr + `="/ui/` + asset + `"`
	busted := attr + `="/ui/` + asset + "?v=" + assetVersion(asset) + `"`
	return strings.Replace(html, target, busted, 1)
}

// cacheBustTags applies cacheBustTag to every asset of one attribute, so a page
// with several bundles and vendored libraries stays readable.
func cacheBustTags(html, attr string, assets ...string) string {
	for _, asset := range assets {
		html = cacheBustTag(html, attr, asset)
	}
	return html
}

// Vendored browser libraries. The pages load them from ui/assets/vendor instead
// of a CDN, so the UI keeps working on a LAN without internet access: nothing in
// the shell of the pages is fetched from a third party. The version is part of
// the file name, and an upgrade means renaming the file, updating the tags in
// the pages and the precache list in ui/service-worker.js.
const (
	vendorAlpineJS       = "assets/vendor/alpine-3.17.5.min.js"
	vendorAlpineCollapse = "assets/vendor/alpine-collapse-3.17.5.min.js"
	vendorHtmx           = "assets/vendor/htmx-1.9.10.min.js"
	vendorTailwind       = "assets/vendor/tailwind-2.2.19.min.css"
)

// Sealed-board entry point. Both the ghostboard page and the home page carry
// the SEALED_BOARD_ENTRY marker comment; the handlers inject the matching link
// only when ghostboard.sealed_enabled, so a disabled feature leaves no dead
// link and no marker in the served markup. The link only navigates to
// /ui/sealed: the page that owns the key runs under its own no-CDN CSP
// (ADR-0010), and no key handling happens on the CDN-policy pages.
const (
	sealedBoardEntryMarker = "<!-- SEALED_BOARD_ENTRY -->"

	ghostboardSealedEntry = `<p class="text-xs gb-muted mt-2">` +
		`<a href="/ui/sealed" class="underline">` +
		`🔐 Create a sealed board: only whoever holds the link can read it</a></p>`

	homeSealedEntry = `<a href="/ui/sealed" class="underline cursor-pointer" ` +
		`style="color: var(--subtle-color);" ` +
		`title="Create a sealed ghostboard: only whoever holds the link can read it">` +
		`🔐 Sealed board</a>`
)

// GhostboardPageConfig carries the limits the ghostboard page needs to enforce
// them before a request: the board size for the .txt picker, the attachment size
// for the upload panel and, for the sealed page, how much plaintext fits in one
// envelope.
type GhostboardPageConfig struct {
	MaxTextBytes            int
	MaxFileBytes            int64
	SealedMaxPlaintextBytes int
	SealedEnabled           bool
}

// serveGhostboardPage serves the ghostboard page with the script and icon tags
// rewritten to carry the content hash of their assets. The configured limits
// are injected as data attributes, so the page can reject an oversized file
// before reading it into memory.
func serveGhostboardPage(cfg GhostboardPageConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")
		html, err := os.ReadFile(uiPath("ghostboard.html"))
		if err != nil {
			logger.WithComponent("ui_router").Errorf("cannot read ghostboard page: %v", err)
			c.Status(http.StatusInternalServerError)
			return
		}

		rewritten := string(html)
		rewritten = cacheBustTags(rewritten, "src", "assets/ghostboard.js", "assets/ghostboard-theme.js", vendorAlpineJS)
		rewritten = cacheBustTags(rewritten, "href",
			vendorTailwind,
			"assets/ghostboard-icon.svg",
			"assets/ghostboard-icon-32.png",
			"assets/ghostboard-icon-180.png",
		)
		rewritten = strings.Replace(rewritten, `data-max-bytes=""`,
			fmt.Sprintf(`data-max-bytes="%d"`, cfg.MaxTextBytes), 1)
		rewritten = strings.Replace(rewritten, `data-max-file-bytes=""`,
			fmt.Sprintf(`data-max-file-bytes="%d"`, cfg.MaxFileBytes), 1)
		// The entry point is injected only when sealed boards are enabled, so a
		// disabled feature leaves neither a dead link nor the marker comment.
		rewritten = strings.Replace(rewritten, sealedBoardEntryMarker, ghostboardSealedEntry, 1)
		if !cfg.SealedEnabled {
			rewritten = strings.ReplaceAll(rewritten, ghostboardSealedEntry, "")
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rewritten))
	}
}

// serveSealedPage serves the page of a sealed board. It carries its own
// Content-Security-Policy, because that page holds the decryption key in
// location.hash and must load nothing from another origin, and it is never
// cached: the ciphertext it shows is a one-time envelope on a burn board.
func serveSealedPage(cfg GhostboardPageConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.SealedEnabled {
			c.JSON(http.StatusNotFound, gin.H{"error": "sealed boards are disabled"})
			return
		}

		c.Header("Content-Security-Policy", middleware.SealedPageContentSecurityPolicy)
		c.Header("Cache-Control", "no-store")

		html, err := os.ReadFile(uiPath("sealed.html"))
		if err != nil {
			logger.WithComponent("ui_router").Errorf("cannot read sealed page: %v", err)
			c.Status(http.StatusInternalServerError)
			return
		}

		rewritten := string(html)
		rewritten = cacheBustTags(rewritten, "src", "assets/sealed.js", vendorAlpineJS)
		rewritten = cacheBustTags(rewritten, "href", vendorTailwind, "assets/ghostboard-icon.svg")
		rewritten = strings.Replace(rewritten, `data-max-plaintext-bytes=""`,
			fmt.Sprintf(`data-max-plaintext-bytes="%d"`, cfg.SealedMaxPlaintextBytes), 1)

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rewritten))
	}
}

// NewUIRouter sets up routes to serve the UI static files under /ui.
// It serves index.html for the root and any sub-paths (SPA routing).
func NewUIRouter(r *gin.Engine, ghostboardPage GhostboardPageConfig) {
	// Serve static assets (JS, CSS, images) with long-term caching.
	// Manifests are excluded from long-term caching so icon/scope changes
	// are picked up immediately by browsers.
	assetsGroup := r.Group("/ui/assets")
	assetsGroup.Use(func(c *gin.Context) {
		if strings.Contains(c.Request.URL.Path, "manifest") {
			c.Header("Content-Type", "application/manifest+json")
			c.Header("Cache-Control", "no-cache, must-revalidate")
		} else {
			c.Header("Cache-Control", "public, max-age=31536000")
		}
		c.Next()
	})
	assetsGroup.Static("/", uiPath("assets"))

	// Serve favicon
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Content-Type", "image/x-icon")
		c.Header("Cache-Control", "public, max-age=86400")
		c.File(uiPath("assets/vite.ico"))
	})

	// Redirect root to /ui
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui")
	})

	// Serve service worker from /ui/ so its default scope covers all /ui/* pages.
	// Service-Worker-Allowed header allows explicit scope override if needed.
	// Browsers cache SW for at most 24h per spec; we use 1h for rapid iteration.
	// MUST be registered before /ui route to avoid Gin routing conflicts.
	r.GET("/ui/service-worker.js", func(c *gin.Context) {
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.Header("Service-Worker-Allowed", "/")
		c.Header("Cache-Control", "public, max-age=3600")
		c.File(uiPath("service-worker.js"))
	})

	// Serve the shared service-worker registration script. It sits next to
	// service-worker.js rather than under /ui/assets so it gets a short cache
	// lifetime without colliding with the static asset wildcard.
	r.GET("/ui/sw-register.js", func(c *gin.Context) {
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.Header("Cache-Control", "public, max-age=3600")
		c.File(uiPath("sw-register.js"))
	})

	// Serve index.html for the /ui root (no caching for HTML).
	// The /ui/assets/app.js <script> tag is rewritten on the fly to include a
	// ?v=<hash> cache-busting query parameter so that deployments are picked
	// up immediately even when the service worker hasn't refreshed yet.
	r.GET("/ui", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")

		html, err := os.ReadFile(uiPath("index.html"))
		if err != nil {
			c.File(uiPath("index.html"))
			return
		}

		// Rewrite the bundle and library tags to carry their content hash, so a
		// page always pairs with the assets of the running binary.
		rewritten := cacheBustTags(string(html), "src",
			"assets/app.js",
			vendorAlpineJS,
			vendorHtmx,
		)
		rewritten = cacheBustTags(rewritten, "href", vendorTailwind)

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rewritten))
	})

	// Serve home.html for the dedicated homepage route (no caching for HTML).
	// The /ui/home.js <script> tag is rewritten on the fly to include a
	// ?v=<hash> cache-busting query parameter so that deployments are picked
	// up immediately even when the service worker hasn't refreshed yet.
	r.GET("/ui/home", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")

		html, err := os.ReadFile(uiPath("home.html"))
		if err != nil {
			c.File(uiPath("home.html"))
			return
		}

		// Rewrite the bundle and library tags to carry their content hash, so a
		// page always pairs with the assets of the running binary. The collapse
		// plugin must stay before Alpine core, which is why the order of the
		// tags in the page is not changed here.
		rewritten := cacheBustTags(string(html), "src",
			"home.js",
			vendorAlpineCollapse,
			vendorAlpineJS,
			vendorHtmx,
		)
		rewritten = cacheBustTags(rewritten, "href", vendorTailwind)

		// Same deal as the ghostboard page: the entry point is injected only
		// when sealed boards are enabled, and it only navigates to /ui/sealed.
		rewritten = strings.Replace(rewritten, sealedBoardEntryMarker, homeSealedEntry, 1)
		if !ghostboardPage.SealedEnabled {
			rewritten = strings.ReplaceAll(rewritten, homeSealedEntry, "")
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(rewritten))
	})
	r.GET("/ui/home.js", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		c.File(uiPath("home.js"))
	})

	// Serve the shared text board. The board name is part of the path
	// (/ui/ghostboard/lavoro) and is resolved client-side, so every sub-path
	// returns the same page.
	servePage := serveGhostboardPage(ghostboardPage)
	r.GET("/ui/ghostboard", servePage)
	r.GET("/ui/ghostboard/*board", servePage)

	// The sealed page. /ui/sealed with no board is the create flow, and
	// /ui/sealed/<board>#k=<key> reveals an existing one. The route is always
	// registered so a disabled feature answers a 404 instead of falling through
	// to the dashboard SPA and looking like a working page.
	sealedPage := serveSealedPage(ghostboardPage)
	r.GET("/ui/sealed", sealedPage)
	r.GET("/ui/sealed/*board", sealedPage)

	// Serve index.html for any sub-path under /ui (SPA client-side routing)
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path

		// Only handle /ui/* paths, return 404 for others
		if p == "/ui" || strings.HasPrefix(p, "/ui/") {
			c.File(uiPath("index.html"))
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})
}
