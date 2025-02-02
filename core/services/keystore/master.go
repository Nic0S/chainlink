package keystore

import (
	"fmt"
	"math/big"
	"reflect"
	"sync"

	"github.com/pkg/errors"

	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/csakey"
	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/ethkey"
	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/ocrkey"
	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/p2pkey"
	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/vrfkey"
	"github.com/smartcontractkit/chainlink/core/services/postgres"
	"github.com/smartcontractkit/chainlink/core/utils"
	"github.com/smartcontractkit/sqlx"
)

var ErrLocked = errors.New("Keystore is locked")

//go:generate mockery --name Master --output ./mocks/ --case=underscore

type Master interface {
	CSA() CSA
	Eth() Eth
	OCR() OCR
	P2P() P2P
	VRF() VRF
	Unlock(password string) error
	Migrate(vrfPassword string, chainID *big.Int) error
	IsEmpty() (bool, error)
}

type master struct {
	*keyManager
	csa *csa
	eth *eth
	ocr *ocr
	p2p *p2p
	vrf *vrf
}

func New(db *sqlx.DB, scryptParams utils.ScryptParams, lggr logger.Logger) Master {
	return newMaster(db, scryptParams, lggr)
}

func newMaster(db *sqlx.DB, scryptParams utils.ScryptParams, lggr logger.Logger) *master {
	km := &keyManager{
		orm:          NewORM(db, lggr),
		scryptParams: scryptParams,
		lock:         &sync.RWMutex{},
		logger:       lggr.Named("KeyStore"),
	}

	return &master{
		keyManager: km,
		csa:        newCSAKeyStore(km),
		eth:        newEthKeyStore(km),
		ocr:        newOCRKeyStore(km),
		p2p:        newP2PKeyStore(km),
		vrf:        newVRFKeyStore(km),
	}
}

func (ks master) CSA() CSA {
	return ks.csa
}

func (ks *master) Eth() Eth {
	return ks.eth
}

func (ks *master) OCR() OCR {
	return ks.ocr
}

func (ks *master) P2P() P2P {
	return ks.p2p
}

func (ks *master) VRF() VRF {
	return ks.vrf
}

func (ks *master) IsEmpty() (bool, error) {
	var count int64
	err := ks.orm.db.QueryRow("SELECT count(*) FROM encrypted_key_rings").Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (ks *master) Migrate(vrfPssword string, chainID *big.Int) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()
	if ks.isLocked() {
		return ErrLocked
	}
	csaKeys, err := ks.csa.GetV1KeysAsV2()
	if err != nil {
		return err
	}
	for _, csaKey := range csaKeys {
		if _, exists := ks.keyRing.CSA[csaKey.ID()]; exists {
			continue
		}
		ks.logger.Debugf("Migrating CSA key %s", csaKey.ID())
		ks.keyRing.CSA[csaKey.ID()] = csaKey
	}
	ocrKeys, err := ks.ocr.GetV1KeysAsV2()
	if err != nil {
		return err
	}
	for _, ocrKey := range ocrKeys {
		if _, exists := ks.keyRing.OCR[ocrKey.ID()]; exists {
			continue
		}
		ks.logger.Debugf("Migrating OCR key %s", ocrKey.ID())
		ks.keyRing.OCR[ocrKey.ID()] = ocrKey
	}
	p2pKeys, err := ks.p2p.GetV1KeysAsV2()
	if err != nil {
		return err
	}
	for _, p2pKey := range p2pKeys {
		if _, exists := ks.keyRing.P2P[p2pKey.ID()]; exists {
			continue
		}
		ks.logger.Debugf("Migrating P2P key %s", p2pKey.ID())
		ks.keyRing.P2P[p2pKey.ID()] = p2pKey
	}
	vrfKeys, err := ks.vrf.GetV1KeysAsV2(vrfPssword)
	if err != nil {
		return err
	}
	for _, vrfKey := range vrfKeys {
		if _, exists := ks.keyRing.VRF[vrfKey.ID()]; exists {
			continue
		}
		ks.logger.Debugf("Migrating VRF key %s", vrfKey.ID())
		ks.keyRing.VRF[vrfKey.ID()] = vrfKey
	}
	if err = ks.keyManager.save(); err != nil {
		return err
	}
	ethKeys, states, err := ks.eth.GetV1KeysAsV2(chainID)
	if err != nil {
		return err
	}
	for idx, ethKey := range ethKeys {
		if _, exists := ks.keyRing.Eth[ethKey.ID()]; exists {
			continue
		}
		ks.logger.Debugf("Migrating Eth key %s (and pegging to default chain ID %s)", ethKey.ID(), chainID.String())
		if err = ks.eth.addEthKeyWithState(ethKey, states[idx]); err != nil {
			return err
		}
		if err = ks.keyManager.save(); err != nil {
			return err
		}
	}
	return nil
}

