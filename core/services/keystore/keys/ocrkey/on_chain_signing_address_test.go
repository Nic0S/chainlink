package ocrkey_test

import (
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/ocrkey"
)

func TestOCR_OnChainSigningAddress_String(t *testing.T) {
	t.Parallel()

	// should contain EIP55CapitalizedAddress
	const ocrSigningKey = "ocrsad_0x30762A700F7d836528dfB14DD60Ec2A3aEaA7694"
	var address ocrkey.OnChainSigningAddress
	err := address.UnmarshalText([]byte(ocrSigningKey))
	require.NoError(t, err)
	require.Equal(t, ocrSigningKey, address.String())
}
