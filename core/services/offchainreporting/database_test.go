package offchainreporting_test

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/smartcontractkit/chainlink/core/internal/cltest"
	"github.com/smartcontractkit/chainlink/core/internal/testutils/pgtest"
	"github.com/smartcontractkit/chainlink/core/services/offchainreporting"
	"github.com/smartcontractkit/chainlink/core/services/postgres"
	"github.com/smartcontractkit/chainlink/core/utils"
	"github.com/smartcontractkit/libocr/gethwrappers/offchainaggregator"
	ocrtypes "github.com/smartcontractkit/libocr/offchainreporting/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

func Test_DB_ReadWriteState(t *testing.T) {
	db := pgtest.NewSqlxDB(t)
	sqlDB := db.DB

	configDigest := cltest.MakeConfigDigest(t)
	ethKeyStore := cltest.NewKeyStore(t, db).Eth()
	key, _ := cltest.MustInsertRandomKey(t, ethKeyStore)
	spec := cltest.MustInsertOffchainreportingOracleSpec(t, db, key.Address)

	t.Run("reads and writes state", func(t *testing.T) {
		odb := offchainreporting.NewTestDB(t, sqlDB, spec.ID)
		state := ocrtypes.PersistentState{
			Epoch:                1,
			HighestSentEpoch:     2,
			HighestReceivedEpoch: []uint32{3},
		}

		err := odb.WriteState(ctx, configDigest, state)
		require.NoError(t, err)

		readState, err := odb.ReadState(ctx, configDigest)
		require.NoError(t, err)

		require.Equal(t, state, *readState)
	})

	t.Run("updates state", func(t *testing.T) {
		odb := offchainreporting.NewTestDB(t, sqlDB, spec.ID)
		newState := ocrtypes.PersistentState{
			Epoch:                2,
			HighestSentEpoch:     3,
			HighestReceivedEpoch: []uint32{4, 5},
		}

		err := odb.WriteState(ctx, configDigest, newState)
		require.NoError(t, err)

		readState, err := odb.ReadState(ctx, configDigest)
		require.NoError(t, err)

		require.Equal(t, newState, *readState)
	})

	t.Run("does not return result for wrong spec", func(t *testing.T) {
		odb := offchainreporting.NewTestDB(t, sqlDB, spec.ID)
		state := ocrtypes.PersistentState{
			Epoch:                3,
			HighestSentEpoch:     4,
			HighestReceivedEpoch: []uint32{5, 6},
		}

		err := odb.WriteState(ctx, configDigest, state)
		require.NoError(t, err)

		// db with different spec
		odb = offchainreporting.NewTestDB(t, sqlDB, -1)

		readState, err := odb.ReadState(ctx, configDigest)
		require.NoError(t, err)

		require.Nil(t, readState)
	})

	t.Run("does not return result for wrong config digest", func(t *testing.T) {
		odb := offchainreporting.NewTestDB(t, sqlDB, spec.ID)
		state := ocrtypes.PersistentState{
			Epoch:                4,
			HighestSentEpoch:     5,
			HighestReceivedEpoch: []uint32{6, 7},
		}

		err := odb.WriteState(ctx, configDigest, state)
		require.NoError(t, err)

		readState, err := odb.ReadState(ctx, cltest.MakeConfigDigest(t))
		require.NoError(t, err)

		require.Nil(t, readState)
	})
}

func Test_DB_ReadWriteConfig(t *testing.T) {
	db := pgtest.NewSqlxDB(t)
	sqlDB := db.DB

	config := ocrtypes.ContractConfig{
		ConfigDigest:         cltest.MakeConfigDigest(t),
		Signers:              []common.Address{cltest.NewAddress(), cltest.NewAddress()},
		Transmitters:         []common.Address{cltest.NewAddress(), cltest.NewAddress()},
		Threshold:            uint8(35),
		EncodedConfigVersion: uint64(987654),
		Encoded:              []byte{1, 2, 3, 4, 5},
	}
	ethKeyStore := cltest.NewKeyStore(t, db).Eth()
	key, _ := cltest.MustInsertRandomKey(t, ethKeyStore)
	spec := cltest.MustInsertOffchainreportingOracleSpec(t, db, key.Address)
	transmitterAddress := key.Address.Address()

	t.Run("reads and writes config", func(t *testing.T) {
		db := offchainreporting.NewTestDB(t, sqlDB, spec.ID)

		err := db.WriteConfig(ctx, config)
		require.NoError(t, err)

		readConfig, err := db.ReadConfig(ctx)
		require.NoError(t, err)

		require.Equal(t, &config, readConfig)
	})

	t.Run("updates config", func(t *testing.T) {
		db := offchainreporting.NewTestDB(t, sqlDB, spec.ID)

		newConfig := ocrtypes.ContractConfig{
			ConfigDigest:         cltest.MakeConfigDigest(t),
			Signers:              []common.Address{utils.ZeroAddress, transmitterAddress, cltest.NewAddress()},
			Transmitters:         []common.Address{utils.ZeroAddress, transmitterAddress, cltest.NewAddress()},
			Threshold:            uint8(36),
			EncodedConfigVersion: uint64(987655),
			Encoded:              []byte{2, 3, 4, 5, 6},
		}

		err := db.WriteConfig(ctx, newConfig)
		require.NoError(t, err)

		readConfig, err := db.ReadConfig(ctx)
		require.NoError(t, err)

		require.Equal(t, &newConfig, readConfig)
	})

	t.Run("does not return result for wrong spec", func(t *testing.T) {
		db := offchainreporting.NewTestDB(t, sqlDB, spec.ID)

		err := db.WriteConfig(ctx, config)
		require.NoError(t, err)

		db = offchainreporting.NewTestDB(t, sqlDB, -1)

		readConfig, err := db.ReadConfig(ctx)
		require.NoError(t, err)

		require.Nil(t, readConfig)
	})
}

