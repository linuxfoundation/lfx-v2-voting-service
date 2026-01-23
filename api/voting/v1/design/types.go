// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package design

import (
	. "goa.design/goa/v3/dsl" //nolint:staticcheck // Goa DSL convention requires dot imports
)

//
// Reusable Attribute Functions
//

// BearerTokenAttribute is a reusable token attribute for JWT authentication.
func BearerTokenAttribute() {
	Token("token", String, "JWT token", func() {
		Example("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...")
	})
}

// VoteIDAttribute is the DSL attribute for vote ID (UUID).
func VoteIDAttribute() {
	Attribute("vote_uid", String, "Vote UID", func() {
		Format(FormatUUID)
		Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
	})
}

// VoteNameAttribute is the DSL attribute for vote name.
func VoteNameAttribute() {
	Attribute("name", String, "Vote name", func() {
		Example("Q1 2026 Technical Steering Committee Election")
		MinLength(1)
		MaxLength(255)
	})
}

// VoteDescriptionAttribute is the DSL attribute for vote description.
func VoteDescriptionAttribute() {
	Attribute("description", String, "Vote description", func() {
		Example("Vote for the TSC members for Q1 2026")
	})
}

// EndTimeAttribute is the DSL attribute for end time.
func EndTimeAttribute() {
	Attribute("end_time", String, "End time in RFC3339 format", func() {
		Format(FormatDateTime)
		Example("2026-02-15T23:59:59Z")
	})
}

// ProjectIDAttribute is the DSL attribute for project ID.
func ProjectIDAttribute() {
	Attribute("project_uid", String, "LFX Project UID", func() {
		Example("a09P000000DsCBuIRT")
	})
}

// CommitteeIDAttribute is the DSL attribute for committee ID.
func CommitteeIDAttribute() {
	Attribute("committee_uid", String, "LFX Committee UID", func() {
		Format(FormatUUID)
		Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
	})
}

// CommitteeFiltersAttribute is the DSL attribute for committee filters.
func CommitteeFiltersAttribute() {
	Attribute("committee_filters", ArrayOf(String, func() {
		Enum("Voting Rep", "Alternate Voting Rep", "Observer", "Emeritus")
	}), "Committee voting status filters", func() {
		Example([]string{"Voting Rep", "Alternate Voting Rep"})
	})
}

// PollQuestionsAttribute is the DSL attribute for poll questions.
func PollQuestionsAttribute() {
	Attribute("poll_questions", ArrayOf(PollQuestion), "Questions for the vote")
}

// PseudoAnonymityAttribute is the DSL attribute for pseudo-anonymity.
func PseudoAnonymityAttribute() {
	Attribute("pseudo_anonymity", Boolean, "Enable pseudo-anonymity", func() {
		Default(false)
	})
}

// PollTypeAttribute is the DSL attribute for poll type.
func PollTypeAttribute() {
	Attribute("poll_type", String, "Type of poll", func() {
		Enum("generic", "condorcet_irv", "meek_stv")
		Default("generic")
	})
}

// NumWinnersAttribute is the DSL attribute for number of winners.
func NumWinnersAttribute() {
	Attribute("num_winners", Int, "Number of winners (meek_stv only)", func() {
		Minimum(2)
		Maximum(10)
		Default(2)
	})
}

// AllowAbstainAttribute is the DSL attribute for allow abstain.
func AllowAbstainAttribute() {
	Attribute("allow_abstain", Boolean, "Allow voters to abstain", func() {
		Default(false)
	})
}

// CommitteeIDsAttribute is the DSL attribute for multiple committee IDs.
func CommitteeIDsAttribute() {
	Attribute("committee_uids", ArrayOf(String), "Multiple committee UIDs", func() {
		Example([]string{"a02bdbaf-53b1-4d47-bc04-dd7e459dd308", "b03cdbaf-53b1-4d47-bc04-dd7e459dd309"})
	})
}

// PollCommentPromptsAttribute is the DSL attribute for poll comment prompts.
func PollCommentPromptsAttribute() {
	Attribute("poll_comment_prompts", ArrayOf(PollCommentPrompt), "Comment prompts for the vote")
}

// QuorumPercentageAttribute is the DSL attribute for quorum percentage.
func QuorumPercentageAttribute() {
	Attribute("quorum_percentage", Int, "Quorum percentage required", func() {
		Minimum(0)
		Maximum(100)
		Example(50)
	})
}

