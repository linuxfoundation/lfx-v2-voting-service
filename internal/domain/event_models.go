// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

// PollDBRaw represents raw poll data from v1 DynamoDB/NATS KV bucket
// This is only used for unmarshaling - numeric fields come as strings from DynamoDB
type PollDBRaw struct {
	PollID                        string                   `json:"poll_id"`
	Name                          string                   `json:"name"`
	Description                   string                   `json:"description"`
	CreationTime                  string                   `json:"creation_time"`
	LastModifiedTime              string                   `json:"last_modified_time"`
	EndTime                       string                   `json:"end_time"`
	EarlyEndTime                  string                   `json:"early_end_time,omitempty"`
	Status                        string                   `json:"status"`
	ProjectID                     string                   `json:"project_id"`
	ProjectName                   string                   `json:"project_name"`
	CommitteeID                   string                   `json:"committee_id"`
	CommitteeName                 string                   `json:"committee_name"`
	CommitteeType                 string                   `json:"committee_type"`
	CommitteeVotingStatus         bool                     `json:"committee_voting_status"`
	CommitteeFilters              []string                 `json:"committee_filters"`
	TotalVotingRequestInvitations int                      `json:"total_voting_request_invitations"`
	PollQuestions                 []itx.PollQuestionOutput `json:"poll_questions"` // Reuse existing model
	NumResponseReceived           int                      `json:"num_response_received"`
	PollType                      string                   `json:"poll_type"`
	PseudoAnonymity               bool                     `json:"pseudo_anonymity"`
	NumWinners                    int                      `json:"num_winners"`
	AllowAbstain                  bool                     `json:"allow_abstain"`
}

// UnmarshalJSON implements custom unmarshaling to handle both string and int inputs for numeric fields.
func (p *PollDBRaw) UnmarshalJSON(data []byte) error {
	tmp := struct {
		PollID                        string                   `json:"poll_id"`
		Name                          string                   `json:"name"`
		Description                   string                   `json:"description"`
		CreationTime                  string                   `json:"creation_time"`
		LastModifiedTime              string                   `json:"last_modified_time"`
		EndTime                       string                   `json:"end_time"`
		EarlyEndTime                  string                   `json:"early_end_time,omitempty"`
		Status                        string                   `json:"status"`
		ProjectID                     string                   `json:"project_id"`
		ProjectName                   string                   `json:"project_name"`
		CommitteeID                   string                   `json:"committee_id"`
		CommitteeName                 string                   `json:"committee_name"`
		CommitteeType                 string                   `json:"committee_type"`
		CommitteeVotingStatus         bool                     `json:"committee_voting_status"`
		CommitteeFilters              []string                 `json:"committee_filters"`
		TotalVotingRequestInvitations interface{}              `json:"total_voting_request_invitations"`
		PollQuestions                 []itx.PollQuestionOutput `json:"poll_questions"`
		NumResponseReceived           interface{}              `json:"num_response_received"`
		PollType                      string                   `json:"poll_type"`
		PseudoAnonymity               bool                     `json:"pseudo_anonymity"`
		NumWinners                    interface{}              `json:"num_winners"`
		AllowAbstain                  bool                     `json:"allow_abstain"`
	}{}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	// Handle TotalVotingRequestInvitations (string from Meltano, int/float64 from other sources)
	switch v := tmp.TotalVotingRequestInvitations.(type) {
	case string:
		if v != "" {
			val, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			p.TotalVotingRequestInvitations = val
		}
	case float64:
		p.TotalVotingRequestInvitations = int(v)
	case int:
		p.TotalVotingRequestInvitations = v
	default:
		if v != nil {
			return fmt.Errorf("invalid type for total_voting_request_invitations: %T", v)
		}
	}

	// Handle NumResponseReceived (string from Meltano, int/float64 from other sources)
	switch v := tmp.NumResponseReceived.(type) {
	case string:
		if v != "" {
			val, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			p.NumResponseReceived = val
		}
	case float64:
		p.NumResponseReceived = int(v)
	case int:
		p.NumResponseReceived = v
	default:
		if v != nil {
			return fmt.Errorf("invalid type for num_response_received: %T", v)
		}
	}

	// Handle NumWinners (string from Meltano, int/float64 from other sources)
	switch v := tmp.NumWinners.(type) {
	case string:
		if v != "" {
			val, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			p.NumWinners = val
		}
	case float64:
		p.NumWinners = int(v)
	case int:
		p.NumWinners = v
	default:
		if v != nil {
			return fmt.Errorf("invalid type for num_winners: %T", v)
		}
	}

	// Assign all other fields
	p.PollID = tmp.PollID
	p.Name = tmp.Name
	p.Description = tmp.Description
	p.CreationTime = tmp.CreationTime
	p.LastModifiedTime = tmp.LastModifiedTime
	p.EndTime = tmp.EndTime
	p.EarlyEndTime = tmp.EarlyEndTime
	p.Status = tmp.Status
	p.ProjectID = tmp.ProjectID
	p.ProjectName = tmp.ProjectName
	p.CommitteeID = tmp.CommitteeID
	p.CommitteeName = tmp.CommitteeName
	p.CommitteeType = tmp.CommitteeType
	p.CommitteeVotingStatus = tmp.CommitteeVotingStatus
	p.CommitteeFilters = tmp.CommitteeFilters
	p.PollQuestions = tmp.PollQuestions
	p.PollType = tmp.PollType
	p.PseudoAnonymity = tmp.PseudoAnonymity
	p.AllowAbstain = tmp.AllowAbstain

	return nil
}

