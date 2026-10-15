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
	body, err := json.Marshal(req)
	if err != nil {
		return OpenResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/open", bytes.NewReader(body))
	if err != nil {
		return OpenResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := client.Do(httpReq)
	if err != nil {
		return OpenResult{}, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusServiceUnavailable {
		return OpenResult{}, ErrUnavailable
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return OpenResult{}, fmt.Errorf("browser open: status %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out OpenResult
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return OpenResult{}, err
	}
	return out, nil
}
