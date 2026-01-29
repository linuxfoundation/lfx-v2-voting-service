// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// IDMapper defines the interface for mapping between LFX v1 and v2 identifiers
type IDMapper interface {
	// MapProjectV2ToV1 maps a v2 project UID to v1 project SFID
	MapProjectV2ToV1(ctx context.Context, v2UID string) (string, error)

	// MapProjectV1ToV2 maps a v1 project SFID to v2 project UID
	MapProjectV1ToV2(ctx context.Context, v1SFID string) (string, error)

	// MapCommitteeV2ToV1 maps a v2 committee UID to v1 committee identifiers (project_sfid:committee_sfid)
	MapCommitteeV2ToV1(ctx context.Context, v2UID string) (string, error)

	// MapCommitteeV1ToV2 maps a v1 committee SFID to v2 committee UID
	MapCommitteeV1ToV2(ctx context.Context, v1SFID string) (string, error)
}
