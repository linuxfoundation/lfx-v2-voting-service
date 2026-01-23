// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// ITXProxyClient defines the interface for calling ITX voting service
type ITXProxyClient interface {
	// CreatePoll creates a new poll in ITX (maps to our "create vote")
	CreatePoll(ctx context.Context, req *CreatePollRequest) (*PollResponse, error)
}

// CreatePollRequest represents the request to create a poll in ITX
type CreatePollRequest struct {
	Name             string                `json:"name"`
	Description      string                `json:"description"`
	EndTime          string                `json:"end_time"`
	ProjectID        string                `json:"project_id"`
	CommitteeID      string                `json:"committee_id"`
	CommitteeFilters []string              `json:"committee_filters,omitempty"`
	PollQuestions    []PollQuestionInput   `json:"poll_questions"`
	PseudoAnonymity  bool                  `json:"pseudo_anonymity"`
	PollType         string                `json:"poll_type,omitempty"`
	NumWinners       *int                  `json:"num_winners,omitempty"`
	AllowAbstain     bool                  `json:"allow_abstain"`
}

// PollQuestionInput represents a question in the request
type PollQuestionInput struct {
	Prompt  string   `json:"prompt"`
	Type    string   `json:"type"`
	Choices []string `json:"choices"`
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
