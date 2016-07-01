// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using static Paymetheus.Decred.Script.Opcodes;

namespace Paymetheus.Decred.Script
{
    public class OutputScript
    {
        private OutputScript(byte[] script)
        {
            if (script == null)
                throw new ArgumentNullException(nameof(script));

            Script = script;
        }

        public byte[] Script { get; }

        public sealed class Secp256k1PubKeyHash : OutputScript
        {
            private const int ScriptLength = 25;

            public Secp256k1PubKeyHash(byte[] pubKeyHash) : base(BuildScript(pubKeyHash))
            {
                Hash160 = pubKeyHash;
            }

            private Secp256k1PubKeyHash(byte[] script, byte[] pubKeyHash) : base(script)
            {
                Hash160 = pubKeyHash;
            }

            public byte[] Hash160 { get; }

            public static Secp256k1PubKeyHash FromScript(byte[] script)
            {
                Secp256k1PubKeyHash pubKeyHash;
                if (!TryFromScript(script, out pubKeyHash))
                    throw new Exception("Script is not pay-to-secp256k1-pubkey-hash");
                return pubKeyHash;
            }

            public static bool TryFromScript(byte[] script, out Secp256k1PubKeyHash pubKeyHash)
            {
                if (script == null)
                    throw new ArgumentNullException(nameof(script));

                var isSecp256k1PubKeyHash =
                    script.Length == ScriptLength &&
                    script[0] == (byte)Opcode.OpDup &&
                    script[1] == (byte)Opcode.OpHash160 &&
                    script[2] == (byte)Opcode.OpData20 &&
                    script[23] == (byte)Opcode.OpEqualVerify &&
                    script[24] == (byte)Opcode.OpChecksig;
                if (!isSecp256k1PubKeyHash)
                {
                    pubKeyHash = null;
                    return false;
                }

                var hash160 = new byte[Ripemd160Hash.Length];
                Array.Copy(script, 3, hash160, 0, Ripemd160Hash.Length);
                pubKeyHash = new Secp256k1PubKeyHash(script, hash160);
                return true;
            }

            private static byte[] BuildScript(byte[] pubKeyHash)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));

                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new ArgumentException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                var outputScript = new byte[ScriptLength];
                outputScript[0] = (byte)Opcode.OpDup;
                outputScript[1] = (byte)Opcode.OpHash160;
                outputScript[2] = (byte)Opcode.OpData20;
                Array.Copy(pubKeyHash, 0, outputScript, 3, Ripemd160Hash.Length);
                outputScript[23] = (byte)Opcode.OpEqualVerify;
                outputScript[24] = (byte)Opcode.OpChecksig;

