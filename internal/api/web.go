package api

import "embed"

//go:embed static/*
var webFS embed.FS
