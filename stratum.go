package stratum

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	simplejson "github.com/bitly/go-simplejson"
	"github.com/fatih/set"
	log "github.com/sirupsen/logrus"
)

var (
	KeepAliveDuration time.Duration = 60 * time.Second
)

type StratumOnWorkHandler func(work *Work)
type StratumContext struct {
	net.Conn
	sync.Mutex
	reader                  *bufio.Reader
	id                      uint64
	SessionID               string
	KeepAliveDuration       time.Duration
	Work                    *Work
	Subscribe               *Subscribe
	workListeners           set.Interface
	submitListeners         set.Interface
	responseListeners       set.Interface
	LastSubmittedWork       *Work
	submittedWorkRequestIds set.Interface
	numAcceptedResults      uint64
	numSubmittedResults     uint64
	url                     string
	username                string
	password                string
	connected               bool
	lastReconnectTime       time.Time
	stopChan                chan struct{}
}

func New() *StratumContext {
	sc := &StratumContext{}
	sc.KeepAliveDuration = KeepAliveDuration
	sc.workListeners = set.New(set.ThreadSafe)
	sc.submitListeners = set.New(set.ThreadSafe)
	sc.responseListeners = set.New(set.ThreadSafe)
	sc.submittedWorkRequestIds = set.New(set.ThreadSafe)
	sc.stopChan = make(chan struct{})
	return sc
}

func (sc *StratumContext) Connect(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	log.Debugf("Dial success")
	sc.url = addr
	sc.Conn = conn
	sc.reader = bufio.NewReader(conn)
	return nil
}

// Call issues a JSONRPC request for the specified serviceMethod.
// It works by calling CallLocked while holding the StratumContext lock
func (sc *StratumContext) Call(serviceMethod string, args interface{}) (*Request, error) {
	sc.Lock()
	defer sc.Unlock()
	return sc.CallLocked(serviceMethod, args)
}

// CallLocked issues a JSONRPC request for the specified serviceMethod.
// The StratumContext lock is expected to be held by the caller
func (sc *StratumContext) CallLocked(serviceMethod string, args interface{}) (*Request, error) {
	sc.id++

	req := NewRequest(&sc.id, serviceMethod, args)
	str, err := req.JsonRPCString()
	if err != nil {
		return nil, err
	}

	if _, err := sc.Write([]byte(str)); err != nil {
		return nil, err
	}
	log.Debugf("Sent to server via conn: %v: %v", sc.Conn.LocalAddr(), str)
	return req, nil
}

