// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package design

import (
	. "goa.design/goa/v3/dsl" //nolint:staticcheck // Goa DSL convention requires dot imports
)

// JWTAuth defines JWT security for the API
var JWTAuth = JWTSecurity("jwt", func() {
	Description("Heimdall JWT authorization")
	Scope("read:projects", "Read project data")
	Scope("manage:projects", "Manage projects")
	Scope("manage:voting", "Manage voting")
})

var _ = API("lfx-v2-voting-service", func() {
	Title("LFX V2 - Voting Service")
	Description("Proxy service for ITX voting system")
	Version("1.0")

	Server("voting-api", func() {
		Host("localhost", func() {
			URI("http://localhost:8080")
		})
	})
})

var _ = Service("vote", func() {
	Description("Vote service that proxies to ITX voting API")

	Security(JWTAuth)

	// Common error responses
	Error("BadRequest", BadRequestError, "Bad request")
	Error("Unauthorized", UnauthorizedError, "Unauthorized")
	Error("Forbidden", ForbiddenError, "Forbidden")
	Error("NotFound", NotFoundError, "Not found")
	Error("Conflict", ConflictError, "Conflict")
	Error("InternalServerError", InternalServerError, "Internal server error")
	Error("ServiceUnavailable", ServiceUnavailableError, "Service unavailable")

	Method("create_vote", func() {
		Description("Create a new vote (proxies to ITX POST /voting/poll)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteNameAttribute()
			VoteDescriptionAttribute()
			EndTimeAttribute()
			ProjectUIDAttribute()
			CommitteeUIDAttribute()
			CommitteeUIDsAttribute()
			CommitteeFiltersAttribute()
			PollQuestionsAttribute()
			PollCommentPromptsAttribute()
			PseudoAnonymityAttribute()
			PollTypeAttribute()
			NumWinnersAttribute()
			AllowAbstainAttribute()
			QuorumPercentageAttribute()
			WinningThresholdPercentageAttribute()

			Required("name", "description", "end_time", "project_uid", "committee_uid", "poll_questions")
		})

		Result(VoteResult)

		HTTP(func() {
			POST("/votes")

			Response(StatusCreated)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("Conflict", StatusConflict)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("get_vote", func() {
		Description("Get vote details (proxies to ITX GET /voting/poll/{poll_id})")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()

			Required("uid")
		})

		Result(VoteResult)

		HTTP(func() {
			GET("/votes/{uid}")
			Response(StatusOK)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("update_vote", func() {
		Description("Update vote (proxies to ITX PUT /voting/poll/{poll_id}). Only allowed when status is 'disabled'")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()
			VoteNameAttribute()
			VoteDescriptionAttribute()
			EndTimeAttribute()
			ProjectUIDAttribute()
			CommitteeUIDAttribute()
			CommitteeUIDsAttribute()
			CommitteeFiltersAttribute()
			PollQuestionsAttribute()
			PollCommentPromptsAttribute()
			PseudoAnonymityAttribute()
			PollTypeAttribute()
			NumWinnersAttribute()
			AllowAbstainAttribute()
			QuorumPercentageAttribute()
			WinningThresholdPercentageAttribute()

			Required("uid", "name", "description", "end_time", "poll_questions")
		})

		Result(VoteResult)

		HTTP(func() {
			PUT("/votes/{uid}")
			Response(StatusOK)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("delete_vote", func() {
		Description("Delete vote (proxies to ITX DELETE /voting/poll/{poll_id}). Only allowed when status is 'disabled'")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()

			Required("uid")
		})

		HTTP(func() {
			DELETE("/votes/{uid}")
			Response(StatusNoContent)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("extend_vote", func() {
		Description("Extend a vote's end time (proxies to ITX POST /voting/poll/{poll_id}/extend)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()
			EndTimeAttribute()

			Required("uid", "end_time")
		})

		Result(VoteResult)

		HTTP(func() {
			POST("/votes/{uid}/extend")
			Response(StatusOK)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("enable_vote", func() {
		Description("Enable a vote for voting (proxies to ITX PUT /voting/poll/{poll_id}/enable)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()

			Required("uid")
		})

		HTTP(func() {
			PUT("/votes/{uid}/enable")
			Response(StatusNoContent)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("bulk_resend_vote", func() {
		Description("Bulk resend vote email to select recipients (proxies to ITX POST /voting/poll/{poll_id}/bulk_resend)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()
			Attribute("recipient_ids", ArrayOf(String), "List of recipient IDs to resend vote email to", func() {
				Example([]string{"cba14f40-1636-11ec-9621-0242ac130002", "cba14f40-1636-11ec-9621-0242ac130003"})
			})

			Required("uid", "recipient_ids")
		})

		HTTP(func() {
			POST("/votes/{uid}/bulk_resend")
			Response(StatusNoContent)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("get_vote_results", func() {
		Description("Get vote results (proxies to ITX GET /voting/poll/{poll_id}/results)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()
			VoteIDAttribute()

			Required("uid")
		})

		Result(VoteResultsResult)

		HTTP(func() {
			GET("/votes/{uid}/results")
			Response(StatusOK)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	// Vote Response Methods (ballot submission)

	Method("create_vote_response", func() {
		Description("Submit a vote response (proxies to ITX POST /voting/vote)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()

			Attribute("vote_response_uid", String, "Vote response identifier", func() {
				Format(FormatUUID)
				Example("b03cdbaf-53b1-4d47-bc04-dd7e459dd309")
			})

			Attribute("vote_uid", String, "Vote/poll identifier this response belongs to", func() {
				Format(FormatUUID)
				Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
			})

			Attribute("user_vote_content", ArrayOf(VoteAnswerInput), "Vote answers", func() {
				Example([]any{
					map[string]any{
						"question_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
						"choice_ids":  []string{"b03cdbaf-53b1-4d47-bc04-dd7e459dd309"},
					},
				})
			})

			Attribute("abstain", Boolean, "Whether to abstain from voting", func() {
				Default(false)
			})

			Attribute("comment_responses", ArrayOf(CommentResponse), "The voter's free-text "+
				"responses to the poll's comment prompts. Optional; only meaningful if the poll "+
				"has poll_comment_prompts. Allowed regardless of whether abstain is true — an "+
				"abstaining voter skips the ballot but may still comment. Each prompt_id must "+
				"match one of the poll's comment prompts and must not repeat; each comment_text "+
				"is required (cannot be empty after trimming) and is rejected with a 400 if "+
				"longer than 5000 characters.", func() {
				Example([]any{
					map[string]any{
						"prompt_id":    "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
						"comment_text": "Because the incumbent has done great work this term.",
					},
				})
			})

			Required("vote_response_uid", "vote_uid", "abstain")
		})

		HTTP(func() {
			POST("/vote_responses")
			Response(StatusNoContent)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("get_vote_response", func() {
		Description("Get vote response details (proxies to ITX GET /voting/vote/{vote_id})")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()

			Attribute("vote_response_uid", String, "Vote response identifier", func() {
				Format(FormatUUID)
				Example("b03cdbaf-53b1-4d47-bc04-dd7e459dd309")
			})

			Required("vote_response_uid")
		})

		Result(VoteResponseResult)

		HTTP(func() {
			GET("/vote_responses/{vote_response_uid}")
			Response(StatusOK)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("update_vote_response", func() {
		Description("Update vote response (proxies to ITX PUT /voting/vote/{vote_id})")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()

			Attribute("vote_response_uid", String, "Vote response identifier", func() {
				Format(FormatUUID)
				Example("b03cdbaf-53b1-4d47-bc04-dd7e459dd309")
			})

			Attribute("user_vote_content", ArrayOf(VoteAnswerInput), "Updated vote answers", func() {
				Example([]any{
					map[string]any{
						"question_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
						"choice_ids":  []string{"b03cdbaf-53b1-4d47-bc04-dd7e459dd309"},
					},
				})
			})

			Attribute("abstain", Boolean, "Whether to abstain from voting", func() {
				Default(false)
			})

			Attribute("comment_responses", ArrayOf(CommentResponse), "The voter's free-text "+
				"responses to the poll's comment prompts. Allowed regardless of whether abstain "+
				"is true — an abstaining voter skips the ballot but may still comment. Otherwise, "+
				"omitting this field entirely preserves the vote's existing comments untouched; "+
				"including it (even as an empty array) validates and replaces them. Each "+
				"prompt_id must match one of the poll's comment prompts and must not repeat; "+
				"each comment_text is required (cannot be empty after trimming) and is "+
				"rejected with a 400 if longer than 5000 characters.", func() {
				Example([]any{
					map[string]any{
						"prompt_id":    "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
						"comment_text": "Because the incumbent has done great work this term.",
					},
				})
			})

			Required("vote_response_uid", "abstain")
		})

		HTTP(func() {
			PUT("/vote_responses/{vote_response_uid}")
			Response(StatusNoContent)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})

	Method("resend_vote_response", func() {
		Description("Resend vote email (proxies to ITX POST /voting/vote/{vote_id}/resend)")

		Security(JWTAuth, func() {
			Scope("manage:projects")
			Scope("manage:voting")
		})

		Payload(func() {
			BearerTokenAttribute()

			Attribute("vote_response_uid", String, "Vote response identifier", func() {
				Format(FormatUUID)
				Example("b03cdbaf-53b1-4d47-bc04-dd7e459dd309")
			})

			Required("vote_response_uid")
		})

		HTTP(func() {
			POST("/vote_responses/{vote_response_uid}/resend")
			Response(StatusNoContent)
			Response("BadRequest", StatusBadRequest)
			Response("Unauthorized", StatusUnauthorized)
			Response("Forbidden", StatusForbidden)
			Response("NotFound", StatusNotFound)
			Response("InternalServerError", StatusInternalServerError)
			Response("ServiceUnavailable", StatusServiceUnavailable)
		})
	})
})

// Serve OpenAPI spec files for API documentation
var _ = Service("openapi", func() {
	Files("/_voting/openapi.json", "gen/http/openapi.json", func() {
		Meta("swagger:generate", "false")
	})
	Files("/_voting/openapi.yaml", "gen/http/openapi.yaml", func() {
		Meta("swagger:generate", "false")
	})
	Files("/_voting/openapi3.json", "gen/http/openapi3.json", func() {
		Meta("swagger:generate", "false")
	})
	Files("/_voting/openapi3.yaml", "gen/http/openapi3.yaml", func() {
		Meta("swagger:generate", "false")
	})
})
