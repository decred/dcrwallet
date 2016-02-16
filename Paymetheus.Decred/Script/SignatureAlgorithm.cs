// Copyright (c) 2016 The Decred developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus.Decred.Script
{
    public static class SignatureAlgorithm
    {
        public enum Kinds : byte
        {
            Secp256k1,
            Ed25519,
            SecSchnorr,
        }

        public const byte Ed25519PushData = (byte)((uint)Opcodes.Opcode.Op1 - 1 + (uint)Kinds.Ed25519);
        public const byte SecSchnorrPushData = (byte)((uint)Opcodes.Opcode.Op1 - 1 + (uint)Kinds.SecSchnorr);
    }
}
