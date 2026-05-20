package defaults

import _ "embed"

// Tags is the default content written to tags.json on first run.
//
//go:embed tags.json
var Tags []byte

// Profiles is the default content written to profiles.json on first run.
//
//go:embed profiles.json
var Profiles []byte
