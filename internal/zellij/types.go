package zellij

import "net/http"

type SessionUpdate struct {
	Name             string `json:"name"`
	ConnectedClients int    `json:"connected_clients"`
}

type UpdateSessionsRequest struct {
	Sessions []SessionUpdate `json:"sessions"`
}

func (u *UpdateSessionsRequest) Bind(r *http.Request) error {

	if u.Sessions == nil {
		u.Sessions = []SessionUpdate{}
	}
	return nil
}
