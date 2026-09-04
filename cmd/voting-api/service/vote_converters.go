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
		Name:                       payload.Name,
		Description:                payload.Description,
		EndTime:                    payload.EndTime,
		ProjectUID:                 payload.ProjectUID,
		CommitteeUID:               payload.CommitteeUID,
		CommitteeUIDs:              payload.CommitteeUids,
		CommitteeFilters:           payload.CommitteeFilters,
		PseudoAnonymity:            payload.PseudoAnonymity,
		PollType:                   payload.PollType,
		NumWinners:                 utils.IntPtr(payload.NumWinners),
		AllowAbstain:               payload.AllowAbstain,
		QuorumPercentage:           payload.QuorumPercentage,
		WinningThresholdPercentage: payload.WinningThresholdPercentage,
	}

	// Add optional fields if present
	if payload.EndTimeTimezone != nil {
		req.EndTimeTimezone = *payload.EndTimeTimezone
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
		Name:                       payload.Name,
		Description:                payload.Description,
		EndTime:                    payload.EndTime,
		CommitteeUIDs:              payload.CommitteeUids,
		CommitteeFilters:           payload.CommitteeFilters,
		PseudoAnonymity:            payload.PseudoAnonymity,
		PollType:                   payload.PollType,
		NumWinners:                 utils.IntPtr(payload.NumWinners),
		AllowAbstain:               payload.AllowAbstain,
		QuorumPercentage:           payload.QuorumPercentage,
		WinningThresholdPercentage: payload.WinningThresholdPercentage,
	}

	// Add optional fields if present
	if payload.ProjectUID != nil {
		req.ProjectUID = *payload.ProjectUID
	}
	if payload.CommitteeUID != nil {
		req.CommitteeUID = *payload.CommitteeUID
	}
	if payload.EndTimeTimezone != nil {
		req.EndTimeTimezone = *payload.EndTimeTimezone
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
		UID:                           poll.PollID, // Map PollID → UID
		Name:                          poll.Name,
		Description:                   poll.Description,
		CreationTime:                  utils.StringPtr(poll.CreationTime),
		LastModifiedTime:              utils.StringPtr(poll.LastModifiedTime),
		EndTime:                       utils.StringPtr(poll.EndTime),
		Status:                        poll.Status,
		ProjectUID:                    poll.ProjectID, // Map ProjectID → ProjectUID
		ProjectName:                   utils.StringPtr(poll.ProjectName),
		CommitteeUID:                  poll.CommitteeID, // Map CommitteeID → CommitteeUID
		CommitteeName:                 utils.StringPtr(poll.CommitteeName),
		CommitteeFilters:              poll.CommitteeFilters,
		CommitteeType:                 utils.StringPtr(poll.CommitteeType),
		CommitteeVotingStatus:         utils.BoolPtr(poll.CommitteeVotingStatus),
		PseudoAnonymity:               utils.BoolPtr(poll.PseudoAnonymity),
		TotalVotingRequestInvitations: utils.IntPtr(poll.TotalVotingRequestInvitations),
		NumResponseReceived:           utils.IntPtr(poll.NumResponseReceived),
		PollType:                      poll.PollType,
		NumWinners:                    utils.IntPtr(poll.NumWinners),
		AllowAbstain:                  utils.BoolPtr(poll.AllowAbstain),
	}
	if v := utils.NormalizeTimestamp(poll.EarlyEndTime); v != "" {
		result.EarlyEndTime = utils.StringPtr(v)
	}
	if poll.EndTimeTimezone != "" {
		result.EndTimeTimezone = utils.StringPtr(poll.EndTimeTimezone)
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

	// Convert poll comment prompts
	if len(poll.PollCommentPrompts) > 0 {
		prompts := make([]*votesvc.PollCommentPrompt, len(poll.PollCommentPrompts))
		for i, p := range poll.PollCommentPrompts {
			prompts[i] = &votesvc.PollCommentPrompt{
				PromptID: p.PromptID,
				Prompt:   p.Prompt,
			}
		}
		result.PollCommentPrompts = prompts
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
	if results.PollEndTimeTimezone != "" {
		result.PollEndTimeTimezone = utils.StringPtr(results.PollEndTimeTimezone)
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

					// Convert Condorcet matrix entries
					if len(rcv.CondorcetMatrix) > 0 {
						rankedVote.CondorcetMatrix = make([]*votesvc.CondorcetMatrixEntry, len(rcv.CondorcetMatrix))
						for k, cm := range rcv.CondorcetMatrix {
							rankedVote.CondorcetMatrix[k] = &votesvc.CondorcetMatrixEntry{
								ChoiceID:          cm.ChoiceID,
								OtherChoiceID:     cm.OtherChoiceID,
								ChoiceIDWins:      cm.ChoiceIDWins,
								OtherChoiceIDWins: cm.OtherChoiceIDWins,
								Result:            cm.Result,
							}
						}
					}

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

			// Convert IRV round summary
			if len(pr.IRVRoundSummary) > 0 {
				pollResultItem.IrvRoundSummary = make([]*votesvc.IRVRoundSummary, len(pr.IRVRoundSummary))
				for j, irv := range pr.IRVRoundSummary {
					irvRound := &votesvc.IRVRoundSummary{
						RoundNumber:        irv.RoundNumber,
						TotalVotes:         irv.TotalVotes,
						MinVotes:           &irv.MinVotes,
						ExhaustedVotes:     irv.ExhaustedVotes,
						Threshold:          irv.Threshold,
						EliminatedChoiceID: &irv.EliminatedChoiceID,
						Message:            irv.Message,
					}

					// Convert votes
					if len(irv.Votes) > 0 {
						irvRound.Votes = make([]*votesvc.VoteCount, len(irv.Votes))
						for k, v := range irv.Votes {
							irvRound.Votes[k] = &votesvc.VoteCount{
								ChoiceID:   v.ChoiceID,
								VoteCount:  v.VoteCount,
								Percentage: &v.Percentage,
							}
						}
					}

					// Convert transferred votes
					if len(irv.TransferredVotes) > 0 {
						irvRound.TransferredVotes = make([]*votesvc.VoteCount, len(irv.TransferredVotes))
						for k, v := range irv.TransferredVotes {
							irvRound.TransferredVotes[k] = &votesvc.VoteCount{
								ChoiceID:   v.ChoiceID,
								VoteCount:  v.VoteCount,
								Percentage: &v.Percentage,
							}
						}
					}

					pollResultItem.IrvRoundSummary[j] = irvRound
				}
			}

			// Convert Meek STV round summary
			if len(pr.MeekSTVRoundSummary) > 0 {
				pollResultItem.MeekStvRoundSummary = make([]*votesvc.MeekSTVRoundSummary, len(pr.MeekSTVRoundSummary))
				for j, meek := range pr.MeekSTVRoundSummary {
					meekRound := &votesvc.MeekSTVRoundSummary{
						RoundNumber:    meek.RoundNumber,
						TotalVotes:     meek.TotalVotes,
						ExhaustedVotes: meek.ExhaustedVotes,
						Threshold:      meek.Threshold,
						Message:        meek.Message,
					}

					// Convert votes
					if len(meek.Votes) > 0 {
						meekRound.Votes = make([]*votesvc.MeekSTVVoteCount, len(meek.Votes))
						for k, v := range meek.Votes {
							meekRound.Votes[k] = &votesvc.MeekSTVVoteCount{
								ChoiceID:  v.ChoiceID,
								VoteCount: v.VoteCount,
							}
						}
					}

					// Convert elected choices
					if len(meek.ElectedChoices) > 0 {
						meekRound.ElectedChoices = make([]*votesvc.MeekSTVElectedChoice, len(meek.ElectedChoices))
						for k, v := range meek.ElectedChoices {
							meekRound.ElectedChoices[k] = &votesvc.MeekSTVElectedChoice{
								ChoiceID:     v.ChoiceID,
								VoteCount:    v.VoteCount,
								SurplusVotes: v.SurplusVotes,
							}
						}
					}

					// Convert eliminated choices
					if len(meek.EliminatedChoices) > 0 {
						meekRound.EliminatedChoices = make([]*votesvc.MeekSTVVoteCount, len(meek.EliminatedChoices))
						for k, v := range meek.EliminatedChoices {
							meekRound.EliminatedChoices[k] = &votesvc.MeekSTVVoteCount{
								ChoiceID:  v.ChoiceID,
								VoteCount: v.VoteCount,
							}
						}
					}

					// Convert transferred votes
					if len(meek.TransferredVotes) > 0 {
						meekRound.TransferredVotes = make([]*votesvc.MeekSTVVoteCount, len(meek.TransferredVotes))
						for k, v := range meek.TransferredVotes {
							meekRound.TransferredVotes[k] = &votesvc.MeekSTVVoteCount{
								ChoiceID:  v.ChoiceID,
								VoteCount: v.VoteCount,
							}
						}
					}

					// Convert surplus votes
					if len(meek.SurplusVotes) > 0 {
						meekRound.SurplusVotes = make([]*votesvc.MeekSTVVoteCount, len(meek.SurplusVotes))
						for k, v := range meek.SurplusVotes {
							meekRound.SurplusVotes[k] = &votesvc.MeekSTVVoteCount{
								ChoiceID:  v.ChoiceID,
								VoteCount: v.VoteCount,
							}
						}
					}

					pollResultItem.MeekStvRoundSummary[j] = meekRound
				}
			}

			result.PollResults[i] = pollResultItem
		}
	}

	// Convert comment results
	if len(results.CommentResults) > 0 {
		result.CommentResults = make([]*votesvc.CommentResultItem, len(results.CommentResults))
		for i, cr := range results.CommentResults {
			commentResultItem := &votesvc.CommentResultItem{
				Prompt: &votesvc.PollCommentPrompt{
					PromptID: cr.Prompt.PromptID,
					Prompt:   cr.Prompt.Prompt,
				},
			}

			if len(cr.Responses) > 0 {
				commentResultItem.Responses = make([]*votesvc.CommentResponseWithUser, len(cr.Responses))
				for j, r := range cr.Responses {
					response := &votesvc.CommentResponseWithUser{
						VoteID:           r.VoteID,
						CommentText:      r.CommentText,
						VoteCreationTime: r.VoteCreationTime,
						Abstained:        r.Abstained,
					}
					// user_id, user_name, and profile_picture are absent (not empty string) under pseudo_anonymity.
					if r.UserID != "" {
						response.UserID = utils.StringPtr(r.UserID)
					}
					if r.UserName != "" {
						response.UserName = utils.StringPtr(r.UserName)
					}
					if r.ProfilePicture != "" {
						response.ProfilePicture = utils.StringPtr(r.ProfilePicture)
					}
					commentResultItem.Responses[j] = response
				}
			}

			result.CommentResults[i] = commentResultItem
		}
	}

	return result
}
