package chrome

import "github.com/eleonorayaya/utena/internal/webext"

var Browser = webext.Browser{
	Name:         "chrome",
	BundleID:     "com.google.Chrome",
	OriginScheme: "chrome-extension://",
	UpdateHint:   "reload it in chrome://extensions",
	TitleSuffix:  " - Google Chrome",
}

func NewDriver() *webext.Driver {
	return webext.NewDriver(Browser)
}
