using Neo;
using Neo.SmartContract.Framework;
using Neo.SmartContract.Framework.Attributes;
using Neo.SmartContract.Framework.Native;
using Neo.SmartContract.Framework.Services;
using System;
using System.Numerics;

namespace FTWContract
{
    [ManifestExtra("Description", "Forthewin NEP17 Contract")]
    [SupportedStandards("NEP-17")]
    [ContractPermission("*", "*")]
    public class FTWToken : Nep17Token
    {
        protected const byte Prefix_ContractOwner = 0xFF;

        public override string Symbol() => "FTW";

        public override byte Decimals() => 8;

        public static void Hole(UInt160 from, BigInteger amount)
        {
            if (from is null || !from.IsValid)
                throw new Exception("The argument \"from\" is invalid.");
            if (!Runtime.CheckWitness(from))
                throw new Exception("No authorization.");

            Burn(from, amount);
        }

        public static void _deploy(object data, bool update)
        {
            if (update) return;
            if (TotalSupply() > 0) throw new Exception("Contract alreay deployed.");

            var tx = (Transaction)Runtime.ScriptContainer;
            var key = new byte[] { Prefix_ContractOwner };
            Storage.Put(Storage.CurrentContext, key, tx.Sender);

            ulong totalSupply = 500_000_000_00000000;
            Mint(tx.Sender, totalSupply);
        }

        public static void Update(ByteString nefFile, string manifest)
        {
            var key = new byte[] { Prefix_ContractOwner };
            var contractOwner = (UInt160)Storage.Get(Storage.CurrentContext, key);
            var tx = (Transaction)Runtime.ScriptContainer;
            if (contractOwner.Equals(tx.Sender) && Runtime.CheckWitness(contractOwner))
            {
                ContractManagement.Update(nefFile, manifest, null);
            }
            else
            {
                throw new Exception("Only contract owner can update the contract");
            }
        }

        public static void OnNEP17Payment(UInt160 from, BigInteger amount, object data)
        {
            throw new Exception("Payment is disable on this contract!");
        }
    }
}
