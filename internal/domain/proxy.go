// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// ITXProxyClient defines the interface for calling ITX voting service
type ITXProxyClient interface {
	// CreatePoll creates a new poll in ITX (maps to our "create vote")
	CreatePoll(ctx context.Context, req *CreatePollProxyRequest) (*PollProxyResponse, error)

	// GetPoll retrieves poll details from ITX
	GetPoll(ctx context.Context, pollID string) (*PollProxyResponse, error)

	// UpdatePoll updates a poll in ITX (only when status is "disabled")
	UpdatePoll(ctx context.Context, pollID string, req *UpdatePollProxyRequest) (*PollProxyResponse, error)

	// DeletePoll deletes a poll in ITX (only when status is "disabled")
	DeletePoll(ctx context.Context, pollID string) error
}

// CreatePollProxyRequest represents the request to create a poll in ITX
type CreatePollProxyRequest struct {
	Name                        string                  `json:"name"`
	Description                 string                  `json:"description"`
	EndTime                     string                  `json:"end_time"`
	ProjectID                   string                  `json:"project_id"`
	CommitteeID                 string                  `json:"committee_id"`
	CommitteeIDs                []string                `json:"committee_ids,omitempty"`
	CommitteeFilters            []string                `json:"committee_filters,omitempty"`
	PollQuestions               []PollQuestionInput     `json:"poll_questions"`
	PollCommentPrompts          []PollCommentPrompt     `json:"poll_comment_prompts,omitempty"`
	PseudoAnonymity             bool                    `json:"pseudo_anonymity"`
	PollType                    string                  `json:"poll_type,omitempty"`
	NumWinners                  *int                    `json:"num_winners,omitempty"`
	AllowAbstain                bool                    `json:"allow_abstain"`
	QuorumPercentage            *int                    `json:"quorum_percentage,omitempty"`
	WinningThresholdPercentage  *int                    `json:"winning_threshold_percentage,omitempty"`
}

// UpdatePollProxyRequest represents the request to update a poll in ITX
type UpdatePollProxyRequest struct {
	Name                        string                `json:"name"`
	Description                 string                `json:"description"`
	EndTime                     string                `json:"end_time"`
	ProjectID                   string                `json:"project_id"`
	CommitteeID                 string                `json:"committee_id"`
	CommitteeIDs                []string              `json:"committee_ids,omitempty"`
	CommitteeFilters            []string              `json:"committee_filters,omitempty"`
	PollQuestions               []PollQuestionInput   `json:"poll_questions"`
	PollCommentPrompts          []PollCommentPrompt   `json:"poll_comment_prompts,omitempty"`
	PseudoAnonymity             bool                  `json:"pseudo_anonymity"`
	PollType                    string                `json:"poll_type,omitempty"`
	NumWinners                  *int                  `json:"num_winners,omitempty"`
	AllowAbstain                bool                  `json:"allow_abstain"`
	QuorumPercentage            *int                  `json:"quorum_percentage,omitempty"`
	WinningThresholdPercentage  *int                  `json:"winning_threshold_percentage,omitempty"`
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

// PollProxyResponse represents a poll from ITX
type PollProxyResponse struct {
	PollID                        string               `json:"poll_id"`
	Name                          string               `json:"name"`
	Description                   string               `json:"description"`
	CreationTime                  string               `json:"creation_time"`
	LastModifiedTime              string               `json:"last_modified_time"`
	EndTime                       string               `json:"end_time"`
	Status                        string               `json:"status"`
	ProjectID                     string               `json:"project_id"`
	CommitteeID                   string               `json:"committee_id"`
	CommitteeName                 string               `json:"committee_name"`
	CommitteeType                 string               `json:"committee_type"`
	CommitteeVotingStatus         bool                 `json:"committee_voting_status"`
	PseudoAnonymity               bool                 `json:"pseudo_anonymity"`
	TotalVotingRequestInvitations int                  `json:"total_voting_request_invitations"`
	NumResponseReceived           int                  `json:"num_response_received"`
	PollQuestions                 []PollQuestionOutput `json:"poll_questions"`
	AllowAbstain                  bool                 `json:"allow_abstain"`
}

// PollQuestionOutput represents a question in the response
type PollQuestionOutput struct {
	QuestionID string            `json:"question_id"`
	Prompt     string            `json:"prompt"`
	Type       string            `json:"type"`
	Choices    []PollChoiceOutput `json:"choices"`
}

// PollChoiceOutput represents a choice in the response
type PollChoiceOutput struct {
	ChoiceID   string `json:"choice_id"`
	ChoiceText string `json:"choice_text"`
}
