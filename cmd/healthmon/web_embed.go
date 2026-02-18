//go:build !dev

package main

import "embed"

//go:embed web/dist/* web/dist/assets/*
var webDist embed.FS

const hasWebDist = true
