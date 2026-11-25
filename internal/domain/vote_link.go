// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

// VoteLinkClaims identifies the ballot a personalized vote link grants access to.
type VoteLinkClaims struct {
	// VoteUID is the vote (ITX poll) the ballot belongs to.
	VoteUID string
	// VoteResponseUID is the ballot (ITX vote_id) the link holder may submit or update.
	VoteResponseUID string
	// KeyID is the signing key that produced the token, for logging and rotation.
	KeyID string
}

// VoteLinkSigner signs and verifies the tokens embedded in personalized vote links.
// A valid token is the only authorization for the login-free vote response endpoints,
// so implementations must use a keyed MAC and constant-time comparison.
type VoteLinkSigner interface {
	// Sign returns a token binding the given vote and ballot.
	Sign(voteUID, voteResponseUID string) (string, error)

	// Verify checks the token's signature and returns its claims. Any malformed,
	// tampered, or unknown-key token returns an error.
	Verify(token string) (*VoteLinkClaims, error)
}
