// Package web holds the interface page, shared by the Windows and Android
// apps.
package web

import _ "embed"

// Index is the whole interface: one HTML page with its CSS and JavaScript.
//
//go:embed index.html
var Index []byte
