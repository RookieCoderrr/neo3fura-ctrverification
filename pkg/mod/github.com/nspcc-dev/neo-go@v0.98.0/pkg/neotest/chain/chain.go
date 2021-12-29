package chain

import (
	"encoding/hex"
	"sort"
	"testing"

	"github.com/nspcc-dev/neo-go/pkg/config"
	"github.com/nspcc-dev/neo-go/pkg/config/netmode"
	"github.com/nspcc-dev/neo-go/pkg/core"
	"github.com/nspcc-dev/neo-go/pkg/core/storage"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/neotest"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const singleValidatorWIF = "KxyjQ8eUa4FHt3Gvioyt1Wz29cTUrE4eTqX3yFSk1YFCsPL8uNsY"

// committeeWIFs is a list of unencrypted WIFs sorted by public key.
var committeeWIFs = []string{
	"KzfPUYDC9n2yf4fK5ro4C8KMcdeXtFuEnStycbZgX3GomiUsvX6W",
	"KzgWE3u3EDp13XPXXuTKZxeJ3Gi8Bsm8f9ijY3ZsCKKRvZUo1Cdn",
	singleValidatorWIF,
	"L2oEXKRAAMiPEZukwR5ho2S6SMeQLhcK9mF71ZnF7GvT8dU4Kkgz",

	// Provide 2 committee extra members so that committee address differs from
	// the validators one.
	"L1Tr1iq5oz1jaFaMXP21sHDkJYDDkuLtpvQ4wRf1cjKvJYvnvpAb",
	"Kz6XTUrExy78q8f4MjDHnwz8fYYyUE8iPXwPRAkHa3qN2JcHYm7e",
}

var (
	// committeeAcc is an account used to sign tx as a committee.
	committeeAcc *wallet.Account

	// multiCommitteeAcc contains committee accounts used in a multi-node setup.
	multiCommitteeAcc []*wallet.Account

	// multiValidatorAcc contains validator accounts used in a multi-node setup.
	multiValidatorAcc []*wallet.Account

	// standByCommittee contains list of committee public keys to use in config.
	standByCommittee []string
)

func init() {
	committeeAcc, _ = wallet.NewAccountFromWIF(singleValidatorWIF)
	pubs := keys.PublicKeys{committeeAcc.PrivateKey().PublicKey()}
	err := committeeAcc.ConvertMultisig(1, pubs)
	if err != nil {
		panic(err)
	}

	mc := smartcontract.GetMajorityHonestNodeCount(len(committeeWIFs))
	mv := smartcontract.GetDefaultHonestNodeCount(4)
	accs := make([]*wallet.Account, len(committeeWIFs))
	pubs = make(keys.PublicKeys, len(accs))
	for i := range committeeWIFs {
		accs[i], _ = wallet.NewAccountFromWIF(committeeWIFs[i])
		pubs[i] = accs[i].PrivateKey().PublicKey()
	}

	// Config entry must contain validators first in a specific order.
	standByCommittee = make([]string, len(pubs))
	standByCommittee[0] = hex.EncodeToString(pubs[2].Bytes())
	standByCommittee[1] = hex.EncodeToString(pubs[0].Bytes())
	standByCommittee[2] = hex.EncodeToString(pubs[3].Bytes())
	standByCommittee[3] = hex.EncodeToString(pubs[1].Bytes())
	standByCommittee[4] = hex.EncodeToString(pubs[4].Bytes())
	standByCommittee[5] = hex.EncodeToString(pubs[5].Bytes())

	multiValidatorAcc = make([]*wallet.Account, mv)
	sort.Sort(pubs[:4])

vloop:
	for i := 0; i < mv; i++ {
		for j := range accs {
			if accs[j].PrivateKey().PublicKey().Equal(pubs[i]) {
				multiValidatorAcc[i] = wallet.NewAccountFromPrivateKey(accs[j].PrivateKey())
				err := multiValidatorAcc[i].ConvertMultisig(mv, pubs[:4])
				if err != nil {
					panic(err)
				}
				continue vloop
			}
		}
		panic("invalid committee WIFs")
	}

	multiCommitteeAcc = make([]*wallet.Account, mc)
	sort.Sort(pubs)

cloop:
	for i := 0; i < mc; i++ {
		for j := range accs {
			if accs[j].PrivateKey().PublicKey().Equal(pubs[i]) {
				multiCommitteeAcc[i] = wallet.NewAccountFromPrivateKey(accs[j].PrivateKey())
				err := multiCommitteeAcc[i].ConvertMultisig(mc, pubs)
				if err != nil {
					panic(err)
				}
				continue cloop
			}
		}
		panic("invalid committee WIFs")
	}
}

// NewSingle creates new blockchain instance with a single validator and
// setups cleanup functions.
func NewSingle(t *testing.T) (*core.Blockchain, neotest.Signer) {
	protoCfg := config.ProtocolConfiguration{
		Magic:              netmode.UnitTestNet,
		SecondsPerBlock:    1,
		StandbyCommittee:   []string{hex.EncodeToString(committeeAcc.PrivateKey().PublicKey().Bytes())},
		ValidatorsCount:    1,
		VerifyBlocks:       true,
		VerifyTransactions: true,
	}

	st := storage.NewMemoryStore()
	log := zaptest.NewLogger(t)
	bc, err := core.NewBlockchain(st, protoCfg, log)
	require.NoError(t, err)
	go bc.Run()
	t.Cleanup(bc.Close)
	return bc, neotest.NewMultiSigner(committeeAcc)
}

// NewMulti creates new blockchain instance with 4 validators and 6 committee members.
// Second return value is for validator signer, third -- for committee.
func NewMulti(t *testing.T) (*core.Blockchain, neotest.Signer, neotest.Signer) {
	protoCfg := config.ProtocolConfiguration{
		Magic:              netmode.UnitTestNet,
		SecondsPerBlock:    1,
		StandbyCommittee:   standByCommittee,
		ValidatorsCount:    4,
		VerifyBlocks:       true,
		VerifyTransactions: true,
	}

	st := storage.NewMemoryStore()
	log := zaptest.NewLogger(t)
	bc, err := core.NewBlockchain(st, protoCfg, log)
	require.NoError(t, err)
	go bc.Run()
	t.Cleanup(bc.Close)
	return bc, neotest.NewMultiSigner(multiValidatorAcc...), neotest.NewMultiSigner(multiCommitteeAcc...)
}
