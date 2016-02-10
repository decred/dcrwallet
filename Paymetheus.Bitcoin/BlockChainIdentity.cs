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
            coinType: 0
        );

        public static readonly BlockChainIdentity TestNet3 = new BlockChainIdentity
        (
            name: "testnet3",
            network: 0x0709110b,
            coinType: 1
        );

        public static readonly BlockChainIdentity SimNet = new BlockChainIdentity
        (
            name: "simnet",
            network: 0x12141c16,
            coinType: 115
        );

        private BlockChainIdentity(string name, uint network, uint coinType)
        {
            Name = name;
            Network = network;
            Bip44CoinType = coinType;
        }

        public string Name { get; }
        public uint Network { get; }
        public uint Bip44CoinType { get; }

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
    }

    [Serializable]
    public class UnknownBlockChainException : Exception
    {
        public UnknownBlockChainException(string message) : base(message) { }

        public UnknownBlockChainException(BlockChainIdentity identity)
            : base($"Unknown blockchain `{identity.Name}`")
        { }
    }
}
