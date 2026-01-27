// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// ConvertCreateVotePayloadToDomain converts Goa CreateVotePayload to service CreateVoteRequest
func ConvertCreateVotePayloadToDomain(payload *votesvc.CreateVotePayload) *service.CreateVoteRequest {
	req := &service.CreateVoteRequest{
		Name:                        payload.Name,
		Description:                 payload.Description,
		EndTime:                     payload.EndTime,
		ProjectUID:                  payload.ProjectUID,
		CommitteeUID:                payload.CommitteeUID,
		CommitteeUIDs:               payload.CommitteeUids,
		CommitteeFilters:            payload.CommitteeFilters,
		PseudoAnonymity:             payload.PseudoAnonymity,
		PollType:                    payload.PollType,
		NumWinners:                  utils.IntPtr(payload.NumWinners),
		AllowAbstain:                payload.AllowAbstain,
		QuorumPercentage:            payload.QuorumPercentage,
		WinningThresholdPercentage:  payload.WinningThresholdPercentage,
	}

	// Convert poll questions
	if len(payload.PollQuestions) > 0 {
		req.PollQuestions = make([]service.PollQuestionRequest, len(payload.PollQuestions))
		for i, q := range payload.PollQuestions {
			choices := make([]service.PollChoiceRequest, len(q.Choices))
			for j, c := range q.Choices {
				choices[j] = service.PollChoiceRequest{
					ChoiceText: c.ChoiceText,
				}
			}
			req.PollQuestions[i] = service.PollQuestionRequest{
				Prompt:  q.Prompt,
				Type:    q.Type,
				Choices: choices,
			}
		}
	}

	// Convert poll comment prompts
	if len(payload.PollCommentPrompts) > 0 {
		req.PollCommentPrompts = make([]service.PollCommentPromptRequest, len(payload.PollCommentPrompts))
		for i, p := range payload.PollCommentPrompts {
			req.PollCommentPrompts[i] = service.PollCommentPromptRequest{
				Prompt: p.Prompt,
			}
		}
	}

	return req
}

// ConvertUpdateVotePayloadToDomain converts Goa UpdateVotePayload to service UpdateVoteRequest
func ConvertUpdateVotePayloadToDomain(payload *votesvc.UpdateVotePayload) *service.UpdateVoteRequest {
	req := &service.UpdateVoteRequest{
		Name:                        payload.Name,
		Description:                 payload.Description,
		EndTime:                     payload.EndTime,
		CommitteeUIDs:               payload.CommitteeUids,
		CommitteeFilters:            payload.CommitteeFilters,
		PseudoAnonymity:             payload.PseudoAnonymity,
		PollType:                    payload.PollType,
		NumWinners:                  utils.IntPtr(payload.NumWinners),
		AllowAbstain:                payload.AllowAbstain,
		QuorumPercentage:            payload.QuorumPercentage,
		WinningThresholdPercentage:  payload.WinningThresholdPercentage,
	}

	// Add optional fields if present
	if payload.ProjectUID != nil {
		req.ProjectUID = *payload.ProjectUID
	}
	if payload.CommitteeUID != nil {
		req.CommitteeUID = *payload.CommitteeUID
	}

	// Convert poll questions
	if len(payload.PollQuestions) > 0 {
		req.PollQuestions = make([]service.PollQuestionRequest, len(payload.PollQuestions))
		for i, q := range payload.PollQuestions {
			choices := make([]service.PollChoiceRequest, len(q.Choices))
			for j, c := range q.Choices {
				choices[j] = service.PollChoiceRequest{
					ChoiceText: c.ChoiceText,
				}
			}
			req.PollQuestions[i] = service.PollQuestionRequest{
				Prompt:  q.Prompt,
				Type:    q.Type,
				Choices: choices,
			}
		}
	}

	// Convert poll comment prompts
	if len(payload.PollCommentPrompts) > 0 {
		req.PollCommentPrompts = make([]service.PollCommentPromptRequest, len(payload.PollCommentPrompts))
		for i, p := range payload.PollCommentPrompts {
			req.PollCommentPrompts[i] = service.PollCommentPromptRequest{
				Prompt: p.Prompt,
			}
		}
	}

	return req
}

// ConvertPollResponseToVoteResult converts ITX PollResponse to Goa VoteResult
func ConvertPollResponseToVoteResult(poll *itx.PollResponse) *votesvc.VoteResult {
	result := &votesvc.VoteResult{
		VoteUID:                       poll.PollID,      // Map PollID → VoteUID
		Name:                          poll.Name,
		Description:                   poll.Description,
		CreationTime:                  utils.StringPtr(poll.CreationTime),
		LastModifiedTime:              utils.StringPtr(poll.LastModifiedTime),
		EndTime:                       utils.StringPtr(poll.EndTime),
		Status:                        poll.Status,
		ProjectUID:                    poll.ProjectID,   // Map ProjectID → ProjectUID
		CommitteeUID:                  poll.CommitteeID, // Map CommitteeID → CommitteeUID
		CommitteeName:                 utils.StringPtr(poll.CommitteeName),
		CommitteeType:                 utils.StringPtr(poll.CommitteeType),
		CommitteeVotingStatus:         utils.BoolPtr(poll.CommitteeVotingStatus),
		PseudoAnonymity:               utils.BoolPtr(poll.PseudoAnonymity),
		TotalVotingRequestInvitations: utils.IntPtr(poll.TotalVotingRequestInvitations),
		NumResponseReceived:           utils.IntPtr(poll.NumResponseReceived),
		AllowAbstain:                  utils.BoolPtr(poll.AllowAbstain),
	}

	// Convert poll questions
	if len(poll.PollQuestions) > 0 {
		questions := make([]*votesvc.PollQuestion, len(poll.PollQuestions))
		for i, q := range poll.PollQuestions {
			choices := make([]*votesvc.PollChoice, len(q.Choices))
			for j, c := range q.Choices {
				choices[j] = &votesvc.PollChoice{
					ChoiceID:   utils.StringPtr(c.ChoiceID),
					ChoiceText: c.ChoiceText,
				}
			}
			questions[i] = &votesvc.PollQuestion{
				QuestionID: utils.StringPtr(q.QuestionID),
				Prompt:     q.Prompt,
				Type:       q.Type,
				Choices:    choices,
			}
		}
		result.PollQuestions = questions
	}

	return result
}
