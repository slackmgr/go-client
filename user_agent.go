package client

import (
	"runtime/debug"
)

// modulePath is the module path of this package, used to find its version in the build info.
const modulePath = "github.com/slackmgr/go-client"

// userAgentProduct is the product name of the default User-Agent header.
const userAgentProduct = "slack-manager-go-client"

// defaultUserAgent returns the default User-Agent header, e.g. "slack-manager-go-client/v0.5.0", with the
// module version from the build info, or "dev" when it is unknown (e.g. in a local build of this module).
func defaultUserAgent() string {
	info, _ := debug.ReadBuildInfo()
	return userAgentProduct + "/" + moduleVersion(info)
}

// moduleVersion returns the version of this module in the build info, or "dev" if it is unknown.
func moduleVersion(info *debug.BuildInfo) string {
	if info == nil {
		return "dev"
	}

	if info.Main.Path == modulePath {
		return knownVersion(info.Main.Version)
	}

	for _, dep := range info.Deps {
		if dep.Path != modulePath {
			continue
		}

		if dep.Replace != nil {
			return knownVersion(dep.Replace.Version)
		}

		return knownVersion(dep.Version)
	}

	return "dev"
}

// knownVersion returns v, or "dev" if it is empty or "(devel)".
func knownVersion(v string) string {
	if v == "" || v == "(devel)" {
		return "dev"
	}

	return v
}