// VoteDBRaw represents raw vote response data from v1 DynamoDB/NATS KV bucket
// This is only used for unmarshaling - some fields may need conversion
type VoteDBRaw struct {
	VoteID                  string          `json:"vote_id"`
	PollID                  string          `json:"poll_id"`
	ProjectID               string          `json:"project_id"`
	VoteCreationTime        string          `json:"vote_creation_time"`
	LastModifiedTime        string          `json:"last_modified_time"`
	UserID                  string          `json:"user_id"`
	UserEmail               string          `json:"user_email"`
	UserRole                string          `json:"user_role"`
	UserName                string          `json:"user_name"`
	Username                string          `json:"username"`
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
	ChoiceRank int    `json:"choice_rank"`
}

// UnmarshalJSON implements custom unmarshaling to handle both string and int inputs for ChoiceRank.
func (r *RankedChoiceAnswerRaw) UnmarshalJSON(data []byte) error {
	tmp := struct {
		ChoiceID   string      `json:"choice_id"`
		ChoiceText string      `json:"choice_text"`
		ChoiceRank interface{} `json:"choice_rank"`
	}{}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	// Handle ChoiceRank (string from Meltano, int/float64 from other sources)
	switch v := tmp.ChoiceRank.(type) {
	case string:
		if v != "" {
			val, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			r.ChoiceRank = val
		}
	case float64:
		r.ChoiceRank = int(v)
	case int:
		r.ChoiceRank = v
	default:
		if v != nil {
			return fmt.Errorf("invalid type for choice_rank: %T", v)
		}
	}

	// Assign other fields
	r.ChoiceID = tmp.ChoiceID
	r.ChoiceText = tmp.ChoiceText

	return nil
}

// PollResultDBRaw represents raw poll result data from v1 DynamoDB/NATS KV bucket.
// Numeric count fields may arrive as strings from the Meltano batch path, so
// custom UnmarshalJSON coerces them to int.
type PollResultDBRaw struct {
	PollID              string                  `json:"poll_id"`
	CommitteeID         string                  `json:"committee_id"`
	ProjectID           string                  `json:"project_id"`
	Status              string                  `json:"status"`
	NumRecipients       int                     `json:"num_recipients"`
	NumVotesCast        int                     `json:"num_votes_cast"`
	NumAbstained        int                     `json:"num_abstained"`
	PollEndTime         string                  `json:"poll_end_time"`
	CreatedTime         string                  `json:"created_time"`
	LastModifiedTime    string                  `json:"last_modified_time"`
	PollQuestionsResult []PollQuestionResultRaw `json:"poll_questions_result"`
}

