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
			Token("token", String, "JWT token", func() {
				Example("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...")
			})

			Attribute("name", String, "Vote name", func() {
				Example("Q1 2026 Technical Steering Committee Election")
				MinLength(1)
				MaxLength(255)
			})

			Attribute("description", String, "Vote description", func() {
				Example("Vote for the TSC members for Q1 2026")
			})

			Attribute("end_time", String, "End time in RFC3339 format", func() {
				Format(FormatDateTime)
				Example("2026-02-15T23:59:59Z")
			})

			Attribute("project_id", String, "LFX Project ID", func() {
				Example("a09P000000DsCBuIRT")
			})

			Attribute("committee_id", String, "LFX Committee ID", func() {
				Format(FormatUUID)
				Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
			})

			Attribute("committee_filters", ArrayOf(String, func() {
				Enum("Voting Rep", "Alternate Voting Rep", "Observer", "Emeritus")
			}), "Committee voting status filters", func() {
				Example([]string{"Voting Rep", "Alternate Voting Rep"})
			})

			Attribute("poll_questions", ArrayOf(PollQuestion), "Questions for the vote")

			Attribute("pseudo_anonymity", Boolean, "Enable pseudo-anonymity", func() {
				Default(false)
			})

			Attribute("poll_type", String, "Type of poll", func() {
				Enum("generic", "condorcet_irv", "meek_stv")
				Default("generic")
			})

			Attribute("num_winners", Int, "Number of winners (meek_stv only)", func() {
				Minimum(2)
				Maximum(10)
				Default(2)
			})

			Attribute("allow_abstain", Boolean, "Allow voters to abstain", func() {
				Default(false)
			})

			Required("name", "description", "end_time", "project_id", "committee_id", "poll_questions")
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
})
