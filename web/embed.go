package web

import "embed"

// Files contains the administration interface bundled into the Go binary.
//
//go:embed static/*
var Files embed.FS
