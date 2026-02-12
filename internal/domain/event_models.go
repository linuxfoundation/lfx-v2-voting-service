// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"

// PollDBRaw represents raw poll data from v1 DynamoDB/NATS KV bucket
// This is only used for unmarshaling - numeric fields come as strings from DynamoDB
type PollDBRaw struct {
	PollID                        string                   `json:"poll_id"`
	Name                          string                   `json:"name"`
	Description                   string                   `json:"description"`
	CreationTime                  string                   `json:"creation_time"`
	LastModifiedTime              string                   `json:"last_modified_time"`
	EndTime                       string                   `json:"end_time"`
	Status                        string                   `json:"status"`
	ProjectID                     string                   `json:"project_id"`
	ProjectName                   string                   `json:"project_name"`
	CommitteeID                   string                   `json:"committee_id"`
	CommitteeName                 string                   `json:"committee_name"`
	CommitteeType                 string                   `json:"committee_type"`
	CommitteeVotingStatus         bool                     `json:"committee_voting_status"`
	CommitteeFilters              []string                 `json:"committee_filters"`
	TotalVotingRequestInvitations string                   `json:"total_voting_request_invitations"` // String in DynamoDB
	PollQuestions                 []itx.PollQuestionOutput `json:"poll_questions"`                   // Reuse existing model
	NumResponseReceived           string                   `json:"num_response_received"`            // String in DynamoDB
	PollType                      string                   `json:"poll_type"`
	PseudoAnonymity               bool                     `json:"pseudo_anonymity"`
	NumWinners                    string                   `json:"num_winners"` // String in DynamoDB
	AllowAbstain                  bool                     `json:"allow_abstain"`
}

// VoteDBRaw represents raw vote response data from v1 DynamoDB/NATS KV bucket
// This is only used for unmarshaling - some fields may need conversion
type VoteDBRaw struct {
	VoteID                  string          `json:"vote_id"`
	PollID                  string          `json:"poll_id"`
	ProjectID               string          `json:"project_id"`
	VoteCreationTime        string          `json:"vote_creation_time"`
	UserID                  string          `json:"user_id"`
	UserEmail               string          `json:"user_email"`
	UserRole                string          `json:"user_role"`
	UserName                string          `json:"user_name"`
	ProfilePicture          string          `json:"profile_picture"`
	UserVotingStatus        string          `json:"user_voting_status"`
	UserOrgID               string          `json:"user_org_id"`
	UserOrgName             string          `json:"user_org_name"`
	PollAnswers             []PollAnswerRaw `json:"poll_answers"` // Custom for string choice_rank
	VoteStatus              string          `json:"vote_status"`
	Abstained               bool            `json:"abstained"`
	VoterRemoved            bool            `json:"voter_removed"`
	SESMessageID            string          `json:"ses_message_id"`
	SESMessageLastSentTime  string          `json:"ses_message_last_sent_time"`
	SESBounceType           string          `json:"ses_bounce_type"`
	SESBounceSubtype        string          `json:"ses_bounce_subtype"`
	SESDeliverySuccessful   bool            `json:"ses_delivery_successful"`
	SESComplaintExists      bool            `json:"ses_complaint_exists"`
	SESComplaintType        string          `json:"ses_complaint_type"`
	SESComplaintDate        string          `json:"ses_complaint_date"`
	SESEmailOpened          bool            `json:"ses_email_opened"`
	SESEmailOpenedFirstTime string          `json:"ses_email_opened_first_time"`
	SESEmailOpenedLastTime  string          `json:"ses_email_opened_last_time"`
	SESLinkClicked          bool            `json:"ses_link_clicked"`
	SESLinkClickedFirstTime string          `json:"ses_link_clicked_first_time"`
	SESLinkClickedLastTime  string          `json:"ses_link_clicked_last_time"`
}

// PollAnswerRaw represents a poll answer with string choice_rank (v1 format)
type PollAnswerRaw struct {
	QuestionID       string                  `json:"question_id"`
	Prompt           string                  `json:"prompt"`
	Type             string                  `json:"type"`
	UserChoice       []itx.PollChoiceAnswer  `json:"user_choice,omitempty"` // Reuse existing model
	RankedUserChoice []RankedChoiceAnswerRaw `json:"ranked_user_choice,omitempty"`
}

