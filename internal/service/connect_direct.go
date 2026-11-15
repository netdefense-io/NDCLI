package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/directpeer"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/pathfinder"
)

// The refusals of POST /connect that a new start answers.
const (
	// APICodeConnectTransportUnsupported: the server would no longer give
	// this session direct-peer + encrypted relay.
	APICodeConnectTransportUnsupported = "CONNECT_TRANSPORT_UNSUPPORTED"
	// APICodeConnectInfoStale: the relay or the expiry the request named is
	// no longer the server's.
	APICodeConnectInfoStale = "CONNECT_INFO_STALE"
	// APICodeConnectIDConflict: the session's id or one of its keys is taken.
	APICodeConnectIDConflict = "CONNECT_ID_CONFLICT"
)

// CodeReadOnlySession refuses a session that would be read-only for a caller
// that cannot use one.
const CodeReadOnlySession = "READ_ONLY_SESSION"

// directConnectStarts is how often one connect asks the server for a
// direct-peer session: the first request, and one more for each refusal a new
// start answers.
const directConnectStarts = 3

// directPollInterval is the pause between two status reads of a direct-peer
// session, which a connected device sets up in seconds.
const directPollInterval = 2 * time.Second

// checkCompanion and newDirectSecret are variables so a test can stand in
// for the companion and fix the session's secret.
var (
	checkCompanion  = pathfinder.CheckCompanion
	newDirectSecret = directpeer.New
)

// DirectConnectOpts controls DirectConnect.
type DirectConnectOpts struct {
	// Timeout bounds the whole connect (5m if ≤0).
	Timeout time.Duration
	// Progress, when set, is told each step.
	Progress func(string)
	// RefuseReadOnly refuses a session the caller's role makes read-only,
	// before the device is asked for one.
	RefuseReadOnly bool
}

// DirectConnect is a session on direct-peer + encrypted relay whose device
// is listening: dial it now.
type DirectConnect struct {
	// Task is the CONNECT task's code. A caller that gives the session up
	// passes it to CloseConnectSession.
	Task       string
	DeviceName string
	ReadOnly   bool
	Relay      models.ConnectRelay
	// Session holds the relay and the session's keys for the pathfinder
	// client.
	Session *pathfinder.DirectSession
}

