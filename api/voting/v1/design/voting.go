package design

import (
	. "goa.design/goa/v3/dsl"
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

var _ = Service("voting", func() {
	Description("Voting service that proxies to ITX voting API")

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
			ProjectIDAttribute()
			CommitteeIDAttribute()
			CommitteeIDsAttribute()
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

			Required("vote_uid")
		})

		Result(VoteResult)

		HTTP(func() {
			GET("/votes/{vote_uid}")
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
			ProjectIDAttribute()
			CommitteeIDAttribute()
			CommitteeIDsAttribute()
			CommitteeFiltersAttribute()
			PollQuestionsAttribute()
			PollCommentPromptsAttribute()
			PseudoAnonymityAttribute()
			PollTypeAttribute()
			NumWinnersAttribute()
			AllowAbstainAttribute()
			QuorumPercentageAttribute()
			WinningThresholdPercentageAttribute()

			Required("vote_uid", "name", "description", "end_time", "poll_questions")
		})

		Result(VoteResult)

		HTTP(func() {
			PUT("/votes/{vote_uid}")
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

			Required("vote_uid")
		})

		HTTP(func() {
			DELETE("/votes/{vote_uid}")
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
