// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// ConvertCreateVoteResponsePayloadToDomain converts Goa payload to service request
func ConvertCreateVoteResponsePayloadToDomain(payload *votesvc.CreateVoteResponsePayload) *service.CreateVoteResponseRequest {
	req := &service.CreateVoteResponseRequest{
		VoteID:          payload.VoteResponseID,
		UserVoteContent: make([]service.VoteAnswerRequest, 0),
		Abstain:         payload.Abstain,
	}

	if payload.UserVoteContent != nil {
		req.UserVoteContent = make([]service.VoteAnswerRequest, len(payload.UserVoteContent))
		for i, answer := range payload.UserVoteContent {
			req.UserVoteContent[i] = service.VoteAnswerRequest{
				QuestionID: answer.QuestionID,
				ChoiceIDs:  answer.ChoiceIds,
			}

			if answer.RankedChoices != nil {
				req.UserVoteContent[i].RankedChoices = make([]service.RankedChoiceRequest, len(answer.RankedChoices))
				for j, rc := range answer.RankedChoices {
					req.UserVoteContent[i].RankedChoices[j] = service.RankedChoiceRequest{
						ChoiceID:   rc.ChoiceID,
						ChoiceRank: rc.ChoiceRank,
					}
				}
			}
		}
	}

	return req
}

// ConvertUpdateVoteResponsePayloadToDomain converts Goa payload to service request
func ConvertUpdateVoteResponsePayloadToDomain(payload *votesvc.UpdateVoteResponsePayload) *service.UpdateVoteResponseRequest {
	req := &service.UpdateVoteResponseRequest{
		UserVoteContent: make([]service.VoteAnswerRequest, 0),
		Abstain:         payload.Abstain,
	}

	if payload.UserVoteContent != nil {
		req.UserVoteContent = make([]service.VoteAnswerRequest, len(payload.UserVoteContent))
		for i, answer := range payload.UserVoteContent {
			req.UserVoteContent[i] = service.VoteAnswerRequest{
				QuestionID: answer.QuestionID,
				ChoiceIDs:  answer.ChoiceIds,
			}

			if answer.RankedChoices != nil {
				req.UserVoteContent[i].RankedChoices = make([]service.RankedChoiceRequest, len(answer.RankedChoices))
				for j, rc := range answer.RankedChoices {
					req.UserVoteContent[i].RankedChoices[j] = service.RankedChoiceRequest{
						ChoiceID:   rc.ChoiceID,
						ChoiceRank: rc.ChoiceRank,
					}
				}
			}
		}
	}

	return req
}

// ConvertVoteResponseToResult converts ITX response to Goa result
func ConvertVoteResponseToResult(resp *itx.VoteResponse) *votesvc.VoteResponseResult {
	result := &votesvc.VoteResponseResult{
		VoteID:                   resp.VoteID,
		PollID:                   resp.PollID,
		ProjectID:                resp.ProjectID,
		VoteStatus:               resp.VoteStatus,
		Abstained:                utils.BoolPtr(resp.Abstained),
		AllowAbstain:             utils.BoolPtr(resp.AllowAbstain),
		VoteCreationTime:         &resp.VoteCreationTime,
		UserName:                 &resp.UserName,
		ProfilePicture:           &resp.ProfilePicture,
		UserID:                   &resp.UserID,
		UserEmail:                &resp.UserEmail,
		UserRole:                 &resp.UserRole,
		UserVotingStatus:         &resp.UserVotingStatus,
		UserOrgName:              &resp.UserOrgName,
		UserOrgID:                &resp.UserOrgID,
		SesMessageID:             &resp.SESMessageID,
		SesMessageLastSentTime:   &resp.SESMessageLastSentTime,
		SesComplaintExists:       utils.BoolPtr(resp.SESComplaintExists),
		SesDeliverySuccessful:    utils.BoolPtr(resp.SESDeliverySuccessful),
		SesEmailOpened:           utils.BoolPtr(resp.SESEmailOpened),
		SesLinkClicked:           utils.BoolPtr(resp.SESLinkClicked),
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

	return result
}
