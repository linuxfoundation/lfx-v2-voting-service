// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	voting "github.com/linuxfoundation/lfx-v2-voting-service/gen/voting"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// ConvertCreateVotePayloadToDomain converts Goa CreateVotePayload to service CreateVoteRequest
func ConvertCreateVotePayloadToDomain(payload *voting.CreateVotePayload) *service.CreateVoteRequest {
	req := &service.CreateVoteRequest{
		Name:             payload.Name,
		Description:      payload.Description,
		EndTime:          payload.EndTime,
		ProjectID:        payload.ProjectID,
		CommitteeID:      payload.CommitteeID,
		CommitteeFilters: payload.CommitteeFilters,
		PseudoAnonymity:  payload.PseudoAnonymity,
		PollType:         payload.PollType,
		NumWinners:       utils.IntPtr(payload.NumWinners),
		AllowAbstain:     payload.AllowAbstain,
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

	return req
}
