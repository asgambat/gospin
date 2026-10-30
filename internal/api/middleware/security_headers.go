package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultContentSecurityPolicy is the policy the shipped UI needs. The vendored
// Alpine, htmx and Tailwind assets come from this origin; the three CDN origins
// keep working a deployment that still loads them from a CDN, and jsDelivr is
// also where the homepage resolves its dashboard icons from. Alpine and htmx
// evaluate expressions with the Function constructor, which needs
// 'unsafe-eval'; Tailwind and Alpine inject style rules and style attributes,
// which need 'unsafe-inline' for styles only. Scripts stay restricted to the
// listed origins, so an injected remote script is still refused.
//
// Images may come from any http(s) origin because homepage.yaml accepts both a
// dashboard-icon name and a full URL, and a homelab icon often lives on a LAN
// device served over plain HTTP. An image cannot execute, so the allowance
// costs little.
const DefaultContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-eval' https://cdn.jsdelivr.net https://unpkg.com https://cdn.tailwindcss.com; " +
	"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://unpkg.com https://cdn.tailwindcss.com; " +
	"img-src 'self' data: https: http:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"media-src 'self'; " +
	"manifest-src 'self'; " +
	"worker-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// WaitingPageContentSecurityPolicy fits the boot page the waiting server renders
// while a container starts. That page is one self-contained file: its style and
// its redirect loop are inline, so the policy has to allow inline script and
// style, and it loads nothing else — no CDN, no font and no image — so every
// other source stays closed. It is a separate policy because the dashboard one
// refuses inline script on purpose.
const WaitingPageContentSecurityPolicy = "default-src 'none'; " +
	"script-src 'unsafe-inline'; " +
	"style-src 'unsafe-inline'; " +
	"connect-src 'self'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// SealedPageContentSecurityPolicy is the policy of the page that holds the
// decryption key of a sealed board (/ui/sealed/<board>). It has no CDN origin at
// all: a third-party script on that page could read location.hash and the
// decrypted text, which would make the whole feature theatre. Alpine needs
// 'unsafe-eval' to evaluate its expressions and injects style rules and
// attributes, so script and style stay as narrow as that allows; every other
// source is closed, including images from another host.
const SealedPageContentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"manifest-src 'none'; " +
	"worker-src 'none'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// defaultPermissionsPolicy turns off the browser features the dashboard never
// uses, so an injected script cannot ask for them either.
const defaultPermissionsPolicy = "camera=(), geolocation=(), microphone=(), payment=(), usb=()"

// SecurityHeaderOptions configures the headers added to every response. An
// empty ContentSecurityPolicy means DefaultContentSecurityPolicy, and an HSTS
// max age of zero leaves Strict-Transport-Security out, which is what a
// plain-HTTP homelab instance needs.
type SecurityHeaderOptions struct {
	ContentSecurityPolicy string
	HSTSMaxAge            time.Duration
	Enabled               bool
}

// SecurityHeaders returns a middleware that adds the security headers to every
// response, including the static UI and the JSON API. A disabled middleware
// adds nothing at all, so an operator can turn the whole set off with one key.
// The caller picks the policy, because the dashboard and the waiting page need
// different ones.
func SecurityHeaders(opts SecurityHeaderOptions) gin.HandlerFunc {
	policy := opts.ContentSecurityPolicy
	if policy == "" {
		policy = DefaultContentSecurityPolicy
	}

	hsts := ""
	if opts.HSTSMaxAge > 0 {
		hsts = fmt.Sprintf("max-age=%d", int64(opts.HSTSMaxAge.Seconds()))
	}

	return func(c *gin.Context) {
		if !opts.Enabled {
			c.Next()
			return
		}

		c.Header("Content-Security-Policy", policy)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Permissions-Policy", defaultPermissionsPolicy)
		if hsts != "" {
			c.Header("Strict-Transport-Security", hsts)
		}

		c.Next()
	}
}
