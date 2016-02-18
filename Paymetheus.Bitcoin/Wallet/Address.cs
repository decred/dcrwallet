// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Script;
using Paymetheus.Bitcoin.Util;
using System;

namespace Paymetheus.Bitcoin.Wallet
{
    public abstract class Address
    {
        private Address(BlockChainIdentity intendedBlockChain)
        {
            IntendedBlockChain = intendedBlockChain;
        }

        public BlockChainIdentity IntendedBlockChain { get; }

        public abstract OutputScript BuildScript();
        public abstract string Encode();

        public override string ToString() => Encode();

        public sealed class PayToPubKeyHash : Address
        {
            public PayToPubKeyHash(BlockChainIdentity intendedBlockChain, byte[] pubKeyHash)
                : base(intendedBlockChain)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));
                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new AddressException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                PubKeyHash = pubKeyHash;
            }

            public byte[] PubKeyHash { get; }

            public override OutputScript BuildScript() => new OutputScript.PubKeyHash(PubKeyHash);

            public override string Encode()
            {
                var buffer = new byte[1 + Ripemd160Hash.Length + Checksum.SumLength];
                buffer[0] = AddressPrefix.PayToPubKeyHashPrefix(IntendedBlockChain);
                Array.Copy(PubKeyHash, 0, buffer, 1, Ripemd160Hash.Length);
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
                var buffer = new byte[1 + Ripemd160Hash.Length + Checksum.SumLength];
                buffer[0] = AddressPrefix.PayToScriptHashPrefix(IntendedBlockChain);
                Array.Copy(ScriptHash, 0, buffer, 1, Ripemd160Hash.Length);
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

            if (base58DecodedAddress.Length != 1 + Ripemd160Hash.Length + Checksum.SumLength)
            {
                address = null;
                return false;
            }

            if (!Checksum.Verify(base58DecodedAddress))
            {
                address = null;
                return false;
            }

            var addressID = base58DecodedAddress[0];
            BlockChainIdentity intendedBlockChain;
            if (AddressPrefix.IdentityForPubkeyHashPrefix(addressID, out intendedBlockChain))
            {
                var pubKeyHash = new byte[Ripemd160Hash.Length];
                Array.Copy(base58DecodedAddress, 1, pubKeyHash, 0, Ripemd160Hash.Length);
                address = new PayToPubKeyHash(intendedBlockChain, pubKeyHash);
                return true;
            }
            else if (AddressPrefix.IdentityForScriptHashPrefix(addressID, out intendedBlockChain))
            {
                var scriptHash = new byte[Ripemd160Hash.Length];
                Array.Copy(base58DecodedAddress, 1, scriptHash, 0, Ripemd160Hash.Length);
                address = new PayToScriptHash(intendedBlockChain, scriptHash);
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
            var payToPubKeyHashScript = pkScript as OutputScript.PubKeyHash;
            if (payToPubKeyHashScript != null)
            {
                address = new PayToPubKeyHash(intendedBlockChain, payToPubKeyHashScript.Hash160);
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

    public class AddressException : Exception
    {
        public AddressException(string message) : base(message) { }
    }
}
