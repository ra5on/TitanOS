package proto

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// PolicyPath is where titand bind-mounts the Chromium policy read-only
// into the viewer container. The file is written by titand on the host
// (root, 0444); the agent refuses to start Chromium unless it exists, is
// not writable by the agent and locks the browser to the app origin.
const PolicyPath = "/etc/chromium/policies/managed/titanos.json"

// BlockedURLs are always in URLBlocklist: everything ("*") and, listed
// explicitly, the local and browser-internal schemes. data:, blob: and
// ws(s): are not listed: "*" already blocks navigations to them and the
// app's own WebSockets and blob downloads must keep working.
var BlockedURLs = []string{
	"*",
	"file://*",
	"chrome://*",
	"chrome-untrusted://*",
	"chrome-extension://*",
	"devtools://*",
	"view-source:*",
	"javascript://*",
}

// AppOrigin validates an app address (VIEWER_URL) and returns its origin
// scheme://host[:port].
func AppOrigin(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("app URL missing")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("app URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("app URL must be http or https")
	}
	if u.Host == "" || u.User != nil || u.Opaque != "" || strings.ContainsAny(u.Host, " *\"\\,") {
		return "", errors.New("app URL host invalid")
	}
	return u.Scheme + "://" + u.Host, nil
}

// ChromiumPolicy returns the managed policy JSON for the viewer browser:
// only the app origin (and about:blank) may be opened, no developer
// tools, extensions, password manager, sync, history, printing or
// dangerous downloads. The file dialog stays allowed for uploads.
func ChromiumPolicy(appOrigin string) ([]byte, error) {
	origin, err := AppOrigin(appOrigin)
	if err != nil {
		return nil, err
	}
	if origin != appOrigin {
		return nil, fmt.Errorf("not an origin: %q (want %q)", appOrigin, origin)
	}
	p := map[string]any{
		"URLBlocklist":                        BlockedURLs,
		"URLAllowlist":                        []string{origin, "about:blank"},
		"DeveloperToolsAvailability":          2,
		"DownloadDirectory":                   "/profile/Downloads",
		"PromptForDownloadLocation":           false,
		"DownloadRestrictions":                1, // block dangerous file types
		"AllowFileSelectionDialogs":           true,
		"PasswordManagerEnabled":              false,
		"PasswordLeakDetectionEnabled":        false,
		"SavingBrowserHistoryDisabled":        true,
		"IncognitoModeAvailability":           1,
		"SyncDisabled":                        true,
		"BrowserSignin":                       0,
		"BrowserAddPersonEnabled":             false,
		"BrowserGuestModeEnabled":             false,
		"DefaultBrowserSettingEnabled":        false,
		"TranslateEnabled":                    false,
		"ExtensionInstallBlocklist":           []string{"*"},
		"ExtensionAllowedTypes":               []string{},
		"BlockExternalExtensions":             true,
		"MetricsReportingEnabled":             false,
		"SearchSuggestEnabled":                false,
		"SafeBrowsingProtectionLevel":         0,
		"NetworkPredictionOptions":            2,
		"DnsOverHttpsMode":                    "off",
		"BackgroundModeEnabled":               false,
		"PrintingEnabled":                     false,
		"DefaultNotificationsSetting":         2,
		"DefaultGeolocationSetting":           2,
		"DefaultWebBluetoothGuardSetting":     2,
		"DefaultWebUsbGuardSetting":           2,
		"DefaultSerialGuardSetting":           2,
		"DefaultWebHidGuardSetting":           2,
		"DefaultFileSystemWriteGuardSetting":  2,
		"DefaultLocalFontsSetting":            2,
		"DefaultClipboardSetting":             1,
		"AudioCaptureAllowed":                 false,
		"VideoCaptureAllowed":                 false,
		"ScreenCaptureAllowed":                false,
		"AutofillCreditCardEnabled":           false,
		"AutofillAddressEnabled":              false,
		"PaymentMethodQueryEnabled":           false,
		"PromotionalTabsEnabled":              false,
		"PrivacySandboxPromptEnabled":         false,
		"PrivacySandboxAdTopicsEnabled":       false,
		"PrivacySandboxSiteEnabledAdsEnabled": false,
		"PrivacySandboxAdMeasurementEnabled":  false,
		"CloudReportingEnabled":               false,
		"EnableMediaRouter":                   false,
		"MediaRouterCastAllowAllIPs":          false,
		"HardwareAccelerationModeEnabled":     true,
		"BrowserLabsEnabled":                  false,
		"FeedbackSurveysEnabled":              false,
		"UserFeedbackAllowed":                 false,
		"GenAiDefaultSettings":                2,
		"TaskManagerEndProcessEnabled":        false,
		"DeveloperToolsDisabled":              true,
		"RemoteDebuggingAllowed":              false,
		"LensOverlaySettings":                 1,
		"ShoppingListEnabled":                 false,
		"SharedClipboardEnabled":              false,
		"QRCodeGeneratorEnabled":              false,
	}
	return json.MarshalIndent(p, "", "  ")
}

// CheckPolicy validates policy JSON as the agent reads it: the blocklist
// must hold "*" and the local/internal schemes, and the allowlist exactly
// the app origin and about:blank.
func CheckPolicy(data []byte, appOrigin string) error {
	var p struct {
		Block []string `json:"URLBlocklist"`
		Allow []string `json:"URLAllowlist"`
		Dev   *int     `json:"DeveloperToolsAvailability"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("policy: %v", err)
	}
	have := map[string]bool{}
	for _, b := range p.Block {
		have[b] = true
	}
	for _, b := range BlockedURLs {
		if !have[b] {
			return fmt.Errorf("policy: URLBlocklist lacks %q", b)
		}
	}
	if len(p.Allow) != 2 || p.Allow[0] != appOrigin || p.Allow[1] != "about:blank" {
		return fmt.Errorf("policy: URLAllowlist %q does not match the app origin %q", p.Allow, appOrigin)
	}
	if p.Dev == nil || *p.Dev != 2 {
		return errors.New("policy: developer tools not disabled")
	}
	return nil
}
