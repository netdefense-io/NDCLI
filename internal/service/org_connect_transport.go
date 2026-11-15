package service

import (
	"context"
	"fmt"
	"net/url"
	"slices"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/models"
)

func connectTransportPath(org string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/connect-transport", url.PathEscape(org))
}

// connectTransportError turns the bare 404 of a server without the route
// into SERVER_TOO_OLD; every refusal of the route itself carries a code.
func connectTransportError(err error) error {
	if api.IsNotFoundError(err) && api.GetErrorCode(err) == "" {
		return &Error{Code: CodeServerTooOld, Message: "this server predates the choice of a remote-session transport: every session runs on the websocket relay", Err: err}
	}
	return wrapAPI("%v", err)
}

// OrgConnectTransport reads which transport the organization's remote
// sessions run on, and the relays the server offers.
func (s *Service) OrgConnectTransport(ctx context.Context, org string) (*models.OrgConnectTransport, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	resp, err := s.api.Get(ctx, connectTransportPath(org), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var answer models.OrgConnectTransport
	if err := api.ParseResponse(resp, &answer); err != nil {
		return nil, connectTransportError(err)
	}
	if answer.ConnectTransport == "" {
		return nil, &Error{Code: CodeAPIError, Message: "the server answered without the transport setting"}
	}
	if answer.Relays == nil {
		answer.Relays = []models.ConnectRelay{}
	}
	return &answer, nil
}

// OrgSetConnectTransport sets the transport of the organization's remote
// sessions. relay names the relay direct-peer sessions meet on; empty keeps
// the one already chosen, and clearRelay removes it. The change applies to
// the next session; open ones keep running. The server's answer must show
// the values asked for.
func (s *Service) OrgSetConnectTransport(ctx context.Context, org, transport, relay string, clearRelay bool) (*models.OrgConnectTransport, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if !slices.Contains(models.ConnectTransports, transport) {
		return nil, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("%q is not a transport: use %s (the websocket relay) or %s (direct-peer + encrypted relay)", transport, models.ConnectTransportRelayLegacy, models.ConnectTransportDirectPeer)}
	}
	if relay != "" && clearRelay {
		return nil, &Error{Code: CodeInvalidInput, Message: "name a relay or clear it, not both"}
	}
	body := map[string]interface{}{"connect_transport": transport}
	switch {
	case clearRelay:
		body["connect_relay"] = nil
	case relay != "":
		body["connect_relay"] = relay
	}
	resp, err := s.api.Put(ctx, connectTransportPath(org), body)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var answer models.OrgConnectTransport
	if err := api.ParseResponse(resp, &answer); err != nil {
		return nil, connectTransportError(err)
	}
	confirmed := answer.ConnectTransport == transport
	switch {
	case clearRelay:
		confirmed = confirmed && answer.ConnectRelay == nil
	case relay != "":
		confirmed = confirmed && answer.ConnectRelay != nil && *answer.ConnectRelay == relay
	}
	if !confirmed {
		return nil, &Error{Code: CodeAPIError, Message: "the server did not confirm the transport setting"}
	}
	if answer.Relays == nil {
		answer.Relays = []models.ConnectRelay{}
	}
	return &answer, nil
}