func assertPendingTransmissionEqual(t *testing.T, pt1, pt2 ocrtypes.PendingTransmission) {
	t.Helper()

	require.Equal(t, pt1.Rs, pt2.Rs)
	require.Equal(t, pt1.Ss, pt2.Ss)
	assert.True(t, bytes.Equal(pt1.Vs[:], pt2.Vs[:]))
	assert.True(t, bytes.Equal(pt1.SerializedReport[:], pt2.SerializedReport[:]))
	assert.Equal(t, pt1.Median, pt2.Median)
	for i := range pt1.Ss {
		assert.True(t, bytes.Equal(pt1.Ss[i][:], pt2.Ss[i][:]))
	}
	for i := range pt1.Rs {
		assert.True(t, bytes.Equal(pt1.Rs[i][:], pt2.Rs[i][:]))
	}
}

func Test_DB_PendingTransmissions(t *testing.T) {
	db := pgtest.NewSqlxDB(t)
	sqlDB := db.DB
	ethKeyStore := cltest.NewKeyStore(t, db).Eth()
	key, _ := cltest.MustInsertRandomKey(t, ethKeyStore)

	spec := cltest.MustInsertOffchainreportingOracleSpec(t, db, key.Address)
	spec2 := cltest.MustInsertOffchainreportingOracleSpec(t, db, key.Address)
	odb := offchainreporting.NewTestDB(t, sqlDB, spec.ID)
	odb2 := offchainreporting.NewTestDB(t, sqlDB, spec2.ID)
	configDigest := cltest.MakeConfigDigest(t)

	k := ocrtypes.PendingTransmissionKey{
		ConfigDigest: configDigest,
		Epoch:        0,
		Round:        1,
	}
	k2 := ocrtypes.PendingTransmissionKey{
		ConfigDigest: configDigest,
		Epoch:        1,
		Round:        2,
	}

	t.Run("stores and retrieves pending transmissions", func(t *testing.T) {
		p := ocrtypes.PendingTransmission{
			Time:             time.Now(),
			Median:           ocrtypes.Observation(big.NewInt(41)),
			SerializedReport: []byte{0, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte(), cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte(), cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}

		err := odb.StorePendingTransmission(ctx, k, p)
		require.NoError(t, err)
		m, err := odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		assertPendingTransmissionEqual(t, m[k], p)

		// Now overwrite value for k to prove that updating works
		p = ocrtypes.PendingTransmission{
			Time:             time.Now(),
			Median:           ocrtypes.Observation(big.NewInt(42)),
			SerializedReport: []byte{1, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}
		err = odb.StorePendingTransmission(ctx, k, p)
		require.NoError(t, err)
		m, err = odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		assertPendingTransmissionEqual(t, m[k], p)

		p2 := ocrtypes.PendingTransmission{
			Time:             time.Now(),
			Median:           ocrtypes.Observation(big.NewInt(43)),
			SerializedReport: []byte{2, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}

		err = odb.StorePendingTransmission(ctx, k2, p2)
		require.NoError(t, err)

		kRedHerring := ocrtypes.PendingTransmissionKey{
			ConfigDigest: ocrtypes.ConfigDigest{43},
			Epoch:        1,
			Round:        2,
		}
		pRedHerring := ocrtypes.PendingTransmission{
			Time:             time.Now(),
			Median:           ocrtypes.Observation(big.NewInt(43)),
			SerializedReport: []byte{3, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}

		err = odb.StorePendingTransmission(ctx, kRedHerring, pRedHerring)
		require.NoError(t, err)

		m, err = odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)

		require.Len(t, m, 2)

		// HACK to get around time equality because otherwise its annoying (time storage into postgres is mildly lossy)
		require.Equal(t, p.Time.Unix(), m[k].Time.Unix())
		require.Equal(t, p2.Time.Unix(), m[k2].Time.Unix())

		var zt time.Time
		p.Time, p2.Time = zt, zt
		for k, v := range m {
			v.Time = zt
			m[k] = v
		}

		require.Equal(t, p, m[k])
		require.Equal(t, p2, m[k2])

		// No keys for this oracleSpecID yet
		m, err = odb2.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		require.Len(t, m, 0)
	})

	t.Run("deletes pending transmission by key", func(t *testing.T) {
		p := ocrtypes.PendingTransmission{
			Time:             time.Unix(100, 0),
			Median:           ocrtypes.Observation(big.NewInt(44)),
			SerializedReport: []byte{1, 4, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}
		err := odb.StorePendingTransmission(ctx, k, p)
		require.NoError(t, err)
		err = odb2.StorePendingTransmission(ctx, k, p)
		require.NoError(t, err)

		err = odb.DeletePendingTransmission(ctx, k)
		require.NoError(t, err)

		m, err := odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		require.Len(t, m, 1)

		// Did not affect other oracleSpecID
		m, err = odb2.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		require.Len(t, m, 1)
	})

	t.Run("allows multiple duplicate keys for different spec ID", func(t *testing.T) {
		p := ocrtypes.PendingTransmission{
			Time:             time.Unix(100, 0),
			Median:           ocrtypes.Observation(big.NewInt(44)),
			SerializedReport: []byte{1, 4, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}
		err := odb.StorePendingTransmission(ctx, k2, p)
		require.NoError(t, err)

		m, err := odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		require.Len(t, m, 1)
		require.Equal(t, p.Median, m[k2].Median)
	})

	t.Run("deletes pending transmission older than", func(t *testing.T) {
		p := ocrtypes.PendingTransmission{
			Time:             time.Unix(100, 0),
			Median:           ocrtypes.Observation(big.NewInt(41)),
			SerializedReport: []byte{0, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}

		err := odb.StorePendingTransmission(ctx, k, p)
		require.NoError(t, err)

		p2 := ocrtypes.PendingTransmission{
			Time:             time.Unix(1000, 0),
			Median:           ocrtypes.Observation(big.NewInt(42)),
			SerializedReport: []byte{1, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}
		err = odb.StorePendingTransmission(ctx, k2, p2)
		require.NoError(t, err)

		p2 = ocrtypes.PendingTransmission{
			Time:             time.Now(),
			Median:           ocrtypes.Observation(big.NewInt(43)),
			SerializedReport: []byte{2, 2, 3},
			Rs:               [][32]byte{cltest.Random32Byte()},
			Ss:               [][32]byte{cltest.Random32Byte()},
			Vs:               cltest.Random32Byte(),
		}

		err = odb.StorePendingTransmission(ctx, k2, p2)
		require.NoError(t, err)

		err = odb.DeletePendingTransmissionsOlderThan(ctx, time.Unix(900, 0))
		require.NoError(t, err)

		m, err := odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		require.Len(t, m, 1)

		// Didn't affect other oracleSpecIDs
		odb = offchainreporting.NewTestDB(t, sqlDB, spec2.ID)
		m, err = odb.PendingTransmissionsWithConfigDigest(ctx, configDigest)
		require.NoError(t, err)
		require.Len(t, m, 1)
	})
}

func Test_DB_LatestRoundRequested(t *testing.T) {
	db := pgtest.NewSqlxDB(t)
	sqlDB := db.DB

	pgtest.MustExec(t, db, `SET CONSTRAINTS offchainreporting_latest_roun_offchainreporting_oracle_spe_fkey DEFERRED`)

	odb := offchainreporting.NewTestDB(t, sqlDB, 1)
	odb2 := offchainreporting.NewTestDB(t, sqlDB, 2)

	rawLog := cltest.LogFromFixture(t, "../../testdata/jsonrpc/round_requested_log_1_1.json")

	rr := offchainaggregator.OffchainAggregatorRoundRequested{
		Requester:    cltest.NewAddress(),
		ConfigDigest: cltest.MakeConfigDigest(t),
		Epoch:        42,
		Round:        9,
		Raw:          rawLog,
	}

	t.Run("saves latest round requested", func(t *testing.T) {
		err := odb.SaveLatestRoundRequested(postgres.WrapDbWithSqlx(sqlDB), rr)
		require.NoError(t, err)

		rawLog.Index = 42

		// Now overwrite to prove that updating works
		rr = offchainaggregator.OffchainAggregatorRoundRequested{
			Requester:    cltest.NewAddress(),
			ConfigDigest: cltest.MakeConfigDigest(t),
			Epoch:        43,
			Round:        8,
			Raw:          rawLog,
		}

		err = odb.SaveLatestRoundRequested(postgres.WrapDbWithSqlx(sqlDB), rr)
		require.NoError(t, err)
	})

	t.Run("loads latest round requested", func(t *testing.T) {
		// There is no round for db2
		lrr, err := odb2.LoadLatestRoundRequested()
		require.NoError(t, err)
		require.Equal(t, 0, int(lrr.Epoch))

		lrr, err = odb.LoadLatestRoundRequested()
		require.NoError(t, err)

		assert.Equal(t, rr, lrr)
	})

	t.Run("spec with latest round requested can be deleted", func(t *testing.T) {
		_, err := sqlDB.Exec(`DELETE FROM offchainreporting_oracle_specs`)
		assert.NoError(t, err)
	})
}
