// Package web embeds the Travel TV launcher and phone-remote pages. Ansible
// only renders the data they display (see internal/tv/config.go); the markup,
// styles and scripts ship in the image.
package web

import "embed"

// FS holds the page templates and static assets.
//
//go:embed *.html *.css *.js
var FS embed.FS
