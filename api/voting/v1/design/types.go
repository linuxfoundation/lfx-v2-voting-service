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
	Attribute("uid", String, "Vote UID", func() {
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

// ProjectUIDAttribute is the DSL attribute for project UID.
func ProjectUIDAttribute() {
	Attribute("project_uid", String, "LFX Project UID", func() {
		Example("a09P000000DsCBuIRT")
	})
}

// CommitteeUIDAttribute is the DSL attribute for committee UID.
func CommitteeUIDAttribute() {
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
		Enum("generic", "condorcet_irv", "meek_stv", "instant_runoff_vote")
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

// CommitteeUIDsAttribute is the DSL attribute for multiple committee UIDs.
func CommitteeUIDsAttribute() {
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

	Attribute("uid", String, "Vote identifier", func() {
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
	Attribute("project_name", String, "Project name")
	Attribute("committee_uid", String, "Committee UID")
	Attribute("committee_name", String, "Committee name")
	Attribute("committee_filters", ArrayOf(String), "Committee voting status filters", func() {
		Example([]string{"Voting Rep", "Alternate Voting Rep"})
	})
	Attribute("committee_type", String, "Committee type")
	Attribute("committee_voting_status", Boolean, "Committee voting status")
	Attribute("pseudo_anonymity", Boolean, "Pseudo-anonymity enabled")
	Attribute("total_voting_request_invitations", Int, "Total invitations sent")
	Attribute("num_response_received", Int, "Responses received")
	Attribute("poll_questions", ArrayOf(PollQuestion), "Vote questions")
	Attribute("poll_type", String, "Poll type", func() {
		Enum("generic", "condorcet_irv", "meek_stv", "instant_runoff_vote")
		Default("generic")
	})
	Attribute("num_winners", Int, "Number of winners (meek_stv only)")
	Attribute("allow_abstain", Boolean, "Allow abstain")

	Required("uid", "name", "description", "status", "project_uid", "committee_uid")
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

// VoteResponseResult represents a vote response (ballot submission)
var VoteResponseResult = Type("VoteResponseResult", func() {
	Description("Vote response details")

	Attribute("vote_response_uid", String, "Vote response identifier", func() {
		Format(FormatUUID)
		Example("b03cdbaf-53b1-4d47-bc04-dd7e459dd309")
	})

	Attribute("vote_uid", String, "Vote/poll identifier", func() {
		Format(FormatUUID)
		Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
	})

	Attribute("project_uid", String, "Project identifier")
	Attribute("vote_status", String, "Vote status", func() {
		Example("submitted")
	})
	Attribute("abstained", Boolean, "Whether the voter abstained")
	Attribute("allow_abstain", Boolean, "Whether abstention is allowed")
	Attribute("vote_creation_time", String, "Vote creation time", func() {
		Format(FormatDateTime)
	})
	Attribute("user_name", String, "Voter name")
	Attribute("profile_picture", String, "Voter profile picture URL")
	Attribute("user_id", String, "User identifier")
	Attribute("user_email", String, "User email")
	Attribute("user_role", String, "User role")
	Attribute("user_voting_status", String, "User voting status")
	Attribute("user_org_name", String, "User organization name")
	Attribute("user_org_id", String, "User organization ID")
	Attribute("ses_message_id", String, "SES message ID")
	Attribute("ses_message_last_sent_time", String, "SES message last sent time")
	Attribute("ses_bounce_type", String, "SES bounce type")
	Attribute("ses_bounce_subtype", String, "SES bounce subtype")
	Attribute("ses_complaint_exists", Boolean, "SES complaint exists")
	Attribute("ses_complaint_type", String, "SES complaint type")
	Attribute("ses_complaint_date", String, "SES complaint date")
	Attribute("ses_delivery_successful", Boolean, "SES delivery successful")
	Attribute("ses_email_opened", Boolean, "SES email opened")
	Attribute("ses_email_opened_last_time", String, "SES email opened last time")
	Attribute("ses_link_clicked", Boolean, "SES link clicked")
	Attribute("ses_link_clicked_last_time", String, "SES link clicked last time")
	Attribute("poll_answers", ArrayOf(VoteAnswer), "Vote answers")

	Required("vote_response_uid", "vote_uid", "project_uid", "vote_status")
})

// VoteAnswer represents an answer to a poll question
var VoteAnswer = Type("VoteAnswer", func() {
	Description("Vote answer for a question")

	Attribute("question_id", String, "Question identifier", func() {
		Format(FormatUUID)
	})
	Attribute("prompt", String, "Question prompt")
	Attribute("type", String, "Question type", func() {
		Enum("single_choice", "multiple_choice")
	})
	Attribute("user_choice", ArrayOf(VoteChoiceAnswer), "User's selected choices")
	Attribute("ranked_user_choice", ArrayOf(RankedVoteChoiceAnswer), "User's ranked choices")

	Required("question_id", "prompt", "type")
})

// VoteChoiceAnswer represents a selected answer choice
var VoteChoiceAnswer = Type("VoteChoiceAnswer", func() {
	Description("Selected answer choice")

	Attribute("choice_id", String, "Choice identifier", func() {
		Format(FormatUUID)
	})
	Attribute("choice_text", String, "Choice text")

	Required("choice_id", "choice_text")
})

// RankedVoteChoiceAnswer represents a ranked answer choice
var RankedVoteChoiceAnswer = Type("RankedVoteChoiceAnswer", func() {
	Description("Ranked answer choice")

	Attribute("choice_id", String, "Choice identifier", func() {
		Format(FormatUUID)
	})
	Attribute("choice_text", String, "Choice text")
	Attribute("choice_rank", Int, "Choice rank (1-based)", func() {
		Minimum(1)
	})

	Required("choice_id", "choice_text", "choice_rank")
})

// VoteAnswerInput represents an answer submission
var VoteAnswerInput = Type("VoteAnswerInput", func() {
	Description("Answer submission for a question")

	Attribute("question_id", String, "Question identifier", func() {
		Format(FormatUUID)
		Example("a02bdbaf-53b1-4d47-bc04-dd7e459dd308")
	})
	Attribute("choice_ids", ArrayOf(String), "Selected choice IDs", func() {
		Example([]string{"b03cdbaf-53b1-4d47-bc04-dd7e459dd309"})
	})
	Attribute("ranked_choices", ArrayOf(RankedChoiceInput), "Ranked choices")

	Required("question_id")
})

// RankedChoiceInput represents a ranked choice submission
var RankedChoiceInput = Type("RankedChoiceInput", func() {
	Description("Ranked choice submission")

	Attribute("choice_id", String, "Choice identifier", func() {
		Format(FormatUUID)
		Example("b03cdbaf-53b1-4d47-bc04-dd7e459dd309")
	})
	Attribute("choice_rank", Int, "Choice rank (1-based)", func() {
		Minimum(1)
		Example(1)
	})

	Required("choice_id", "choice_rank")
})

// VoteResultsResult represents aggregated poll/vote results (matches ITX structure)
var VoteResultsResult = Type("VoteResultsResult", func() {
	Description("Aggregated poll/vote results")

	Attribute("poll_results", ArrayOf(PollResultItem), "Poll results data")
	Attribute("comment_results", ArrayOf(CommentResultItem), "Comment results")
	Attribute("num_recipients", Int, "Number of recipients")
	Attribute("num_votes_cast", Int, "Number of votes cast")
	Attribute("num_abstained", Int, "Number who abstained")
	Attribute("poll_end_time", String, "Poll end time", func() {
		Format(FormatDateTime)
	})

	Required("poll_results", "num_recipients", "num_votes_cast", "num_abstained")
})

// PollResultItem represents results for a single poll question
var PollResultItem = Type("PollResultItem", func() {
	Description("Results for a single poll question")

	Attribute("question", PollQuestionDetail, "Question details")
	Attribute("generic_choice_votes", ArrayOf(GenericChoiceVote), "Vote counts for non-ranked questions")
	Attribute("ranked_choice_votes", ArrayOf(RankedChoiceVote), "Ranked choice voting results")
	Attribute("ranked_choice_winner_info", RankedChoiceWinnerInfo, "Winner information for ranked choice")
	Attribute("irv_round_summary", ArrayOf(IRVRoundSummary), "IRV round summary")
	Attribute("meek_stv_round_summary", ArrayOf(MeekSTVRoundSummary), "Meek STV round summary")

	Required("question")
})

// PollQuestionDetail represents question details in results
var PollQuestionDetail = Type("PollQuestionDetail", func() {
	Description("Question details")

	Attribute("question_id", String, "Question identifier", func() {
		Format(FormatUUID)
	})
	Attribute("prompt", String, "Question prompt")
	Attribute("type", String, "Question type")
	Attribute("choices", ArrayOf(PollChoice), "Answer choices")

	Required("question_id", "prompt", "type", "choices")
})

// GenericChoiceVote represents vote counts for non-ranked questions
var GenericChoiceVote = Type("GenericChoiceVote", func() {
	Description("Vote count for a single choice")

	Attribute("choice_id", String, "Choice identifier", func() {
		Format(FormatUUID)
	})
	Attribute("vote_count", Int, "Number of votes")
	Attribute("percentage", Float64, "Percentage of votes")

	Required("choice_id", "vote_count", "percentage")
})

// RankedChoiceVote represents ranked choice voting results
var RankedChoiceVote = Type("RankedChoiceVote", func() {
	Description("Ranked choice voting results for a choice")

	Attribute("choice_id", String, "Choice identifier", func() {
		Format(FormatUUID)
	})
	Attribute("rank_counts", ArrayOf(RankCount), "Vote counts per rank")
	Attribute("condorcet_matrix", ArrayOf(CondorcetMatrixEntry), "Condorcet matrix entries")

	Required("choice_id", "rank_counts")
})

// CondorcetMatrixEntry represents a pairwise comparison in the Condorcet matrix
var CondorcetMatrixEntry = Type("CondorcetMatrixEntry", func() {
	Description("Condorcet matrix entry for pairwise comparison")

	Attribute("choice_id", String, "Choice identifier")
	Attribute("other_choice_id", String, "Other choice identifier")
	Attribute("choice_id_wins", Int, "Number of times choice_id wins against other_choice_id")
	Attribute("other_choice_id_wins", Int, "Number of times other_choice_id wins against choice_id")
	Attribute("result", String, "Result string (e.g., '23(10)')")

	Required("choice_id", "other_choice_id", "choice_id_wins", "other_choice_id_wins", "result")
})

// RankCount represents vote count at a specific rank
var RankCount = Type("RankCount", func() {
	Description("Vote count at a specific rank")

	Attribute("rank", Int, "Rank position", func() {
		Minimum(1)
	})
	Attribute("count", Int, "Number of votes at this rank")

	Required("rank", "count")
})

// RankedChoiceWinnerInfo represents winner information for ranked choice
var RankedChoiceWinnerInfo = Type("RankedChoiceWinnerInfo", func() {
	Description("Winner information for ranked choice voting")

	Attribute("poll_choices", ArrayOf(PollChoice), "Winning choices")
	Attribute("condorcet_irv_used_for_eliminations", Boolean, "Whether Condorcet IRV was used")

	Required("poll_choices")
})

// IRVRoundSummary represents instant runoff voting round summary
var IRVRoundSummary = Type("IRVRoundSummary", func() {
	Description("Instant runoff voting round summary")

	Attribute("round_number", Int, "Round number")
	Attribute("votes", ArrayOf(VoteCount), "Vote counts per choice")
	Attribute("total_votes", Int, "Total votes in this round")
	Attribute("min_votes", Int, "Minimum votes")
	Attribute("exhausted_votes", Int, "Number of exhausted votes")
	Attribute("transferred_votes", ArrayOf(VoteCount), "Transferred votes")
	Attribute("threshold", Int, "Winning threshold")
	Attribute("eliminated_choice_id", String, "ID of eliminated choice")
	Attribute("message", String, "Round message")

	Required("round_number", "votes", "total_votes", "exhausted_votes", "threshold", "message")
})

// VoteCount represents vote count for a choice
var VoteCount = Type("VoteCount", func() {
	Description("Vote count for a choice")

	Attribute("choice_id", String, "Choice identifier")
	Attribute("vote_count", Int, "Number of votes")
	Attribute("percentage", Float64, "Percentage of votes")

	Required("choice_id", "vote_count")
})

// MeekSTVRoundSummary represents Meek STV round summary
var MeekSTVRoundSummary = Type("MeekSTVRoundSummary", func() {
	Description("Meek STV round summary")

	Attribute("round_number", Int, "Round number")
	Attribute("votes", ArrayOf(MeekSTVVoteCount), "Vote counts per choice")
	Attribute("total_votes", Int, "Total votes in this round")
	Attribute("exhausted_votes", Int, "Number of exhausted votes")
	Attribute("threshold", Int, "Election threshold")
	Attribute("message", String, "Round message")
	Attribute("elected_choices", ArrayOf(MeekSTVElectedChoice), "Elected choices in this round")
	Attribute("eliminated_choices", ArrayOf(MeekSTVVoteCount), "Eliminated choices in this round")
	Attribute("transferred_votes", ArrayOf(MeekSTVVoteCount), "Transferred votes")
	Attribute("surplus_votes", ArrayOf(MeekSTVVoteCount), "Surplus votes")

	Required("round_number", "votes", "total_votes", "exhausted_votes", "threshold", "message")
})

// MeekSTVVoteCount represents vote count in Meek STV
var MeekSTVVoteCount = Type("MeekSTVVoteCount", func() {
	Description("Vote count for Meek STV")

	Attribute("choice_id", String, "Choice identifier")
	Attribute("vote_count", Int, "Number of votes")

	Required("choice_id", "vote_count")
})

// MeekSTVElectedChoice represents an elected choice in Meek STV
var MeekSTVElectedChoice = Type("MeekSTVElectedChoice", func() {
	Description("Elected choice in Meek STV")

	Attribute("choice_id", String, "Choice identifier")
	Attribute("vote_count", Int, "Number of votes")
	Attribute("surplus_votes", Int, "Surplus votes over threshold")

	Required("choice_id", "vote_count", "surplus_votes")
})

// CommentResultItem represents comment results
var CommentResultItem = Type("CommentResultItem", func() {
	Description("Comment results")

	Attribute("prompt", String, "Comment prompt")
	Attribute("comments", ArrayOf(String), "List of comments")

	Required("prompt", "comments")
})
