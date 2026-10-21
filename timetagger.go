package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type record struct {
	Key string  `json:"key"`
	T1  int64   `json:"t1"`
	T2  int64   `json:"t2"`
	DS  string  `json:"ds"`
	MT  int64   `json:"mt"`
	ST  float64 `json:"st"`
}

type timetagger struct {
	api   string
	token string
	http  http.Client
}

func newRecord(start time.Time, description string) record {
	return record{Key: randomKey(), T1: start.Unix(), T2: start.Unix(), DS: description}
}

func (t *timetagger) put(records ...record) error {
	now := time.Now().Unix()
	for i := range records {
		records[i].MT = now
	}
	body, err := json.Marshal(records)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, strings.TrimSuffix(t.api, "/")+"/records", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("authtoken", t.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		_, _ = msg.ReadFrom(resp.Body)
		return fmt.Errorf("timetagger: %s: %s", resp.Status, msg.String())
	}
	var result struct {
		Failed []string `json:"failed"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if len(result.Failed) > 0 || len(result.Errors) > 0 {
		return fmt.Errorf("timetagger rejected %v: %v", result.Failed, result.Errors)
	}
	return nil
}

func randomKey() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
