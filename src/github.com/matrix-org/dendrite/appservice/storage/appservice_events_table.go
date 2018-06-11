// Copyright 2018 New Vector Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/matrix-org/gomatrixserverlib"
)

const appserviceEventsSchema = `
-- Stores events to be sent to application services
CREATE TABLE IF NOT EXISTS appservice_events (
	-- An auto-incrementing id unique to each event in the table
	id BIGSERIAL NOT NULL PRIMARY KEY,
	-- The ID of the application service the event will be sent to
	as_id TEXT NOT NULL,
	-- The ID of the event
	event_id TEXT NOT NULL,
	-- Unix seconds that the event was sent at from the originating server
	origin_server_ts BIGINT NOT NULL,
	-- The ID of the room that the event was sent in
	room_id TEXT NOT NULL,
	-- The type of the event (e.g. m.text)
	type TEXT NOT NULL,
	-- The ID of the user that sent the event
	sender TEXT NOT NULL,
	-- The JSON representation of the event's content. Text to avoid db JSON parsing
	event_content TEXT,
	-- The ID of the transaction that this event is a part of
	txn_id INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS appservice_events_as_id ON appservice_event(as_id);
`

const selectEventsByApplicationServiceIDSQL = "" +
	"SELECT event_id, origin_server_ts, room_id, type, sender, event_content FROM appservice_events " +
	"WHERE as_id = $1 ORDER BY id ASC LIMIT $2"

const countEventsByApplicationServiceIDSQL = "" +
	"SELECT COUNT(event_id) FROM appservice_events WHERE as_id = $1"

const insertEventSQL = "" +
	"INSERT INTO appservice_events(as_id, event_id, origin_server_ts, room_id, type, sender, event_content, txn_id) " +
	"VALUES ($1, $2, $3, $4, $5, $6, $7, $8)"

const deleteEventsBeforeAndIncludingIDSQL = "" +
	"DELETE FROM appservice_events WHERE event_id <= $1"

type eventsStatements struct {
	selectEventsByApplicationServiceIDStmt *sql.Stmt
	countEventsByApplicationServiceIDStmt  *sql.Stmt
	insertEventStmt                        *sql.Stmt
	deleteEventsBeforeAndIncludingIDStmt   *sql.Stmt
}

func (s *eventsStatements) prepare(db *sql.DB) (err error) {
	_, err = db.Exec(appserviceEventsSchema)
	if err != nil {
		return
	}

	if s.selectEventsByApplicationServiceIDStmt, err = db.Prepare(selectEventsByApplicationServiceIDSQL); err != nil {
		return
	}
	if s.countEventsByApplicationServiceIDStmt, err = db.Prepare(countEventsByApplicationServiceIDSQL); err != nil {
		return
	}
	if s.insertEventStmt, err = db.Prepare(insertEventSQL); err != nil {
		return
	}
	if s.deleteEventsBeforeAndIncludingIDStmt, err = db.Prepare(deleteEventsBeforeAndIncludingIDSQL); err != nil {
		return
	}

	return
}

// selectEventsByApplicationServiceID takes in an application service ID and
// returns a slice of events that need to be sent to that application service.
func (s *eventsStatements) selectEventsByApplicationServiceID(
	ctx context.Context,
	applicationServiceID string,
	limit int,
) (
	eventIDs []string,
	events []gomatrixserverlib.ApplicationServiceEvent,
	err error,
) {
	eventRows, err := s.selectEventsByApplicationServiceIDStmt.QueryContext(ctx, applicationServiceID, limit)
	if err != nil {
		return nil, nil, err
	}
	defer eventRows.Close() // nolint: errcheck

	// Iterate through each row and store event contents
	for eventRows.Next() {
		var event gomatrixserverlib.ApplicationServiceEvent
		var eventContent sql.NullString
		err = eventRows.Scan(
			&event.EventID,
			&event.OriginServerTimestamp,
			&event.RoomID,
			&event.Type,
			&event.UserID,
			&eventContent,
		)
		if err != nil {
			return nil, nil, err
		}
		if eventContent.Valid {
			event.Content = gomatrixserverlib.RawJSON(eventContent.String)
		}
		eventIDs = append(eventIDs, event.EventID)

		// Get age of the event from original timestamp and current time
		ageMilli := time.Now().UnixNano() / int64(time.Millisecond)
		event.Age = ageMilli - event.OriginServerTimestamp

		// TODO: Synapse does this. Do app services really require this? :)
		event.Sender = event.UserID

		events = append(events, event)
	}

	return
}

// countEventsByApplicationServiceID inserts an event mapped to its corresponding application service
// IDs into the db.
func (s *eventsStatements) countEventsByApplicationServiceID(
	ctx context.Context,
	appServiceID string,
) (int, error) {
	var count int
	err := s.countEventsByApplicationServiceIDStmt.QueryRowContext(ctx, appServiceID).Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	return count, nil
}

// insertEvent inserts an event mapped to its corresponding application service
// IDs into the db.
func (s *eventsStatements) insertEvent(
	ctx context.Context,
	appServiceID string,
	event gomatrixserverlib.Event,
) (err error) {
	_, err = s.insertEventStmt.ExecContext(
		ctx,
		appServiceID,
		event.EventID(),
		event.OriginServerTS(),
		event.RoomID(),
		event.Type(),
		event.Sender(),
		event.Content(),
		nil,
	)
	return
}

// deleteEventsBeforeAndIncludingID removes events matching given IDs from the database.
func (s *eventsStatements) deleteEventsBeforeAndIncludingID(
	ctx context.Context,
	eventID string,
) (err error) {
	_, err = s.deleteEventsBeforeAndIncludingIDStmt.ExecContext(ctx, eventID)
	return err
}
