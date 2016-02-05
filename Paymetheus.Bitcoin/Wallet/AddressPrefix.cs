// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Bitcoin.Wallet
{
    public struct AddressPrefix
    {
        const byte MainNetPayToPubKeyHashPrefix = 0x00;
        const byte MainNetPayToScriptHashPrefix = 0x05;
        const byte MainNetWifPrefix = 0x80;
        const byte TestNet3PayToPubKeyHashPrefix = 0x6f;
        const byte TestNet3PayToScriptHashPrefix = 0xc4;
        const byte TestNet3WifPrefix = 0xef;
        const byte SimNetPayToPubKeyHashPrefix = 0x3f;
        const byte SimNetPayToScriptHashPrefix = 0x7b;
        const byte SimNetWifPrefix = 0x64;

        public AddressPrefix(byte prefix)
        {
            Prefix = prefix;
        }

        public byte Prefix { get; }

        public static implicit operator byte(AddressPrefix ap) => ap.Prefix;
        public static implicit operator AddressPrefix(byte b) => new AddressPrefix(b);

        public static AddressPrefix PayToPubKeyHashPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetPayToPubKeyHashPrefix;
            else if (identity == BlockChainIdentity.TestNet3) return TestNet3PayToPubKeyHashPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetPayToPubKeyHashPrefix;
            else throw new UnknownBlockChainException($"Unknown blockchain `{identity.Name}`");
        }

        public static AddressPrefix PayToScriptHashPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetPayToScriptHashPrefix;
            else if (identity == BlockChainIdentity.TestNet3) return TestNet3PayToScriptHashPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetPayToScriptHashPrefix;
            else throw new UnknownBlockChainException($"Unknown blockchain `{identity.Name}`");
        }

        public static bool IdentityForPubkeyHashPrefix(AddressPrefix prefix, out BlockChainIdentity identity)
        {
            if (prefix == MainNetPayToPubKeyHashPrefix)
            {
                identity = BlockChainIdentity.MainNet;
                return true;
            }
            else if (prefix == TestNet3PayToPubKeyHashPrefix)
            {
                identity = BlockChainIdentity.TestNet3;
                return true;
            }
            else if (prefix == SimNetPayToPubKeyHashPrefix)
            {
                identity = BlockChainIdentity.SimNet;
                return true;
            }
            else
            {
                identity = null;
                return false;
            }
        }

        public static bool IdentityForScriptHashPrefix(AddressPrefix prefix, out BlockChainIdentity identity)
        {
            if (prefix == MainNetPayToScriptHashPrefix)
            {
                identity = BlockChainIdentity.MainNet;
                return true;
            }
            else if (prefix == TestNet3PayToScriptHashPrefix)
            {
                identity = BlockChainIdentity.TestNet3;
                return true;
            }
            else if (prefix == SimNetPayToScriptHashPrefix)
            {
                identity = BlockChainIdentity.SimNet;
                return true;
            }
            else
            {
                identity = null;
                return false;
            }
        }
    }
}
