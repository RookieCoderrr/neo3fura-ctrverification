package core

import (
	"math"
	"math/big"
	"sort"
	"testing"

	"github.com/nspcc-dev/neo-go/internal/random"
	"github.com/nspcc-dev/neo-go/internal/testchain"
	"github.com/nspcc-dev/neo-go/pkg/config/netmode"
	"github.com/nspcc-dev/neo-go/pkg/core/native"
	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/callflag"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/trigger"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm"
	"github.com/nspcc-dev/neo-go/pkg/vm/emit"
	"github.com/nspcc-dev/neo-go/pkg/vm/opcode"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/stretchr/testify/require"
)

func setSigner(tx *transaction.Transaction, h util.Uint160) {
	tx.Signers = []transaction.Signer{{
		Account: h,
		Scopes:  transaction.Global,
	}}
}

func checkTxHalt(t *testing.T, bc *Blockchain, h util.Uint256) {
	aer, err := bc.GetAppExecResults(h, trigger.Application)
	require.NoError(t, err)
	require.Equal(t, 1, len(aer))
	require.Equal(t, vm.HaltState, aer[0].VMState, aer[0].FaultException)
}

func TestNEO_Vote(t *testing.T) {
	bc := newTestChain(t)

	neo := bc.contracts.NEO
	tx := transaction.New([]byte{byte(opcode.PUSH1)}, 0)
	ic := bc.newInteropContext(trigger.Application, bc.dao, nil, tx)
	ic.SpawnVM()
	ic.Block = bc.newBlock(tx)

	freq := testchain.ValidatorsCount + testchain.CommitteeSize()
	advanceChain := func(t *testing.T) {
		for i := 0; i < freq; i++ {
			require.NoError(t, bc.AddBlock(bc.newBlock()))
			ic.Block.Index++
		}
	}

	standBySorted := bc.GetStandByValidators()
	sort.Sort(standBySorted)
	pubs, err := neo.ComputeNextBlockValidators(bc, ic.DAO)
	require.NoError(t, err)
	require.Equal(t, standBySorted, pubs)

	sz := testchain.CommitteeSize()
	accs := make([]*wallet.Account, sz)
	candidates := make(keys.PublicKeys, sz)
	txs := make([]*transaction.Transaction, 0, len(accs))
	for i := 0; i < sz; i++ {
		priv, err := keys.NewPrivateKey()
		require.NoError(t, err)
		candidates[i] = priv.PublicKey()
		accs[i], err = wallet.NewAccount()
		require.NoError(t, err)
		if i > 0 {
			require.NoError(t, neo.RegisterCandidateInternal(ic, candidates[i]))
		}

		to := accs[i].Contract.ScriptHash()
		w := io.NewBufBinWriter()
		emit.AppCall(w.BinWriter, bc.contracts.NEO.Hash, "transfer", callflag.All,
			neoOwner.BytesBE(), to.BytesBE(),
			big.NewInt(int64(sz-i)*1000000).Int64(), nil)
		emit.Opcodes(w.BinWriter, opcode.ASSERT)
		emit.AppCall(w.BinWriter, bc.contracts.GAS.Hash, "transfer", callflag.All,
			neoOwner.BytesBE(), to.BytesBE(),
			int64(1_000_000_000), nil)
		emit.Opcodes(w.BinWriter, opcode.ASSERT)
		require.NoError(t, w.Err)
		tx := transaction.New(w.Bytes(), 1000_000_000)
		tx.ValidUntilBlock = bc.BlockHeight() + 1
		setSigner(tx, testchain.MultisigScriptHash())
		require.NoError(t, testchain.SignTx(bc, tx))
		txs = append(txs, tx)
	}
	require.NoError(t, bc.AddBlock(bc.newBlock(txs...)))
	for _, tx := range txs {
		checkTxHalt(t, bc, tx.Hash())
	}
	transferBlock := bc.BlockHeight()

	for i := 1; i < sz; i++ {
		priv := accs[i].PrivateKey()
		h := priv.GetScriptHash()
		setSigner(tx, h)
		ic.VM.Load(priv.PublicKey().GetVerificationScript())
		require.NoError(t, neo.VoteInternal(ic, h, candidates[i]))
	}

	// We still haven't voted enough validators in.
	pubs, err = neo.ComputeNextBlockValidators(bc, ic.DAO)
	require.NoError(t, err)
	require.Equal(t, standBySorted, pubs)

	advanceChain(t)
	pubs = neo.GetNextBlockValidatorsInternal()
	require.EqualValues(t, standBySorted, pubs)

	// Register and give some value to the last validator.
	require.NoError(t, neo.RegisterCandidateInternal(ic, candidates[0]))
	priv := accs[0].PrivateKey()
	h := priv.GetScriptHash()
	setSigner(tx, h)
	ic.VM.Load(priv.PublicKey().GetVerificationScript())
	require.NoError(t, neo.VoteInternal(ic, h, candidates[0]))

	_, err = ic.DAO.Persist()
	require.NoError(t, err)
	advanceChain(t)
	pubs, err = neo.ComputeNextBlockValidators(bc, ic.DAO)
	require.NoError(t, err)
	sortedCandidates := candidates.Copy()[:testchain.Size()]
	sort.Sort(sortedCandidates)
	require.EqualValues(t, sortedCandidates, pubs)

	pubs = neo.GetNextBlockValidatorsInternal()
	require.EqualValues(t, sortedCandidates, pubs)

	t.Run("check voter rewards", func(t *testing.T) {
		gasBalance := make([]*big.Int, len(accs))
		neoBalance := make([]*big.Int, len(accs))
		txs := make([]*transaction.Transaction, 0, len(accs))
		for i := range accs {
			w := io.NewBufBinWriter()
			h := accs[i].PrivateKey().GetScriptHash()
			gasBalance[i] = bc.GetUtilityTokenBalance(h)
			neoBalance[i], _ = bc.GetGoverningTokenBalance(h)
			emit.AppCall(w.BinWriter, bc.contracts.NEO.Hash, "transfer", callflag.All,
				h.BytesBE(), h.BytesBE(), int64(1), nil)
			emit.Opcodes(w.BinWriter, opcode.ASSERT)
			require.NoError(t, w.Err)
			tx := transaction.New(w.Bytes(), 0)
			tx.ValidUntilBlock = bc.BlockHeight() + 1
			tx.NetworkFee = 2_000_000
			tx.SystemFee = 11_000_000
			setSigner(tx, h)
			require.NoError(t, accs[i].SignTx(netmode.UnitTestNet, tx))
			txs = append(txs, tx)
		}
		require.NoError(t, bc.AddBlock(bc.newBlock(txs...)))
		for _, tx := range txs {
			checkTxHalt(t, bc, tx.Hash())
		}

		// GAS increase consists of 2 parts: NEO holding + voting for committee nodes.
		// Here we check that 2-nd part exists and is proportional to the amount of NEO given.
		for i := range accs {
			newGAS := bc.GetUtilityTokenBalance(accs[i].Contract.ScriptHash())
			newGAS.Sub(newGAS, gasBalance[i])

			gasForHold, err := bc.contracts.NEO.CalculateNEOHolderReward(bc.dao, neoBalance[i], transferBlock, bc.BlockHeight())
			require.NoError(t, err)
			newGAS.Sub(newGAS, gasForHold)
			require.True(t, newGAS.Sign() > 0)
			gasBalance[i] = newGAS
		}
		// First account voted later than the others.
		require.Equal(t, -1, gasBalance[0].Cmp(gasBalance[1]))
		for i := 2; i < testchain.ValidatorsCount; i++ {
			require.Equal(t, 0, gasBalance[i].Cmp(gasBalance[1]))
		}
		require.Equal(t, 1, gasBalance[1].Cmp(gasBalance[testchain.ValidatorsCount]))
		for i := testchain.ValidatorsCount; i < testchain.CommitteeSize(); i++ {
			require.Equal(t, 0, gasBalance[i].Cmp(gasBalance[testchain.ValidatorsCount]))
		}
	})

	require.NoError(t, neo.UnregisterCandidateInternal(ic, candidates[0]))
	require.Error(t, neo.VoteInternal(ic, h, candidates[0]))
	advanceChain(t)

	pubs, err = neo.ComputeNextBlockValidators(bc, ic.DAO)
	require.NoError(t, err)
	for i := range pubs {
		require.NotEqual(t, candidates[0], pubs[i])
	}
}

