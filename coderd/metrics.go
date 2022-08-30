package coderd

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"cdr.dev/slog"
	"github.com/coder/coder/coderd/database"
	"github.com/coder/coder/coderd/httpapi"
	"github.com/coder/coder/coderd/httpmw"
	"github.com/coder/coder/coderd/rbac"
	"github.com/coder/coder/codersdk"
)

const AgentStatIntervalEnv = "CODER_AGENT_STAT_INTERVAL"

func FillEmptyDAUDays(rows []database.GetDAUsFromAgentStatsRow) []database.GetDAUsFromAgentStatsRow {
	var newRows []database.GetDAUsFromAgentStatsRow

	for i, row := range rows {
		if i == 0 {
			newRows = append(newRows, row)
			continue
		}

		last := rows[i-1]

		const day = time.Hour * 24
		diff := row.Date.Sub(last.Date)
		for diff > day {
			if diff <= day {
				break
			}
			last.Date = last.Date.Add(day)
			last.Daus = 0
			newRows = append(newRows, last)
			diff -= day
		}

		newRows = append(newRows, row)
		continue
	}

	return newRows
}

func (api *API) daus(rw http.ResponseWriter, r *http.Request) {
	if !api.Authorize(r, rbac.ActionRead, rbac.ResourceMetrics) {
		httpapi.Forbidden(rw)
		return
	}

	daus, err := api.Database.GetDAUsFromAgentStats(r.Context())
	if err != nil {
		httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to get DAUs.",
			Detail:  err.Error(),
		})
		return
	}

	var resp codersdk.GetDAUsResponse
	for _, ent := range FillEmptyDAUDays(daus) {
		resp.Entries = append(resp.Entries, codersdk.DAUEntry{
			Date: ent.Date,
			DAUs: int(ent.Daus),
		})
	}

	httpapi.Write(rw, http.StatusOK, resp)
}

func (api *API) workspaceAgentReportStats(rw http.ResponseWriter, r *http.Request) {
	api.websocketWaitMutex.Lock()
	api.websocketWaitGroup.Add(1)
	api.websocketWaitMutex.Unlock()
	defer api.websocketWaitGroup.Done()

	workspaceAgent := httpmw.WorkspaceAgent(r)
	resource, err := api.Database.GetWorkspaceResourceByID(r.Context(), workspaceAgent.ResourceID)
	if err != nil {
		httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to get workspace resource.",
			Detail:  err.Error(),
		})
		return
	}

	build, err := api.Database.GetWorkspaceBuildByJobID(r.Context(), resource.JobID)
	if err != nil {
		httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to get build.",
			Detail:  err.Error(),
		})
		return
	}

	workspace, err := api.Database.GetWorkspaceByID(r.Context(), build.WorkspaceID)
	if err != nil {
		httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to get workspace.",
			Detail:  err.Error(),
		})
		return
	}

	conn, err := websocket.Accept(rw, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to accept websocket.",
			Detail:  err.Error(),
		})
		return
	}
	defer conn.Close(websocket.StatusAbnormalClosure, "")

	var interval = time.Minute

	// Allow overriding the stat interval for debugging and testing purposes.
	intervalEnv, ok := os.LookupEnv(AgentStatIntervalEnv)
	if ok {
		intervalMs, err := strconv.Atoi(intervalEnv)
		if err != nil {
			api.Logger.Error(r.Context(), "parse agent stat interval",
				slog.F("interval", intervalEnv),
				slog.Error(err),
			)
		} else {
			interval = time.Millisecond * time.Duration(intervalMs)
		}
	}

	ctx := r.Context()
	timer := time.NewTicker(interval)
	for {
		err := wsjson.Write(ctx, conn, codersdk.AgentStatsReportRequest{})
		if err != nil {
			httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
				Message: "Failed to write report request.",
				Detail:  err.Error(),
			})
			return
		}
		var rep codersdk.AgentStatsReportResponse

		err = wsjson.Read(ctx, conn, &rep)
		if err != nil {
			httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
				Message: "Failed to read report response.",
				Detail:  err.Error(),
			})
			return
		}

		repJSON, err := json.Marshal(rep)
		if err != nil {
			httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
				Message: "Failed to marshal stat json.",
				Detail:  err.Error(),
			})
			return
		}

		api.Logger.Debug(ctx, "read stats report",
			slog.F("agent", workspaceAgent.ID),
			slog.F("resource", resource.ID),
			slog.F("workspace", workspace.ID),
			slog.F("payload", rep),
		)

		// Avoid inserting empty rows to preserve DB space.
		if len(rep.ProtocolStats) > 0 {
			_, err = api.Database.InsertAgentStat(ctx, database.InsertAgentStatParams{
				ID:          uuid.NewString(),
				CreatedAt:   time.Now(),
				AgentID:     workspaceAgent.ID,
				WorkspaceID: build.WorkspaceID,
				UserID:      workspace.OwnerID,
				Payload:     json.RawMessage(repJSON),
			})
			if err != nil {
				httpapi.Write(rw, http.StatusBadRequest, codersdk.Response{
					Message: "Failed to insert agent stat.",
					Detail:  err.Error(),
				})
				return
			}
		}

		select {
		case <-timer.C:
			continue
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}
