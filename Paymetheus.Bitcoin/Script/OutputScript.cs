// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using static Paymetheus.Bitcoin.Script.Opcodes;

namespace Paymetheus.Bitcoin.Script
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

        public sealed class PubKey : OutputScript
        {
            public PubKey(byte[] pubKey) : base(BuildScript(pubKey))
            {
                PubKeyBytes = pubKey;
            }

            public byte[] PubKeyBytes { get; }

            public static PubKey FromScript(byte[] script)
            {
                PubKey pubKey;
                if (!TryFromScript(script, out pubKey))
                    throw new Exception("Script is not pay-to-pubkey");
                return pubKey;
            }

            public static bool TryFromScript(byte[] script, out PubKey pubKey)
            {
                if (script == null)
                    throw new ArgumentNullException(nameof(script));

                throw new NotImplementedException();
            }

            private static byte[] BuildScript(byte[] pubKey)
            {
                if (pubKey == null)
                    throw new ArgumentNullException(nameof(pubKey));

                throw new NotImplementedException();
            }
        }

        public sealed class PubKeyHash : OutputScript
        {
            public PubKeyHash(byte[] pubKeyHash) : base(BuildScript(pubKeyHash))
            {
                Hash160 = pubKeyHash;
            }

            private PubKeyHash(byte[] script, byte[] pubKeyHash) : base(script)
            {
                Hash160 = pubKeyHash;
            }

            public byte[] Hash160 { get; }

            public static PubKeyHash FromScript(byte[] script)
            {
                PubKeyHash pubKeyHash;
                if (!TryFromScript(script, out pubKeyHash))
                    throw new Exception("Script is not pay-to-pubkey-hash");
                return pubKeyHash;
            }

            public static bool TryFromScript(byte[] script, out PubKeyHash pubKeyHash)
            {
                if (script == null)
                    throw new ArgumentNullException(nameof(script));

                var isPubKeyHash =
                    script.Length == 25 &&
                    script[0] == (byte)Opcode.OpDup &&
                    script[1] == (byte)Opcode.OpHash160 &&
                    script[2] == (byte)Opcode.OpData20 &&
                    script[23] == (byte)Opcode.OpEqualVerify &&
                    script[24] == (byte)Opcode.OpChecksig;
                if (!isPubKeyHash)
                {
                    pubKeyHash = null;
                    return false;
                }

                var hash160 = new byte[Ripemd160Hash.Length];
                Array.Copy(script, 3, hash160, 0, Ripemd160Hash.Length);
                pubKeyHash = new PubKeyHash(script, hash160);
                return true;
            }

            private static byte[] BuildScript(byte[] pubKeyHash)
            {
                if (pubKeyHash == null)
                    throw new ArgumentNullException(nameof(pubKeyHash));

                if (pubKeyHash.Length != Ripemd160Hash.Length)
                    throw new ArgumentException($"{nameof(pubKeyHash)} must have RIPEMD160 hash length ({Ripemd160Hash.Length})");

                var outputScript = new byte[25];
                outputScript[0] = (byte)Opcode.OpDup;
                outputScript[1] = (byte)Opcode.OpHash160;
                outputScript[2] = (byte)Opcode.OpData20;
                Array.Copy(pubKeyHash, 0, outputScript, 3, Ripemd160Hash.Length);
                outputScript[23] = (byte)Opcode.OpEqualVerify;
                outputScript[24] = (byte)Opcode.OpChecksig;

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
            PubKeyHash pubKeyHash;
            if (PubKeyHash.TryFromScript(script, out pubKeyHash))
                return pubKeyHash;

            ScriptHash scriptHash;
            if (ScriptHash.TryFromScript(script, out scriptHash))
                return scriptHash;

            PubKey pubKey;
            if (PubKey.TryFromScript(script, out pubKey))
                return pubKey;

            return new Unrecognized(script);
        }
    }
}