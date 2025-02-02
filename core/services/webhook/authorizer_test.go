package webhook_test

import (
	"context"
	"testing"

	"github.com/smartcontractkit/chainlink/core/bridges"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/sqlx"

	uuid "github.com/satori/go.uuid"
	"github.com/smartcontractkit/chainlink/core/internal/cltest"
	"github.com/smartcontractkit/chainlink/core/internal/testutils/pgtest"
	"github.com/smartcontractkit/chainlink/core/services/webhook"
	"github.com/smartcontractkit/chainlink/core/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBridgeORM(t *testing.T, db *sqlx.DB) bridges.ORM {
	return bridges.NewORM(db, logger.TestLogger(t))
}

type eiEnabledCfg struct{}

func (eiEnabledCfg) FeatureExternalInitiators() bool { return true }

type eiDisabledCfg struct{}

func (eiDisabledCfg) FeatureExternalInitiators() bool { return false }

func Test_Authorizer(t *testing.T) {
	db := pgtest.NewSqlxDB(t)
	borm := newBridgeORM(t, db)

	eiFoo := cltest.MustInsertExternalInitiator(t, borm)
	eiBar := cltest.MustInsertExternalInitiator(t, borm)

	jobWithFooAndBarEI, webhookSpecWithFooAndBarEI := cltest.MustInsertWebhookSpec(t, db)
	jobWithBarEI, webhookSpecWithBarEI := cltest.MustInsertWebhookSpec(t, db)
	jobWithNoEI, _ := cltest.MustInsertWebhookSpec(t, db)

	_, err := db.Exec(`INSERT INTO external_initiator_webhook_specs (external_initiator_id, webhook_spec_id, spec) VALUES ($1,$2,$3)`, eiFoo.ID, webhookSpecWithFooAndBarEI.ID, `{"ei": "foo", "name": "webhookSpecWithFooAndBarEI"}`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO external_initiator_webhook_specs (external_initiator_id, webhook_spec_id, spec) VALUES ($1,$2,$3)`, eiBar.ID, webhookSpecWithFooAndBarEI.ID, `{"ei": "bar", "name": "webhookSpecWithFooAndBarEI"}`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO external_initiator_webhook_specs (external_initiator_id, webhook_spec_id, spec) VALUES ($1,$2,$3)`, eiBar.ID, webhookSpecWithBarEI.ID, `{"ei": "bar", "name": "webhookSpecTwoEIs"}`)
	require.NoError(t, err)

	t.Run("no user no ei never authorizes", func(t *testing.T) {
		a := webhook.NewAuthorizer(db.DB, nil, nil)

		can, err := a.CanRun(context.Background(), nil, jobWithFooAndBarEI.ExternalJobID)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = a.CanRun(context.Background(), nil, jobWithNoEI.ExternalJobID)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = a.CanRun(context.Background(), nil, uuid.NewV4())
		require.NoError(t, err)
		assert.False(t, can)
	})

	t.Run("with user no ei always authorizes", func(t *testing.T) {
		a := webhook.NewAuthorizer(db.DB, &sessions.User{}, nil)

		can, err := a.CanRun(context.Background(), nil, jobWithFooAndBarEI.ExternalJobID)
		require.NoError(t, err)
		assert.True(t, can)
		can, err = a.CanRun(context.Background(), nil, jobWithNoEI.ExternalJobID)
		require.NoError(t, err)
		assert.True(t, can)
		can, err = a.CanRun(context.Background(), nil, uuid.NewV4())
		require.NoError(t, err)
		assert.True(t, can)
	})

	t.Run("no user with ei authorizes conditionally", func(t *testing.T) {
		a := webhook.NewAuthorizer(db.DB, nil, &eiFoo)

		can, err := a.CanRun(context.Background(), eiEnabledCfg{}, jobWithFooAndBarEI.ExternalJobID)
		require.NoError(t, err)
		assert.True(t, can)
		can, err = a.CanRun(context.Background(), eiDisabledCfg{}, jobWithFooAndBarEI.ExternalJobID)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = a.CanRun(context.Background(), eiEnabledCfg{}, jobWithBarEI.ExternalJobID)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = a.CanRun(context.Background(), eiEnabledCfg{}, jobWithNoEI.ExternalJobID)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = a.CanRun(context.Background(), eiEnabledCfg{}, uuid.NewV4())
		require.NoError(t, err)
		assert.False(t, can)
	})
}
