/*
Copyright 2019 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package worker

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"vitess.io/vitess/go/sqltypes"
	"vitess.io/vitess/go/vt/discovery"
	"vitess.io/vitess/go/vt/throttler"
	"vitess.io/vitess/go/vt/topo/topoproto"
	"vitess.io/vitess/go/vt/vterrors"
	"vitess.io/vitess/go/vt/wrangler"

	querypb "vitess.io/vitess/go/vt/proto/query"
	topodatapb "vitess.io/vitess/go/vt/proto/topodata"
)

// executor takes care of the write-side of the copy.
// There is one executor for each destination shard and writer thread.
// To-be-written data will be passed in through a channel.
// The main purpose of this struct is to aggregate the objects which won't
// change during the execution and remove them from method signatures.
// executor is also used for executing vreplication and RefreshState commands.
type executor struct {
	wr        *wrangler.Wrangler
	tsc       *discovery.LegacyTabletStatsCache
	throttler *throttler.Throttler
	keyspace  string
	shard     string
	threadID  int
	// statsKey is the cached metric key which we need when we increment the stats
	// variable when we get throttled.
	statsKey []string
}

func newExecutor(wr *wrangler.Wrangler, tsc *discovery.LegacyTabletStatsCache, throttler *throttler.Throttler, keyspace, shard string, threadID int) *executor {
	return &executor{
		wr:        wr,
		tsc:       tsc,
		throttler: throttler,
		keyspace:  keyspace,
		shard:     shard,
		threadID:  threadID,
		statsKey:  []string{keyspace, shard, strconv.FormatInt(int64(threadID), 10)},
	}
}

// fetchLoop loops over the provided insertChannel and sends the commands to the
// current primary.
func (e *executor) fetchLoop(ctx context.Context, insertChannel chan string) error {
	for {
		select {
		case cmd, ok := <-insertChannel:
			if !ok {
				// no more to read, we're done
				return nil
			}
			if err := e.fetchWithRetries(ctx, func(ctx context.Context, tablet *topodatapb.Tablet) error {
				_, err := e.wr.TabletManagerClient().ExecuteFetchAsApp(ctx, tablet, true, []byte(cmd), 0)
				return err
			}); err != nil {
				return vterrors.Wrap(err, "ExecuteFetch failed")
			}
		case <-ctx.Done():
			// Doesn't really matter if this select gets starved, because the other case
			// will also return an error due to executeFetch's context being closed. This case
			// does prevent us from blocking indefinitely on insertChannel when the worker is canceled.
			return nil
		}
	}
}

func (e *executor) vreplicationExec(ctx context.Context, cmd string) (qr *sqltypes.Result, err error) {
	var result *querypb.QueryResult
	err = e.fetchWithRetries(ctx, func(ctx context.Context, tablet *topodatapb.Tablet) error {
		var err error
		result, err = e.wr.TabletManagerClient().VReplicationExec(ctx, tablet, cmd)
		return err
	})
	if err != nil {
		return nil, err
	}
	return sqltypes.Proto3ToResult(result), err
}

func (e *executor) refreshState(ctx context.Context) error {
	return e.fetchWithRetries(ctx, func(ctx context.Context, tablet *topodatapb.Tablet) error {
		return e.wr.TabletManagerClient().RefreshState(ctx, tablet)
	})
}

// fetchWithRetries will attempt to run ExecuteFetch for a single command, with
// a reasonably small timeout.
// If will keep retrying the ExecuteFetch (for a finite but longer duration) if
// it fails due to a timeout or a retriable application error.
//
// executeFetchWithRetries will always get the current PRIMARY tablet from the
// LegacyTabletStatsCache instance. If no PRIMARY is available, it will keep retrying.
func (e *executor) fetchWithRetries(ctx context.Context, action func(ctx context.Context, tablet *topodatapb.Tablet) error) error {
	retryDuration := *retryDuration
	// We should keep retrying up until the retryCtx runs out.
	retryCtx, retryCancel := context.WithTimeout(ctx, retryDuration)
	defer retryCancel()
	// Is this current attempt a retry of a previous attempt?
	isRetry := false
	for {
		var primary *discovery.LegacyTabletStats
		var err error

		// Get the current primary from the LegacyTabletStatsCache.
		primaries := e.tsc.GetHealthyTabletStats(e.keyspace, e.shard, topodatapb.TabletType_PRIMARY)
		if len(primaries) == 0 {
			e.wr.Logger().Warningf("ExecuteFetch failed for keyspace/shard %v/%v because no PRIMARY is available; will retry until there is PRIMARY again", e.keyspace, e.shard)
			statsRetryCount.Add(1)
			statsRetryCounters.Add(retryCategoryNoPrimaryAvailable, 1)
			goto retry
		}
		primary = &primaries[0]

		// Block if we are throttled.
		if e.throttler != nil {
			for {
				backoff := e.throttler.Throttle(e.threadID)
				if backoff == throttler.NotThrottled {
					break
				}
				statsThrottledCounters.Add(e.statsKey, 1)
				time.Sleep(backoff)
			}
		}

		// Run the command (in a block since goto above does not allow to introduce
		// new variables until the label is reached.)
		{
			tryCtx, cancel := context.WithTimeout(retryCtx, 2*time.Minute)
			err = action(tryCtx, primary.Tablet)
			cancel()

			if err == nil {
				// success!
				return nil
			}

			succeeded, finalErr := e.checkError(tryCtx, err, isRetry, primary)
			if succeeded {
				// We can ignore the error and don't have to retry.
				return nil
			}
			if finalErr != nil {
				// Non-retryable error.
				return finalErr
			}
		}

	retry:
		primaryAlias := "no-primary-was-available"
		if primary != nil {
			primaryAlias = topoproto.TabletAliasString(primary.Tablet.Alias)
		}
		tabletString := fmt.Sprintf("%v (%v/%v)", primaryAlias, e.keyspace, e.shard)

		select {
		case <-retryCtx.Done():
			err := retryCtx.Err()
			if err == context.DeadlineExceeded {
				return vterrors.Wrapf(err, "failed to connect to destination tablet %v after retrying for %v", tabletString, retryDuration)
			}
			return vterrors.Wrapf(err, "interrupted while trying to run a command on tablet %v", tabletString)
		case <-time.After(*executeFetchRetryTime):
			// Retry 30s after the failure using the current primary seen by the LegacyHealthCheck.
		}
		isRetry = true
	}
}

// checkError returns true if the error can be ignored and the command
// succeeded, false if the error is retryable and a non-nil error if the
// command must not be retried.
func (e *executor) checkError(ctx context.Context, err error, isRetry bool, primary *discovery.LegacyTabletStats) (bool, error) {
	tabletString := fmt.Sprintf("%v (%v/%v)", topoproto.TabletAliasString(primary.Tablet.Alias), e.keyspace, e.shard)

	// first see if it was a context timeout.
	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			e.wr.Logger().Warningf("ExecuteFetch failed on %v; will retry because it was a timeout error on the context", tabletString)
			statsRetryCount.Add(1)
			statsRetryCounters.Add(retryCategoryTimeoutError, 1)
			return false, nil
		}
	default:
	}

	// If the ExecuteFetch call failed because of an application error, we will try to figure out why.
	// We need to extract the MySQL error number, and will attempt to retry if we think the error is recoverable.
	match := errExtract.FindStringSubmatch(err.Error())
	var errNo string
	if len(match) == 2 {
		errNo = match[1]
	}
	switch {
	case errNo == "1290":
		e.wr.Logger().Warningf("ExecuteFetch failed on %v; will reresolve and retry because it's due to a MySQL read-only error: %v", tabletString, err)
		statsRetryCount.Add(1)
		statsRetryCounters.Add(retryCategoryReadOnly, 1)
	case errNo == "2002" || errNo == "2006" || errNo == "2013" || errNo == "1053":
		// Note:
		// "2006" happens if the connection is already dead. Retrying a query in
		// this case is safe.
		// "2013" happens if the connection dies in the middle of a query. This is
		// also safe to retry because either the query went through on the server or
		// it was aborted. If we retry the query and get a duplicate entry error, we
		// assume that the previous execution was successful and ignore the error.
		// See below for the handling of duplicate entry error "1062".
		// "1053" is mysql shutting down
		e.wr.Logger().Warningf("ExecuteFetch failed on %v; will reresolve and retry because it's due to a MySQL connection error: %v", tabletString, err)
		statsRetryCount.Add(1)
		statsRetryCounters.Add(retryCategoryConnectionError, 1)
	case errNo == "1062":
		if !isRetry {
			return false, vterrors.Wrapf(err, "ExecuteFetch failed on %v on the first attempt; not retrying as this is not a recoverable error", tabletString)
		}
		e.wr.Logger().Infof("ExecuteFetch failed on %v with a duplicate entry error; marking this as a success, because of the likelihood that this query has already succeeded before being retried: %v", tabletString, err)
		return true, nil
	default:
		// Unknown error.
		return false, err
	}
	return false, nil
}
