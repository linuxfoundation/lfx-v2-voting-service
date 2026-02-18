// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package itx

// CreatePollRequest represents the request to create a poll in ITX
type CreatePollRequest struct {
	Name                        string              `json:"name"`
	Description                 string              `json:"description"`
	EndTime                     string              `json:"end_time"`
	ProjectID                   string              `json:"project_id"`
	CommitteeID                 string              `json:"committee_id"`
	CommitteeIDs                []string            `json:"committee_ids,omitempty"`
	CommitteeFilters            []string            `json:"committee_filters,omitempty"`
	PollQuestions               []PollQuestionInput `json:"poll_questions"`
	PollCommentPrompts          []PollCommentPrompt `json:"poll_comment_prompts,omitempty"`
	PseudoAnonymity             bool                `json:"pseudo_anonymity"`
	PollType                    string              `json:"poll_type,omitempty"`
	NumWinners                  *int                `json:"num_winners,omitempty"`
	AllowAbstain                bool                `json:"allow_abstain"`
	QuorumPercentage            *int                `json:"quorum_percentage,omitempty"`
	WinningThresholdPercentage  *int                `json:"winning_threshold_percentage,omitempty"`
}

// UpdatePollRequest represents the request to update a poll in ITX
type UpdatePollRequest struct {
	Name                        string              `json:"name"`
	Description                 string              `json:"description"`
	EndTime                     string              `json:"end_time"`
	ProjectID                   string              `json:"project_id"`
	CommitteeID                 string              `json:"committee_id"`
	CommitteeIDs                []string            `json:"committee_ids,omitempty"`
	CommitteeFilters            []string            `json:"committee_filters,omitempty"`
	PollQuestions               []PollQuestionInput `json:"poll_questions"`
	PollCommentPrompts          []PollCommentPrompt `json:"poll_comment_prompts,omitempty"`
	PseudoAnonymity             bool                `json:"pseudo_anonymity"`
	PollType                    string              `json:"poll_type,omitempty"`
	NumWinners                  *int                `json:"num_winners,omitempty"`
	AllowAbstain                bool                `json:"allow_abstain"`
	QuorumPercentage            *int                `json:"quorum_percentage,omitempty"`
	WinningThresholdPercentage  *int                `json:"winning_threshold_percentage,omitempty"`
}

// ExtendPollRequest represents the request to extend a poll's end time in ITX
type ExtendPollRequest struct {
	EndTime string `json:"end_time"`
}

// BulkResendRequest represents the request to bulk resend poll emails in ITX
type BulkResendRequest struct {
	RecipientIDs []string `json:"recipient_ids"`
}

// PollQuestionInput represents a question in the request
type PollQuestionInput struct {
	Prompt  string   `json:"prompt"`
	Type    string   `json:"type"`
	Choices []string `json:"choices"`
}

// PollCommentPrompt represents a comment prompt in the request
type PollCommentPrompt struct {
	Prompt string `json:"prompt"`
}

// PollResponse represents a poll from ITX
type PollResponse struct {
	PollID                        string               `json:"poll_id"`
	Name                          string               `json:"name"`
	Description                   string               `json:"description"`
	CreationTime                  string               `json:"creation_time"`
	LastModifiedTime              string               `json:"last_modified_time"`
	EndTime                       string               `json:"end_time"`
	Status                        string               `json:"status"`
	ProjectID                     string               `json:"project_id"`
	ProjectName                   string               `json:"project_name,omitempty"`
	CommitteeID                   string               `json:"committee_id"`
	CommitteeName                 string               `json:"committee_name"`
	CommitteeFilters              []string             `json:"committee_filters,omitempty"`
	CommitteeType                 string               `json:"committee_type"`
	CommitteeVotingStatus         bool                 `json:"committee_voting_status"`
	PseudoAnonymity               bool                 `json:"pseudo_anonymity"`
	TotalVotingRequestInvitations int                  `json:"total_voting_request_invitations"`
	NumResponseReceived           int                  `json:"num_response_received"`
	PollQuestions                 []PollQuestionOutput `json:"poll_questions"`
	PollType                      string               `json:"poll_type,omitempty"`
	NumWinners                    int                  `json:"num_winners,omitempty"`
	AllowAbstain                  bool                 `json:"allow_abstain"`
}

