package mempool

import (
	"math/big"

	"github.com/nspcc-dev/neo-go/pkg/util"
)

// Feer is an interface that abstract the implementation of the fee calculation.
type Feer interface {
	FeePerByte() int64
	GetUtilityTokenBalance(util.Uint160) *big.Int
	BlockHeight() uint32
	P2PSigExtensionsEnabled() bool
}
