// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Bitcoin
{
    public sealed class BlockChainIdentity
    {
        public static readonly BlockChainIdentity MainNet = new BlockChainIdentity
        (
            name: "mainnet",
            network: 0xd9b4bef9,
            coinType: 0,
            pubKeyHashID: 0x00,
            scriptHashID: 0x05,
            wifID: 0x80
        );

        public static readonly BlockChainIdentity TestNet3 = new BlockChainIdentity
        (
            name: "testnet3",
            network: 0x0709110b,
            coinType: 1,
            pubKeyHashID: 0x6f,
            scriptHashID: 0xc4,
            wifID: 0xef
        );

        public static readonly BlockChainIdentity SimNet = new BlockChainIdentity
        (
            name: "simnet",
            network: 0x12141c16,
            coinType: 115,
            pubKeyHashID: 0x3f,
            scriptHashID: 0x7b,
            wifID: 0x64
        );

        private BlockChainIdentity(string name, uint network, uint coinType, byte pubKeyHashID, byte scriptHashID, byte wifID)
        {
            Name = name;
            Network = network;
            Bip44CoinType = coinType;
            AddressPubKeyHashID = pubKeyHashID;
            AddressScriptHashID = scriptHashID;
            WifID = wifID;
        }

        public string Name { get; }
        public uint Network { get; }
        public uint Bip44CoinType { get; }
        public byte AddressPubKeyHashID { get; }
        public byte AddressScriptHashID { get; }
        public byte WifID { get; }

        public static BlockChainIdentity FromNetworkBits(uint network)
        {
            if (network == MainNet.Network)
                return MainNet;
            else if (network == TestNet3.Network)
                return TestNet3;
            else if (network == SimNet.Network)
                return SimNet;
            else
                throw new UnknownBlockChainException($"Unrecognized or unsupported blockchain described by network ID `{network}`");
        }

        public static bool IdentityForPubKeyHashID(byte pubKeyHashID, out BlockChainIdentity identity)
        {
            if (pubKeyHashID == MainNet.AddressPubKeyHashID)
            {
                identity = MainNet;
                return true;
            }
            else if (pubKeyHashID == TestNet3.AddressPubKeyHashID)
            {
                identity = TestNet3;
                return true;
            }
            else if (pubKeyHashID == SimNet.AddressPubKeyHashID)
            {
                identity = SimNet;
                return true;
            }
            else
            {
                identity = null;
                return false;
            }
        }

        public static bool IdentityForScriptHashID(byte scriptHashID, out BlockChainIdentity identity)
        {
            if (scriptHashID == MainNet.AddressScriptHashID)
            {
                identity = MainNet;
                return true;
            }
            else if (scriptHashID == TestNet3.AddressScriptHashID)
            {
                identity = TestNet3;
                return true;
            }
            else if (scriptHashID == SimNet.AddressScriptHashID)
            {
                identity = SimNet;
                return true;
            }
            else
            {
                identity = null;
                return false;
            }
        }
    }

    [Serializable]
    public class UnknownBlockChainException : Exception
    {
        public UnknownBlockChainException(string message) : base(message) { }
    }
}