// PollQuestionOutput represents a question in the response
type PollQuestionOutput struct {
	QuestionID string             `json:"question_id"`
	Prompt     string             `json:"prompt"`
	Type       string             `json:"type"`
	Choices    []PollChoiceOutput `json:"choices"`
}

// PollChoiceOutput represents a choice in the response
type PollChoiceOutput struct {
	ChoiceID   string `json:"choice_id"`
	ChoiceText string `json:"choice_text"`
}

// CreateVoteRequest represents the request to submit a vote in ITX
type CreateVoteRequest struct {
	VoteID          string            `json:"vote_id"`
	UserVoteContent []PollAnswerInput `json:"user_vote_content"`
	Abstain         bool              `json:"abstain"`
}

// UpdateVoteRequest represents the request to update a vote in ITX
type UpdateVoteRequest struct {
	UserVoteContent []PollAnswerInput `json:"user_vote_content"`
	Abstain         bool              `json:"abstain"`
}

// PollAnswerInput represents a vote answer submission
type PollAnswerInput struct {
	QuestionID    string              `json:"question_id"`
	ChoiceIDs     []string            `json:"choice_ids,omitempty"`
	RankedChoices []RankedChoiceInput `json:"ranked_choices,omitempty"`
}

// RankedChoiceInput represents a ranked choice answer
type RankedChoiceInput struct {
	ChoiceID   string `json:"choice_id"`
	ChoiceRank int    `json:"choice_rank"`
}

// VoteResponse represents a vote response from ITX
type VoteResponse struct {
	VoteID                 string       `json:"vote_id"`
	PollID                 string       `json:"poll_id"`
	ProjectID              string       `json:"project_id"`
	VoteStatus             string       `json:"vote_status"`
	Abstained              bool         `json:"abstained"`
	AllowAbstain           bool         `json:"allow_abstain"`
	VoteCreationTime       string       `json:"vote_creation_time"`
	UserName               string       `json:"user_name"`
	ProfilePicture         string       `json:"profile_picture"`
	UserID                 string       `json:"user_id"`
	UserEmail              string       `json:"user_email"`
	UserRole               string       `json:"user_role"`
	UserVotingStatus       string       `json:"user_voting_status"`
	UserOrgName            string       `json:"user_org_name"`
	UserOrgID              string       `json:"user_org_id"`
	SESMessageID           string       `json:"ses_message_id"`
	SESMessageLastSentTime string       `json:"ses_message_last_sent_time"`
	SESBounceType          string       `json:"ses_bounce_type,omitempty"`
	SESBounceSubtype       string       `json:"ses_bounce_subtype,omitempty"`
	SESComplaintExists     bool         `json:"ses_complaint_exists"`
	SESComplaintType       string       `json:"ses_complaint_type,omitempty"`
	SESComplaintDate       string       `json:"ses_complaint_date,omitempty"`
	SESDeliverySuccessful  bool         `json:"ses_delivery_successful"`
	SESEmailOpened         bool         `json:"ses_email_opened"`
	SESEmailOpenedLastTime string       `json:"ses_email_opened_last_time,omitempty"`
	SESLinkClicked         bool         `json:"ses_link_clicked"`
	SESLinkClickedLastTime string       `json:"ses_link_clicked_last_time,omitempty"`
	PollAnswers            []PollAnswer `json:"poll_answers"`
}

// PollAnswer represents a vote answer in the response
type PollAnswer struct {
	QuestionID       string                     `json:"question_id"`
	Prompt           string                     `json:"prompt"`
	Type             string                     `json:"type"`
	UserChoice       []PollChoiceAnswer         `json:"user_choice,omitempty"`
	RankedUserChoice []RankedPollChoiceAnswer   `json:"ranked_user_choice,omitempty"`
}

// PollChoiceAnswer represents an answer choice
type PollChoiceAnswer struct {
	ChoiceID   string `json:"choice_id"`
	ChoiceText string `json:"choice_text"`
}

// RankedPollChoiceAnswer represents a ranked answer choice
type RankedPollChoiceAnswer struct {
	ChoiceID   string `json:"choice_id"`
	ChoiceText string `json:"choice_text"`
	ChoiceRank int    `json:"choice_rank"`
}