// TestNEO_RecursiveDistribution is a test for https://github.com/nspcc-dev/neo-go/pull/2181.
func TestNEO_RecursiveGASMint(t *testing.T) {
	bc := newTestChain(t)
	initBasicChain(t, bc)

	contractHash, err := bc.GetContractScriptHash(1) // deployed rpc/server/testdata/test_contract.go contract
	require.NoError(t, err)
	tx := transferTokenFromMultisigAccount(t, bc, contractHash, bc.contracts.GAS.Hash, 2_0000_0000)
	checkTxHalt(t, bc, tx.Hash())

	// Transfer 10 NEO to test contract, the contract should earn some GAS by owning this NEO.
	tx = transferTokenFromMultisigAccount(t, bc, contractHash, bc.contracts.NEO.Hash, 10)
	res, err := bc.GetAppExecResults(tx.Hash(), trigger.Application)
	require.NoError(t, err)
	require.Equal(t, vm.HaltState, res[0].VMState)

	// Add blocks to be able to trigger NEO transfer from contract address to owner
	// address inside onNEP17Payment (the contract starts NEO transfers from chain height = 100).
	for i := bc.BlockHeight(); i < 100; i++ {
		require.NoError(t, bc.AddBlock(bc.newBlock()))
	}

	// Transfer 1 more NEO to the contract. Transfer will trigger onNEP17Payment. OnNEP17Payment will
	// trigger transfer of 11 NEO to the contract owner (based on the contract code). 11 NEO Transfer will
	// trigger GAS distribution. GAS transfer will trigger OnNEP17Payment one more time. The recursion
	// shouldn't occur here, because contract's balance LastUpdated height has already been updated in
	// this block.
	tx = transferTokenFromMultisigAccount(t, bc, contractHash, bc.contracts.NEO.Hash, 1)
	res, err = bc.GetAppExecResults(tx.Hash(), trigger.Application)
	require.NoError(t, err)
	require.Equal(t, vm.HaltState, res[0].VMState, res[0].FaultException)
}

