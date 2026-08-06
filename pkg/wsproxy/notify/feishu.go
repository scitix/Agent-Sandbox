// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// feishuWebhookPayload is the Feishu (Lark) bot webhook request body for an
// interactive card message.
type feishuWebhookPayload struct {
	MsgType string     `json:"msg_type"`
	Card    FeishuCard `json:"card"`
}

type feishuWebhookResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// feishuRetryDelays is the fixed 1s/2s/4s backoff schedule between the 3
// send attempts — a scheduled report or alert firing once a minute or
// once a day should tolerate a single transient Feishu outage rather than
// silently drop.
var feishuRetryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// sendToFeishu posts card to webhookURL, retrying up to len(feishuRetryDelays)
// additional times on failure (HTTP error, non-2xx, or a non-zero Feishu
// `code`). Returns the last error if every attempt failed.
func sendToFeishu(ctx context.Context, webhookURL string, card FeishuCard) error {
	body, err := json.Marshal(feishuWebhookPayload{MsgType: "interactive", Card: card})
	if err != nil {
		return fmt.Errorf("marshal feishu card: %w", err)
	}

	attempts := len(feishuRetryDelays) + 1
	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(feishuRetryDelays[attempt-1]):
			}
		}

		if err := postToFeishu(ctx, webhookURL, body); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("send to feishu failed after %d attempts: %w", attempts, lastErr)
}

func postToFeishu(ctx context.Context, webhookURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Timeout: 15 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed feishuWebhookResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("decode feishu webhook response: %w", err)
	}
	if parsed.Code != 0 {
		return fmt.Errorf("feishu webhook rejected card: code=%d msg=%s", parsed.Code, parsed.Msg)
	}
	return nil
}
