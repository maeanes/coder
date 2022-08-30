package codersdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"

	"golang.org/x/xerrors"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"cdr.dev/slog"
	"github.com/coder/coder/agent"
	"github.com/coder/retry"
)

type CloseFunc func() error

func (c CloseFunc) Close() error {
	return c()
}

// AgentReportStats begins a stat streaming connection with the Coder server.
// It is resilient to network failures and intermittent coderd issues.
func (c *Client) AgentReportStats(
	ctx context.Context,
	log slog.Logger,
	stats func() *agent.Stats,
) (io.Closer, error) {
	serverURL, err := c.URL.Parse("/api/v2/metrics/report-agent-stats")
	if err != nil {
		return nil, xerrors.Errorf("parse url: %w", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, xerrors.Errorf("create cookie jar: %w", err)
	}

	jar.SetCookies(serverURL, []*http.Cookie{{
		Name:  SessionTokenKey,
		Value: c.SessionToken,
	}})

	httpClient := &http.Client{
		Jar: jar,
	}

	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		defer close(doneCh)

		for r := retry.New(time.Second, time.Hour); r.Wait(ctx); {
			err = func() error {
				conn, res, err := websocket.Dial(ctx, serverURL.String(), &websocket.DialOptions{
					HTTPClient: httpClient,
					// Need to disable compression to avoid a data-race.
					CompressionMode: websocket.CompressionDisabled,
				})
				if err != nil {
					if res == nil {
						return err
					}
					return readBodyAsError(res)
				}

				for {
					var req AgentStatsReportRequest
					err := wsjson.Read(ctx, conn, &req)
					if err != nil {
						return err
					}

					s := stats()
					var numComms int
					for _, ps := range s.ProtocolStats {
						numComms += int(ps.NumConns)
					}

					resp := AgentStatsReportResponse{
						NumComms:      numComms,
						ProtocolStats: s.ProtocolStats,
					}

					err = wsjson.Write(ctx, conn, resp)
					if err != nil {
						return err
					}
				}
			}()
			if err != nil && ctx.Err() == nil {
				log.Error(ctx, "report stats", slog.Error(err))
			}
		}
	}()

	return CloseFunc(func() error {
		cancel()
		<-doneCh
		return nil
	}), nil
}

type DAUEntry struct {
	Date time.Time `json:"date"`
	DAUs int       `json:"daus"`
}

type GetDAUsResponse struct {
	Entries []DAUEntry `json:"entries"`
}

func (c *Client) GetDAUsFromAgentStats(ctx context.Context) (*GetDAUsResponse, error) {
	res, err := c.Request(ctx, http.MethodGet, "/api/v2/metrics/daus", nil)
	if err != nil {
		return nil, xerrors.Errorf("execute request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, readBodyAsError(res)
	}

	var resp GetDAUsResponse
	return &resp, json.NewDecoder(res.Body).Decode(&resp)
}

// AgentStatsReportRequest is a WebSocket request by coderd
// to the agent for stats.
type AgentStatsReportRequest struct {
}

// AgentStatsReportResponse is returned for each report
// request by the agent.
type AgentStatsReportResponse struct {
	NumComms      int                             `json:"num_comms"`
	ProtocolStats map[string]*agent.ProtocolStats `json:"protocol_stats"`
}
