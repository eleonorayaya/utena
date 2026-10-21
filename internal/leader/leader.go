package leader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type Address struct {
	mu  sync.RWMutex
	url string
}

func NewAddress(rawURL string) *Address {
	return &Address{url: strings.TrimRight(rawURL, "/")}
}

func (a *Address) URL() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.url
}

func (a *Address) Set(rawURL string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.url = strings.TrimRight(rawURL, "/")
}

func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid leader url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" || u.Path != "" && u.Path != "/" {
		return fmt.Errorf("leader url %q must be http://127.0.0.1:<port>", rawURL)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || port == "" {
		return fmt.Errorf("leader url %q must be http://127.0.0.1:<port>", rawURL)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("leader url %q must point at a loopback address", rawURL)
	}
	return nil
}

type Module struct {
	address *Address
}

func NewModule(address *Address) *Module {
	return &Module{address: address}
}

func (m *Module) OnAppStart(context.Context) error { return nil }

func (m *Module) OnAppEnd(context.Context) error { return nil }

func (m *Module) Routes() chi.Router {
	r := chi.NewRouter()
	r.Put("/", m.put)
	return r
}

type SetRequest struct {
	URL string `json:"url"`
}

func (s *SetRequest) Bind(*http.Request) error {
	return ValidateURL(s.URL)
}

func (m *Module) put(w http.ResponseWriter, r *http.Request) {
	if m.address == nil {
		common.RenderError(w, r, common.NewNotFound("not a follower"))
		return
	}
	req := &SetRequest{}
	if err := render.Bind(r, req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}
	m.address.Set(req.URL)
	w.WriteHeader(http.StatusNoContent)
}
