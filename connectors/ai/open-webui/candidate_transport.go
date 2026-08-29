package openwebui

import (
	"context"
	"encoding/json"
)

type candidateTransport struct{}

func (candidateTransport) Do(context.Context, Request) (Response, error) {
	raw, _ := json.Marshal(chatResponse{Model: DefaultHealthModel, Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "ok"}}}})
	return Response{StatusCode: 200, Body: raw}, nil
}
