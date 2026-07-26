package cmd

// Version is set at build time via -ldflags "-X github.com/LinPr/lazys3/cmd.Version=...".
// Falls back to "dev" when built without the flag (e.g. go run .).
var Version = "dev"
