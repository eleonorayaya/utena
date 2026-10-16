package firefox

import "github.com/eleonorayaya/utena/internal/webext"

var Browser = webext.Browser{
	Name:         "firefox",
	BundleID:     "org.mozilla.firefox",
	OriginScheme: "moz-extension://",
	UpdateHint:   "install the latest signed build (task extension:firefox:sign)",
}

func NewDriver() *webext.Driver {
	return webext.NewDriver(Browser)
}
