// Copyright 2017 Vector Creations Ltd
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

package common

import (
	"database/sql"
	"fmt"

	"github.com/gchaincl/sqlhooks"
	"github.com/gchaincl/sqlhooks/hooks/othooks"
	"github.com/lib/pq"
	"github.com/matrix-org/util"
	opentracing "github.com/opentracing/opentracing-go"
)

// A Transaction is something that can be committed or rolledback.
type Transaction interface {
	// Commit the transaction
	Commit() error
	// Rollback the transaction.
	Rollback() error
}

// EndTransaction ends a transaction.
// If the transaction succeeded then it is committed, otherwise it is rolledback.
func EndTransaction(txn Transaction, succeeded *bool) {
	if *succeeded {
		txn.Commit() // nolint: errcheck
	} else {
		txn.Rollback() // nolint: errcheck
	}
}

// WithTransaction runs a block of code passing in an SQL transaction
// If the code returns an error or panics then the transactions is rolledback
// Otherwise the transaction is committed.
func WithTransaction(db *sql.DB, fn func(txn *sql.Tx) error) (err error) {
	txn, err := db.Begin()
	if err != nil {
		return
	}
	succeeded := false
	defer EndTransaction(txn, &succeeded)

	err = fn(txn)
	if err != nil {
		return
	}

	succeeded = true
	return
}

// TxStmt wraps an SQL stmt inside an optional transaction.
// If the transaction is nil then it returns the original statement that will
// run outside of a transaction.
// Otherwise returns a copy of the statement that will run inside the transaction.
func TxStmt(transaction *sql.Tx, statement *sql.Stmt) *sql.Stmt {
	if transaction != nil {
		statement = transaction.Stmt(statement)
	}
	return statement
}

type NewTracerFactory interface {
	CreateNewTracer(name string) opentracing.Tracer
}

// OpenPostgresWithTracing creates a new DB instance where calls will be
// traced with the given tracer
func OpenPostgresWithTracing(tracerFactory NewTracerFactory, databaseName, connstr string) (*sql.DB, error) {
	tracer := tracerFactory.CreateNewTracer("sql - " + databaseName)

	hooks := othooks.New(tracer)

	// This is a hack to get around the fact that you can't directly open
	// a sql.DB with a given driver, you *have* to register it.
	registrationName := fmt.Sprintf("postgres-ot-%s", util.RandomString(5))
	sql.Register(registrationName, sqlhooks.Wrap(&pq.Driver{}, hooks))

	return sql.Open(registrationName, connstr)
}