func TestNEO_SetGasPerBlock(t *testing.T) {
	bc := newTestChain(t)

	testGetSet(t, bc, bc.contracts.NEO.Hash, "GasPerBlock",
		5*native.GASFactor, 0, 10*native.GASFactor)
}

func TestNEO_CalculateBonus(t *testing.T) {
	bc := newTestChain(t)

	neo := bc.contracts.NEO
	tx := transaction.New([]byte{byte(opcode.PUSH1)}, 0)
	ic := bc.newInteropContext(trigger.Application, bc.dao, nil, tx)
	ic.SpawnVM()
	ic.VM.LoadScript([]byte{byte(opcode.RET)})
	t.Run("Invalid", func(t *testing.T) {
		_, err := neo.CalculateNEOHolderReward(ic.DAO, new(big.Int).SetInt64(-1), 0, 1)
		require.Error(t, err)
	})
	t.Run("Zero", func(t *testing.T) {
		res, err := neo.CalculateNEOHolderReward(ic.DAO, big.NewInt(0), 0, 100)
		require.NoError(t, err)
		require.EqualValues(t, 0, res.Int64())
	})
	t.Run("ManyBlocks", func(t *testing.T) {
		setSigner(tx, neo.GetCommitteeAddress())
		err := neo.SetGASPerBlock(ic, 10, big.NewInt(1*native.GASFactor))
		require.NoError(t, err)

		res, err := neo.CalculateNEOHolderReward(ic.DAO, big.NewInt(100), 5, 15)
		require.NoError(t, err)
		require.EqualValues(t, (100*5*5/10)+(100*5*1/10), res.Int64())
	})
}

func TestNEO_GetAccountState(t *testing.T) {
	bc := newTestChain(t)

	acc, err := wallet.NewAccount()
	require.NoError(t, err)

	h := acc.Contract.ScriptHash()
	t.Run("empty", func(t *testing.T) {
		res, err := invokeContractMethod(bc, 1_0000000, bc.contracts.NEO.Hash, "getAccountState", h)
		require.NoError(t, err)
		checkResult(t, res, stackitem.Null{})
	})

	const amount = 123
	transferTokenFromMultisigAccountCheckOK(t, bc, h, bc.GoverningTokenHash(), int64(amount))

	t.Run("with funds", func(t *testing.T) {
		bs := stackitem.NewStruct([]stackitem.Item{
			stackitem.Make(123),
			stackitem.Make(bc.BlockHeight()),
			stackitem.Null{},
		})
		res, err := invokeContractMethod(bc, 1_0000000, bc.contracts.NEO.Hash, "getAccountState", h)
		require.NoError(t, err)
		checkResult(t, res, bs)
	})
}

func TestNEO_CommitteeBountyOnPersist(t *testing.T) {
	bc := newTestChain(t)

	hs := make([]util.Uint160, testchain.CommitteeSize())
	for i := range hs {
		hs[i] = testchain.PrivateKeyByID(i).GetScriptHash()
	}

	const singleBounty = 50000000
	bs := map[int]int64{0: singleBounty}
	checkBalances := func() {
		for i := 0; i < testchain.CommitteeSize(); i++ {
			require.EqualValues(t, bs[i], bc.GetUtilityTokenBalance(hs[i]).Int64(), i)
		}
	}
	for i := 0; i < testchain.CommitteeSize()*2; i++ {
		require.NoError(t, bc.AddBlock(bc.newBlock()))
		bs[(i+1)%testchain.CommitteeSize()] += singleBounty
		checkBalances()
	}
}

