// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// ConvertCreateVoteResponsePayloadToDomain converts Goa payload to service request.
// Returns a validation error (400) when comment_responses contains null entries —
// the generated validator skips nil array elements, so they are rejected here
// before they can cause a nil dereference.
func ConvertCreateVoteResponsePayloadToDomain(payload *votesvc.CreateVoteResponsePayload) (*service.CreateVoteResponseRequest, error) {
	req := &service.CreateVoteResponseRequest{
		VoteID:  payload.VoteResponseUID,
		Abstain: payload.Abstain,
	}

	answers, err := convertVoteAnswers(payload.UserVoteContent)
	if err != nil {
		return nil, err
	}
	req.UserVoteContent = answers

	comments, err := convertCommentResponses(payload.CommentResponses)
	if err != nil {
		return nil, err
	}
	req.CommentResponses = comments

	return req, nil
}

// ConvertUpdateVoteResponsePayloadToDomain converts Goa payload to service request.
// Returns a validation error (400) when comment_responses contains null entries —
// the generated validator skips nil array elements, so they are rejected here
// before they can cause a nil dereference.
func ConvertUpdateVoteResponsePayloadToDomain(payload *votesvc.UpdateVoteResponsePayload) (*service.UpdateVoteResponseRequest, error) {
	req := &service.UpdateVoteResponseRequest{
		Abstain: payload.Abstain,
	}

	answers, err := convertVoteAnswers(payload.UserVoteContent)
	if err != nil {
		return nil, err
	}
	req.UserVoteContent = answers

	// Convert comment responses. A nil payload.CommentResponses means the field
	// was omitted (preserve existing comments); a non-nil value (including an
	// empty slice) means it was included (validate and replace them).
	if payload.CommentResponses != nil {
		converted, err := convertCommentResponses(payload.CommentResponses)
		if err != nil {
			return nil, err
		}
		req.CommentResponses = &converted
	}

	return req, nil
}

// ConvertCreateVoteResponseByLinkPayloadToDomain converts the vote-link submit payload to a
// service request. VoteID is left empty: the service sets it from the verified token so the
// ballot can only come from the link, never from the request body.
func ConvertCreateVoteResponseByLinkPayloadToDomain(payload *votesvc.CreateVoteResponseByLinkPayload) (*service.CreateVoteResponseRequest, error) {
	answers, err := convertVoteAnswers(payload.UserVoteContent)
	if err != nil {
		return nil, err
	}
	comments, err := convertCommentResponses(payload.CommentResponses)
	if err != nil {
		return nil, err
	}
	return &service.CreateVoteResponseRequest{
		UserVoteContent:  answers,
		Abstain:          payload.Abstain,
		CommentResponses: comments,
	}, nil
}

// ConvertUpdateVoteResponseByLinkPayloadToDomain converts the vote-link update payload to a
// service request, keeping the omitted-vs-empty comment_responses distinction of the
// authenticated update.
func ConvertUpdateVoteResponseByLinkPayloadToDomain(payload *votesvc.UpdateVoteResponseByLinkPayload) (*service.UpdateVoteResponseRequest, error) {
	answers, err := convertVoteAnswers(payload.UserVoteContent)
	if err != nil {
		return nil, err
	}
	req := &service.UpdateVoteResponseRequest{
		UserVoteContent: answers,
		Abstain:         payload.Abstain,
	}
	if payload.CommentResponses != nil {
		converted, err := convertCommentResponses(payload.CommentResponses)
		if err != nil {
			return nil, err
		}
		req.CommentResponses = &converted
	}
	return req, nil
}

// convertVoteAnswers converts Goa vote answers to service requests. It returns a validation
// error (400) for null answers or null ranked choices — the generated validator skips nil
// array elements, so they are rejected here before they can cause a nil dereference. A nil
// input yields an empty, non-nil slice.
func convertVoteAnswers(answers []*votesvc.VoteAnswerInput) ([]service.VoteAnswerRequest, error) {
	converted := make([]service.VoteAnswerRequest, len(answers))
	for i, answer := range answers {
		if answer == nil {
			return nil, domain.NewValidationError(fmt.Sprintf("user_vote_content[%d] must not be null", i))
		}
		converted[i] = service.VoteAnswerRequest{
			QuestionID: answer.QuestionID,
			ChoiceIDs:  answer.ChoiceIds,
		}

		if answer.RankedChoices != nil {
			converted[i].RankedChoices = make([]service.RankedChoiceRequest, len(answer.RankedChoices))
			for j, rc := range answer.RankedChoices {
				if rc == nil {
					return nil, domain.NewValidationError(fmt.Sprintf("user_vote_content[%d].ranked_choices[%d] must not be null", i, j))
				}
				converted[i].RankedChoices[j] = service.RankedChoiceRequest{
					ChoiceID:   rc.ChoiceID,
					ChoiceRank: rc.ChoiceRank,
				}
			}
		}
	}
	return converted, nil
}

