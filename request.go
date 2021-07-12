package stratum

import "encoding/json"

type Request struct {
	MessageID    *uint64     `json:"id"`
	RemoteMethod string      `json:"method"`
	Parameters   interface{} `json:"params"`
}

func NewRequest(id *uint64, method string, args interface{}) *Request {
	return &Request{
		id,
		method,
		args,
	}
}

func (r *Request) JsonRPCString() (string, error) {
	payload := make(map[string]interface{})
	payload["jsonrpc"] = "2.0"
	payload["method"] = r.RemoteMethod
	payload["id"] = r.MessageID
	payload["params"] = r.Parameters

	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil

}
