package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type EmailSender struct {
	apiKey string
	from   string
	client *http.Client
}

func NewEmailSender(apiKey, from string) *EmailSender {
	return &EmailSender{
		apiKey: apiKey,
		from:   from,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *EmailSender) Send(to, subject, html string) error {
	if e.apiKey == "" {
		return fmt.Errorf("RESEND_API_KEY 未配置")
	}

	body := map[string]any{
		"from":    e.from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var errResp map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("resend API 错误: status=%d, body=%v", resp.StatusCode, errResp)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	log.Printf("邮件发送成功: id=%v", result["id"])
	return nil
}
