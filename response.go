package stratum

import (
	"encoding/json"
)

type Response struct {
	MessageID *uint64          `json:"id"`
	Error     *StratumError    `json:"error"`
	Method    string           `json:"method"`
	Result    *json.RawMessage `json:"result,omitempty"`
	Params    *json.RawMessage `json:"params,omitempty"`
}

func (r *Response) String() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// OkResponse generates a response with the following format:
// {"id": "<request.MessageID>", "error": null, "result": {"status": "OK"}}
// func OkResponse(r *Request) (*Response, error) {
// 	return &Response{
// 		"2.0",
// 		r.MessageID,
// 		map[string]interface{}{
// 			"status": "OK",
// 		},
// 		nil,
// 	}, nil
// }
