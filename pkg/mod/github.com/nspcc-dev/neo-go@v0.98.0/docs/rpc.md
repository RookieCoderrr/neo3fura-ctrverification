# RPC

## Client

Client is provided as a Go package, so please refer to the
[relevant godocs page](https://godoc.org/github.com/nspcc-dev/neo-go/pkg/rpc).

## Server

The server is written to support as much of the [JSON-RPC 2.0 Spec](http://www.jsonrpc.org/specification) as possible. The server is run as part of the node currently.

### Example call

An example would be viewing the version of the node:

```bash
$ curl -X POST -d '{"jsonrpc": "2.0", "method": "getversion", "params": [], "id": 1}' http://localhost:20332
```

which would yield the response:

```json
{
  "result" : {
    "useragent" : "/NEO-GO:0.97.2/",
    "tcpport" : 10333,
    "network" : 860833102,
    "nonce" : 105745208
  },
  "jsonrpc" : "2.0",
  "id" : 1
}
```
### Supported methods

| Method  |
| ------- |
| `calculatenetworkfee` |
| `findstates` |
| `getapplicationlog` |
| `getbestblockhash` |
| `getblock` |
| `getblockcount` |
| `getblockhash` |
| `getblockheader` |
| `getblockheadercount` |
| `getcommittee` |
| `getconnectioncount` |
| `getcontractstate` |
| `getnativecontracts` |
| `getnep11balances` |
| `getnep11properties` |
| `getnep11transfers` |
| `getnep17balances` |
| `getnep17transfers` |
| `getnextblockvalidators` |
| `getpeers` |
| `getproof` |
| `getrawmempool` |
| `getrawtransaction` |
| `getstate` |
| `getstateheight` |
| `getstateroot` |
| `getstorage` |
| `gettransactionheight` |
| `getunclaimedgas` |
| `getversion` |
| `invokecontractverify` |
| `invokefunction` |
| `invokescript` |
| `sendrawtransaction` |
| `submitblock` |
| `submitoracleresponse` |
| `validateaddress` |
| `verifyproof` |

#### Implementation notices

##### `invokefunction`

neo-go's implementation of `invokefunction` does not return `tx`
field in the answer because that requires signing the transaction with some
key in the server which doesn't fit the model of our node-client interactions.
Lacking this signature the transaction is almost useless, so there is no point
in returning it.

It's possible to use `invokefunction` not only with contract scripthash, but also 
with contract name (for native contracts) or contract ID (for all contracts). This
feature is not supported by the C# node.

##### `getcontractstate`

It's possible to get non-native contract state by its ID, unlike with C# node where
it only works for native contracts.

##### `getrawtransaction`

VM state is included to verbose response along with other transaction fields if
the transaction is already on chain.

##### `getstateroot`

This method is able to accept state root hash instead of index, unlike the C# node
where only index is accepted.

##### `getstorage`

This method doesn't work for the Ledger contract, you can get data via regular
`getblock` and `getrawtransaction` calls. This method is able to get storage of
the native contract by its name (case-insensitive), unlike the C# node where
it only possible for index or hash.

#### `getnep11balances` and `getnep17balances`
neo-go's implementation of `getnep11balances` and `getnep17balances` does not
perform tracking of NEP-11 and NEP-17 balances for each account as it is done
in the C# node. Instead, neo-go node maintains the list of standard-compliant
contracts, i.e. those contracts that have `NEP-11` or `NEP-17` declared in the
supported standards section of the manifest. Each time balances are queried,
neo-go node asks every NEP-11/NEP-17 contract for the account balance by
invoking `balanceOf` method with the corresponding args. Invocation GAS limit
is set to be 3 GAS. All non-zero balances are included in the RPC call result.

Thus, if token contract doesn't have proper standard declared in the list of
supported standards but emits compliant NEP-11/NEP-17 `Transfer`
notifications, the token balance won't be shown in the list of balances
returned by the neo-go node (unlike the C# node behavior). However, transfer
logs of such tokens are still available via respective `getnepXXtransfers` RPC
calls.

The behaviour of the `LastUpdatedBlock` tracking for archival nodes as far as for
governing token balances matches the C# node's one. For non-archival nodes and
other NEP-11/NEP-17 tokens if transfer's `LastUpdatedBlock` is lower than the
latest state synchronization point P the node working against, then
`LastUpdatedBlock` equals P. For NEP-11 NFTs `LastUpdatedBlock` is equal for
all tokens of the same asset.

#### `getnep11transfers` and `getnep17transfers`
`transfernotifyindex` is not tracked by NeoGo, thus this field is always zero.

### Unsupported methods

Methods listed down below are not going to be supported for various reasons
and we're not accepting issues related to them.

| Method  | Reason |
| ------- | ------------|
| `closewallet` | Doesn't fit neo-go wallet model |
| `dumpprivkey` | Shouldn't exist for security reasons, see `closewallet` comment also |
| `getnewaddress` | See `closewallet` comment, use CLI to do that |
| `getwalletbalance` | See `closewallet` comment, use `getnep17balances` for that |
| `getwalletunclaimedgas` | See `closewallet` comment, use `getunclaimedgas` for that |
| `importprivkey` | Not applicable to neo-go, see `closewallet` comment |
| `listaddress` | Not applicable to neo-go, see `closewallet` comment |
| `listplugins` | neo-go doesn't have any plugins, so it makes no sense |
| `openwallet` | Doesn't fit neo-go wallet model |
| `sendfrom` | Not applicable to neo-go, see `openwallet` comment |
| `sendmany` | Not applicable to neo-go, see `openwallet` comment |
| `sendtoaddress` | Not applicable to neo-go, see `claimgas` comment |

### Extensions

Some additional extensions are implemented as a part of this RPC server.

#### `getblocksysfee` call

This method returns cumulative system fee for all transactions included in a
block. It can be removed in future versions, but at the moment you can use it
to see how much GAS is burned with particular block (because system fees are
burned).

#### `submitnotaryrequest` call

This method can be used on P2P Notary enabled networks to submit new notary
payloads to be relayed from RPC to P2P.

#### Limits and paging for getnep11transfers and getnep17transfers

`getnep11transfers` and `getnep17transfers` RPC calls never return more than
1000 results for one request (within specified time frame). You can pass your
own limit via an additional parameter and then use paging to request the next
batch of transfers.

Example requesting 10 events for address NbTiM6h8r99kpRtb428XcsUk1TzKed2gTc
within 0-1600094189000 timestamps:

```json
{ "jsonrpc": "2.0", "id": 5, "method": "getnep17transfers", "params":
["NbTiM6h8r99kpRtb428XcsUk1TzKed2gTc", 0, 1600094189000, 10] }
```

Get the next 10 transfers for the same account within the same time frame:

```json
{ "jsonrpc": "2.0", "id": 5, "method": "getnep17transfers", "params":
["NbTiM6h8r99kpRtb428XcsUk1TzKed2gTc", 0, 1600094189000, 10, 1] }
```

#### Websocket server

This server accepts websocket connections on `ws://$BASE_URL/ws` address. You
can use it to perform regular RPC calls over websockets (it's supposed to be a
little faster than going regular HTTP route) and you can also use it for
additional functionality provided only via websockets (like notifications).

#### Notification subsystem

Notification subsystem consists of two additional RPC methods (`subscribe` and
`unsubscribe` working only over websocket connection) that allow to subscribe
to various blockchain events (with simple event filtering) and receive them on
the client as JSON-RPC notifications. More details on that are written in the
[notifications specification](notifications.md).

## Reference

* [JSON-RPC 2.0 Specification](http://www.jsonrpc.org/specification)
* [NEO JSON-RPC 2.0 docs](https://docs.neo.org/docs/en-us/reference/rpc/latest-version/api.html)
