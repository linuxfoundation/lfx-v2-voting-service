// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	voting "github.com/linuxfoundation/lfx-v2-voting-service/gen/voting"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// ConvertPollResponseToVoteResult converts domain PollResponse to Goa VoteResult
func ConvertPollResponseToVoteResult(poll *domain.PollResponse) *voting.VoteResult {
	result := &voting.VoteResult{
		PollID:                        poll.PollID,
		Name:                          poll.Name,
		Description:                   poll.Description,
		CreationTime:                  utils.StringPtr(poll.CreationTime),
		LastModifiedTime:              utils.StringPtr(poll.LastModifiedTime),
		EndTime:                       utils.StringPtr(poll.EndTime),
		Status:                        poll.Status,
		ProjectID:                     poll.ProjectID,
		CommitteeID:                   poll.CommitteeID,
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
		questions := make([]*voting.PollQuestion, len(poll.PollQuestions))
		for i, q := range poll.PollQuestions {
			choices := make([]*voting.PollChoice, len(q.Choices))
			for j, c := range q.Choices {
				choices[j] = &voting.PollChoice{
					ChoiceID:   utils.StringPtr(c.ChoiceID),
					ChoiceText: c.ChoiceText,
				}
			}
			questions[i] = &voting.PollQuestion{
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
