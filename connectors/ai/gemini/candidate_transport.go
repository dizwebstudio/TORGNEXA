package gemini

import (
	"context"
	"encoding/json"
)

type candidateTransport struct{}

func (candidateTransport) Do(context.Context, Request) (Response, error) {
	raw, _ := json.Marshal(generateResponse{Candidates: []candidate{{Content: content{Parts: []part{{Text: "ok"}}}}}, ModelVersion: DefaultHealthModel})
	return Response{StatusCode: 200, Body: raw}, nil
}