func TestNEO_TransferOnPayment(t *testing.T) {
	bc := newTestChain(t)

	cs, _ := getTestContractState(bc)
	require.NoError(t, bc.contracts.Management.PutContractState(bc.dao, cs))

	const amount = 2
	tx := transferTokenFromMultisigAccount(t, bc, cs.Hash, bc.contracts.NEO.Hash, amount)
	aer, err := bc.GetAppExecResults(tx.Hash(), trigger.Application)
	require.NoError(t, err)
	require.Equal(t, vm.HaltState, aer[0].VMState)
	require.Len(t, aer[0].Events, 3) // transfer + GAS claim for sender + onPayment

	e := aer[0].Events[2]
	require.Equal(t, "LastPayment", e.Name)
	arr := e.Item.Value().([]stackitem.Item)
	require.Equal(t, bc.contracts.NEO.Hash.BytesBE(), arr[0].Value())
	require.Equal(t, neoOwner.BytesBE(), arr[1].Value())
	require.Equal(t, big.NewInt(amount), arr[2].Value())

	tx = transferTokenFromMultisigAccount(t, bc, cs.Hash, bc.contracts.NEO.Hash, amount)
	aer, err = bc.GetAppExecResults(tx.Hash(), trigger.Application)
	require.NoError(t, err)
	require.Equal(t, vm.HaltState, aer[0].VMState)
	// Now we must also have GAS claim for contract and corresponding `onPayment`.
	require.Len(t, aer[0].Events, 5)

	e = aer[0].Events[2] // onPayment for GAS claim
	require.Equal(t, "LastPayment", e.Name)
	arr = e.Item.Value().([]stackitem.Item)
	require.Equal(t, stackitem.Null{}, arr[1])
	require.Equal(t, bc.contracts.GAS.Hash.BytesBE(), arr[0].Value())

	e = aer[0].Events[4] // onPayment for NEO transfer
	require.Equal(t, "LastPayment", e.Name)
	arr = e.Item.Value().([]stackitem.Item)
	require.Equal(t, bc.contracts.NEO.Hash.BytesBE(), arr[0].Value())
	require.Equal(t, neoOwner.BytesBE(), arr[1].Value())
	require.Equal(t, big.NewInt(amount), arr[2].Value())
}

func TestRegisterPrice(t *testing.T) {
	bc := newTestChain(t)
	testGetSet(t, bc, bc.contracts.NEO.Hash, "RegisterPrice",
		native.DefaultRegisterPrice, 1, math.MaxInt64)
}

func TestNEO_Roundtrip(t *testing.T) {
	bc := newTestChain(t)
	initialBalance, initialHeight := bc.GetGoverningTokenBalance(neoOwner)
	require.NotNil(t, initialBalance)

	t.Run("bad: amount > initial balance", func(t *testing.T) {
		tx := transferTokenFromMultisigAccountWithAssert(t, bc, neoOwner, bc.contracts.NEO.Hash, initialBalance.Int64()+1, false)
		aer, err := bc.GetAppExecResults(tx.Hash(), trigger.Application)
		require.NoError(t, err)
		require.Equal(t, vm.HaltState, aer[0].VMState, aer[0].FaultException) // transfer without assert => HALT state
		checkResult(t, &aer[0], stackitem.NewBool(false))
		require.Len(t, aer[0].Events, 0) // failed transfer => no events
		// check balance and height were not changed
		updatedBalance, updatedHeight := bc.GetGoverningTokenBalance(neoOwner)
		require.Equal(t, initialBalance, updatedBalance)
		require.Equal(t, initialHeight, updatedHeight)
	})

	t.Run("good: amount == initial balance", func(t *testing.T) {
		tx := transferTokenFromMultisigAccountWithAssert(t, bc, neoOwner, bc.contracts.NEO.Hash, initialBalance.Int64(), false)
		aer, err := bc.GetAppExecResults(tx.Hash(), trigger.Application)
		require.NoError(t, err)
		require.Equal(t, vm.HaltState, aer[0].VMState, aer[0].FaultException)
		checkResult(t, &aer[0], stackitem.NewBool(true))
		require.Len(t, aer[0].Events, 2) // roundtrip + GAS claim
		// check balance wasn't changed and height was updated
		updatedBalance, updatedHeight := bc.GetGoverningTokenBalance(neoOwner)
		require.Equal(t, initialBalance, updatedBalance)
		require.Equal(t, bc.BlockHeight(), updatedHeight)
	})
}