// UnmarshalJSON coerces string/float64 representations of integer counts to int.
func (p *PollResultDBRaw) UnmarshalJSON(data []byte) error {
	tmp := struct {
		PollID              string                  `json:"poll_id"`
		CommitteeID         string                  `json:"committee_id"`
		ProjectID           string                  `json:"project_id"`
		Status              string                  `json:"status"`
		NumRecipients       interface{}             `json:"num_recipients"`
		NumVotesCast        interface{}             `json:"num_votes_cast"`
		NumAbstained        interface{}             `json:"num_abstained"`
		PollEndTime         string                  `json:"poll_end_time"`
		CreatedTime         string                  `json:"created_time"`
		LastModifiedTime    string                  `json:"last_modified_time"`
		PollQuestionsResult []PollQuestionResultRaw `json:"poll_questions_result"`
	}{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	p.PollID = tmp.PollID
	p.CommitteeID = tmp.CommitteeID
	p.ProjectID = tmp.ProjectID
	p.Status = tmp.Status
	p.PollEndTime = tmp.PollEndTime
	p.CreatedTime = tmp.CreatedTime
	p.LastModifiedTime = tmp.LastModifiedTime
	p.PollQuestionsResult = tmp.PollQuestionsResult
	var err error
	if p.NumRecipients, err = CoerceToInt(tmp.NumRecipients, "num_recipients"); err != nil {
		return err
	}
	if p.NumVotesCast, err = CoerceToInt(tmp.NumVotesCast, "num_votes_cast"); err != nil {
		return err
	}
	if p.NumAbstained, err = CoerceToInt(tmp.NumAbstained, "num_abstained"); err != nil {
		return err
	}
	return nil
}

// CoerceToInt converts string, float64, or int interface values to int, matching
// the pattern used by PollDBRaw for fields that Meltano may send as strings.
func CoerceToInt(v interface{}, field string) (int, error) {
	switch val := v.(type) {
	case string:
		if val == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("invalid string value for %s: %w", field, err)
		}
		return n, nil
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("invalid type for %s: %T", field, v)
	}
}

// PollQuestionResultRaw is the per-question tally from a v1 poll result snapshot.
type PollQuestionResultRaw struct {
	QuestionID    string            `json:"question_id"`
	Prompt        string            `json:"prompt"`
	ChoiceResults []ChoiceResultRaw `json:"choice_results"`
}

// ChoiceResultRaw is the per-choice tally; VoteCount may arrive as string from Meltano.
type ChoiceResultRaw struct {
	ChoiceID   string      `json:"choice_id"`
	ChoiceText string      `json:"choice_text"`
	VoteCount  interface{} `json:"vote_count"`
	Percentage float64     `json:"percentage"`
}

// PollQuestionResult is the v2 per-question tally with proper int VoteCount.
type PollQuestionResult struct {
	QuestionID    string        `json:"question_id"`
	Prompt        string        `json:"prompt"`
	ChoiceResults []ChoiceResult `json:"choice_results"`
}

// ChoiceResult is the v2 per-choice tally.
type ChoiceResult struct {
	ChoiceID   string  `json:"choice_id"`
	ChoiceText string  `json:"choice_text"`
	VoteCount  int     `json:"vote_count"`
	Percentage float64 `json:"percentage"`
}

//
// V2 Transformed Data Models (after parsing and ID mapping)
//

// PollResultData represents v2 poll result data after transformation.
type PollResultData struct {
	VoteUID             string               `json:"vote_uid"`      // v2 primary key (same as PollID)
	PollID              string               `json:"poll_id"`       // v1 primary key
	CommitteeID         string               `json:"committee_id"`  // v1 SFID
	CommitteeUID        string               `json:"committee_uid"` // v2 UID
	ProjectID           string               `json:"project_id"`    // v1 SFID
	ProjectUID          string               `json:"project_uid"`   // v2 UID
	Status              string               `json:"status"`
	NumRecipients       int                  `json:"num_recipients"`
	NumVotesCast        int                  `json:"num_votes_cast"`
	NumAbstained        int                  `json:"num_abstained"`
	PollEndTime         string               `json:"poll_end_time"`
	CreatedTime         string               `json:"created_time"`
	LastModifiedTime    string               `json:"last_modified_time"`
	PollQuestionsResult []PollQuestionResult `json:"poll_questions_result"`
}

// VoteData represents v2 vote (poll) data after transformation
type VoteData struct {
	VoteUID                       string                   `json:"vote_uid"` // v2 primary key (same as PollID)
	PollID                        string                   `json:"poll_id"`  // v1 primary key
	Name                          string                   `json:"name"`
	Description                   string                   `json:"description"`
	CreationTime                  string                   `json:"creation_time"`
	LastModifiedTime              string                   `json:"last_modified_time"`
	EndTime                       string                   `json:"end_time"`
	EarlyEndTime                  string                   `json:"early_end_time,omitempty"`
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
	LastModifiedTime        string           `json:"last_modified_time"`
	UserID                  string           `json:"user_id"`
	UserEmail               string           `json:"user_email"`
	UserRole                string           `json:"user_role"`
	UserName                string           `json:"user_name"` // actual user's name
	Username                string           `json:"username"`  // LFX username
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
