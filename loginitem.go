package main

// loginItem is what the settings page needs to draw the "Open at login" control:
// whether this build can offer it at all, whether it is on, and, when macOS wants
// the person to confirm it themselves, where to go and do that.
type loginItem struct {
	// Supported is false for every build but the macOS app, so the page can leave
	// the control out rather than show one that cannot work.
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`

	// NeedsApproval means macOS has the registration but someone turned it off in
	// System Settings. Registering again will not override that, by design, so the
	// only honest thing to do is say where the switch is.
	NeedsApproval bool `json:"needs_approval"`
}
