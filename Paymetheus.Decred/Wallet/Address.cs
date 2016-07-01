// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Script;
using Paymetheus.Decred.Util;
using System;

namespace Paymetheus.Decred.Wallet
{
    public abstract class Address
    {
        private Address(BlockChainIdentity intendedBlockChain)
        {
            if (intendedBlockChain == null)
                throw new ArgumentNullException(nameof(intendedBlockChain));

            IntendedBlockChain = intendedBlockChain;
        }

        public BlockChainIdentity IntendedBlockChain { get; }

        public abstract OutputScript BuildScript();
        public abstract string Encode();

        public override string ToString() => Encode();

        public sealed class PayToSecp256k1PubKeyHash : Address
        {
            public PayToSecp256k1PubKeyHash(BlockChainIdentity intendedBlockChain, byte[] pubKeyHash)
                : base(intendedBlockChain)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));
                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new AddressException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                PubKeyHash = pubKeyHash;
            }

            public byte[] PubKeyHash { get; }

            public override OutputScript BuildScript() => new OutputScript.Secp256k1PubKeyHash(PubKeyHash);

            public override string Encode()
            {
                var buffer = new byte[2 + Ripemd160Hash.Length + Checksum.SumLength];
                var prefix = AddressPrefix.PayToSecp256k1PubKeyHashPrefix(IntendedBlockChain);
                buffer[0] = prefix.FirstByte;
                buffer[1] = prefix.SecondByte;
                Array.Copy(PubKeyHash, 0, buffer, 2, Ripemd160Hash.Length);
                Checksum.WriteSum(buffer);
                return Base58.Encode(buffer);
            }
        }

        public sealed class PayToEd25519PubKeyHash : Address
        {
            public PayToEd25519PubKeyHash(BlockChainIdentity intendedBlockChain, byte[] pubKeyHash)
                : base(intendedBlockChain)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));
                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new AddressException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");
            }

            public byte[] PubKeyHash { get; }

            public override OutputScript BuildScript() => new OutputScript.Ed25519PubKeyHash(PubKeyHash);

            public override string Encode()
            {
                var buffer = new byte[2 + Ripemd160Hash.Length + Checksum.SumLength];
                var prefix = AddressPrefix.PayToEd25519PubKeyHashPrefix(IntendedBlockChain);
                buffer[0] = prefix.FirstByte;
                buffer[1] = prefix.SecondByte;
                Array.Copy(PubKeyHash, 0, buffer, 2, Ripemd160Hash.Length);
                Checksum.WriteSum(buffer);
                return Base58.Encode(buffer);
            }
        }

        public sealed class PayToSecSchnorrPubKeyHash : Address
        {
            public PayToSecSchnorrPubKeyHash(BlockChainIdentity intendedBlockChain, byte[] pubKeyHash)
                : base(intendedBlockChain)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));
                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new AddressException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");
            }

            public byte[] PubKeyHash { get; }

            public override OutputScript BuildScript() => new OutputScript.SecSchnorrPubKeyHash(PubKeyHash);

            public override string Encode()
            {
                var buffer = new byte[2 + Ripemd160Hash.Length + Checksum.SumLength];
                var prefix = AddressPrefix.PayToSchnorrPubKeyHashPrefix(IntendedBlockChain);
                buffer[0] = prefix.FirstByte;
                buffer[1] = prefix.SecondByte;
                Array.Copy(PubKeyHash, 0, buffer, 2, Ripemd160Hash.Length);
                Checksum.WriteSum(buffer);
                return Base58.Encode(buffer);
            }
        }

        public sealed class PayToScriptHash : Address
        {
            public PayToScriptHash(BlockChainIdentity intendedBlockChain, byte[] scriptHash)
                : base(intendedBlockChain)
            {
                if (scriptHash == null)
                    throw new ArgumentNullException(nameof(scriptHash));
                if (scriptHash.Length != Ripemd160Hash.Length)
                    throw new AddressException($"{nameof(scriptHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                ScriptHash = scriptHash;
            }

            public byte[] ScriptHash { get; }

            public override OutputScript BuildScript() => new OutputScript.ScriptHash(ScriptHash);

            public override string Encode()
            {
                var buffer = new byte[2 + Ripemd160Hash.Length + Checksum.SumLength];
                var prefix = AddressPrefix.PayToScriptHashPrefix(IntendedBlockChain);
                buffer[0] = prefix.FirstByte;
                buffer[1] = prefix.SecondByte;
                Array.Copy(ScriptHash, 0, buffer, 2, Ripemd160Hash.Length);
                Checksum.WriteSum(buffer);
                return Base58.Encode(buffer);
            }
        }

        public static Address Decode(string encodedAddress)
        {
            Address address;
            if (!TryDecode(encodedAddress, out address))
                throw new AddressException("Encoded address is ill-formed or intended for an unrecognized network");
            return address;
        }

        public static bool TryDecode(string encodedAddress, out Address address)
        {
            byte[] base58DecodedAddress;
            if (!Base58.TryDecode(encodedAddress, out base58DecodedAddress))
            {
                address = null;
                return false;
            }

            // TODO: This has to be updated to parse pubkey addresses, which have different
            // base58 -decoded lengths than the p2pkh and p2sh addresses.

            if (base58DecodedAddress.Length != 2 + Ripemd160Hash.Length + Checksum.SumLength)
            {
                address = null;
                return false;
            }

            if (!Checksum.Verify(base58DecodedAddress))
            {
                address = null;
                return false;
            }

            // All p2pkh and p2sh addresses store the pubkey hash in the same location.
            var hash160 = new byte[Ripemd160Hash.Length];
            Array.Copy(base58DecodedAddress, 2, hash160, 0, Ripemd160Hash.Length);

            var prefix = new AddressPrefix(base58DecodedAddress[0], base58DecodedAddress[1]);
            BlockChainIdentity intendedBlockChain;
            if (AddressPrefix.IdentityForSecp256k1PubkeyHashPrefix(prefix, out intendedBlockChain))
            {
                address = new PayToSecp256k1PubKeyHash(intendedBlockChain, hash160);
                return true;
            }
            else if (AddressPrefix.IdentityForEd25519PubKeyHashPrefix(prefix, out intendedBlockChain))
            {
                address = new PayToEd25519PubKeyHash(intendedBlockChain, hash160);
                return true;
            }
            else if (AddressPrefix.IdentityForSecSchnorrPubKeyHashPrefix(prefix, out intendedBlockChain))
            {
                address = new PayToSecSchnorrPubKeyHash(intendedBlockChain, hash160);
                return true;
            }
            else if (AddressPrefix.IdentityForScriptHashPrefix(prefix, out intendedBlockChain))
            {
                address = new PayToScriptHash(intendedBlockChain, hash160);
                return true;
            }
            else
            {
                address = null;
                return false;
            }
        }

        public static bool TryFromOutputScript(OutputScript pkScript, BlockChainIdentity intendedBlockChain, out Address address)
        {
            var payToPubKeyHashScript = pkScript as OutputScript.Secp256k1PubKeyHash;
            if (payToPubKeyHashScript != null)
            {
                address = new PayToSecp256k1PubKeyHash(intendedBlockChain, payToPubKeyHashScript.Hash160);
                return true;
            }

            var payToEd25519PubKeyHashScript = pkScript as OutputScript.Ed25519PubKeyHash;
            if (payToEd25519PubKeyHashScript != null)
            {
                address = new PayToEd25519PubKeyHash(intendedBlockChain, payToEd25519PubKeyHashScript.Hash160);
                return true;
            }

            var payToSecSchnorrPubKeyHash = pkScript as OutputScript.SecSchnorrPubKeyHash;
            if (payToSecSchnorrPubKeyHash != null)
            {
                address = new PayToSecSchnorrPubKeyHash(intendedBlockChain, payToSecSchnorrPubKeyHash.Hash160);
                return true;
            }

            var payToScriptHashScript = pkScript as OutputScript.ScriptHash;
            if (payToScriptHashScript != null)
            {
                address = new PayToScriptHash(intendedBlockChain, payToScriptHashScript.Hash160);
                return true;
            }

            address = null;
            return false;
        }
    }

    [Serializable]
    public class AddressException : Exception
    {
        public AddressException(string message) : base(message) { }
    }
}
