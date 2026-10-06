package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 2 * time.Second}

type daemonStatus struct {
	Running bool `json:"running"`
	PID     int  `json:"pid"`
}

// fetchStatus asks addr for /status. Anything but a bridge daemon answering 200 counts as not
// running; body is the raw JSON for bridge status to print.
func fetchStatus(addr string) (st daemonStatus, body []byte, err error) {
	res, err := httpClient.Get("http://" + addr + "/status")
	if err != nil {
		return st, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return st, nil, fmt.Errorf("/status answered %d", res.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return st, nil, err
	}
	if json.Unmarshal(body, &st) != nil || !st.Running || st.PID == 0 {
		return st, nil, errors.New("not a bridge daemon")
	}
	return st, body, nil
}

func requestShutdown(addr string) error {
	res, err := httpClient.Post("http://"+addr+"/shutdown", "application/json", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("/shutdown answered %d", res.StatusCode)
	}
	return nil
}
