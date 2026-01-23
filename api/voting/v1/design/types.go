package design

import (
	. "goa.design/goa/v3/dsl"
)

// VoteResult represents a created vote (maps to ITX PollService)
var VoteResult = Type("VoteResult", func() {
	Description("Vote details")

	Attribute("poll_id", String, "Vote identifier (ITX poll_id)", func() {
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

	Attribute("project_id", String, "Project ID")
	Attribute("committee_id", String, "Committee ID")
	Attribute("committee_name", String, "Committee name")
	Attribute("committee_type", String, "Committee type")
	Attribute("committee_voting_status", Boolean, "Committee voting status")
	Attribute("pseudo_anonymity", Boolean, "Pseudo-anonymity enabled")
	Attribute("total_voting_request_invitations", Int, "Total invitations sent")
	Attribute("num_response_received", Int, "Responses received")
	Attribute("poll_questions", ArrayOf(PollQuestion), "Vote questions")
	Attribute("allow_abstain", Boolean, "Allow abstain")

	Required("poll_id", "name", "description", "status", "project_id", "committee_id")
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