func (sc *StratumContext) ReadLine() (string, error) {
	line, err := sc.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (sc *StratumContext) ReadJSON() (map[string]interface{}, error) {
	line, err := sc.reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var ret map[string]interface{}
	if err = json.Unmarshal([]byte(line), &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func (sc *StratumContext) ReadResponse() (*Response, error) {
	line, err := sc.ReadLine()
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	log.Debugf("Server sent back: %v", line)
	return ParseResponse([]byte(line))
}

func (sc *StratumContext) Authorize(username, password string) error {
	sc.Lock()
	defer sc.Unlock()
	return sc.authorizeLocked(username, password)
}

func (sc *StratumContext) authorizeLocked(username, password string) error {
	log.Debugf("Beginning authorize")
	args := []string{username, password}

	_, err := sc.CallLocked("mining.authorize", args)
	if err != nil {
		return err
	}

	log.Debugf("Triggered login..awaiting response")
	response, err := sc.ReadResponse()
	if err != nil {
		return err
	}
	if response.Error != nil {
		return response.Error
	}

	var ok bool
	if err := json.Unmarshal(*response.Result, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("auth fail: %v", response.String())
	}

	sc.connected = true
	sc.username = username
	sc.password = password

	// _, err := sc.Call("mining.subscribe", []string{})
	_, err = sc.CallLocked("mining.subscribe", []string{})
	if err != nil {
		return err
	}

	log.Debugf("Triggered subscription..awaiting response")
	response, err = sc.ReadResponse()
	if err != nil {
		return err
	}
	if response.Error != nil {
		return response.Error
	}

	res, err := simplejson.NewJson(*response.Result)
	if err != nil {
		return err
	}

	sub := Subscribe{
		MiningNotify:        res.GetIndex(0).GetIndex(0).GetIndex(1).MustString(),
		MiningSetDifficulty: res.GetIndex(0).GetIndex(1).GetIndex(1).MustString(),
		ExtraNonce1:         res.GetIndex(0).GetIndex(2).MustString(),
		Extranonce2_size:    res.GetIndex(0).GetIndex(3).MustInt(),
	}
	sc.Subscribe = &sub

	// Handle messages
	go sc.RunHandleMessages()
	// Keep-alive
	// go sc.RunKeepAlive()

	log.Debugf("Returning from authorizeLocked")
	return nil
}

func (sc *StratumContext) RunKeepAlive() {
	sendKeepAlive := func() {
		// args := make(map[string]interface{})
		// args["id"] = sc.SessionID
		if _, err := sc.Call("mining.ping", []string{}); err != nil {
			log.Errorf("Failed keepalive: %v", err)
		} else {
			log.Debugf("Posted keepalive")
		}
	}

	for {
		select {
		case <-sc.stopChan:
			return
		case <-time.After(sc.KeepAliveDuration):
			go sendKeepAlive()
		}
	}
}

func (sc *StratumContext) RunHandleMessages() {
	// This loop only ends on error
	defer func() {
		sc.Reconnect()
	}()

	for {
		line, err := sc.ReadLine()
		if err != nil {
			log.Debugf("Failed to read string from stratum: %v", err)
			break
		}
		// log.Debugf("Received line from server: %v", line)

		var msg map[string]interface{}
		if err = json.Unmarshal([]byte(line), &msg); err != nil {
			log.Errorf("Failed to unmarshal line into JSON: '%s': %v", line, err)
			break
		}

		id := msg["id"]
		switch id.(type) {
		case uint64, float64:
			// This is a response
			response, err := ParseResponse([]byte(line))
			if err != nil {
				log.Errorf("Failed to parse response from server: %v", err)
				continue
			}
			log.Errorf("Received message from stratum server: %v", response)
			// isError := false
			// if response.Result == nil {
			// 	// This is an error
			// 	isError = true
			// }
			// id := *response.MessageID
			// if sc.submittedWorkRequestIds.Has(id) {
			// 	if !isError {
			// 		// This is a response from the server signalling that our work has been accepted
			// 		sc.submittedWorkRequestIds.Remove(id)
			// 		sc.numAcceptedResults++
			// 		sc.numSubmittedResults++
			// 		log.Infof("accepted %d/%d", sc.numAcceptedResults, sc.numSubmittedResults)
			// 	} else {
			// 		sc.submittedWorkRequestIds.Remove(id)
			// 		sc.numSubmittedResults++
			// 		log.Errorf("rejected %d/%d: %s", (sc.numSubmittedResults - sc.numAcceptedResults), sc.numSubmittedResults, response.Error.Message)
			// 	}
			// } else {
			// 	// statusIntf, ok := response.Result["status"]
			// 	// if !ok {
			// 	// 	log.Warnf("Server sent back unknown message: %v", response.String())
			// 	// } else {
			// 	// 	status := statusIntf.(string)
			// 	// 	switch status {
			// 	// 	case "KEEPALIVED":
			// 	// 		// Nothing to do
			// 	// 	case "OK":
			// 	// 		log.Errorf("Failed to properly mark submitted work as accepted. work ID: %v, message=%s", response.MessageID, response.String())
			// 	// 		log.Errorf("Works: %v", sc.submittedWorkRequestIds.List())
			// 	// 	}
			// 	// }
			// }
			sc.NotifyResponse(response)
		default:
			// this is a notification
			// log.Debugf("Received message from stratum server: %#v", msg)
			switch msg["method"].(string) {
			case "mining.notify":
				log.Debugf("Received line from server: %v", msg)
				if work, err := ParseWork(msg["params"].(map[string]interface{})); err != nil {
					log.Errorf("Failed to parse job: %v", err)
					continue
				} else {
					sc.NotifyNewWork(work)
				}
			case "mining.set_difficulty":
				log.Debugf("Received line from server: %v", msg)
				// if work, err := ParseWork(msg["params"].(map[string]interface{})); err != nil {
				// 	log.Errorf("Failed to parse job: %v", err)
				// 	continue
				// } else {
				// 	sc.NotifyNewWork(work)
				// }
			default:
				log.Errorf("Unknown method: %v", msg["method"])
			}
		}
	}
}

func (sc *StratumContext) Reconnect() {
	sc.Lock()
	defer sc.Unlock()
	sc.stopChan <- struct{}{}
	if sc.Conn != nil {
		sc.Close()
		sc.Conn = nil
	}
	reconnectTimeout := 1 * time.Second
	for {
		log.Infof("Reconnecting ...")
		now := time.Now()
		if now.Sub(sc.lastReconnectTime) < reconnectTimeout {
			time.Sleep(reconnectTimeout) //XXX: Should we sleeping the remaining time?
		}
		if err := sc.Connect(sc.url); err != nil {
			// TODO: We should probably try n-times before crashing
			log.Errorf("Failled to reconnect to %v: %v", sc.url, err)
			reconnectTimeout = 5 * time.Second
		} else {
			break
		}
	}
	log.Debugf("Connected. Authorizing ...")
	sc.authorizeLocked(sc.username, sc.password)
}

func (sc *StratumContext) SubmitWork(work *Work, hash string) error {
	if work == sc.LastSubmittedWork {
		// log.Warnf("Prevented submission of stale work")
		return nil
	}
	args := make(map[string]interface{})
	nonceStr, err := BinToHex(work.Data[39:43])
	if err != nil {
		return err
	}
	args["id"] = sc.SessionID
	args["job_id"] = work.JobID
	args["nonce"] = nonceStr
	args["result"] = hash
	if req, err := sc.Call("submit", args); err != nil {
		return err
	} else {
		sc.submittedWorkRequestIds.Add(*req.MessageID)
		// Successfully submitted result
		log.Debugf("Successfully submitted work result: job=%v result=%v", work.JobID, hash)
		args["work"] = work
		sc.NotifySubmit(args)
		sc.LastSubmittedWork = work
	}
	return nil
}

func (sc *StratumContext) RegisterSubmitListener(sChan chan interface{}) {
	log.Debugf("Registerd stratum.submitListener")
	sc.submitListeners.Add(sChan)
}

func (sc *StratumContext) RegisterWorkListener(workChan chan *Work) {
	log.Debugf("Registerd stratum.workListener")
	sc.workListeners.Add(workChan)
}

func (sc *StratumContext) RegisterResponseListener(rChan chan *Response) {
	log.Debugf("Registerd stratum.responseListener")
	sc.responseListeners.Add(rChan)
}

func (sc *StratumContext) GetJob() error {
	// args := make(map[string]interface{})
	// args["id"] = sc.SessionID
	_, err := sc.Call("mining.subscribe", []string{})
	return err
}

func (sc *StratumContext) subscribe() error {
	_, err := sc.Call("mining.subscribe", []string{})
	return err
}

func ParseResponse(b []byte) (*Response, error) {
	var response Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (sc *StratumContext) NotifyNewWork(work *Work) {
	if (sc.Work != nil && strings.Compare(work.JobID, sc.Work.JobID) == 0) || sc.submittedWorkRequestIds.Has(work.JobID) {
		log.Warnf("Duplicate job request. Reconnecting to: %v", sc.url)
		// Just disconnect
		sc.connected = false
		sc.Close()
		return
	}
	log.Infof("\x1B[01;35mnew job\x1B[0m from \x1B[01;37m%v\x1B[0m diff \x1B[01;37m%d \x1B[0m ", sc.url, int(work.Difficulty))
	sc.Work = work
	for _, obj := range sc.workListeners.List() {
		ch := obj.(chan *Work)
		ch <- work
	}
}

func (sc *StratumContext) NotifySubmit(data interface{}) {
	for _, obj := range sc.submitListeners.List() {
		ch := obj.(chan interface{})
		ch <- data
	}
}

func (sc *StratumContext) NotifyResponse(response *Response) {
	for _, obj := range sc.responseListeners.List() {
		ch := obj.(chan *Response)
		ch <- response
	}
}

func (sc *StratumContext) Lock() {
	sc.Mutex.Lock()
}

func (sc *StratumContext) lockDebug() {
	sc.Mutex.Lock()
	log.Debugf("Lock acquired by: %v", MyCaller())
}

func (sc *StratumContext) Unlock() {
	sc.Mutex.Unlock()
}

func (sc *StratumContext) unlockDebug() {
	sc.Mutex.Unlock()
	log.Debugf("Lock released by: %v", MyCaller())
}