// ConnectPreflight asks the server which transport a session to the device
// would run on now. A nil answer means the server predates the question, and
// the session runs on the websocket relay as it always has. Any other failure
// is an error: which transport a session gets is the server's decision, and
// without its answer nothing may be assumed.
func (s *Service) ConnectPreflight(ctx context.Context, org, deviceName string) (*models.ConnectInfo, error) {
	if org == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "organization is required"}
	}
	if deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	// This program always offers both transports. A companion that is
	// missing fails a direct-peer session; it never chooses the other one.
	params := url.Values{
		"transport": {models.ConnectTransportDirectPeer, models.ConnectTransportRelayLegacy},
		"protocol":  {strconv.Itoa(pathfinder.DirectProtocol)},
	}
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/connect-info", org, deviceName)
	for attempt := 0; ; attempt++ {
		var info models.ConnectInfo
		resp, err := s.api.GetValues(ctx, endpoint, params)
		if err == nil {
			err = api.ParseResponse(resp, &info)
			if err == nil {
				return &info, nil
			}
			// Every refusal of the route carries a code; a 404 without one
			// is the answer of a server that has no such route.
			if api.IsNotFoundError(err) && api.GetErrorCode(err) == "" {
				return nil, nil
			}
			if !api.IsAPIError(err) {
				return nil, wrapAPI("parse connect-info response: %v", err)
			}
		}
		wait, ok := consoleRetryWait(err, attempt, true)
		if !ok {
			return nil, wrapAPI("%v", err)
		}
		if deadline, has := ctx.Deadline(); has && time.Now().Add(wait).After(deadline) {
			return nil, wrapAPI("%v", err)
		}
		if err := consoleWait(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// checkDirectInfo refuses a direct-peer answer that lacks what a session is
// sealed to, or that names another tunnel generation than this build's.
func checkDirectInfo(info *models.ConnectInfo) error {
	missing := ""
	switch {
	case info.Relay == nil || info.Relay.ID == "" || info.Relay.Hostname == "" || info.Relay.DERPPort <= 0 || info.Relay.STUNPort <= 0:
		missing = "the relay"
	case info.ExpiresAt == nil || *info.ExpiresAt <= 0:
		missing = "the expiry"
	case info.Device.UUID == "" || info.Device.Pubkey == "":
		missing = "the device's identity"
	}
	if missing != "" {
		return &Error{Code: CodeAPIError, Message: "the server chose " + models.ConnectTransportName(models.ConnectTransportDirectPeer) + " for this session and did not send " + missing}
	}
	if info.Protocol == nil || *info.Protocol != pathfinder.DirectProtocol {
		return &Error{Code: CodeAPIError, Message: fmt.Sprintf("the server chose %s for this session, and the device's agent is not on this program's generation of it (%d)", models.ConnectTransportName(models.ConnectTransportDirectPeer), pathfinder.DirectProtocol)}
	}
	return nil
}

// DirectConnect asks the server which transport a session to the device gets
// and, when that is direct-peer + encrypted relay, opens it: a new secret
// sealed to the device, the request, and the wait until the device listens.
//
// A nil session with no error means the server named the websocket relay,
// and the caller goes on as it always has. The answer returned with it (nil
// from a server that predates the question) says why when the organization is
// set to direct-peer (ConnectInfo.FallsBack). Only the server's answer leads
// there; a direct-peer session that cannot be opened is an error.
func (s *Service) DirectConnect(ctx context.Context, org, deviceName string, opts DirectConnectOpts) (*DirectConnect, *models.ConnectInfo, error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string) {}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	retryBy := deadline
	if d, ok := ctx.Deadline(); ok && d.Before(retryBy) {
		retryBy = d
	}

	checked := false
	throttled := 0
	var last error
	for start := 0; start < directConnectStarts; start++ {
		info, err := s.ConnectPreflight(ctx, org, deviceName)
		if err != nil {
			return nil, nil, err
		}
		if info == nil || info.Transport == models.ConnectTransportRelayLegacy {
			return nil, info, nil
		}
		if info.Transport != models.ConnectTransportDirectPeer {
			// Not an instruction to use the websocket relay: only the
			// server naming it is.
			return nil, nil, &Error{Code: CodeAPIError, Message: fmt.Sprintf("the server chose a transport this program does not know (%q); update ndcli", info.Transport)}
		}
		if err := checkDirectInfo(info); err != nil {
			return nil, nil, err
		}
		if opts.RefuseReadOnly && info.ReadOnly {
			return nil, nil, &Error{Code: CodeReadOnlySession, Message: pathfinder.ErrReadOnlySession.Message}
		}
		if !checked {
			if err := checkCompanion(); err != nil {
				return nil, nil, &Error{Code: CodeAPIError, Message: err.Error(), Err: err}
			}
			checked = true
		}

		progress(fmt.Sprintf("Requesting a %s session to %s", models.ConnectTransportName(models.ConnectTransportDirectPeer), deviceName))
		opened, err := s.openDirect(ctx, org, deviceName, info)
		if err == nil {
			return s.awaitDirect(ctx, deviceName, info, opened, deadline, retryBy, progress)
		}
		last = err
		switch api.GetErrorCode(err) {
		case APICodeConnectTransportUnsupported, APICodeConnectInfoStale, APICodeConnectIDConflict:
			// Each is answered by a new start: the server's answer again, a
			// new secret, a new request.
			continue
		}
		// A 429 comes before anything is created. The expiry held through
		// the wait may be too close by then, so this too starts anew.
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
			wait, ok := consoleRetryWait(err, throttled, false)
			if !ok || time.Now().Add(wait).After(retryBy) {
				return nil, nil, err
			}
			throttled++
			start--
			progress(fmt.Sprintf("The control plane refused the connect request; retrying in %ds", pathfinder.RetryAfterSeconds(wait)))
			if err := consoleWait(ctx, wait); err != nil {
				return nil, nil, err
			}
			continue
		}
		return nil, nil, err
	}
	return nil, nil, last
}

// directOpened is a direct-peer request the server accepted.
type directOpened struct {
	task    string
	session *pathfinder.DirectSession
}

// openDirect makes one session's secret, seals it to the device and asks the
// server for the session. The secret is used for this one request: whatever
// the answer, a second request gets a new one.
func (s *Service) openDirect(ctx context.Context, org, deviceName string, info *models.ConnectInfo) (*directOpened, error) {
	secret, err := newDirectSecret()
	if err != nil {
		return nil, &Error{Code: CodeAPIError, Message: fmt.Sprintf("the session's secret could not be made: %v", err), Err: err}
	}
	defer secret.Wipe()
	sealed, err := secret.Seal(info.Device.Pubkey, directpeer.Binding{
		DeviceUUID:    info.Device.UUID,
		RelayHostname: info.Relay.Hostname,
		RelayPort:     info.Relay.DERPPort,
		RelaySTUNPort: info.Relay.STUNPort,
		ExpiresAt:     *info.ExpiresAt,
	})
	if err != nil {
		return nil, &Error{Code: CodeAPIError, Message: err.Error(), Err: err}
	}
	deviceKey, clientKey := secret.AdmitKeys()
	body := models.ConnectDirectRequest{
		Transport: models.ConnectTransportDirectPeer,
		Protocol:  pathfinder.DirectProtocol,
		Relay:     info.Relay.ID,
		ConnectID: secret.ConnectID,
		ExpiresAt: *info.ExpiresAt,
		Sealed:    sealed,
		AdmitKeys: models.ConnectAdmitKeys{Server: deviceKey, Client: clientKey},
	}

	var initResp models.ConnectInitResponse
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/devices/%s/connect", org, deviceName), body)
	if err != nil {
		return nil, wrapAPI("initiate connect: %v", err)
	}
	if err := api.ParseResponse(resp, &initResp); err != nil {
		if !api.IsAPIError(err) {
			return nil, wrapAPI("parse connect init response: %v", err)
		}
		return nil, wrapAPI("initiate connect: %v", err)
	}
	if initResp.Task == "" {
		return nil, &Error{Code: CodeAPIError, Message: "connect response missing task ID"}
	}
	// A server that ignored the request's body started a websocket relay
	// session. It is never used in place of the one that was asked for.
	if initResp.Transport != models.ConnectTransportDirectPeer || (initResp.ConnectID != "" && initResp.ConnectID != secret.ConnectID) {
		_ = s.CloseConnectSession(initResp.Task)
		return nil, &Error{Code: CodeAPIError, Message: fmt.Sprintf("the server did not start the %s session that was asked for; what it started was cancelled", models.ConnectTransportName(models.ConnectTransportDirectPeer))}
	}
	return &directOpened{
		task: initResp.Task,
		session: &pathfinder.DirectSession{
			RelayHost:     info.Relay.Hostname,
			RelayPort:     info.Relay.DERPPort,
			RelaySTUNPort: info.Relay.STUNPort,
			ClientKey:     secret.ClientKey,
			DeviceKey:     secret.DeviceKey,
			PresharedKey:  secret.PresharedKey,
		},
	}, nil
}

