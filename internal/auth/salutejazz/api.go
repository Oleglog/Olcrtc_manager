// Package salutejazz is the auth provider for the SaluteJazz service. It
// creates / joins a Jazz room over HTTP and returns the connector
// WebSocket URL, room ID and password that the salutejazz engine consumes.
package salutejazz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/openlibrecommunity/olcrtc/internal/protect"
)

const (
	authTypeAnonymous = "ANONYMOUS"
	headerAccept      = "Accept"
	headerAuthType    = "X-Jazz-AuthType"
	headerClientID    = "X-Jazz-ClientId"
	headerClientType  = "X-Client-AuthType"
	headerContentType = "Content-Type"
	headerJazzUA      = "X-Jazz-Ua"
	headerOrigin      = "Origin"
	headerReferer     = "Referer"
	contentTypeJSON   = "application/json"
	jazzOrigin        = "https://salutejazz.ru"
	jazzReferer       = jazzOrigin + "/"
	jazzUA            = "osName=Linux;osVersion=;appName=jazz;appVersion=26.21.7;" +
		"surface=WEB;browserName=Firefox;browserVersion=150.0"
)

var apiBase = "https://bk.salutejazz.ru" //nolint:gochecknoglobals // package-level state intentional

// roomInfo contains connection details for a SaluteJazz room.
type roomInfo struct {
	RoomID       string
	Password     string
	ConnectorURL string
}

var (
	errCreateRoomFailed = errors.New("create room failed")
	errPreconnectFailed = errors.New("preconnect failed")
)

func anonymousHeaders() map[string]string {
	return map[string]string{
		headerAccept:      "application/json, text/plain, */*",
		headerAuthType:    authTypeAnonymous,
		headerClientID:    uuid.New().String(),
		headerClientType:  authTypeAnonymous,
		headerContentType: contentTypeJSON,
		headerJazzUA:      jazzUA,
		headerOrigin:      jazzOrigin,
		headerReferer:     jazzReferer,
	}
}

func createRoom(ctx context.Context) (*roomInfo, error) {
	headers := anonymousHeaders()

	createResp, err := createMeeting(ctx, headers)
	if err != nil {
		return nil, fmt.Errorf("create meeting: %w", err)
	}

	connectorURL, err := preconnect(ctx, createResp.RoomID, createResp.Password, headers)
	if err != nil {
		return nil, fmt.Errorf("preconnect: %w", err)
	}

	return &roomInfo{
		RoomID:       createResp.RoomID,
		Password:     createResp.Password,
		ConnectorURL: connectorURL,
	}, nil
}

type createResponse struct {
	RoomID   string `json:"roomId"`
	Password string `json:"password"`
}

func createMeeting(ctx context.Context, headers map[string]string) (*createResponse, error) {
	createPayload := map[string]any{
		"title":                             "Video meeting",
		"guestEnabled":                      true,
		"lobbyEnabled":                      false,
		"serverVideoRecordAutoStartEnabled": false,
		"sipEnabled":                        false,
		"moderatorEmails":                   []string{},
		"summarizationEnabled":              false,
		"room3dEnabled":                     false,
		"room3dScene":                       "XRLobby",
	}

	body, err := json.Marshal(createPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal create payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/room/create-meeting",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := protect.NewHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do create request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, statusError(errCreateRoomFailed, resp)
	}

	var res createResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}
	return &res, nil
}

func preconnect(ctx context.Context, roomID, password string, headers map[string]string) (string, error) {
	preconnectPayload := map[string]any{
		"password": password,
		"jazzNextMigration": map[string]any{
			// SaluteJazz preconnect contract. Newly-created rooms (Jazz Next /
			// "видеовстречи") refuse the connect with HTTP 406
			//   {"code":"ROOM_NOT_SUPPORTED_BY_CLIENT",
			//    "message":"Media without auto-subscribe not supported by client"}
			// when this flag is false. The PoC scripts in code/jazz_*.py advertise
			// it as true and join successfully, so we mirror that. If a legacy
			// "old jazz" room ever rejects true with the symmetric error, this
			// flag will need to become per-room dynamic — for now, true matches
			// what the production Sber web client does.
			"b2bBaseRoomSupport":               true,
			"demoRoomBaseSupport":              true,
			"demoRoomVersionSupport":           2,
			"mediaWithoutAutoSubscribeSupport": true,
			"webinarSpeakerSupport":            true,
			"webinarViewerSupport":             true,
			"sdkRoomSupport":                   true,
			"sberclassRoomSupport":             true,
		},
	}

	preBody, err := json.Marshal(preconnectPayload)
	if err != nil {
		return "", fmt.Errorf("marshal preconnect payload: %w", err)
	}

	preReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/room/%s/preconnect", apiBase, roomID),
		bytes.NewReader(preBody),
	)
	if err != nil {
		return "", fmt.Errorf("create preconnect request: %w", err)
	}

	for k, v := range headers {
		preReq.Header.Set(k, v)
	}

	client := protect.NewHTTPClient()
	preResp, err := client.Do(preReq)
	if err != nil {
		return "", fmt.Errorf("do preconnect request: %w", err)
	}
	defer func() { _ = preResp.Body.Close() }()

	if preResp.StatusCode != http.StatusOK {
		return "", statusError(errPreconnectFailed, preResp)
	}

	var preconnectResp struct {
		ConnectorURL string `json:"connectorUrl"`
	}
	if err := json.NewDecoder(preResp.Body).Decode(&preconnectResp); err != nil {
		return "", fmt.Errorf("decode preconnect response: %w", err)
	}
	return preconnectResp.ConnectorURL, nil
}

func statusError(base error, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		return fmt.Errorf("%w: status %d", base, resp.StatusCode)
	}
	return fmt.Errorf("%w: status %d: %s", base, resp.StatusCode, bodyText)
}

func joinRoom(ctx context.Context, roomID, password string) (*roomInfo, error) {
	headers := anonymousHeaders()
	connectorURL, err := preconnect(ctx, roomID, password, headers)
	if err != nil {
		return nil, err
	}
	return &roomInfo{
		RoomID:       roomID,
		Password:     password,
		ConnectorURL: connectorURL,
	}, nil
}
