package metricscache

import (
	"context"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/xerrors"

	"cdr.dev/slog"
	"github.com/coder/coder/coderd/database"
	"github.com/coder/coder/codersdk"
	"github.com/coder/retry"
)

type Cache struct {
	Database database.Store
	Log      slog.Logger

	getDAUsResponse atomic.Pointer[codersdk.GetDAUsResponse]

	wg     sync.WaitGroup
	doneCh chan struct{}
}

func New(db database.Store, log slog.Logger) *Cache {
	return &Cache{
		Database: db,
		Log:      log,
		doneCh:   make(chan struct{}),
	}
}

const CacheRefreshIntervalEnv = "CODER_METRICS_CACHE_INTERVAL_MS"

func fillEmptyDAUDays(rows []database.GetDAUsFromAgentStatsRow) []database.GetDAUsFromAgentStatsRow {
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

func (c *Cache) refresh(ctx context.Context) error {
	err := c.Database.DeleteOldAgentStats(ctx)
	if err != nil {
		return xerrors.Errorf("delete old stats: %w", err)
	}

	daus, err := c.Database.GetDAUsFromAgentStats(ctx)
	if err != nil {
		return err
	}

	var resp codersdk.GetDAUsResponse
	for _, ent := range fillEmptyDAUDays(daus) {
		resp.Entries = append(resp.Entries, codersdk.DAUEntry{
			Date: ent.Date,
			DAUs: int(ent.Daus),
		})
	}

	c.getDAUsResponse.Store(&resp)
	return nil
}

func (c *Cache) Start(
	ctx context.Context,
) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		interval := time.Hour

		intervalEnv, ok := os.LookupEnv(CacheRefreshIntervalEnv)
		if ok {
			intervalMs, err := strconv.Atoi(intervalEnv)
			if err != nil {
				c.Log.Error(
					ctx,
					"could not parse interval from env",
					slog.F("interval", intervalEnv),
				)
			} else {
				interval = time.Duration(intervalMs) * time.Millisecond
			}
		}

		ticker := time.NewTicker(interval)
		for {
			for r := retry.New(time.Second, time.Minute); r.Wait(ctx); {
				start := time.Now()
				err := c.refresh(ctx)
				if err != nil {
					c.Log.Error(ctx, "refresh", slog.Error(err))
					continue
				}
				c.Log.Debug(
					ctx,
					"metrics refreshed",
					slog.F("took", time.Since(start)),
					slog.F("interval", interval),
				)
				break
			}

			select {
			case <-ticker.C:
			case <-c.doneCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Cache) Close() error {
	close(c.doneCh)
	c.wg.Wait()
	return nil
}

// GetDAUs returns the DAUs or nil if they aren't ready yet.
func (c *Cache) GetDAUs() codersdk.GetDAUsResponse {
	r := c.getDAUsResponse.Load()
	if r == nil {
		return codersdk.GetDAUsResponse{}
	}
	return *r
}
