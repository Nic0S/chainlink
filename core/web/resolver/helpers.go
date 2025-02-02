package resolver

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/graph-gophers/graphql-go"
	"github.com/pkg/errors"

	"github.com/smartcontractkit/chainlink/core/assets"
	"github.com/smartcontractkit/chainlink/core/bridges"
)

const (
	// PageDefaultOffset defines the default offset to use if none is provided
	PageDefaultOffset = 0

	// PageDefaultLimit defines the default limit to use if none is provided
	PageDefaultLimit = 50
)

func int32GQLID(i int32) graphql.ID {
	return graphql.ID(strconv.Itoa(int(i)))
}

// pageOffset returns the default page offset if nil, otherwise it returns the
// provided offset.
func pageOffset(offset *int) int {
	if offset == nil {
		return PageDefaultOffset
	}

	return *offset
}

// pageLimit returns the default page limit if nil, otherwise it returns the
// provided limit.
func pageLimit(limit *int) int {
	if limit == nil {
		return PageDefaultLimit
	}

	return *limit
}

// ValidateBridgeTypeUniqueness checks that a bridge has not already been created
//
/// This validation function should be moved into a bridge service.
func ValidateBridgeTypeUniqueness(bt *bridges.BridgeTypeRequest, orm bridges.ORM) error {
	_, err := orm.FindBridge(bt.Name)
	if err == nil {
		return fmt.Errorf("bridge type %v already exists", bt.Name)
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("error determining if bridge type %v already exists", bt.Name)
	}

	return nil
}

// ValidateBridgeType checks that the bridge type doesn't have a duplicate
// or invalid name or invalid url
//
// This validation function should be moved into a bridge service and return
// multiple errors.
func ValidateBridgeType(bt *bridges.BridgeTypeRequest, orm bridges.ORM) error {
	if len(bt.Name.String()) < 1 {
		return errors.New("No name specified")
	}
	if _, err := bridges.NewTaskType(bt.Name.String()); err != nil {
		return errors.Wrap(err, "invalid bridge name")
	}
	u := bt.URL.String()
	if len(strings.TrimSpace(u)) == 0 {
		return errors.New("url must be present")
	}
	if bt.MinimumContractPayment != nil &&
		bt.MinimumContractPayment.Cmp(assets.NewLinkFromJuels(0)) < 0 {

		return errors.New("MinimumContractPayment must be positive")
	}

	return nil
}