func TestNEO_TransferZeroWithZeroBalance(t *testing.T) {
	bc := newTestChain(t)

	check := func(t *testing.T, roundtrip bool) {
		acc := newAccountWithGAS(t, bc)
		from := acc.PrivateKey().GetScriptHash()
		to := from
		if !roundtrip {
			to = random.Uint160()
		}
		transferTx := newNEP17TransferWithAssert(bc.contracts.NEO.Hash, acc.PrivateKey().GetScriptHash(), to, 0, true)
		transferTx.SystemFee = 100000000
		transferTx.NetworkFee = 10000000
		transferTx.ValidUntilBlock = bc.BlockHeight() + 1
		addSigners(acc.PrivateKey().GetScriptHash(), transferTx)
		require.NoError(t, acc.SignTx(bc.config.Magic, transferTx))
		b := bc.newBlock(transferTx)
		require.NoError(t, bc.AddBlock(b))

		aer, err := bc.GetAppExecResults(transferTx.Hash(), trigger.Application)
		require.NoError(t, err)
		require.Equal(t, vm.HaltState, aer[0].VMState, aer[0].FaultException)
		require.Len(t, aer[0].Events, 1)                                                                              // roundtrip only, no GAS claim
		require.Equal(t, stackitem.NewBigInteger(big.NewInt(0)), aer[0].Events[0].Item.Value().([]stackitem.Item)[2]) // amount is 0
		// check balance wasn't changed and height wasn't updated
		updatedBalance, updatedHeight := bc.GetGoverningTokenBalance(acc.PrivateKey().GetScriptHash())
		require.Equal(t, big.NewInt(0), updatedBalance)
		require.Equal(t, uint32(0), updatedHeight)
	}
	t.Run("roundtrip: amount == initial balance == 0", func(t *testing.T) {
		check(t, true)
	})
	t.Run("non-roundtrip: amount == initial balance == 0", func(t *testing.T) {
		check(t, false)
	})
}

func TestNEO_TransferZeroWithNonZeroBalance(t *testing.T) {
	bc := newTestChain(t)

	check := func(t *testing.T, roundtrip bool) {
		acc := newAccountWithGAS(t, bc)
		transferTokenFromMultisigAccount(t, bc, acc.PrivateKey().GetScriptHash(), bc.contracts.NEO.Hash, 100)
		initialBalance, _ := bc.GetGoverningTokenBalance(acc.PrivateKey().GetScriptHash())
		require.True(t, initialBalance.Sign() > 0)

		from := acc.PrivateKey().GetScriptHash()
		to := from
		if !roundtrip {
			to = random.Uint160()
		}
		transferTx := newNEP17TransferWithAssert(bc.contracts.NEO.Hash, acc.PrivateKey().GetScriptHash(), to, 0, true)
		transferTx.SystemFee = 100000000
		transferTx.NetworkFee = 10000000
		transferTx.ValidUntilBlock = bc.BlockHeight() + 1
		addSigners(acc.PrivateKey().GetScriptHash(), transferTx)
		require.NoError(t, acc.SignTx(bc.config.Magic, transferTx))
		b := bc.newBlock(transferTx)
		require.NoError(t, bc.AddBlock(b))

		aer, err := bc.GetAppExecResults(transferTx.Hash(), trigger.Application)
		require.NoError(t, err)
		require.Equal(t, vm.HaltState, aer[0].VMState, aer[0].FaultException)
		require.Len(t, aer[0].Events, 2)                                                                              // roundtrip + GAS claim
		require.Equal(t, stackitem.NewBigInteger(big.NewInt(0)), aer[0].Events[1].Item.Value().([]stackitem.Item)[2]) // amount is 0
		// check balance wasn't changed and height was updated
		updatedBalance, updatedHeight := bc.GetGoverningTokenBalance(acc.PrivateKey().GetScriptHash())
		require.Equal(t, initialBalance, updatedBalance)
		require.Equal(t, bc.BlockHeight(), updatedHeight)
	}
	t.Run("roundtrip", func(t *testing.T) {
		check(t, true)
	})
	t.Run("non-roundtrip", func(t *testing.T) {
		check(t, false)
	})
}

func newAccountWithGAS(t *testing.T, bc *Blockchain) *wallet.Account {
	acc, err := wallet.NewAccount()
	require.NoError(t, err)
	transferTokenFromMultisigAccount(t, bc, acc.PrivateKey().GetScriptHash(), bc.contracts.GAS.Hash, 1000_00000000)
	return acc
}
