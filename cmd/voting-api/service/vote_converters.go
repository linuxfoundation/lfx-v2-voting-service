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
		UID:                           poll.PollID,      // Map PollID → UID
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

// ConvertVoteResultsToResult converts ITX VoteResults to Goa VoteResultsResult
// This is mostly a pass-through conversion matching ITX structure
func ConvertVoteResultsToResult(results *itx.VoteResults) *votesvc.VoteResultsResult {
	result := &votesvc.VoteResultsResult{
		NumRecipients: results.NumRecipients,
		NumVotesCast:  results.NumVotesCast,
		NumAbstained:  results.NumAbstained,
		PollEndTime:   &results.PollEndTime,
	}

	// Convert poll results
	if len(results.PollResults) > 0 {
		result.PollResults = make([]*votesvc.PollResultItem, len(results.PollResults))
		for i, pr := range results.PollResults {
			pollResultItem := &votesvc.PollResultItem{
				Question: &votesvc.PollQuestionDetail{
					QuestionID: pr.Question.QuestionID,
					Prompt:     pr.Question.Prompt,
					Type:       pr.Question.Type,
				},
			}

			// Convert question choices
			if len(pr.Question.Choices) > 0 {
				pollResultItem.Question.Choices = make([]*votesvc.PollChoice, len(pr.Question.Choices))
				for j, c := range pr.Question.Choices {
					pollResultItem.Question.Choices[j] = &votesvc.PollChoice{
						ChoiceID:   utils.StringPtr(c.ChoiceID),
						ChoiceText: c.ChoiceText,
					}
				}
			}

			// Convert generic choice votes
			if len(pr.GenericChoiceVotes) > 0 {
				pollResultItem.GenericChoiceVotes = make([]*votesvc.GenericChoiceVote, len(pr.GenericChoiceVotes))
				for j, gcv := range pr.GenericChoiceVotes {
					pollResultItem.GenericChoiceVotes[j] = &votesvc.GenericChoiceVote{
						ChoiceID:   gcv.ChoiceID,
						VoteCount:  gcv.VoteCount,
						Percentage: gcv.Percentage,
					}
				}
			}

			// Convert ranked choice votes
			if len(pr.RankedChoiceVotes) > 0 {
				pollResultItem.RankedChoiceVotes = make([]*votesvc.RankedChoiceVote, len(pr.RankedChoiceVotes))
				for j, rcv := range pr.RankedChoiceVotes {
					rankedVote := &votesvc.RankedChoiceVote{
						ChoiceID: rcv.ChoiceID,
					}

					// Convert rank counts
					if len(rcv.RankCounts) > 0 {
						rankedVote.RankCounts = make([]*votesvc.RankCount, len(rcv.RankCounts))
						for k, rc := range rcv.RankCounts {
							rankedVote.RankCounts[k] = &votesvc.RankCount{
								Rank:  rc.Rank,
								Count: rc.Count,
							}
						}
					}

					// Condorcet matrix as Any type - just pass through
					rankedVote.CondorcetMatrix = rcv.CondorcetMatrix

					pollResultItem.RankedChoiceVotes[j] = rankedVote
				}
			}

			// Convert ranked choice winner info
			if pr.RankedChoiceWinnerInfo != nil {
				pollResultItem.RankedChoiceWinnerInfo = &votesvc.RankedChoiceWinnerInfo{
					CondorcetIrvUsedForEliminations: &pr.RankedChoiceWinnerInfo.CondorcetIRVUsedForEliminations,
				}

				if len(pr.RankedChoiceWinnerInfo.PollChoices) > 0 {
					pollResultItem.RankedChoiceWinnerInfo.PollChoices = make([]*votesvc.PollChoice, len(pr.RankedChoiceWinnerInfo.PollChoices))
					for j, c := range pr.RankedChoiceWinnerInfo.PollChoices {
						pollResultItem.RankedChoiceWinnerInfo.PollChoices[j] = &votesvc.PollChoice{
							ChoiceID:   utils.StringPtr(c.ChoiceID),
							ChoiceText: c.ChoiceText,
						}
					}
				}
			}

			// IRV and Meek STV round summaries as Any type - just pass through
			pollResultItem.IrvRoundSummary = pr.IRVRoundSummary
			pollResultItem.MeekStvRoundSummary = pr.MeekSTVRoundSummary

			result.PollResults[i] = pollResultItem
		}
	}

	// Convert comment results
	if len(results.CommentResults) > 0 {
		result.CommentResults = make([]*votesvc.CommentResultItem, len(results.CommentResults))
		for i, cr := range results.CommentResults {
			result.CommentResults[i] = &votesvc.CommentResultItem{
				Prompt:   cr.Prompt,
				Comments: cr.Comments,
			}
		}
	}

	return result
}
