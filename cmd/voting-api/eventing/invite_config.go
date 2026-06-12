// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

// InviteFeatureConfig holds LFID invite feature settings shared by the event
// processor (outbound invites) and the invite_accepted subscriber.
type InviteFeatureConfig struct {
	// Enabled reflects INVITES_ENABLED — starts the invite_accepted subscriber and
	// wires outbound invite infrastructure when true.
	Enabled bool
	// SelfServeBaseURL is the LFX self-serve app URL embedded in invite emails as
	// return_url. When empty, outbound invite sending is disabled via inviteEnabled()
	// but the invite_accepted subscriber may still run.
	SelfServeBaseURL string
}