// convertCommentResponses converts Goa comment responses to service requests. It returns a
// validation error (400) for null entries — the generated validator skips nil array
// elements, so they are rejected here before they can cause a nil dereference. A nil input
// yields a nil slice.
func convertCommentResponses(comments []*votesvc.CommentResponse) ([]service.CommentResponseRequest, error) {
	if comments == nil {
		return nil, nil
	}
	converted := make([]service.CommentResponseRequest, len(comments))
	for i, c := range comments {
		if c == nil {
			return nil, domain.NewValidationError(fmt.Sprintf("comment_responses[%d] must not be null", i))
		}
		converted[i] = service.CommentResponseRequest{
			PromptID:    c.PromptID,
			CommentText: c.CommentText,
		}
	}
	return converted, nil
}

// ConvertVoteResponseToResult converts ITX response to Goa result
func ConvertVoteResponseToResult(resp *itx.VoteResponse) *votesvc.VoteResponseResult {
	result := &votesvc.VoteResponseResult{
		VoteResponseUID:        resp.VoteID,
		VoteUID:                resp.PollID,
		ProjectUID:             resp.ProjectID,
		VoteStatus:             resp.VoteStatus,
		Abstained:              utils.BoolPtr(resp.Abstained),
		AllowAbstain:           utils.BoolPtr(resp.AllowAbstain),
		VoteCreationTime:       &resp.VoteCreationTime,
		UserName:               &resp.UserName,
		ProfilePicture:         &resp.ProfilePicture,
		UserID:                 &resp.UserID,
		UserEmail:              &resp.UserEmail,
		UserRole:               &resp.UserRole,
		UserVotingStatus:       &resp.UserVotingStatus,
		UserOrgName:            &resp.UserOrgName,
		UserOrgID:              &resp.UserOrgID,
		SesMessageID:           &resp.SESMessageID,
		SesMessageLastSentTime: &resp.SESMessageLastSentTime,
		SesComplaintExists:     utils.BoolPtr(resp.SESComplaintExists),
		SesDeliverySuccessful:  utils.BoolPtr(resp.SESDeliverySuccessful),
		SesEmailOpened:         utils.BoolPtr(resp.SESEmailOpened),
		SesLinkClicked:         utils.BoolPtr(resp.SESLinkClicked),
	}

	// Handle optional SES fields
	if resp.SESBounceType != "" {
		result.SesBounceType = &resp.SESBounceType
	}
	if resp.SESBounceSubtype != "" {
		result.SesBounceSubtype = &resp.SESBounceSubtype
	}
	if resp.SESComplaintType != "" {
		result.SesComplaintType = &resp.SESComplaintType
	}
	if resp.SESComplaintDate != "" {
		result.SesComplaintDate = &resp.SESComplaintDate
	}
	if resp.SESEmailOpenedLastTime != "" {
		result.SesEmailOpenedLastTime = &resp.SESEmailOpenedLastTime
	}
	if resp.SESLinkClickedLastTime != "" {
		result.SesLinkClickedLastTime = &resp.SESLinkClickedLastTime
	}

	// Convert poll answers
	if resp.PollAnswers != nil {
		result.PollAnswers = make([]*votesvc.VoteAnswer, len(resp.PollAnswers))
		for i, answer := range resp.PollAnswers {
			voteAnswer := &votesvc.VoteAnswer{
				QuestionID: answer.QuestionID,
				Prompt:     answer.Prompt,
				Type:       answer.Type,
			}

			// Convert regular choices
			if answer.UserChoice != nil {
				voteAnswer.UserChoice = make([]*votesvc.VoteChoiceAnswer, len(answer.UserChoice))
				for j, choice := range answer.UserChoice {
					voteAnswer.UserChoice[j] = &votesvc.VoteChoiceAnswer{
						ChoiceID:   choice.ChoiceID,
						ChoiceText: choice.ChoiceText,
					}
				}
			}

			// Convert ranked choices
			if answer.RankedUserChoice != nil {
				voteAnswer.RankedUserChoice = make([]*votesvc.RankedVoteChoiceAnswer, len(answer.RankedUserChoice))
				for j, choice := range answer.RankedUserChoice {
					voteAnswer.RankedUserChoice[j] = &votesvc.RankedVoteChoiceAnswer{
						ChoiceID:   choice.ChoiceID,
						ChoiceText: choice.ChoiceText,
						ChoiceRank: choice.ChoiceRank,
					}
				}
			}

			result.PollAnswers[i] = voteAnswer
		}
	}

	// Convert comment responses. Always an array (empty if the poll has no
	// comment prompts or the voter left none).
	result.CommentResponses = make([]*votesvc.CommentResponse, len(resp.CommentResponses))
	for i, c := range resp.CommentResponses {
		result.CommentResponses[i] = &votesvc.CommentResponse{
			PromptID:    c.PromptID,
			CommentText: c.CommentText,
		}
	}

	return result
}
