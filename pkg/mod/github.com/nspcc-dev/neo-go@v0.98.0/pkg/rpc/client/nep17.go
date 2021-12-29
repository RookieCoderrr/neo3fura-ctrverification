package client

import (
	"fmt"

	"github.com/nspcc-dev/neo-go/pkg/core/transaction"
	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/io"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/callflag"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/manifest"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/emit"
	"github.com/nspcc-dev/neo-go/pkg/vm/opcode"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
)

// TransferTarget represents target address, token amount and data for transfer.
type TransferTarget struct {
	Token   util.Uint160
	Address util.Uint160
	Amount  int64
	Data    interface{}
}

// SignerAccount represents combination of the transaction.Signer and the
// corresponding wallet.Account.
type SignerAccount struct {
	Signer  transaction.Signer
	Account *wallet.Account
}

// NEP17Decimals invokes `decimals` NEP-17 method on a specified contract.
func (c *Client) NEP17Decimals(tokenHash util.Uint160) (int64, error) {
	return c.nepDecimals(tokenHash)
}

// NEP17Symbol invokes `symbol` NEP-17 method on a specified contract.
func (c *Client) NEP17Symbol(tokenHash util.Uint160) (string, error) {
	return c.nepSymbol(tokenHash)
}

// NEP17TotalSupply invokes `totalSupply` NEP-17 method on a specified contract.
func (c *Client) NEP17TotalSupply(tokenHash util.Uint160) (int64, error) {
	return c.nepTotalSupply(tokenHash)
}

// NEP17BalanceOf invokes `balanceOf` NEP-17 method on a specified contract.
func (c *Client) NEP17BalanceOf(tokenHash, acc util.Uint160) (int64, error) {
	return c.nepBalanceOf(tokenHash, acc, nil)
}

// NEP17TokenInfo returns full NEP-17 token info.
func (c *Client) NEP17TokenInfo(tokenHash util.Uint160) (*wallet.Token, error) {
	return c.nepTokenInfo(tokenHash, manifest.NEP17StandardName)
}

// CreateNEP17TransferTx creates an invocation transaction for the 'transfer'
// method of a given contract (token) to move specified amount of NEP-17 assets
// (in FixedN format using contract's number of decimals) to given account and
// returns it. The returned transaction is not signed.
func (c *Client) CreateNEP17TransferTx(acc *wallet.Account, to util.Uint160,
	token util.Uint160, amount int64, gas int64, data interface{}, cosigners []SignerAccount) (*transaction.Transaction, error) {
	return c.CreateNEP17MultiTransferTx(acc, gas, []TransferTarget{
		{Token: token,
			Address: to,
			Amount:  amount,
			Data:    data,
		},
	}, cosigners)
}

// CreateNEP17MultiTransferTx creates an invocation transaction for performing
// NEP-17 transfers from a single sender to multiple recipients with the given
// data and cosigners. Transaction's sender is included with the CalledByEntry
// scope by default.
func (c *Client) CreateNEP17MultiTransferTx(acc *wallet.Account, gas int64,
	recipients []TransferTarget, cosigners []SignerAccount) (*transaction.Transaction, error) {
	from, err := address.StringToUint160(acc.Address)
	if err != nil {
		return nil, fmt.Errorf("bad account address: %w", err)
	}
	w := io.NewBufBinWriter()
	for i := range recipients {
		emit.AppCall(w.BinWriter, recipients[i].Token, "transfer", callflag.All,
			from, recipients[i].Address, recipients[i].Amount, recipients[i].Data)
		emit.Opcodes(w.BinWriter, opcode.ASSERT)
	}
	if w.Err != nil {
		return nil, fmt.Errorf("failed to create transfer script: %w", w.Err)
	}
	return c.CreateTxFromScript(w.Bytes(), acc, -1, gas, append([]SignerAccount{{
		Signer: transaction.Signer{
			Account: from,
			Scopes:  transaction.CalledByEntry,
		},
		Account: acc,
	}}, cosigners...))
}

// CreateTxFromScript creates transaction and properly sets cosigners and NetworkFee.
// If sysFee <= 0, it is determined via result of `invokescript` RPC. You should
// initialize network magic with Init before calling CreateTxFromScript.
func (c *Client) CreateTxFromScript(script []byte, acc *wallet.Account, sysFee, netFee int64,
	cosigners []SignerAccount) (*transaction.Transaction, error) {
	signers, accounts, err := getSigners(acc, cosigners)
	if err != nil {
		return nil, fmt.Errorf("failed to construct tx signers: %w", err)
	}
	if sysFee < 0 {
		result, err := c.InvokeScript(script, signers)
		if err != nil {
			return nil, fmt.Errorf("can't add system fee to transaction: %w", err)
		}
		if result.State != "HALT" {
			return nil, fmt.Errorf("can't add system fee to transaction: bad vm state: %s due to an error: %s", result.State, result.FaultException)
		}
		sysFee = result.GasConsumed
	}

	tx := transaction.New(script, sysFee)
	tx.Signers = signers

	tx.ValidUntilBlock, err = c.CalculateValidUntilBlock()
	if err != nil {
		return nil, fmt.Errorf("failed to add validUntilBlock to transaction: %w", err)
	}

	err = c.AddNetworkFee(tx, netFee, accounts...)
	if err != nil {
		return nil, fmt.Errorf("failed to add network fee: %w", err)
	}

	return tx, nil
}

// TransferNEP17 creates an invocation transaction that invokes 'transfer' method
// on a given token to move specified amount of NEP-17 assets (in FixedN format
// using contract's number of decimals) to given account with data specified and
// sends it to the network returning just a hash of it. Cosigners argument
// specifies a set of the transaction cosigners (may be nil or may include sender)
// with proper scope and accounts to cosign the transaction. If cosigning is
// impossible (e.g. due to locked cosigner's account) an error is returned.
func (c *Client) TransferNEP17(acc *wallet.Account, to util.Uint160, token util.Uint160,
	amount int64, gas int64, data interface{}, cosigners []SignerAccount) (util.Uint256, error) {
	if !c.initDone {
		return util.Uint256{}, errNetworkNotInitialized
	}

	tx, err := c.CreateNEP17TransferTx(acc, to, token, amount, gas, data, cosigners)
	if err != nil {
		return util.Uint256{}, err
	}

	return c.SignAndPushTx(tx, acc, cosigners)
}

// MultiTransferNEP17 is similar to TransferNEP17, buf allows to have multiple recipients.
func (c *Client) MultiTransferNEP17(acc *wallet.Account, gas int64, recipients []TransferTarget, cosigners []SignerAccount) (util.Uint256, error) {
	if !c.initDone {
		return util.Uint256{}, errNetworkNotInitialized
	}

	tx, err := c.CreateNEP17MultiTransferTx(acc, gas, recipients, cosigners)
	if err != nil {
		return util.Uint256{}, err
	}

	return c.SignAndPushTx(tx, acc, cosigners)
}
