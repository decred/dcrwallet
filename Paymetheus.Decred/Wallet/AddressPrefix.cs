// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred.Wallet
{
    public struct AddressPrefix
    {
        static readonly AddressPrefix MainNetPubKeyPrefix = new AddressPrefix(0x13, 0x86);
        static readonly AddressPrefix MainNetSecp256k1PubKeyHashPrefix = new AddressPrefix(0x07, 0x3f);
        static readonly AddressPrefix MainNetEd25519PubKeyHashPrefix = new AddressPrefix(); // TODO
        static readonly AddressPrefix MainNetSchnorrPubKeyHashPrefix = new AddressPrefix(); // TODO
        static readonly AddressPrefix MainNetScriptHashPrefix = new AddressPrefix(0x07, 0x1a);

        static readonly AddressPrefix TestNetPubKeyPrefix = new AddressPrefix(0, 0); // TODO
        static readonly AddressPrefix TestNetSecp256k1PubKeyHashPrefix = new AddressPrefix(0x0f, 0x21);
        static readonly AddressPrefix TestNetEd25519PubKeyHashPrefix = new AddressPrefix(); // TODO
        static readonly AddressPrefix TestNetSchnorrPubKeyHashPrefix = new AddressPrefix(); // TODO
        static readonly AddressPrefix TestNetScriptHashPrefix = new AddressPrefix(0x0e, 0xfc);

        static readonly AddressPrefix SimNetPubKeyPrefix = new AddressPrefix(0, 0); // TODO
        static readonly AddressPrefix SimNetSecp256k1PubKeyHashPrefix = new AddressPrefix(0x0e, 0x91);
        static readonly AddressPrefix SimNetEd25519PubKeyHashPrefix = new AddressPrefix(); // TODO
        static readonly AddressPrefix SimNetSchnorrPubKeyHashPrefix = new AddressPrefix(); // TODO
        static readonly AddressPrefix SimNetScriptHashPrefix = new AddressPrefix(0x0e, 0x6c);

        public AddressPrefix(byte first, byte second)
        {
            FirstByte = first;
            SecondByte = second;
        }

        public byte FirstByte { get; }
        public byte SecondByte { get; }

        public static bool operator ==(AddressPrefix first, AddressPrefix second) =>
            first.FirstByte == second.FirstByte && first.SecondByte == second.SecondByte;

        public static bool operator !=(AddressPrefix first, AddressPrefix second) => !(first == second);

        public override bool Equals(object obj) => (obj is AddressPrefix) && (AddressPrefix)obj == this;

        public override int GetHashCode() => FirstByte.GetHashCode() ^ SecondByte.GetHashCode();

        public static AddressPrefix PayToPubKeyPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetPubKeyPrefix;
            else if (identity == BlockChainIdentity.TestNet) return TestNetPubKeyPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetPubKeyPrefix;
            else throw new UnknownBlockChainException(identity);
        }

        public static AddressPrefix PayToSecp256k1PubKeyHashPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetSecp256k1PubKeyHashPrefix;
            else if (identity == BlockChainIdentity.TestNet) return TestNetSecp256k1PubKeyHashPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetSecp256k1PubKeyHashPrefix;
            else throw new UnknownBlockChainException(identity);
        }

        public static AddressPrefix PayToEd25519PubKeyHashPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetEd25519PubKeyHashPrefix;
            else if (identity == BlockChainIdentity.TestNet) return TestNetEd25519PubKeyHashPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetEd25519PubKeyHashPrefix;
            else throw new UnknownBlockChainException(identity);
        }

        public static AddressPrefix PayToSchnorrPubKeyHashPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetSchnorrPubKeyHashPrefix;
            else if (identity == BlockChainIdentity.TestNet) return TestNetSchnorrPubKeyHashPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetSchnorrPubKeyHashPrefix;
            else throw new UnknownBlockChainException(identity);
        }

        public static AddressPrefix PayToScriptHashPrefix(BlockChainIdentity identity)
        {
            if (identity == null)
                throw new ArgumentNullException(nameof(identity));

            if (identity == BlockChainIdentity.MainNet) return MainNetScriptHashPrefix;
            else if (identity == BlockChainIdentity.TestNet) return TestNetScriptHashPrefix;
            else if (identity == BlockChainIdentity.SimNet) return SimNetScriptHashPrefix;
            else throw new UnknownBlockChainException(identity);
        }

        public static bool IdentityForSecp256k1PubkeyHashPrefix(AddressPrefix prefix, out BlockChainIdentity identity)
        {
            if (prefix == MainNetSecp256k1PubKeyHashPrefix)
            {
                identity = BlockChainIdentity.MainNet;
                return true;
            }
            else if (prefix == TestNetSecp256k1PubKeyHashPrefix)
            {
                identity = BlockChainIdentity.TestNet;
                return true;
            }
            else if (prefix == SimNetSecp256k1PubKeyHashPrefix)
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

        public static bool IdentityForEd25519PubKeyHashPrefix(AddressPrefix prefix, out BlockChainIdentity identity)
        {
            if (prefix == MainNetEd25519PubKeyHashPrefix)
            {
                identity = BlockChainIdentity.MainNet;
                return true;
            }
            else if (prefix == TestNetEd25519PubKeyHashPrefix)
            {
                identity = BlockChainIdentity.TestNet;
                return true;
            }
            else if (prefix == SimNetEd25519PubKeyHashPrefix)
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

        public static bool IdentityForSecSchnorrPubKeyHashPrefix(AddressPrefix prefix, out BlockChainIdentity identity)
        {
            if (prefix == MainNetSchnorrPubKeyHashPrefix)
            {
                identity = BlockChainIdentity.MainNet;
                return true;
            }
            else if (prefix == TestNetSchnorrPubKeyHashPrefix)
            {
                identity = BlockChainIdentity.TestNet;
                return true;
            }
            else if (prefix == SimNetSchnorrPubKeyHashPrefix)
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
            if (prefix == MainNetScriptHashPrefix)
            {
                identity = BlockChainIdentity.MainNet;
                return true;
            }
            else if (prefix == TestNetScriptHashPrefix)
            {
                identity = BlockChainIdentity.TestNet;
                return true;
            }
            else if (prefix == SimNetScriptHashPrefix)
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
