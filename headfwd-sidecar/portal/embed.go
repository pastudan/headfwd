package portal

import "embed"

//go:embed all:frontend/dist
var frontendDist embed.FS