// WinningThresholdPercentageAttribute is the DSL attribute for winning threshold percentage.
func WinningThresholdPercentageAttribute() {
	Attribute("winning_threshold_percentage", Int, "Winning threshold percentage", func() {
		Minimum(0)
		Maximum(100)
		Example(51)
	})
}

//
// Type Definitions
//

// VoteResult represents a created vote (maps to ITX PollService)
var VoteResult = Type("VoteResult", func() {
	Description("Vote details")

	Attribute("vote_uid", String, "Vote identifier", func() {
		Format(FormatUUID)
		Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
	})

	Attribute("name", String, "Vote name")
	Attribute("description", String, "Vote description")
	Attribute("creation_time", String, "Creation time", func() {
		Format(FormatDateTime)
	})

	Attribute("last_modified_time", String, "Last modified time", func() {
		Format(FormatDateTime)
	})

	Attribute("end_time", String, "End time", func() {
		Format(FormatDateTime)
	})

	Attribute("status", String, "Vote status", func() {
		Enum("disabled", "active", "ended")
		Example("disabled")
	})

	Attribute("project_uid", String, "Project UID")
	Attribute("committee_uid", String, "Committee UID")
	Attribute("committee_name", String, "Committee name")
	Attribute("committee_type", String, "Committee type")
	Attribute("committee_voting_status", Boolean, "Committee voting status")
	Attribute("pseudo_anonymity", Boolean, "Pseudo-anonymity enabled")
	Attribute("total_voting_request_invitations", Int, "Total invitations sent")
	Attribute("num_response_received", Int, "Responses received")
	Attribute("poll_questions", ArrayOf(PollQuestion), "Vote questions")
	Attribute("allow_abstain", Boolean, "Allow abstain")

	Required("vote_uid", "name", "description", "status", "project_uid", "committee_uid")
})

// PollQuestion represents a question in a vote
var PollQuestion = Type("PollQuestion", func() {
	Description("Vote question")

	Attribute("question_id", String, "Question identifier", func() {
		Format(FormatUUID)
	})

	Attribute("prompt", String, "Question prompt", func() {
		Example("Who should be elected to the TSC?")
	})

	Attribute("type", String, "Question type", func() {
		Enum("single_choice", "multiple_choice")
		Example("single_choice")
	})

	Attribute("choices", ArrayOf(PollChoice), "Answer choices")

	Required("prompt", "type", "choices")
})

// PollChoice represents an answer choice
var PollChoice = Type("PollChoice", func() {
	Description("Answer choice")

	Attribute("choice_id", String, "Choice identifier", func() {
		Format(FormatUUID)
	})

	Attribute("choice_text", String, "Choice text", func() {
		Example("John Doe")
	})

	Required("choice_text")
})

// PollCommentPrompt represents a comment prompt in a vote
var PollCommentPrompt = Type("PollCommentPrompt", func() {
	Description("Comment prompt for collecting feedback")

	Attribute("prompt", String, "Comment prompt text", func() {
		Example("Please provide any additional feedback")
	})

	Required("prompt")
})

// BadRequestError represents a 400 Bad Request error
var BadRequestError = Type("BadRequestError", func() {
	Description("Bad request error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})

// NotFoundError represents a 404 Not Found error
var NotFoundError = Type("NotFoundError", func() {
	Description("Not found error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})

// ConflictError represents a 409 Conflict error
var ConflictError = Type("ConflictError", func() {
	Description("Conflict error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})

// InternalServerError represents a 500 Internal Server Error
var InternalServerError = Type("InternalServerError", func() {
	Description("Internal server error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})

// ServiceUnavailableError represents a 503 Service Unavailable error
var ServiceUnavailableError = Type("ServiceUnavailableError", func() {
	Description("Service unavailable error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})

// UnauthorizedError represents a 401 Unauthorized error
var UnauthorizedError = Type("UnauthorizedError", func() {
	Description("Unauthorized error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})

// ForbiddenError represents a 403 Forbidden error
var ForbiddenError = Type("ForbiddenError", func() {
	Description("Forbidden error response")
	Attribute("code", String, "HTTP status code")
	Attribute("message", String, "Error message")
	Required("code", "message")
})