type keyManager struct {
	orm          ksORM
	scryptParams utils.ScryptParams
	keyRing      keyRing
	keyStates    keyStates
	lock         *sync.RWMutex
	password     string
	logger       logger.Logger
}

func (km *keyManager) Unlock(password string) error {
	km.lock.Lock()
	defer km.lock.Unlock()
	// DEV: allow Unlock() to be idempotent - this is especially useful in tests,
	if km.password != "" {
		if password != km.password {
			return errors.New("attempting to unlock keystore again with a different password")
		}
		return nil
	}
	ekr, err := km.orm.getEncryptedKeyRing()
	if err != nil {
		return errors.Wrap(err, "unable to get encrypted key ring")
	}
	kr, err := ekr.Decrypt(password)
	if err != nil {
		return errors.Wrap(err, "unable to decrypt encrypted key ring")
	}
	kr.logPubKeys(km.logger)
	km.keyRing = kr

	ks, err := km.orm.loadKeyStates()
	if err != nil {
		return errors.Wrap(err, "unable to load key states")
	}

	if err = ks.validate(kr); err != nil {
		return err
	}
	km.keyStates = ks

	km.password = password
	return nil
}

// caller must hold lock!
func (km *keyManager) save(callbacks ...func(postgres.Queryer) error) error {
	ekb, err := km.keyRing.Encrypt(km.password, km.scryptParams)
	if err != nil {
		return errors.Wrap(err, "unable to encrypt keyRing")
	}
	return km.orm.saveEncryptedKeyRing(&ekb, callbacks...)
}

// caller must hold lock!
func (km *keyManager) safeAddKey(unknownKey Key, callbacks ...func(postgres.Queryer) error) error {
	fieldName, err := getFieldNameForKey(unknownKey)
	if err != nil {
		return err
	}
	// add key to keyring
	id := reflect.ValueOf(unknownKey.ID())
	key := reflect.ValueOf(unknownKey)
	keyRing := reflect.Indirect(reflect.ValueOf(km.keyRing))
	keyMap := keyRing.FieldByName(fieldName)
	keyMap.SetMapIndex(id, key)
	// save keyring to DB
	err = km.save(callbacks...)
	// if save fails, remove key from keyring
	if err != nil {
		keyMap.SetMapIndex(id, reflect.Value{})
		return err
	}
	return nil
}

// caller must hold lock!
func (km *keyManager) safeRemoveKey(unknownKey Key, callbacks ...func(postgres.Queryer) error) (err error) {
	fieldName, err := getFieldNameForKey(unknownKey)
	if err != nil {
		return err
	}
	id := reflect.ValueOf(unknownKey.ID())
	key := reflect.ValueOf(unknownKey)
	keyRing := reflect.Indirect(reflect.ValueOf(km.keyRing))
	keyMap := keyRing.FieldByName(fieldName)
	keyMap.SetMapIndex(id, reflect.Value{})
	// save keyring to DB
	err = km.save(callbacks...)
	// if save fails, add key back to keyRing
	if err != nil {
		keyMap.SetMapIndex(id, key)
		return err
	}
	return nil
}

// caller must hold lock!
func (km *keyManager) isLocked() bool {
	return len(km.password) == 0
}

func getFieldNameForKey(unknownKey Key) (string, error) {
	switch unknownKey.(type) {
	case csakey.KeyV2:
		return "CSA", nil
	case ethkey.KeyV2:
		return "Eth", nil
	case ocrkey.KeyV2:
		return "OCR", nil
	case p2pkey.KeyV2:
		return "P2P", nil
	case vrfkey.KeyV2:
		return "VRF", nil
	}
	return "", fmt.Errorf("unknown key type: %T", unknownKey)
}

type Key interface {
	ID() string
}
