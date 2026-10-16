package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RequestOpen(ctx context.Context, client *http.Client, baseURL string, req OpenRequest) (OpenResult, error) {
	return postJSON[OpenRequest, OpenResult](ctx, client, baseURL+"/open", req)
}

func RequestFocus(ctx context.Context, client *http.Client, baseURL string, req FocusRequest) (FocusResult, error) {
	return postJSON[FocusRequest, FocusResult](ctx, client, baseURL+"/focus", req)
}

func postJSON[Req, Res any](ctx context.Context, client *http.Client, url string, req Req) (Res, error) {
	var out Res
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := client.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusServiceUnavailable {
		return out, ErrUnavailable
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(msg, &errResp) == nil && errResp.Error != "" {
			return out, fmt.Errorf("%s", errResp.Error)
		}
		return out, fmt.Errorf("POST %s: status %d: %s", url, res.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}