// awaitDirect reads the session's status until the device listens. The
// session is dialed when, and only when, its task is COMPLETED and the server
// calls the session ready. The server ends a task the device never answers,
// so the caller's timeout is only the backstop.
func (s *Service) awaitDirect(ctx context.Context, deviceName string, info *models.ConnectInfo, opened *directOpened, deadline, retryBy time.Time, progress func(string)) (*DirectConnect, *models.ConnectInfo, error) {
	// settled: the task ended on its own, or the session is the caller's.
	// Anything else that ends the wait cancels the task, so the device stops
	// listening for nobody.
	settled, handed := false, false
	defer func() {
		if !handed {
			clear(opened.session.ClientKey[:])
			clear(opened.session.DeviceKey[:])
			clear(opened.session.PresharedKey[:])
		}
		if !settled {
			_ = s.CloseConnectSession(opened.task)
		}
	}()

	wait := time.Second
	refusals := 0
	for {
		if time.Now().After(deadline) {
			return nil, nil, &Error{Code: CodeAPIError, Message: "connect timeout: the device did not start the session in time"}
		}
		if err := consoleWait(ctx, wait); err != nil {
			return nil, nil, err
		}
		wait = directPollInterval

		var status models.ConnectStatusResponse
		resp, pollErr := s.api.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s/connect-status", opened.task), nil)
		if pollErr == nil {
			pollErr = api.ParseResponse(resp, &status)
			if pollErr != nil && !api.IsAPIError(pollErr) {
				return nil, nil, wrapAPI("parse connect status: %v", pollErr)
			}
		}
		if pollErr != nil {
			retryWait, ok := consoleRetryWait(pollErr, refusals, true)
			next := max(directPollInterval, retryWait)
			if !ok || time.Now().Add(next).After(retryBy) {
				return nil, nil, wrapAPI("poll connect status: %v", pollErr)
			}
			progress(fmt.Sprintf("The control plane did not answer the status poll; retrying in %ds", pathfinder.RetryAfterSeconds(next)))
			refusals++
			wait = next
			continue
		}
		refusals = 0

		if status.Transport != models.ConnectTransportDirectPeer {
			return nil, nil, &Error{Code: CodeAPIError, Message: "the server no longer reports the session as " + models.ConnectTransportName(models.ConnectTransportDirectPeer) + "; it was cancelled"}
		}
		msg := status.Message
		switch status.Status {
		case models.TaskStatusCompleted:
			if status.Session != models.ConnectSessionReady {
				if msg == "" {
					msg = "the session ended before it was joined"
				}
				return nil, nil, &Error{Code: CodeAPIError, Message: "connect failed: " + msg}
			}
			settled, handed = true, true
			readOnly := info.ReadOnly
			if status.ReadOnly != nil {
				readOnly = *status.ReadOnly
			}
			return &DirectConnect{
				Task:       opened.task,
				DeviceName: deviceName,
				ReadOnly:   readOnly,
				Relay:      *info.Relay,
				Session:    opened.session,
			}, info, nil
		case models.TaskStatusFailed:
			settled = true
			if msg == "" {
				msg = "unknown error"
			}
			return nil, nil, &Error{Code: CodeAPIError, Message: "connect task failed: " + msg}
		case models.TaskStatusCanceled:
			settled = true
			return nil, nil, &Error{Code: CodeAPIError, Message: "connect task was cancelled"}
		case models.TaskStatusExpired:
			settled = true
			return nil, nil, &Error{Code: CodeAPIError, Message: "connect task expired: the device did not answer in time"}
		}
		progress(fmt.Sprintf("Waiting for %s to start the session (%s)", deviceName, status.Status))
	}
}