                return outputScript;
            }
        }

        public sealed class Ed25519PubKeyHash : OutputScript
        {
            private const int ScriptLength = 26;

            public Ed25519PubKeyHash(byte[] pubKeyHash) : base(BuildScript(pubKeyHash))
            {
                Hash160 = pubKeyHash;
            }

            private Ed25519PubKeyHash(byte[] script, byte[] pubKeyHash) : base(script)
            {
                Hash160 = pubKeyHash;
            }

            public byte[] Hash160 { get; }

            public static Ed25519PubKeyHash FromScript(byte[] script)
            {
                Ed25519PubKeyHash pubKeyHash;
                if (!TryFromScript(script, out pubKeyHash))
                    throw new Exception("Script is not pay-to-ed25519-pubkey-hash");
                return pubKeyHash;
            }

            public static bool TryFromScript(byte[] script, out Ed25519PubKeyHash pubKeyHash)
            {
                if (script == null)
                    throw new ArgumentNullException(nameof(script));

                var isEd25519PubKeyHash =
                    script.Length == ScriptLength &&
                    script[0] == (byte)Opcode.OpDup &&
                    script[1] == (byte)Opcode.OpHash160 &&
                    script[2] == (byte)Opcode.OpData20 &&
                    script[23] == (byte)Opcode.OpEqualVerify &&
                    script[24] == SignatureAlgorithm.Ed25519PushData &&
                    script[25] == (byte)Opcode.OpChecksigAlt;
                if (!isEd25519PubKeyHash)
                {
                    pubKeyHash = null;
                    return false;
                }

                var hash160 = new byte[Ripemd160Hash.Length];
                Array.Copy(script, 3, hash160, 0, Ripemd160Hash.Length);
                pubKeyHash = new Ed25519PubKeyHash(script, hash160);
                return true;
            }

            private static byte[] BuildScript(byte[] pubKeyHash)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));

                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new ArgumentException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                var outputScript = new byte[ScriptLength];
                outputScript[0] = (byte)Opcode.OpDup;
                outputScript[1] = (byte)Opcode.OpHash160;
                outputScript[2] = (byte)Opcode.OpData20;
                Array.Copy(pubKeyHash, 0, outputScript, 3, Ripemd160Hash.Length);
                outputScript[23] = (byte)Opcode.OpEqualVerify;
                outputScript[24] = SignatureAlgorithm.Ed25519PushData;
                outputScript[25] = (byte)Opcode.OpChecksigAlt;

                return outputScript;
            }
        }

        public sealed class SecSchnorrPubKeyHash : OutputScript
        {
            private const int ScriptLength = 26;

            public SecSchnorrPubKeyHash(byte[] pubKeyHash) : base(BuildScript(pubKeyHash))
            {
                Hash160 = pubKeyHash;
            }

            private SecSchnorrPubKeyHash(byte[] script, byte[] pubKeyHash) : base(script)
            {
                Hash160 = pubKeyHash;
            }

            public byte[] Hash160 { get; }

            public static SecSchnorrPubKeyHash FromScript(byte[] script)
            {
                SecSchnorrPubKeyHash pubKeyHash;
                if (!TryFromScript(script, out pubKeyHash))
                    throw new Exception("Script is not pay-to-secschnorr-pubkey-hash");
                return pubKeyHash;
            }

            public static bool TryFromScript(byte[] script, out SecSchnorrPubKeyHash pubKeyHash)
            {
                if (script == null)
                    throw new ArgumentNullException(nameof(script));

                var isSecSchnorrPubKeyHash =
                    script.Length == ScriptLength &&
                    script[0] == (byte)Opcode.OpDup &&
                    script[1] == (byte)Opcode.OpHash160 &&
                    script[2] == (byte)Opcode.OpData20 &&
                    script[23] == (byte)Opcode.OpEqualVerify &&
                    script[24] == SignatureAlgorithm.SecSchnorrPushData &&
                    script[25] == (byte)Opcode.OpChecksigAlt;
                if (!isSecSchnorrPubKeyHash)
                {
                    pubKeyHash = null;
                    return false;
                }

                var hash160 = new byte[Ripemd160Hash.Length];
                Array.Copy(script, 3, hash160, 0, Ripemd160Hash.Length);
                pubKeyHash = new SecSchnorrPubKeyHash(script, hash160);
                return true;
            }

            private static byte[] BuildScript(byte[] pubKeyHash)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));

                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new ArgumentException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                var outputScript = new byte[ScriptLength];
                outputScript[0] = (byte)Opcode.OpDup;
                outputScript[1] = (byte)Opcode.OpHash160;
                outputScript[2] = (byte)Opcode.OpData20;
                Array.Copy(pubKeyHash, 0, outputScript, 3, Ripemd160Hash.Length);
                outputScript[23] = (byte)Opcode.OpEqualVerify;
                outputScript[24] = SignatureAlgorithm.SecSchnorrPushData;
                outputScript[25] = (byte)Opcode.OpChecksigAlt;

                return outputScript;
            }
        }

        public sealed class ScriptHash : OutputScript
        {
            public ScriptHash(byte[] scriptHash) : base(BuildScript(scriptHash))
            {
                Hash160 = scriptHash;
            }

            private ScriptHash(byte[] script, byte[] scriptHash) : base(script)
            {
                Hash160 = scriptHash;
            }

            public byte[] Hash160 { get; }

            public static ScriptHash FromScript(byte[] script)
            {
                ScriptHash scriptHash;
                if (!TryFromScript(script, out scriptHash))
                    throw new Exception("Script is not pay-to-script-hash");
                return scriptHash;
            }

            public static bool TryFromScript(byte[] script, out ScriptHash scriptHash)
            {
                if (script == null)
                    throw new ArgumentNullException(nameof(script));

                var isScriptHash =
                    script.Length == 23 &&
                    script[0] == (byte)Opcode.OpHash160 &&
                    script[1] == (byte)Opcode.OpData20 &&
                    script[22] == (byte)Opcode.OpEqual;
                if (!isScriptHash)
                {
                    scriptHash = null;
                    return false;
                }

                var hash160 = new byte[Ripemd160Hash.Length];
                Array.Copy(script, 2, hash160, 0, Ripemd160Hash.Length);
                scriptHash = new ScriptHash(script, hash160);
                return true;
            }

            private static byte[] BuildScript(byte[] scriptHash)
            {
                if (scriptHash == null)
                    throw new ArgumentNullException(nameof(scriptHash));

                if (scriptHash.Length != Ripemd160Hash.Length)
                    throw new ArgumentException($"{nameof(scriptHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                var outputScript = new byte[1 + 1 + Ripemd160Hash.Length + 1];
                outputScript[0] = (byte)Opcode.OpHash160;
                outputScript[1] = (byte)Opcode.OpData20;
                Array.Copy(scriptHash, 0, outputScript, 2, Ripemd160Hash.Length);
                outputScript[22] = (byte)Opcode.OpEqual;

                return outputScript;
            }
        }

        // Unrecognized output scripts include all non-standard scripts as well as several
        // standard scripts that don't need to be handled specially, such as multisig and
        // null data scripts.
        public sealed class Unrecognized : OutputScript
        {
            public Unrecognized(byte[] script) : base(script) { }
        }

        public static OutputScript ParseScript(byte[] script)
        {
            Secp256k1PubKeyHash secp256k1PubKeyHash;
            if (Secp256k1PubKeyHash.TryFromScript(script, out secp256k1PubKeyHash))
                return secp256k1PubKeyHash;

            Ed25519PubKeyHash ed25519PubKeyHash;
            if (Ed25519PubKeyHash.TryFromScript(script, out ed25519PubKeyHash))
                return ed25519PubKeyHash;

            SecSchnorrPubKeyHash secSchnorrPubKeyHash;
            if (SecSchnorrPubKeyHash.TryFromScript(script, out secSchnorrPubKeyHash))
                return secSchnorrPubKeyHash;

            ScriptHash scriptHash;
            if (ScriptHash.TryFromScript(script, out scriptHash))
                return scriptHash;

            return new Unrecognized(script);
        }
    }
}