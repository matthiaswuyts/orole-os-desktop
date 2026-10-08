//go:build windows

package platform

import "golang.org/x/sys/windows/registry"

const webView2Client = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// WebView2Version returns the installed Evergreen WebView2 runtime version, or "" if not found.
func WebView2Version() string {
	locations := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + webView2Client},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + webView2Client},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + webView2Client},
	}
	for _, l := range locations {
		k, err := registry.OpenKey(l.root, l.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("pv")
		k.Close()
		if err == nil && v != "" && v != "0.0.0.0" {
			return v
		}
	}
	return ""
}
