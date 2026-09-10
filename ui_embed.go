package pyro

import _ "embed"

// ServiceUnit is the systemd unit pyro setup installs. Go's embed directive
// cannot cross a package directory boundary, so this lives at the module
// root next to deploy/, not in cmd/pyro where it's consumed.
//
//go:embed deploy/pyro.service
var ServiceUnit []byte