// RankedChoiceAnswerRaw represents a ranked choice with string choice_rank (v1 format)
type RankedChoiceAnswerRaw struct {
	ChoiceID   string `json:"choice_id"`
	ChoiceText string `json:"choice_text"`
	ChoiceRank string `json:"choice_rank"` // String in DynamoDB, needs conversion to int
}

//
// V2 Transformed Data Models (after parsing and ID mapping)
//

// VoteData represents v2 vote (poll) data after transformation
type VoteData struct {
	VoteUID                       string                   `json:"vote_uid"` // v2 primary key (same as PollID)
	PollID                        string                   `json:"poll_id"`  // v1 primary key
	Name                          string                   `json:"name"`
	Description                   string                   `json:"description"`
	CreationTime                  string                   `json:"creation_time"`
	LastModifiedTime              string                   `json:"last_modified_time"`
	EndTime                       string                   `json:"end_time"`
	Status                        string                   `json:"status"`
	ProjectID                     string                   `json:"project_id"`  // v1 project ID (SFID)
	ProjectUID                    string                   `json:"project_uid"` // v2 project UID
	ProjectName                   string                   `json:"project_name"`
	CommitteeID                   string                   `json:"committee_id"`  // v1 committee ID (SFID)
	CommitteeUID                  string                   `json:"committee_uid"` // v2 committee UID
	CommitteeName                 string                   `json:"committee_name"`
	CommitteeType                 string                   `json:"committee_type"`
	CommitteeVotingStatus         bool                     `json:"committee_voting_status"`
	CommitteeFilters              []string                 `json:"committee_filters"`
	TotalVotingRequestInvitations int                      `json:"total_voting_request_invitations"` // Int in v2
	PollQuestions                 []itx.PollQuestionOutput `json:"poll_questions"`
	NumResponseReceived           int                      `json:"num_response_received"` // Int in v2
	PollType                      string                   `json:"poll_type"`
	PseudoAnonymity               bool                     `json:"pseudo_anonymity"`
	NumWinners                    int                      `json:"num_winners"` // Int in v2
	AllowAbstain                  bool                     `json:"allow_abstain"`
}

// VoteResponseData represents v2 vote response data after transformation
type VoteResponseData struct {
	UID                     string           `json:"uid"`         // v2 primary key (same as VoteID)
	VoteID                  string           `json:"vote_id"`     // v1 primary key
	VoteUID                 string           `json:"vote_uid"`    // v2 poll/vote UID (same as PollID)
	PollID                  string           `json:"poll_id"`     // v1 poll ID
	ProjectID               string           `json:"project_id"`  // v1 project ID (SFID)
	ProjectUID              string           `json:"project_uid"` // v2 project UID
	VoteCreationTime        string           `json:"vote_creation_time"`
	UserID                  string           `json:"user_id"`
	UserEmail               string           `json:"user_email"`
	UserRole                string           `json:"user_role"`
	UserName                string           `json:"user_name"` // actual user's name
	Username                string           `json:"username"`  // Auth0 username
	ProfilePicture          string           `json:"profile_picture"`
	UserVotingStatus        string           `json:"user_voting_status"`
	UserOrgID               string           `json:"user_org_id"`
	UserOrgName             string           `json:"user_org_name"`
	PollAnswers             []itx.PollAnswer `json:"poll_answers"` // Converted to proper int choice_rank
	VoteStatus              string           `json:"vote_status"`
	Abstained               bool             `json:"abstained"`
	VoterRemoved            bool             `json:"voter_removed"`
	SESMessageID            string           `json:"ses_message_id"`
	SESMessageLastSentTime  string           `json:"ses_message_last_sent_time"`
	SESBounceType           string           `json:"ses_bounce_type"`
	SESBounceSubtype        string           `json:"ses_bounce_subtype"`
	SESDeliverySuccessful   bool             `json:"ses_delivery_successful"`
	SESComplaintExists      bool             `json:"ses_complaint_exists"`
	SESComplaintType        string           `json:"ses_complaint_type"`
	SESComplaintDate        string           `json:"ses_complaint_date"`
	SESEmailOpened          bool             `json:"ses_email_opened"`
	SESEmailOpenedFirstTime string           `json:"ses_email_opened_first_time"`
	SESEmailOpenedLastTime  string           `json:"ses_email_opened_last_time"`
	SESLinkClicked          bool             `json:"ses_link_clicked"`
	SESLinkClickedFirstTime string           `json:"ses_link_clicked_first_time"`
	SESLinkClickedLastTime  string           `json:"ses_link_clicked_last_time"`
}