// VoteResults represents the aggregated results of a poll/vote from ITX
type VoteResults struct {
	PollResults       []PollResult      `json:"poll_results"`
	CommentResults    []CommentResult   `json:"comment_results"`
	NumRecipients     int               `json:"num_recipients"`
	NumVotesCast      int               `json:"num_votes_cast"`
	NumAbstained      int               `json:"num_abstained"`
	PollEndTime       string            `json:"poll_end_time"`
}

// PollResult represents the results for a single poll question
type PollResult struct {
	Question                 PollQuestionDetail        `json:"question"`
	GenericChoiceVotes       []GenericChoiceVote       `json:"generic_choice_votes"`
	RankedChoiceVotes        []RankedChoiceVote        `json:"ranked_choice_votes"`
	RankedChoiceWinnerInfo   *RankedChoiceWinnerInfo   `json:"ranked_choice_winner_info,omitempty"`
	IRVRoundSummary          []IRVRoundSummary         `json:"irv_round_summary"`
	MeekSTVRoundSummary      []MeekSTVRoundSummary     `json:"meek_stv_round_summary"`
}

// PollQuestionDetail represents question details in results
type PollQuestionDetail struct {
	QuestionID string                `json:"question_id"`
	Prompt     string                `json:"prompt"`
	Type       string                `json:"type"`
	Choices    []PollChoiceOutput    `json:"choices"`
}

// GenericChoiceVote represents vote counts for non-ranked questions
type GenericChoiceVote struct {
	ChoiceID   string  `json:"choice_id"`
	VoteCount  int     `json:"vote_count"`
	Percentage float64 `json:"percentage"`
}

// RankedChoiceVote represents ranked choice voting results
type RankedChoiceVote struct {
	ChoiceID        string             `json:"choice_id"`
	RankCounts      []RankCount        `json:"rank_counts"`
	CondorcetMatrix [][]int            `json:"condorcet_matrix"`
}

// RankCount represents vote count at a specific rank
type RankCount struct {
	Rank  int `json:"rank"`
	Count int `json:"count"`
}

// RankedChoiceWinnerInfo represents winner information for ranked choice
type RankedChoiceWinnerInfo struct {
	PollChoices                     []PollChoiceOutput `json:"poll_choices"`
	CondorcetIRVUsedForEliminations bool               `json:"condorcet_irv_used_for_eliminations"`
}

// IRVRoundSummary represents instant runoff voting round summary
type IRVRoundSummary struct {
	RoundNumber        int                  `json:"round_number"`
	Votes              []VoteCount          `json:"votes"`
	TotalVotes         int                  `json:"total_votes"`
	ExhaustedVotes     int                  `json:"exhausted_votes"`
	Threshold          float64              `json:"threshold"`
	EliminatedChoices  []VoteCount          `json:"eliminated_choices"`
	ElectedChoices     []ElectedChoice      `json:"elected_choices"`
	TransferredVotes   []VoteCount          `json:"transferred_votes"`
	Message            string               `json:"message"`
}

// MeekSTVRoundSummary represents Meek STV round summary
type MeekSTVRoundSummary struct {
	RoundNumber        int             `json:"round_number"`
	Votes              []VoteCount     `json:"votes"`
	TotalVotes         float64         `json:"total_votes"`
	ExhaustedVotes     float64         `json:"exhausted_votes"`
	Threshold          float64         `json:"threshold"`
	EliminatedChoices  []VoteCount     `json:"eliminated_choices"`
	ElectedChoices     []ElectedChoice `json:"elected_choices"`
	TransferredVotes   []VoteCount     `json:"transferred_votes"`
	SurplusVotes       []VoteCount     `json:"surplus_votes"`
	Message            string          `json:"message"`
}

// VoteCount represents vote count for a choice
type VoteCount struct {
	ChoiceID  string  `json:"choice_id"`
	VoteCount float64 `json:"vote_count"`
}

// ElectedChoice represents an elected choice with surplus
type ElectedChoice struct {
	ChoiceID     string  `json:"choice_id"`
	VoteCount    float64 `json:"vote_count"`
	SurplusVotes float64 `json:"surplus_votes"`
}

// CommentResult represents comment results
type CommentResult struct {
	Prompt   string `json:"prompt"`
	Comments []string `json:"comments"`
}
